package appconfig

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const APIVersion = "dzjjy/v1"

type Document struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Spec struct {
	Artifact  string    `yaml:"artifact,omitempty" json:"artifact,omitempty"`
	Runtime   *Runtime  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Command   []string  `yaml:"command,omitempty" json:"command,omitempty"`
	Env       []EnvVar  `yaml:"env,omitempty" json:"env,omitempty"`
	Restart   string    `yaml:"restart,omitempty" json:"restart,omitempty"`
	Sandbox   *Sandbox  `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	Network   *Network  `yaml:"network,omitempty" json:"network,omitempty"`
	Secrets   []Secret  `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Volumes   []Volume  `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Resources Resources `yaml:"resources,omitempty" json:"resources,omitempty"`
	Port      *int      `yaml:"port,omitempty" json:"port,omitempty"`
	Exposure  *Exposure `yaml:"exposure,omitempty" json:"exposure,omitempty"`
	Lifecycle Lifecycle `yaml:"lifecycle,omitempty" json:"lifecycle,omitempty"`
}

// EnvVar is a literal environment variable passed to the process or container.
type EnvVar struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// Secret injects a platform-managed secret as an environment variable.
// Only the key name is declared in the manifest; the value is never stored
// in the desired config, the state file, or any API response.
type Secret struct {
	Key string `yaml:"key" json:"key"`
	Env string `yaml:"env,omitempty" json:"env,omitempty"`
}

// Volume mounts a named volume at a container path. Only named volumes
// maintained by Docker are accepted; host paths are rejected.
type Volume struct {
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
}

// Network describes port publishing for a container. The bridge network is
// maintained by the platform; request data cannot select arbitrary networks.
type Network struct {
	Ports []PortMapping `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// PortMapping publishes a container port on the host.
type PortMapping struct {
	Container int `yaml:"container" json:"container"`
	Host      int `yaml:"host,omitempty" json:"host,omitempty"`
}

// Resources are the container resource limits enforced by Docker.
type Resources struct {
	Memory     string `yaml:"memory,omitempty" json:"memory,omitempty"`
	CPUs       string `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Pids       *int   `yaml:"pids,omitempty" json:"pids,omitempty"`
	MemorySwap string `yaml:"memorySwap,omitempty" json:"memorySwap,omitempty"`
}

// Exposure makes a StaticSite reachable through an unguessable token path.
type Exposure struct {
	Token string `yaml:"token,omitempty" json:"token,omitempty"`
}

type Runtime struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
}

type Sandbox struct {
	Type string `yaml:"type" json:"type"`
	// Isolator selects an additional isolation layer maintained by the
	// platform. "runsc" requires a gVisor runtime on the host; it is only
	// valid with the docker sandbox.
	Isolator string `yaml:"isolator,omitempty" json:"isolator,omitempty"`
}

type Lifecycle struct {
	TTL string `yaml:"ttl,omitempty" json:"ttl,omitempty"`
	// IdleTimeout suspends the app after no HTTP traffic for this duration.
	// Suspended apps are restarted on the next request.
	IdleTimeout string `yaml:"idleTimeout,omitempty" json:"idleTimeout,omitempty"`
}

func Parse(data []byte) (*Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if err := document.Validate(); err != nil {
		return nil, err
	}
	return &document, nil
}

func (d *Document) Validate() error {
	if d.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if d.Kind != "App" && d.Kind != "StaticSite" {
		return fmt.Errorf("kind must be App or StaticSite")
	}
	if d.Metadata.Name == "" || len(d.Metadata.Name) > 64 || strings.ContainsAny(d.Metadata.Name, `/\`) || strings.Contains(d.Metadata.Name, "..") {
		return fmt.Errorf("metadata.name is invalid")
	}
	if d.Kind == "StaticSite" && d.Spec.Artifact == "" {
		return fmt.Errorf("spec.artifact is required")
	}
	if d.Spec.Restart == "" {
		d.Spec.Restart = "never"
	}
	if d.Spec.Restart != "never" && d.Spec.Restart != "on-failure" {
		return fmt.Errorf("spec.restart must be never or on-failure")
	}
	if d.Spec.Lifecycle.TTL != "" {
		ttl, err := time.ParseDuration(d.Spec.Lifecycle.TTL)
		if err != nil || ttl <= 0 {
			return fmt.Errorf("spec.lifecycle.ttl must be a positive duration")
		}
	}
	if d.Spec.Lifecycle.IdleTimeout != "" {
		idle, err := time.ParseDuration(d.Spec.Lifecycle.IdleTimeout)
		if err != nil || idle <= 0 {
			return fmt.Errorf("spec.lifecycle.idleTimeout must be a positive duration")
		}
	}
	if d.Spec.Sandbox == nil {
		d.Spec.Sandbox = &Sandbox{Type: "process"}
	}
	if d.Spec.Sandbox.Type != "process" && d.Spec.Sandbox.Type != "docker" {
		return fmt.Errorf("spec.sandbox.type must be process or docker")
	}
	if d.Spec.Sandbox.Isolator != "" && d.Spec.Sandbox.Isolator != "runsc" {
		return fmt.Errorf("spec.sandbox.isolator must be runsc when set")
	}
	if d.Spec.Sandbox.Isolator != "" && d.Spec.Sandbox.Type != "docker" {
		return fmt.Errorf("spec.sandbox.isolator requires sandbox.type \"docker\"")
	}
	for _, secret := range d.Spec.Secrets {
		if secret.Key == "" {
			return fmt.Errorf("spec.secrets[].key is required")
		}
		if strings.Contains(secret.Key, "..") || strings.ContainsAny(secret.Key, `\\:*?"<>|`) {
			return fmt.Errorf("spec.secrets[].key contains unsupported characters")
		}
		for _, segment := range strings.Split(secret.Key, "/") {
			if segment == "" {
				return fmt.Errorf("spec.secrets[].key contains an empty path segment")
			}
		}
		if secret.Env != "" && !validEnvName(secret.Env) {
			return fmt.Errorf("spec.secrets[].env %q is not a valid environment variable name", secret.Env)
		}
	}
	for _, volume := range d.Spec.Volumes {
		if err := validateVolumeName(volume.Name); err != nil {
			return err
		}
		if !strings.HasPrefix(volume.Path, "/") || strings.Contains(volume.Path, "..") {
			return fmt.Errorf("spec.volumes[].path must be an absolute container path without \"..\"")
		}
	}
	if d.Spec.Network != nil {
		for _, mapping := range d.Spec.Network.Ports {
			if mapping.Container < 1 || mapping.Container > 65535 {
				return fmt.Errorf("spec.network.ports[].container must be between 1 and 65535")
			}
			if mapping.Host < 0 || mapping.Host > 65535 {
				return fmt.Errorf("spec.network.ports[].host must be between 0 and 65535")
			}
		}
	}
	if err := d.Spec.Resources.validate(); err != nil {
		return err
	}
	for _, env := range d.Spec.Env {
		if !validEnvName(env.Name) {
			return fmt.Errorf("spec.env[].name %q is not a valid environment variable name", env.Name)
		}
	}
	if d.Spec.Exposure != nil && d.Spec.Exposure.Token != "" {
		if !validToken(d.Spec.Exposure.Token) {
			return fmt.Errorf("spec.exposure.token must be 32-128 URL-safe characters")
		}
	}
	if d.Kind == "StaticSite" {
		if d.Spec.Runtime != nil || len(d.Spec.Command) != 0 || d.Spec.Sandbox.Type != "process" {
			return fmt.Errorf("StaticSite does not support runtime, command, or Docker sandbox")
		}
		if len(d.Spec.Secrets) != 0 || len(d.Spec.Volumes) != 0 {
			return fmt.Errorf("StaticSite does not support secrets or volumes")
		}
		if d.Spec.Network != nil && len(d.Spec.Network.Ports) != 0 {
			return fmt.Errorf("StaticSite does not support port mappings")
		}
		if d.Spec.Sandbox.Isolator != "" {
			return fmt.Errorf("StaticSite does not support a sandbox isolator")
		}
		if !d.Spec.Resources.IsZero() {
			return fmt.Errorf("StaticSite does not support resource limits")
		}
		if d.Spec.Port != nil && (*d.Spec.Port < 0 || *d.Spec.Port > 65535) {
			return fmt.Errorf("spec.port must be between 0 and 65535")
		}
		return nil
	}
	if d.Spec.Port != nil {
		return fmt.Errorf("spec.port is only valid for StaticSite")
	}
	if len(d.Spec.Command) == 0 {
		return fmt.Errorf("spec.command is required for App")
	}
	if d.Spec.Runtime != nil {
		if err := validateTemplate(d.Spec.Runtime.Type, d.Spec.Runtime.Version); err != nil {
			return err
		}
	}
	return nil
}

