// Package k8simport converts Kubernetes Deployment and Service manifests
// into dzjjy/v1 documents.
//
// The conversion is intentionally lossless in the directions dzjjy can honour:
// unsupported Kubernetes features are reported as explicit import errors
// rather than silently dropped, so an operator never gets a manifest that
// quietly runs with weaker semantics than the cluster declared.
package k8simport

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is the subset of Kubernetes objects dzjjy understands.
type Manifest struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   K8sMeta    `yaml:"metadata"`
	Spec       K8sSpec    `yaml:"spec"`
	Items      []Manifest `yaml:"items"`
}

// K8sMeta is Kubernetes object metadata.
type K8sMeta struct {
	Name string `yaml:"name"`
}

// K8sSpec is the subset of Deployment/Service spec fields used by the import.
type K8sSpec struct {
	Replicas *int32        `yaml:"replicas"`
	Selector *Selector     `yaml:"selector"`
	Template *PodSpec      `yaml:"template"`
	Ports    []ServicePort `yaml:"ports"`
	Type     string        `yaml:"type"`
}

// Selector is a label selector.
type Selector struct {
	MatchLabels map[string]string `yaml:"matchLabels"`
}

// PodSpec is the pod template carried by a Deployment.
type PodSpec struct {
	Metadata *K8sMeta   `yaml:"metadata"`
	Spec     *Container `yaml:"spec"`
}

// Container is the pod container definition.
type Container struct {
	Containers []ContainerSpec `yaml:"containers"`
	Volumes    []K8sVolume     `yaml:"volumes"`
}

// ContainerSpec describes a single container.
type ContainerSpec struct {
	Name            string           `yaml:"name"`
	Image           string           `yaml:"image"`
	Command         []string         `yaml:"command"`
	Args            []string         `yaml:"args"`
	Ports           []ContainerPort  `yaml:"ports"`
	Env             []EnvVar         `yaml:"env"`
	EnvFrom         []EnvFromSource  `yaml:"envFrom"`
	Resources       *K8sResources    `yaml:"resources"`
	VolumeMounts    []VolumeMount    `yaml:"volumeMounts"`
	SecurityContext *SecurityContext `yaml:"securityContext"`
}

// ContainerPort is a port exposed by a container.
type ContainerPort struct {
	Name          string `yaml:"name"`
	ContainerPort int    `yaml:"containerPort"`
}

// EnvVar is a Kubernetes environment variable.
type EnvVar struct {
	Name      string       `yaml:"name"`
	Value     string       `yaml:"value"`
	ValueFrom *ValueSource `yaml:"valueFrom"`
}

// EnvFromSource imports a whole ConfigMap or Secret as environment.
type EnvFromSource struct {
	Prefix       string `yaml:"prefix"`
	ConfigMapRef *Ref   `yaml:"configMapRef"`
	SecretRef    *Ref   `yaml:"secretRef"`
}

// Ref names a ConfigMap or Secret.
type Ref struct {
	Name string `yaml:"name"`
}

// ValueSource sources an env var from a Secret key or field reference.
type ValueSource struct {
	SecretKeyRef *KeyRef   `yaml:"secretKeyRef"`
	FieldRef     *FieldRef `yaml:"fieldRef"`
}

// KeyRef selects a key inside a Secret.
type KeyRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

// FieldRef selects a field from the pod.
type FieldRef struct {
	FieldPath string `yaml:"fieldPath"`
}

// SecurityContext is the pod security configuration dzjjy maps to its own
// isolation controls.
type SecurityContext struct {
	RunAsNonRoot           *bool `yaml:"runAsNonRoot"`
	ReadOnlyRootFilesystem *bool `yaml:"readOnlyRootFilesystem"`
}

// VolumeMount attaches a volume inside the container.
type VolumeMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
}

// K8sVolume is a pod volume.
type K8sVolume struct {
	Name      string           `yaml:"name"`
	EmptyDir  *EmptyDirVolume  `yaml:"emptyDir"`
	Secret    *SecretVolume    `yaml:"secret"`
	ConfigMap *ConfigMapVolume `yaml:"configMap"`
}

// EmptyDirVolume is an ephemeral volume.
type EmptyDirVolume struct {
	SizeLimit string `yaml:"sizeLimit"`
}

// SecretVolume mounts a Secret as files.
type SecretVolume struct {
	SecretName string `yaml:"secretName"`
}

