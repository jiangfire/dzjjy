//go:build docker_e2e

// Real-daemon E2E tests for the Docker backend. They are excluded from normal
// `go test ./...` runs via the docker_e2e build tag: they pull maintained
// template images and take minutes. CI runs them on an ubuntu runner with a
// real Docker daemon (see .github/workflows/ci.yml, job docker-e2e).
// Local runs: go test -tags=docker_e2e -timeout 20m -run TestE2E ./internal/server/dockerbackend/...
package dockerbackend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireLinuxDockerDaemon skips the test when no Docker daemon is reachable
// or the daemon is not in Linux-container mode (templates only support linux).
func requireLinuxDockerDaemon(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.OSType}}").CombinedOutput() // #nosec G204 - fixed docker arguments
	if err != nil {
		t.Skipf("docker daemon unavailable, skipping E2E: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if osType := strings.TrimSpace(string(output)); osType != "linux" {
		t.Skipf("docker daemon OS type %q is not linux, skipping E2E", osType)
	}
}

func dockerCLI(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() // #nosec G204 - fixed docker arguments supplied by the test
	if err != nil {
		t.Fatalf("docker %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output)
}

func dockerInspect(t *testing.T, containerID, format string) string {
	t.Helper()
	return strings.TrimSpace(dockerCLI(t, "inspect", "--format", format, containerID))
}

func eventuallyDocker(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func containerGone(t *testing.T, containerID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "inspect", containerID).Run() != nil // #nosec G204 - fixed docker arguments
}

func writeArtifact(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

// verifyManagedContainer asserts the acceptance criteria for containers the
// engine created: dzjjy labels, exactly one readonly artifact mount at
// /workspace, no Docker socket or host paths, and an unprivileged container.
func verifyManagedContainer(t *testing.T, containerID, appName, artifactDir string) {
	t.Helper()
	require.Equal(t, "true", dockerInspect(t, containerID, `{{index .Config.Labels "dzjjy.managed"}}`))
	require.Equal(t, appName, dockerInspect(t, containerID, `{{index .Config.Labels "dzjjy.app"}}`))
	require.Equal(t, "1", dockerInspect(t, containerID, `{{index .Config.Labels "dzjjy.generation"}}`))
	require.Equal(t, "false", dockerInspect(t, containerID, "{{.HostConfig.Privileged}}"))

	mounts := dockerInspect(t, containerID, "{{json .HostConfig.Mounts}}")
	expectedSource := strconv.Quote(mustAbs(t, artifactDir))
	require.Contains(t, mounts, `"Target":"/workspace"`, "artifact must mount at /workspace: %s", mounts)
	require.Contains(t, mounts, expectedSource, "only the artifact dir may be mounted: %s", mounts)
	require.True(t, strings.Contains(mounts, `"ReadOnly":true`) || strings.Contains(mounts, `"RO":true`),
		"artifact mount must be readonly: %s", mounts)

	full := dockerInspect(t, containerID, "{{json .}}")
	require.NotContains(t, full, "docker.sock", "container must not see the Docker socket")
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

func waitForLogMarker(t *testing.T, containerID, marker string, timeout time.Duration) {
	t.Helper()
	eventuallyDocker(t, timeout, fmt.Sprintf("marker %q in container logs", marker), func() bool {
		return strings.Contains(dockerCLI(t, "logs", containerID), marker)
	})
}

// TestE2EManagedContainerLifecycle exercises the full container lifecycle of
// every first-class runtime template against a real daemon:
// pull -> create -> start -> logs -> stop(timeout) -> remove.
func TestE2EManagedContainerLifecycle(t *testing.T) {
	requireLinuxDockerDaemon(t)

	cases := []struct {
		name        string
		runtimeType string
		version     string
		files       map[string]string
		command     []string
		marker      string
		logTimeout  time.Duration
	}{
		{
			name: "python", runtimeType: "python", version: "3.12",
			files: map[string]string{
				"app.py": "import time\nprint(\"dzjjy-e2e-python\", flush=True)\ntime.sleep(120)\n",
			},
			command:    []string{"python", "app.py"},
			marker:     "dzjjy-e2e-python",
			logTimeout: 2 * time.Minute,
		},
		{
			name: "node", runtimeType: "node", version: "22",
			files: map[string]string{
				"app.js": "console.log(\"dzjjy-e2e-node\");\nsetInterval(() => {}, 1000);\n",
			},
			command:    []string{"node", "app.js"},
			marker:     "dzjjy-e2e-node",
			logTimeout: 2 * time.Minute,
		},
		{
			name: "go", runtimeType: "go", version: "1.24",
			files: map[string]string{
				"go.mod":  "module e2e\n\ngo 1.24\n",
				"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"time\"\n)\n\nfunc main() {\n\tfmt.Println(\"dzjjy-e2e-go\")\n\ttime.Sleep(120 * time.Second)\n}\n",
			},
			command:    []string{"go", "run", "main.go"},
			marker:     "dzjjy-e2e-go",
			logTimeout: 4 * time.Minute, // golang template compiles the program on first run
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			appName := "e2e-" + testCase.name
			artifactDir := writeArtifact(t, testCase.files)
			ctx := context.Background()

			engine := NewEngine()
			containerID, err := engine.Start(ctx, appName, 1, testCase.runtimeType, testCase.version, artifactDir, testCase.command)
			require.NoError(t, err)
			require.NotEmpty(t, containerID)
			t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", containerID).Run() }) // #nosec G204 - container ID from the engine

			eventuallyDocker(t, 30*time.Second, "container to be running", func() bool {
				return dockerInspect(t, containerID, "{{.State.Status}}") == "running"
			})
			verifyManagedContainer(t, containerID, appName, artifactDir)
			waitForLogMarker(t, containerID, testCase.marker, testCase.logTimeout)

			require.NoError(t, engine.StopAndRemove(ctx, containerID, 10))
			eventuallyDocker(t, 30*time.Second, "container to be removed", func() bool {
				return containerGone(t, containerID)
			})
		})
	}
}

// TestE2EStopTimeoutFallsBackToKill starts a container that ignores SIGTERM
// and verifies stop honours the timeout, falls back to SIGKILL, and removes
// the container.
func TestE2EStopTimeoutFallsBackToKill(t *testing.T) {
	requireLinuxDockerDaemon(t)

	artifactDir := writeArtifact(t, nil)
	command := []string{"python", "-c",
		"import signal, time\n" +
			"signal.signal(signal.SIGTERM, signal.SIG_IGN)\n" +
			"print(\"dzjjy-e2e-stubborn\", flush=True)\n" +
			"time.sleep(300)\n"}

	engine := NewEngine()
	containerID, err := engine.Start(context.Background(), "e2e-stubborn", 1, "python", "3.12", artifactDir, command)
	require.NoError(t, err)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", containerID).Run() }) // #nosec G204 - container ID from the engine

	waitForLogMarker(t, containerID, "dzjjy-e2e-stubborn", 2*time.Minute)

	require.NoError(t, engine.StopAndRemove(context.Background(), containerID, 2))
	eventuallyDocker(t, 30*time.Second, "stubborn container to be killed and removed", func() bool {
		return containerGone(t, containerID)
	})
}

// TestE2ERefusesUnlabelledContainer verifies the ownership guard: containers
// without dzjjy management labels are never stopped or removed by the engine.
func TestE2ERefusesUnlabelledContainer(t *testing.T) {
	requireLinuxDockerDaemon(t)

	dockerCLI(t, "pull", "python:3.12-slim")
	containerID := strings.TrimSpace(dockerCLI(t, "create", "python:3.12-slim",
		"python", "-c", "import time; time.sleep(300)"))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", containerID).Run() }) // #nosec G204 - container ID created here
	dockerCLI(t, "start", containerID)

	engine := NewEngine()
	err := engine.StopAndRemove(context.Background(), containerID, 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to manage unlabelled container")

	require.Equal(t, "running", dockerInspect(t, containerID, "{{.State.Status}}"),
		"unlabelled container must be left untouched")
}

// TestE2EStartExistingRestoresStoppedContainer covers the server-restart
// recovery path: a stopped managed container is started again in place.
func TestE2EStartExistingRestoresStoppedContainer(t *testing.T) {
	requireLinuxDockerDaemon(t)

	artifactDir := writeArtifact(t, map[string]string{
		"app.py": "import time\nprint(\"dzjjy-e2e-restore\", flush=True)\ntime.sleep(300)\n",
	})
	engine := NewEngine()
	containerID, err := engine.Start(context.Background(), "e2e-restore", 1, "python", "3.12", artifactDir, []string{"python", "app.py"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", containerID).Run() }) // #nosec G204 - container ID from the engine

	waitForLogMarker(t, containerID, "dzjjy-e2e-restore", 2*time.Minute)

	// 进程外停止（模拟服务停机），容器进入 exited。
	dockerCLI(t, "stop", "--time", "2", containerID)
	eventuallyDocker(t, 30*time.Second, "container to be externally stopped", func() bool {
		return dockerInspect(t, containerID, "{{.State.Status}}") == "exited"
	})

	require.NoError(t, engine.StartExisting(context.Background(), containerID))
	eventuallyDocker(t, 30*time.Second, "container to be running again", func() bool {
		return dockerInspect(t, containerID, "{{.State.Status}}") == "running"
	})

	require.NoError(t, engine.StopAndRemove(context.Background(), containerID, 5))
	eventuallyDocker(t, 30*time.Second, "container to be removed", func() bool {
		return containerGone(t, containerID)
	})
}
