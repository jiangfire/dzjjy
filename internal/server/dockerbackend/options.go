package dockerbackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options carries the Phase 3 container settings resolved from a dzjjy/v1
// document. Every field is validated by appconfig before it reaches here, and
// the engine re-checks the constraints that Docker itself enforces.
type Options struct {
	// Env holds literal environment variables.
	Env map[string]string
	// SecretEnv maps environment variable name to secret key. Values are
	// resolved by the caller from the platform secret store; the engine never
	// sees a secret key it cannot resolve.
	SecretEnv map[string]string
	// Volumes are named volumes mounted at container paths.
	Volumes []VolumeMount
	// Ports are host-to-container port bindings. A zero Host means Docker
	// assigns an ephemeral host port.
	Ports []PortBinding
	// Resources are container resource limits.
	Resources ResourceLimits
	// Isolator selects an extra isolation layer, currently only "runsc".
	Isolator string
}

// VolumeMount is a named volume mounted at a container path.
type VolumeMount struct {
	Name string
	Path string
}

// PortBinding is a resolved host-to-container port binding.
type PortBinding struct {
	Container int
	Host      int
}

// ResourceLimits constrains a container.
type ResourceLimits struct {
	Memory     int64
	CPUs       float64
	MemorySwap int64
	Pids       int
}

// IsolatorRuntime is the Docker runtime name registered by gVisor.
const IsolatorRuntime = "runsc"

// ErrIsolatorUnavailable reports that the requested isolator is not installed
// on the host. Callers must surface it rather than silently degrading to the
// default Docker runtime.
type ErrIsolatorUnavailable struct {
	Isolator string
	Reason   string
}

func (e *ErrIsolatorUnavailable) Error() string {
	return fmt.Sprintf("sandbox isolator %q is unavailable: %s", e.Isolator, e.Reason)
}

// SupportsIsolator reports whether the host can run the given isolator.
func (e *Engine) SupportsIsolator(ctx context.Context, isolator string) error {
	if isolator == "" {
		return nil
	}
	if isolator != IsolatorRuntime {
		return fmt.Errorf("unsupported isolator %q", isolator)
	}
	output, err := e.runner.Run(ctx, "info", "--format", "{{json .Runtimes}}")
	if err != nil {
		return &ErrIsolatorUnavailable{Isolator: isolator, Reason: "docker engine unavailable: " + strings.TrimSpace(string(output))}
	}
	if !strings.Contains(string(output), IsolatorRuntime) {
		return &ErrIsolatorUnavailable{Isolator: isolator, Reason: "the runsc runtime is not registered with the docker daemon"}
	}
	return nil
}