// IsZero reports whether no resource limit is declared.
func (r Resources) IsZero() bool {
	return r.Memory == "" && r.CPUs == "" && r.MemorySwap == "" && r.Pids == nil
}

func (r Resources) validate() error {
	if r.Memory != "" {
		if err := validateByteSize(r.Memory); err != nil {
			return fmt.Errorf("spec.resources.memory: %w", err)
		}
	}
	if r.MemorySwap != "" {
		if err := validateByteSize(r.MemorySwap); err != nil {
			return fmt.Errorf("spec.resources.memorySwap: %w", err)
		}
	}
	if r.CPUs != "" {
		if _, err := ParseCPUs(r.CPUs); err != nil {
			return fmt.Errorf("spec.resources.cpus: %w", err)
		}
	}
	if r.Pids != nil && (*r.Pids < 1 || *r.Pids > 1048576) {
		return fmt.Errorf("spec.resources.pids must be between 1 and 1048576")
	}
	return nil
}

// ParseCPUs converts a decimal CPU string into a NanoCPUs value accepted by
// the Docker CLI --cpus flag, which takes a decimal number of CPUs.
func ParseCPUs(value string) (float64, error) {
	var cpus float64
	if _, err := fmt.Sscanf(value, "%g", &cpus); err != nil {
		return 0, fmt.Errorf("must be a decimal number of CPUs")
	}
	if cpus <= 0 || cpus > 64 {
		return 0, fmt.Errorf("must be between 0 and 64")
	}
	return cpus, nil
}