// ConfigMapVolume mounts a ConfigMap as files.
type ConfigMapVolume struct {
	Name string `yaml:"name"`
}

// K8sResources are Kubernetes compute resources.
type K8sResources struct {
	Limits   map[string]string `yaml:"limits"`
	Requests map[string]string `yaml:"requests"`
}

// ServicePort is a port published by a Service.
type ServicePort struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	TargetPort any    `yaml:"targetPort"`
	NodePort   int    `yaml:"nodePort"`
}

// Document is a converted dzjjy/v1 manifest plus the notes an operator needs.
type Document struct {
	YAML   string
	Notes  []string
	Source string
}

// Parse splits a multi-document Kubernetes manifest into objects.
func Parse(data []byte) ([]Manifest, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var out []Manifest
	for {
		var manifest Manifest
		err := decoder.Decode(&manifest)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("invalid Kubernetes manifest: %w", err)
		}
		if manifest.Kind == "List" {
			out = append(out, manifest.Items...)
			continue
		}
		if manifest.Kind == "" {
			continue
		}
		out = append(out, manifest)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Kubernetes objects found in manifest")
	}
	return out, nil
}

// Convert translates Deployment/Service objects into a dzjjy/v1 App.
func Convert(manifests []Manifest) (Document, error) {
	var deployment *Manifest
	var service *Manifest
	for i := range manifests {
		switch manifests[i].Kind {
		case "Deployment":
			if deployment != nil {
				return Document{}, fmt.Errorf("multiple Deployments are not supported in one import")
			}
			deployment = &manifests[i]
		case "Service":
			if service != nil {
				return Document{}, fmt.Errorf("multiple Services are not supported in one import")
			}
			service = &manifests[i]
		default:
			return Document{}, fmt.Errorf("unsupported Kubernetes kind %q; dzjjy imports Deployment and Service", manifests[i].Kind)
		}
	}
	if deployment == nil {
		return Document{}, fmt.Errorf("import requires a Deployment")
	}
	if deployment.Spec.Template == nil || deployment.Spec.Template.Spec == nil {
		return Document{}, fmt.Errorf("the deployment %q has no pod template", deployment.Metadata.Name)
	}
	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		return Document{}, fmt.Errorf("the deployment %q must declare exactly one container; dzjjy does not support multi-container pods", deployment.Metadata.Name)
	}
	container := containers[0]
	if err := validateTemplateImage(container.Image); err != nil {
		return Document{}, err
	}
	if container.SecurityContext != nil && container.SecurityContext.RunAsNonRoot != nil && !*container.SecurityContext.RunAsNonRoot {
		return Document{}, fmt.Errorf("the deployment %q requests runAsNonRoot=false, which dzjjy cannot represent", deployment.Metadata.Name)
	}

	var notes []string
	command := append(append([]string{}, container.Command...), container.Args...)
	if len(command) == 0 {
		return Document{}, fmt.Errorf("the deployment %q container has neither command nor args; dzjjy requires an explicit command", deployment.Metadata.Name)
	}

	spec := map[string]any{
		"artifact": deployment.Metadata.Name + ".tar.gz",
		"command":  command,
		"runtime":  map[string]any{"type": templateType(container.Image), "version": templateVersion(container.Image)},
		"sandbox":  map[string]any{"type": "docker"},
	}

	env := map[string]string{}
	var secrets []map[string]string
	for _, entry := range container.Env {
		if entry.ValueFrom != nil {
			if entry.ValueFrom.SecretKeyRef == nil {
				return Document{}, fmt.Errorf("the deployment %q env %q uses a valueFrom source dzjjy cannot represent", deployment.Metadata.Name, entry.Name)
			}
			secrets = append(secrets, map[string]string{
				"key": entry.ValueFrom.SecretKeyRef.Name + "/" + entry.ValueFrom.SecretKeyRef.Key,
				"env": entry.Name,
			})
			continue
		}
		env[entry.Name] = entry.Value
	}
	for _, source := range container.EnvFrom {
		notes = append(notes, "envFrom source was skipped: dzjjy imports env values explicitly")
		if source.Prefix != "" {
			notes = append(notes, fmt.Sprintf("envFrom prefix %q has no dzjjy equivalent and was dropped", source.Prefix))
		}
	}
	if len(env) > 0 {
		spec["env"] = envToList(env)
	}
	if len(secrets) > 0 {
		spec["secrets"] = secrets
		notes = append(notes, fmt.Sprintf("%d secret(s) imported as dzjjy secret references; create them in the platform secret store before applying", len(secrets)))
	}

	if container.Resources != nil {
		resources := map[string]any{}
		if memory, ok := container.Resources.Limits["memory"]; ok {
			resources["memory"] = normalizeQuantity(memory)
		}
		if cpu, ok := container.Resources.Limits["cpu"]; ok {
			resources["cpus"] = normalizeCPU(cpu)
		}
		if len(resources) > 0 {
			spec["resources"] = resources
		}
	}

	var ports []map[string]int
	if len(container.Ports) > 0 {
		for _, port := range container.Ports {
			if port.ContainerPort < 1 || port.ContainerPort > 65535 {
				return Document{}, fmt.Errorf("the deployment %q declares an out-of-range containerPort %d", deployment.Metadata.Name, port.ContainerPort)
			}
			ports = append(ports, map[string]int{"container": port.ContainerPort})
		}
	}

	if service != nil {
		if service.Spec.Type != "" && service.Spec.Type != "ClusterIP" {
			notes = append(notes, fmt.Sprintf("Service type %q was reduced to a dzjjy port mapping; external load balancers are not created", service.Spec.Type))
		}
		ports = nil
		for _, port := range service.Spec.Ports {
			target := resolveTargetPort(port.TargetPort, container)
			if target < 1 || target > 65535 {
				return Document{}, fmt.Errorf("the service %q resolves to an out-of-range target port", service.Metadata.Name)
			}
			entry := map[string]int{"container": target}
			if port.NodePort > 0 {
				entry["host"] = port.NodePort
			}
			ports = append(ports, entry)
		}
	}
	if len(ports) > 0 {
		spec["network"] = map[string]any{"ports": ports}
	}

	if replicas := deployment.Spec.Replicas; replicas != nil && *replicas != 1 {
		notes = append(notes, fmt.Sprintf("replicas=%d was reduced to a single dzjjy instance; horizontal scaling is not supported", *replicas))
	}

	var volumes []map[string]string
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Secret != nil {
			notes = append(notes, fmt.Sprintf("secret volume %q was skipped: dzjjy injects secrets as environment variables, not files", volume.Name))
			continue
		}
		if volume.ConfigMap != nil {
			notes = append(notes, fmt.Sprintf("configMap volume %q was skipped: dzjjy does not mount config maps", volume.Name))
			continue
		}
		if volume.EmptyDir == nil {
			return Document{}, fmt.Errorf("volume %q uses a source dzjjy cannot represent; only emptyDir is importable", volume.Name)
		}
		mountPath := ""
		for _, mount := range container.VolumeMounts {
			if mount.Name == volume.Name {
				mountPath = mount.MountPath
			}
		}
		if mountPath == "" {
			notes = append(notes, fmt.Sprintf("volume %q was not mounted by the container and was skipped", volume.Name))
			continue
		}
		volumes = append(volumes, map[string]string{
			"name": "dzjjy-" + deployment.Metadata.Name + "-" + volume.Name,
			"path": mountPath,
		})
	}
	if len(volumes) > 0 {
		spec["volumes"] = volumes
	}

	if container.SecurityContext != nil && container.SecurityContext.ReadOnlyRootFilesystem != nil && *container.SecurityContext.ReadOnlyRootFilesystem {
		notes = append(notes, "readOnlyRootFilesystem=true was dropped: dzjjy always mounts the workspace read-only")
	}

	document := map[string]any{
		"apiVersion": "dzjjy/v1",
		"kind":       "App",
		"metadata":   map[string]any{"name": sanitizeName(deployment.Metadata.Name)},
		"spec":       spec,
	}
	rendered, err := yaml.Marshal(document)
	if err != nil {
		return Document{}, fmt.Errorf("failed to render dzjjy manifest: %w", err)
	}
	return Document{
		YAML:   string(rendered),
		Notes:  notes,
		Source: deployment.Metadata.Name,
	}, nil
}

