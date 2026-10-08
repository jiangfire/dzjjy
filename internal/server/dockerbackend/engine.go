package dockerbackend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const managedLabel = "dzjjy.managed"
const appLabel = "dzjjy.app"
const generationLabel = "dzjjy.generation"

// Template definitions are maintained by dzjjy; request data cannot supply images or Docker flags.
type Template struct {
	ID      string
	Version string
	Image   string
	OSTypes []string
}

var templates = map[string]Template{
	"python:3.12": {ID: "python", Version: "3.12", Image: "python:3.12-slim", OSTypes: []string{"linux"}},
	"node:22":     {ID: "node", Version: "22", Image: "node:22-alpine", OSTypes: []string{"linux"}},
	"go:1.24":     {ID: "go", Version: "1.24", Image: "golang:1.24-alpine", OSTypes: []string{"linux"}},
}

func Resolve(runtimeType, version, osType string) (Template, error) {
	template, ok := templates[runtimeType+":"+version]
	if !ok {
		return Template{}, fmt.Errorf("unsupported runtime template %q:%q", runtimeType, version)
	}
	for _, supported := range template.OSTypes {
		if supported == osType {
			return template, nil
		}
	}
	return Template{}, fmt.Errorf("runtime template %s:%s does not support Docker OS %q", runtimeType, version, osType)
}

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

type Engine struct {
	runner Runner
}

func NewEngine() *Engine                        { return &Engine{runner: commandRunner{}} }
func NewEngineWithRunner(runner Runner) *Engine { return &Engine{runner: runner} }

func (e *Engine) daemonOS(ctx context.Context) (string, error) {
	output, err := e.runner.Run(ctx, "info", "--format", "{{.OSType}}")
	if err != nil {
		return "", fmt.Errorf("Docker Engine unavailable: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *Engine) Start(ctx context.Context, appName string, generation uint64, runtimeType, version, artifactDir string, command []string) (string, error) {
	if appName == "" || len(command) == 0 {
		return "", fmt.Errorf("app name and command are required")
	}
	absArtifact, err := filepath.Abs(artifactDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absArtifact)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("artifact directory is unavailable")
	}
	if strings.Contains(absArtifact, ",") {
		return "", fmt.Errorf("artifact path contains unsupported Docker mount delimiter")
	}
	osType, err := e.daemonOS(ctx)
	if err != nil {
		return "", err
	}
	template, err := Resolve(runtimeType, version, osType)
	if err != nil {
		return "", err
	}
	if output, err := e.runner.Run(ctx, "pull", template.Image); err != nil {
		return "", fmt.Errorf("failed to pull maintained runtime template: %w: %s", err, strings.TrimSpace(string(output)))
	}
	args := []string{
		"create", "--label", managedLabel + "=true",
		"--label", appLabel + "=" + appName,
		"--label", generationLabel + fmt.Sprintf("=%d", generation),
		"--workdir", "/workspace",
		"--mount", "type=bind,source=" + absArtifact + ",target=/workspace,readonly",
		template.Image,
	}
	args = append(args, command...)
	output, err := e.runner.Run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return "", fmt.Errorf("Docker returned an empty container ID")
	}
	if output, err := e.runner.Run(ctx, "start", containerID); err != nil {
		_, _ = e.runner.Run(ctx, "rm", "-f", containerID)
		return "", fmt.Errorf("failed to start container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return containerID, nil
}

func (e *Engine) StopAndRemove(ctx context.Context, containerID string, timeoutSeconds int) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}
	output, err := e.runner.Run(ctx, "inspect", "--format", "{{index .Config.Labels \"dzjjy.managed\"}}", containerID)
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("refusing to manage unlabelled container %q", containerID)
	}
	if _, err := e.runner.Run(ctx, "stop", "--time", fmt.Sprintf("%d", timeoutSeconds), containerID); err != nil {
		if _, killErr := e.runner.Run(ctx, "kill", containerID); killErr != nil {
			return fmt.Errorf("container stop and kill failed: %w", killErr)
		}
	}
	if _, err := e.runner.Run(ctx, "rm", containerID); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

func (e *Engine) StartExisting(ctx context.Context, containerID string) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}
	output, err := e.runner.Run(ctx, "inspect", "--format", "{{index .Config.Labels \"dzjjy.managed\"}}", containerID)
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("refusing to restore unlabelled container %q", containerID)
	}
	if output, err := e.runner.Run(ctx, "start", containerID); err != nil {
		return fmt.Errorf("failed to restore container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