// Multiplier is the smallest allowed memory and memory-swap size, 6m.
const minMemoryBytes = 6 << 20

func validateByteSize(value string) error {
	size, err := ParseByteSize(value)
	if err != nil {
		return err
	}
	if size < minMemoryBytes {
		return fmt.Errorf("must be at least 6m")
	}
	return nil
}

// ParseByteSize parses Docker-style size suffixes (b, k, m, g, t) with
// optional binary (1024) multipliers, as accepted by --memory.
func ParseByteSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return 0, fmt.Errorf("must not be empty")
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(trimmed, "b"):
		trimmed = strings.TrimSuffix(trimmed, "b")
	case strings.HasSuffix(trimmed, "k"), strings.HasSuffix(trimmed, "kb"):
		trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "b"), "k")
		multiplier = 1 << 10
	case strings.HasSuffix(trimmed, "m"), strings.HasSuffix(trimmed, "mb"):
		trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "b"), "m")
		multiplier = 1 << 20
	case strings.HasSuffix(trimmed, "g"), strings.HasSuffix(trimmed, "gb"):
		trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "b"), "g")
		multiplier = 1 << 30
	case strings.HasSuffix(trimmed, "t"), strings.HasSuffix(trimmed, "tb"):
		trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "b"), "t")
		multiplier = 1 << 40
	}
	var number float64
	if _, err := fmt.Sscanf(trimmed, "%g", &number); err != nil {
		return 0, fmt.Errorf("must be a number with an optional b/k/m/g/t suffix")
	}
	if number < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	if float64(int64(number)) != number {
		return 0, fmt.Errorf("must be a whole number of bytes")
	}
	return int64(number) * multiplier, nil
}

func validateTemplate(runtimeType, version string) error {
	allowed := map[string]map[string]bool{
		"python": {"3.12": true},
		"node":   {"22": true},
		"go":     {"1.24": true},
	}
	if !allowed[runtimeType][version] {
		return fmt.Errorf("unsupported runtime template %q:%q", runtimeType, version)
	}
	return nil
}

func validateVolumeName(name string) error {
	if name == "" || len(name) > 128 {
		return fmt.Errorf("spec.volumes[].name must be between 1 and 128 characters")
	}
	for _, ch := range name {
		isLower := ch >= 'a' && ch <= 'z'
		isUpper := ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if !isLower && !isUpper && !isDigit && ch != '_' && ch != '-' && ch != '.' {
			return fmt.Errorf("spec.volumes[].name contains unsupported character %q", ch)
		}
	}
	return nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		isUpper := ch >= 'A' && ch <= 'Z'
		isLower := ch >= 'a' && ch <= 'z'
		isDigit := ch >= '0' && ch <= '9'
		if i == 0 && isDigit {
			return false
		}
		if !isUpper && !isLower && !isDigit && ch != '_' {
			return false
		}
	}
	return true
}

func validToken(token string) bool {
	if len(token) < 32 || len(token) > 128 {
		return false
	}
	for _, ch := range token {
		isLower := ch >= 'a' && ch <= 'z'
		isUpper := ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if !isLower && !isUpper && !isDigit && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}