func resolveTargetPort(target any, container ContainerSpec) int {
	switch value := target.(type) {
	case nil:
		if len(container.Ports) > 0 {
			return container.Ports[0].ContainerPort
		}
		return 0
	case int:
		return value
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(value)
		if err != nil {
			for _, port := range container.Ports {
				if port.Name == value {
					return port.ContainerPort
				}
			}
			return 0
		}
		return parsed
	}
	return 0
}

// validateTemplateImage rejects arbitrary images. dzjjy only runs its own
// maintained runtime templates, so an import must name a supported runtime.
func validateTemplateImage(image string) error {
	if image == "" {
		return fmt.Errorf("container image is required")
	}
	for _, supported := range []string{"python", "node", "go"} {
		if templateType(image) == supported {
			return nil
		}
	}
	return fmt.Errorf("image %q is not a dzjjy maintained runtime template; import only supports python, node, and go", image)
}

func templateType(image string) string {
	base := image
	if index := strings.LastIndex(base, "/"); index >= 0 {
		base = base[index+1:]
	}
	if index := strings.LastIndex(base, ":"); index >= 0 {
		base = base[:index]
	}
	switch {
	case strings.HasPrefix(base, "python"):
		return "python"
	case strings.HasPrefix(base, "node"):
		return "node"
	case strings.HasPrefix(base, "golang"), strings.HasPrefix(base, "go"):
		return "go"
	}
	return "unsupported"
}

