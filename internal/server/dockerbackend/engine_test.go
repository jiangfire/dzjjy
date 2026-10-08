package dockerbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	calls   [][]string
	osType  string
	stopErr error
	labels  string
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	switch args[0] {
	case "info":
		return []byte(f.osType), nil
	case "create":
		return []byte("container-id"), nil
	case "inspect":
		return []byte(f.labels), nil
	case "stop":
		return nil, f.stopErr
	default:
		return nil, nil
	}
}

func TestResolveOnlyAcceptsMaintainedTemplatesAndPlatforms(t *testing.T) {
	template, err := Resolve("python", "3.12", "linux")
	require.NoError(t, err)
	require.Equal(t, "python:3.12-slim", template.Image)
	_, err = Resolve("python", "latest", "linux")
	require.Error(t, err)
	_, err = Resolve("node", "22", "windows")
	require.Error(t, err)
}

func TestStartUsesManagedReadOnlyArtifactMountAndLabels(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{osType: "linux"}
	engine := NewEngineWithRunner(runner)
	id, err := engine.Start(context.Background(), "demo", 4, "node", "22", root, []string{"node", "app.js", "hello world"})
	require.NoError(t, err)
	require.Equal(t, "container-id", id)
	require.Len(t, runner.calls, 4)
	create := strings.Join(runner.calls[2], " ")
	require.Contains(t, create, "dzjjy.managed=true")
	require.Contains(t, create, "dzjjy.app=demo")
	require.Contains(t, create, "dzjjy.generation=4")
	require.Contains(t, create, "target=/workspace,readonly")
	require.Contains(t, create, "node:22-alpine")
	require.NotContains(t, create, "docker.sock")
}

func TestDockerUnavailableAndUnsupportedDaemonFailExplicitly(t *testing.T) {
	unavailable := NewEngineWithRunner(errorRunner{})
	_, err := unavailable.Start(context.Background(), "demo", 1, "python", "3.12", t.TempDir(), []string{"python", "app.py"})
	require.ErrorContains(t, err, "Docker Engine unavailable")
	engine := NewEngineWithRunner(&fakeRunner{osType: "windows"})
	_, err = engine.Start(context.Background(), "demo", 1, "python", "3.12", t.TempDir(), []string{"python", "app.py"})
	require.ErrorContains(t, err, "does not support Docker OS")
}

type errorRunner struct{}

func (errorRunner) Run(context.Context, ...string) ([]byte, error) {
	return nil, errors.New("not installed")
}

func TestStopTimeoutKillsThenRemovesOnlyManagedContainer(t *testing.T) {
	runner := &fakeRunner{labels: "true", stopErr: errors.New("timeout")}
	engine := NewEngineWithRunner(runner)
	require.NoError(t, engine.StopAndRemove(context.Background(), "container-id", 10))
	require.Equal(t, []string{"inspect", "stop", "kill", "rm"}, []string{runner.calls[0][0], runner.calls[1][0], runner.calls[2][0], runner.calls[3][0]})

	unlabelled := NewEngineWithRunner(&fakeRunner{labels: ""})
	require.ErrorContains(t, unlabelled.StopAndRemove(context.Background(), "other-container", 10), "unlabelled")
}

func TestStartRejectsMountDelimiterInArtifactPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "has,comma")
	require.NoError(t, os.MkdirAll(root, 0o700))
	runner := &fakeRunner{osType: "linux"}
	_, err := NewEngineWithRunner(runner).Start(context.Background(), "app", 1, "go", "1.24", root, []string{"go", "run", "."})
	require.ErrorContains(t, err, "mount delimiter")
	require.Empty(t, runner.calls)
}