// Pause freezes a managed container without removing it, so an idle app can
// be resumed later without re-pulling its image or re-creating its container.
func (e *Engine) Pause(ctx context.Context, containerID string) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}
	output, err := e.runner.Run(ctx, "inspect", "--format", "{{index .Config.Labels \"dzjjy.managed\"}}", containerID)
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("refusing to pause unlabelled container %q", containerID)
	}
	if output, err := e.runner.Run(ctx, "pause", containerID); err != nil {
		return fmt.Errorf("failed to pause container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Unpause thaws a container previously paused by Pause.
func (e *Engine) Unpause(ctx context.Context, containerID string) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}
	output, err := e.runner.Run(ctx, "inspect", "--format", "{{index .Config.Labels \"dzjjy.managed\"}}", containerID)
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("refusing to unpause unlabelled container %q", containerID)
	}
	if output, err := e.runner.Run(ctx, "unpause", containerID); err != nil {
		return fmt.Errorf("failed to unpause container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// StartWithOptions creates and starts a container with the full Phase 3
// option set. The caller resolves secrets; the engine only forwards them.
func (e *Engine) StartWithOptions(ctx context.Context, appName string, generation uint64, runtimeType, version, artifactDir string, command []string, opts Options) (string, []PortBinding, error) {
	if appName == "" || len(command) == 0 {
		return "", nil, fmt.Errorf("app name and command are required")
	}
	absArtifact, err := filepath.Abs(artifactDir)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(absArtifact)
	if err != nil || !info.IsDir() {
		return "", nil, fmt.Errorf("artifact directory is unavailable")
	}
	if strings.Contains(absArtifact, ",") {
		return "", nil, fmt.Errorf("artifact path contains unsupported Docker mount delimiter")
	}
	for _, volume := range opts.Volumes {
		if strings.ContainsAny(volume.Name, ",=") {
			return "", nil, fmt.Errorf("volume name contains unsupported Docker delimiter")
		}
		if strings.Contains(volume.Path, ",") {
			return "", nil, fmt.Errorf("volume path contains unsupported Docker mount delimiter")
		}
	}
	if err := e.SupportsIsolator(ctx, opts.Isolator); err != nil {
		return "", nil, err
	}
	osType, err := e.daemonOS(ctx)
	if err != nil {
		return "", nil, err
	}
	template, err := Resolve(runtimeType, version, osType)
	if err != nil {
		return "", nil, err
	}
	if output, err := e.runner.Run(ctx, "pull", template.Image); err != nil {
		return "", nil, fmt.Errorf("failed to pull maintained runtime template: %w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, volume := range opts.Volumes {
		// Named volumes are created lazily by Docker; an explicit create keeps
		// the label ownership check consistent with container ownership.
		if output, err := e.runner.Run(ctx, "volume", "create", "--label", managedLabel+"=true", "--label", appLabel+"="+appName, volume.Name); err != nil {
			return "", nil, fmt.Errorf("failed to create volume %q: %w: %s", volume.Name, err, strings.TrimSpace(string(output)))
		}
	}

	args := []string{
		"create", "--label", managedLabel + "=true",
		"--label", appLabel + "=" + appName,
		"--label", generationLabel + fmt.Sprintf("=%d", generation),
		"--workdir", "/workspace",
		"--mount", "type=bind,source=" + absArtifact + ",target=/workspace,readonly",
	}
	for _, volume := range opts.Volumes {
		args = append(args, "--mount", "type=volume,source="+volume.Name+",target="+volume.Path)
	}
	for _, binding := range opts.Ports {
		args = append(args, "-p", portSpec(binding))
	}
	for _, name := range sortedKeys(opts.Env) {
		args = append(args, "-e", name+"="+opts.Env[name])
	}
	for _, name := range sortedKeys(opts.SecretEnv) {
		args = append(args, "-e", name+"="+opts.SecretEnv[name])
	}
	if opts.Resources.Memory > 0 {
		args = append(args, "--memory", strconv.FormatInt(opts.Resources.Memory, 10))
	}
	if opts.Resources.MemorySwap > 0 {
		args = append(args, "--memory-swap", strconv.FormatInt(opts.Resources.MemorySwap, 10))
	}
	if opts.Resources.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(opts.Resources.CPUs, 'f', -1, 64))
	}
	if opts.Resources.Pids > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(opts.Resources.Pids))
	}
	if opts.Isolator == IsolatorRuntime {
		args = append(args, "--runtime", IsolatorRuntime)
	}
	args = append(args, template.Image)
	args = append(args, command...)
	output, err := e.runner.Run(ctx, args...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return "", nil, fmt.Errorf("docker returned an empty container ID")
	}
	if output, err := e.runner.Run(ctx, "start", containerID); err != nil {
		_, _ = e.runner.Run(ctx, "rm", "-f", containerID)
		return "", nil, fmt.Errorf("failed to start container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	bindings, err := e.resolvePublishedPorts(ctx, containerID, opts.Ports)
	if err != nil {
		_, _ = e.runner.Run(ctx, "rm", "-f", containerID)
		return "", nil, err
	}
	return containerID, bindings, nil
}

// resolvePublishedPorts asks Docker for the host ports actually bound, which
// matters when the manifest left the host side at 0.
func (e *Engine) resolvePublishedPorts(ctx context.Context, containerID string, requested []PortBinding) ([]PortBinding, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	output, err := e.runner.Run(ctx, "port", containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve published ports: %w: %s", err, strings.TrimSpace(string(output)))
	}
	resolved := make([]PortBinding, 0, len(requested))
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "docker port" reports "8000/tcp -> 0.0.0.0:49153" (and the IPv6
		// form ":::49153"); keep the first mapping seen per container port.
		_, mapping, found := strings.Cut(line, "->")
		if !found {
			continue
		}
		mapping = strings.TrimSpace(mapping)
		colon := strings.LastIndex(mapping, ":")
		if colon < 0 {
			continue
		}
		hostPort, err := strconv.Atoi(strings.TrimSpace(mapping[colon+1:]))
		if err != nil {
			continue
		}
		portText, _, _ := strings.Cut(strings.TrimSpace(line[:strings.Index(line, "->")]), "/")
		containerPort, err := strconv.Atoi(strings.TrimSpace(portText))
		if err != nil {
			continue
		}
		if !containsPort(resolved, containerPort) {
			resolved = append(resolved, PortBinding{Container: containerPort, Host: hostPort})
		}
	}
	if len(resolved) == 0 {
		for _, binding := range requested {
			if binding.Host > 0 {
				resolved = append(resolved, binding)
			}
		}
	}
	return resolved, nil
}

func containsPort(bindings []PortBinding, containerPort int) bool {
	for _, binding := range bindings {
		if binding.Container == containerPort {
			return true
		}
	}
	return false
}

func portSpec(binding PortBinding) string {
	if binding.Host == 0 {
		return strconv.Itoa(binding.Container)
	}
	return fmt.Sprintf("%d:%d", binding.Host, binding.Container)
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
