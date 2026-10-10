//go:build docker_e2e

package dockerbackend_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/dockerbackend"
)

// requireRunsc skips the test unless the host has a registered runsc runtime.
// A missing isolator is reported as a skip rather than a pass so CI cannot
// silently claim gVisor coverage it did not actually run.
func requireRunsc(t *testing.T) {
	t.Helper()
	out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").CombinedOutput()
	if err != nil {
		t.Skipf("docker daemon unavailable: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "runsc") {
		t.Skip("the runsc runtime is not registered with this docker daemon")
	}
}

// TestE2EIsolatorLifecycle starts a container under runsc and drives the full
// lifecycle, proving the isolator is not merely accepted by the CLI flags.
func TestE2EIsolatorLifecycle(t *testing.T) {
	requireRunsc(t)

	engine := dockerbackend.NewEngine()
	ctx := context.Background()
	artifact := t.TempDir()

	containerID, _, err := engine.StartWithOptions(ctx, "gvisor-e2e", 1, "python", "3.12", artifact,
		[]string{"python", "-c", "import time\nfor _ in range(600):\n    time.sleep(1)\n"},
		dockerbackend.Options{Isolator: dockerbackend.IsolatorRuntime})
	require.NoError(t, err)
	require.NotEmpty(t, containerID)
	t.Cleanup(func() {
		_ = engine.StopAndRemove(context.Background(), containerID, 5)
		_ = os.RemoveAll(artifact)
	})

	// The container must actually be running under runsc, not just created.
	inspect, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.Runtime}}", containerID).CombinedOutput()
	require.NoError(t, err, "inspect output: %s", string(inspect))
	assert.Equal(t, dockerbackend.IsolatorRuntime, strings.TrimSpace(string(inspect)),
		"container must run under the runsc runtime")

	// Pause / unpause drives the idle-suspend path end to end.
	require.NoError(t, engine.Pause(ctx, containerID))
	require.NoError(t, engine.Unpause(ctx, containerID))

	require.NoError(t, engine.StopAndRemove(ctx, containerID, 5))
}

// TestE2EIsolatorWithSecretsVolumesAndLimits exercises the Phase 3 option set
// together under runsc: secret env injection, a named volume, and a memory
// limit that the container must respect.
func TestE2EIsolatorWithSecretsVolumesAndLimits(t *testing.T) {
	requireRunsc(t)

	engine := dockerbackend.NewEngine()
	ctx := context.Background()
	artifact := t.TempDir()

	containerID, bindings, err := engine.StartWithOptions(ctx, "gvisor-full", 1, "python", "3.12", artifact,
		[]string{"python", "-c", "import time\nfor _ in range(600):\n    time.sleep(1)\n"},
		dockerbackend.Options{
			Isolator:  dockerbackend.IsolatorRuntime,
			SecretEnv: map[string]string{"DZJJY_TEST_SECRET": "injected-value"},
			Env:       map[string]string{"MODE": "e2e"},
			Volumes:   []dockerbackend.VolumeMount{{Name: "dzjjy-e2e-cache", Path: "/cache"}},
			Ports:     []dockerbackend.PortBinding{{Container: 8080}},
			Resources: dockerbackend.ResourceLimits{Memory: 256 << 20, Pids: 128},
		})
	require.NoError(t, err)
	require.NotEmpty(t, containerID)
	require.NotEmpty(t, bindings, "a published port must be resolved from the running container")
	t.Cleanup(func() {
		_ = engine.StopAndRemove(context.Background(), containerID, 5)
		_ = exec.Command("docker", "volume", "rm", "-f", "dzjjy-e2e-cache").Run()
		_ = os.RemoveAll(artifact)
	})

	// Confirm the secret reached the container without ever being persisted.
	logs, err := exec.Command("docker", "exec", containerID, "python", "-c",
		"import os;print(os.environ.get('DZJJY_TEST_SECRET',''),os.environ.get('MODE',''))").CombinedOutput()
	require.NoError(t, err, "exec output: %s", string(logs))
	assert.Contains(t, string(logs), "injected-value")
	assert.Contains(t, string(logs), "e2e")

	// The memory limit must be recorded on the container.
	limit, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.Memory}}", containerID).CombinedOutput()
	require.NoError(t, err, "inspect output: %s", string(limit))
	assert.Equal(t, "268435456", strings.TrimSpace(string(limit)))

	// The named volume must be mounted at the requested path.
	mounts, err := exec.Command("docker", "inspect", "--format", "{{range .Mounts}}{{.Destination}} {{end}}", containerID).CombinedOutput()
	require.NoError(t, err, "inspect output: %s", string(mounts))
	assert.Contains(t, string(mounts), "/cache")

	require.NoError(t, engine.StopAndRemove(ctx, containerID, 5))
}

// TestE2EIsolatorUnavailableFailsClosed verifies that an unsupported isolator
// stops the apply instead of degrading to the default runtime.
func TestE2EIsolatorUnavailableFailsClosed(t *testing.T) {
	engine := dockerbackend.NewEngine()
	ctx := context.Background()

	err := engine.SupportsIsolator(ctx, "definitely-not-a-runtime")
	require.Error(t, err)

	out, dockerErr := exec.Command("docker", "info", "--format", "{{.OSType}}").CombinedOutput()
	if dockerErr != nil {
		t.Skip("docker daemon unavailable")
	}
	if strings.TrimSpace(string(out)) != "linux" {
		t.Skip("gVisor requires a Linux daemon")
	}

	_, _, err = engine.StartWithOptions(ctx, "gvisor-missing", 1, "python", "3.12", t.TempDir(),
		[]string{"python", "-c", "pass"}, dockerbackend.Options{Isolator: "definitely-not-a-runtime"})
	require.Error(t, err)
}

// TestE2EResourceLimitIsEnforced starts a container with a tight memory limit
// and confirms the daemon accepts and applies it.
func TestE2EResourceLimitIsEnforced(t *testing.T) {
	out, err := exec.Command("docker", "info", "--format", "{{.OSType}}").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "linux" {
		t.Skip("this test requires a Linux docker daemon")
	}

	engine := dockerbackend.NewEngine()
	artifact := t.TempDir()
	containerID, _, err := engine.StartWithOptions(context.Background(), "limits-e2e", 1, "python", "3.12", artifact,
		[]string{"python", "-c", "import time\nfor _ in range(300):\n    time.sleep(1)\n"},
		dockerbackend.Options{
			Resources: dockerbackend.ResourceLimits{Memory: 128 << 20, CPUs: 0.5, Pids: 64},
		})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = engine.StopAndRemove(context.Background(), containerID, 5)
		_ = os.RemoveAll(artifact)
	})

	inspect, err := exec.Command("docker", "inspect", "--format",
		"{{.HostConfig.Memory}} {{.HostConfig.NanoCpus}} {{.HostConfig.PidsLimit}}", containerID).CombinedOutput()
	require.NoError(t, err, "inspect output: %s", string(inspect))
	fields := strings.Fields(string(inspect))
	require.Len(t, fields, 3)
	assert.Equal(t, "134217728", fields[0])
	assert.Equal(t, "500000000", fields[1])
	assert.Equal(t, "64", fields[2])
}
