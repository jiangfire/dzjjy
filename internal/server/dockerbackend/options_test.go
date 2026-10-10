package dockerbackend_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/dockerbackend"
)

// fakeRunner records the docker invocations and replays canned output.
type fakeRunner struct {
	mu        sync.Mutex
	calls     [][]string
	responses map[string]string
	failOn    map[string]string
	// osType answers "docker info --format {{.OSType}}" and runtimes answers
	// the "{{json .Runtimes}}" probe used for isolator detection.
	osType   string
	runtimes string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		responses: map[string]string{},
		failOn:    map[string]string{},
		osType:    "linux",
		runtimes:  `{"runc":{"path":"runc"}}`,
	}
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
	if len(args) > 0 {
		if message, ok := f.failOn[args[0]]; ok {
			return []byte(message), context.DeadlineExceeded
		}
		// "docker info" is queried with two different --format templates, so
		// the reply depends on the requested field rather than the subcommand.
		if args[0] == "info" {
			for i, arg := range args {
				if !strings.HasPrefix(arg, "--format") || i+1 >= len(args) {
					continue
				}
				if strings.Contains(args[i+1], "Runtimes") {
					return []byte(f.runtimes), nil
				}
			}
			return []byte(f.osType), nil
		}
		if response, ok := f.responses[args[0]]; ok {
			return []byte(response), nil
		}
	}
	if len(args) > 0 && args[0] == "create" {
		return []byte("container-abc123\n"), nil
	}
	return []byte{}, nil
}

func (f *fakeRunner) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var builder strings.Builder
	for _, call := range f.calls {
		builder.WriteString(strings.Join(call, " "))
		builder.WriteString("\n")
	}
	return builder.String()
}

func (f *fakeRunner) hasPrefix(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if len(call) > 0 && call[0] == prefix {
			return true
		}
	}
	return false
}

func stagingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	return abs
}

func TestStartWithOptionsAppliesPortsVolumesAndResources(t *testing.T) {
	runner := newFakeRunner()
	runner.osType = "linux"
	runner.responses["port"] = "8000/tcp -> 0.0.0.0:49153\n8000/tcp -> [::]:49153\n"
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, bindings, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"},
		dockerbackend.Options{
			Env: map[string]string{"MODE": "prod"},
			Volumes: []dockerbackend.VolumeMount{
				{Name: "cache", Path: "/cache"},
			},
			Ports: []dockerbackend.PortBinding{{Container: 8000}},
			Resources: dockerbackend.ResourceLimits{
				Memory: 268435456,
				CPUs:   1.5,
				Pids:   128,
			},
		})
	require.NoError(t, err)

	log := runner.joined()
	assert.Contains(t, log, "--mount type=volume,source=cache,target=/cache")
	assert.Contains(t, log, "-p 8000")
	assert.Contains(t, log, "-e MODE=prod")
	assert.Contains(t, log, "--memory 268435456")
	assert.Contains(t, log, "--cpus 1.5")
	assert.Contains(t, log, "--pids-limit 128")
	assert.Contains(t, log, "--workdir /workspace")
	assert.Contains(t, log, "readonly")
	assert.True(t, runner.hasPrefix("volume"), "named volumes must be created before the container")
	require.NotEmpty(t, bindings)
}

func TestStartWithOptionsInjectsSecretEnvironment(t *testing.T) {
	runner := newFakeRunner()
	runner.osType = "linux"
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"},
		dockerbackend.Options{
			SecretEnv: map[string]string{"API_KEY": "s3cret-value"},
		})
	require.NoError(t, err)
	assert.Contains(t, runner.joined(), "-e API_KEY=s3cret-value")
}

func TestStartWithOptionsRejectsUnsupportedIsolator(t *testing.T) {
	runner := newFakeRunner()
	runner.osType = "linux"
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"}, dockerbackend.Options{Isolator: "firejail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported isolator")
}

func TestStartWithOptionsFailsWhenIsolatorMissing(t *testing.T) {
	runner := newFakeRunner()
	// The daemon reports runtimes but runsc is not registered.
	runner.runtimes = `{"runc":{"path":"runc"}}`
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"}, dockerbackend.Options{Isolator: dockerbackend.IsolatorRuntime})
	require.Error(t, err)

	var unavailable *dockerbackend.ErrIsolatorUnavailable
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, dockerbackend.IsolatorRuntime, unavailable.Isolator)
	// The container must not be created when the isolator is unavailable.
	assert.False(t, runner.hasPrefix("create"), "no container may be created when runsc is unavailable")
}

func TestStartWithOptionsUsesRunscRuntimeWhenAvailable(t *testing.T) {
	runner := newFakeRunner()
	runner.runtimes = `{"runc":{"path":"runc"},"runsc":{"path":"runsc"}}`
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"}, dockerbackend.Options{Isolator: dockerbackend.IsolatorRuntime})
	require.NoError(t, err)
	assert.Contains(t, runner.joined(), "--runtime runsc")
}

func TestSupportsIsolatorAcceptsEmpty(t *testing.T) {
	engine := dockerbackend.NewEngineWithRunner(newFakeRunner())
	require.NoError(t, engine.SupportsIsolator(context.Background(), ""))
}

func TestStartWithOptionsRejectsVolumeDelimiterInjection(t *testing.T) {
	runner := newFakeRunner()
	runner.osType = "linux"
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"},
		dockerbackend.Options{Volumes: []dockerbackend.VolumeMount{{Name: "bad,name", Path: "/data"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Docker delimiter")
}

func TestStartWithOptionsRemovesContainerWhenPortResolutionFails(t *testing.T) {
	runner := newFakeRunner()
	runner.osType = "linux"
	runner.failOn["port"] = "no such container"
	engine := dockerbackend.NewEngineWithRunner(runner)

	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", stagingDir(t),
		[]string{"python", "app.py"},
		dockerbackend.Options{Ports: []dockerbackend.PortBinding{{Container: 8000, Host: 18000}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve published ports")
	assert.True(t, runner.hasPrefix("rm"), "a failed apply must clean up the container")
}

func TestStartWithOptionsRequiresExistingArtifactDir(t *testing.T) {
	engine := dockerbackend.NewEngineWithRunner(newFakeRunner())
	missing := filepath.Join(os.TempDir(), "dzjjy-missing-artifact-dir")
	_, _, err := engine.StartWithOptions(context.Background(), "demo", 1, "python", "3.12", missing,
		[]string{"python", "app.py"}, dockerbackend.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact directory is unavailable")
}