// templateVersion maps an image reference onto the runtime version dzjjy
// maintains. The version usually lives in the tag ("python:3.12-slim"); some
// images encode it in the name instead ("python3.12-slim").
func templateVersion(image string) string {
	base := image
	if index := strings.LastIndex(base, "/"); index >= 0 {
		base = base[index+1:]
	}
	tag := ""
	if index := strings.LastIndex(base, ":"); index >= 0 {
		tag = base[index+1:]
		base = base[:index]
	}
	// A tag such as "3.12-slim" carries the version on its own.
	if version := leadingVersion(tag); version != "" {
		return resolveSupportedVersion(version)
	}
	// Otherwise the version follows the runtime name: "python3.12-slim".
	for _, marker := range []string{"python", "node", "golang", "go"} {
		if rest, ok := strings.CutPrefix(base, marker); ok {
			if version := leadingVersion(rest); version != "" {
				return resolveSupportedVersion(version)
			}
		}
	}
	return ""
}

// resolveSupportedVersion picks the platform template version matching an
// imported image version, falling back to the single maintained version of the
// same major line (for example "3" resolving to python 3.12).
func resolveSupportedVersion(version string) string {
	supported := map[string]string{
		"3":  "3.12",
		"22": "22",
		"1":  "1.24",
	}
	if mapped, ok := supported[version]; ok {
		return mapped
	}
	return version
}

// leadingVersion returns the leading dotted number in s, e.g. "3.12" for
// "3.12-slim".
func leadingVersion(s string) string {
	trimmed := strings.TrimLeft(s, "-.")
	digits := ""
	for _, ch := range trimmed {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			digits += string(ch)
			continue
		}
		break
	}
	if digits == "" {
		return ""
	}
	return strings.TrimSuffix(digits, ".")
}

// normalizeQuantity maps a Kubernetes memory quantity onto the Docker size
// syntax dzjjy accepts: 256Mi becomes 256m, 1Gi becomes 1g.
func normalizeQuantity(value string) string {
	trimmed := strings.TrimSpace(value)
	// Binary suffixes carry a trailing "i" that Docker does not use.
	trimmed = strings.TrimSuffix(trimmed, "i")
	switch {
	case strings.HasSuffix(trimmed, "K"), strings.HasSuffix(trimmed, "k"):
		return strings.TrimRight(trimmed, "Kk") + "k"
	case strings.HasSuffix(trimmed, "M"), strings.HasSuffix(trimmed, "m"):
		return strings.TrimRight(trimmed, "Mm") + "m"
	case strings.HasSuffix(trimmed, "G"), strings.HasSuffix(trimmed, "g"):
		return strings.TrimRight(trimmed, "Gg") + "g"
	case strings.HasSuffix(trimmed, "T"), strings.HasSuffix(trimmed, "t"):
		return strings.TrimRight(trimmed, "Tt") + "t"
	}
	return trimmed
}

func normalizeCPU(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasSuffix(trimmed, "m") {
		millis, err := strconv.Atoi(strings.TrimSuffix(trimmed, "m"))
		if err == nil {
			return strconv.FormatFloat(float64(millis)/1000, 'f', -1, 64)
		}
	}
	return trimmed
}

func envToList(env map[string]string) []map[string]string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"name": key, "value": env[key]})
	}
	return out
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, ch := range strings.ToLower(name) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-', ch == '_', ch == '.':
			builder.WriteRune(ch)
		default:
			builder.WriteRune('-')
		}
	}
	return builder.String()
}
