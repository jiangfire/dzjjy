package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/secretstore"
	"github.com/jiangfire/dzjjy/internal/server/state"
)

// phase3Handler builds a handler wired with a real state store so the Phase 3
// transitions, including persistence, can be exercised end to end.
func phase3Handler(t *testing.T) *Handler {
	t.Helper()
	root := t.TempDir()
	h := NewHandlerWithState(
		filepath.Join(root, "uploads"),
		filepath.Join(root, "workspace"),
		filepath.Join(root, "logs"),
		filepath.Join(root, "state.json"),
	)
	h.resuming = map[string]bool{}
	t.Cleanup(func() { _ = h.Shutdown() })
	return h
}

func TestBuildDockerOptionsResolvesSecretsFromStore(t *testing.T) {
	h := phase3Handler(t)
	h.SetSecrets(secretstore.New(map[string]string{"web/api-key": "s3cret"}))

	document := &appconfig.Document{}
	document.Spec.Sandbox = &appconfig.Sandbox{Type: "docker"}
	document.Spec.Secrets = []appconfig.Secret{{Key: "web/api-key", Env: "API_KEY"}}
	document.Spec.Env = []appconfig.EnvVar{{Name: "MODE", Value: "prod"}}
	document.Spec.Resources.Memory = "256m"
	document.Spec.Resources.CPUs = "1.5"
	document.Spec.Resources.Pids = intPtr(64)
	document.Spec.Network = &appconfig.Network{Ports: []appconfig.PortMapping{{Container: 8000}}}
	document.Spec.Volumes = []appconfig.Volume{{Name: "cache", Path: "/cache"}}

	options, err := h.buildDockerOptions(document)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", options.SecretEnv["API_KEY"])
	assert.Equal(t, "prod", options.Env["MODE"])
	assert.Equal(t, int64(268435456), options.Resources.Memory)
	assert.Equal(t, 1.5, options.Resources.CPUs)
	assert.Equal(t, 64, options.Resources.Pids)
	require.Len(t, options.Ports, 1)
	assert.Equal(t, 8000, options.Ports[0].Container)
	require.Len(t, options.Volumes, 1)
	assert.Equal(t, "/cache", options.Volumes[0].Path)
}

func TestBuildDockerOptionsFailsWhenSecretMissing(t *testing.T) {
	h := phase3Handler(t)
	h.SetSecrets(secretstore.New(nil))

	document := &appconfig.Document{}
	document.Spec.Sandbox = &appconfig.Sandbox{Type: "docker"}
	document.Spec.Secrets = []appconfig.Secret{{Key: "web/api-key", Env: "API_KEY"}}

	_, err := h.buildDockerOptions(document)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not present in the platform secret store")
}

func TestBuildDockerOptionsDerivesEnvNameFromKey(t *testing.T) {
	h := phase3Handler(t)
	h.SetSecrets(secretstore.New(map[string]string{"web/api-key": "value"}))

	document := &appconfig.Document{}
	document.Spec.Sandbox = &appconfig.Sandbox{Type: "docker"}
	document.Spec.Secrets = []appconfig.Secret{{Key: "web/api-key"}}

	options, err := h.buildDockerOptions(document)
	require.NoError(t, err)
	assert.Equal(t, "value", options.SecretEnv["web_api_key"])
}

func TestIdleTimeoutParsing(t *testing.T) {
	document := &appconfig.Document{}
	assert.Equal(t, time.Duration(0), idleTimeout(document))

	document.Spec.Lifecycle.IdleTimeout = "90s"
	assert.Equal(t, 90*time.Second, idleTimeout(document))

	document.Spec.Lifecycle.IdleTimeout = "not-a-duration"
	assert.Equal(t, time.Duration(0), idleTimeout(document))
}

func TestSuspendAndResumeStaticSite(t *testing.T) {
	h := phase3Handler(t)
	// Create a site artifact on disk to resume from.
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>hi</h1>"), 0600))

	h.syncManager.ReplaceApp("web", &state.AppState{
		Backend: "static", Generation: 2, Status: state.StatusRunning,
		WorkPath: root, IdleTimeoutSeconds: 60,
	})
	// apply and restore both populate this registry; the idle transition is
	// driven from it rather than from state alone.
	h.rememberIdleApp("web", idleAppRecord{
		generation: 2, backend: "static", workPath: root, timeout: time.Minute,
	})

	require.NoError(t, h.suspendIdleApp("web", 2))
	app := h.syncManager.GetState().Apps["web"]
	require.NotNil(t, app)
	assert.True(t, app.Suspended)
	assert.Equal(t, state.StatusStopped, app.Status)
	assert.Equal(t, "idle", app.TerminationReason)

	require.NoError(t, h.resumeSuspendedApp("web", 2))
	app = h.syncManager.GetState().Apps["web"]
	require.NotNil(t, app)
	assert.False(t, app.Suspended)
	assert.Equal(t, state.StatusRunning, app.Status)
	assert.NotZero(t, app.Port, "a resumed static site must listen again")
}

func TestSuspendIsIgnoredForStaleGeneration(t *testing.T) {
	h := phase3Handler(t)
	root := t.TempDir()
	h.syncManager.ReplaceApp("web", &state.AppState{
		Backend: "static", Generation: 5, Status: state.StatusRunning, WorkPath: root,
	})
	h.rememberIdleApp("web", idleAppRecord{generation: 5, backend: "static", workPath: root})
	require.NoError(t, h.suspendIdleApp("web", 1))
	app := h.syncManager.GetState().Apps["web"]
	require.NotNil(t, app)
	assert.False(t, app.Suspended, "a stale idle callback must not suspend the current generation")
	assert.Equal(t, state.StatusRunning, app.Status)
}

func TestSuspendIsIgnoredForTerminalState(t *testing.T) {
	h := phase3Handler(t)
	root := t.TempDir()
	h.syncManager.ReplaceApp("web", &state.AppState{
		Backend: "static", Generation: 1, Status: state.StatusExpired, WorkPath: root,
	})
	h.rememberIdleApp("web", idleAppRecord{generation: 1, backend: "static", workPath: root})
	require.NoError(t, h.suspendIdleApp("web", 1))
	app := h.syncManager.GetState().Apps["web"]
	require.NotNil(t, app)
	assert.Equal(t, state.StatusExpired, app.Status)
}

func TestResumeFailsWhenArtifactIsGone(t *testing.T) {
	h := phase3Handler(t)
	h.syncManager.ReplaceApp("web", &state.AppState{
		Backend: "static", Generation: 1, Status: state.StatusStopped, Suspended: true,
		WorkPath: filepath.Join(t.TempDir(), "removed"),
	})
	h.rememberIdleApp("web", idleAppRecord{
		generation: 1, backend: "static", workPath: filepath.Join(t.TempDir(), "removed"), suspended: true,
	})
	err := h.resumeSuspendedApp("web", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact")
}

func TestResumeGeneratesTokenWhenSiteIsExposed(t *testing.T) {
	h := phase3Handler(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>hi</h1>"), 0600))
	h.syncManager.ReplaceApp("web", &state.AppState{
		Backend: "static", Generation: 1, Status: state.StatusStopped, Suspended: true,
		WorkPath: root, ExposureToken: "/_dzjjy/0123456789abcdef0123456789abcdef",
	})
	h.rememberIdleApp("web", idleAppRecord{
		generation: 1, backend: "static", workPath: root,
		token: "0123456789abcdef0123456789abcdef", suspended: true,
	})

	require.NoError(t, h.resumeSuspendedApp("web", 1))
	server, err := h.ensureProxy()
	require.NoError(t, err)
	_, found := server.Lookup("0123456789abcdef0123456789abcdef")
	assert.True(t, found, "a resumed exposed site must be re-registered with the proxy")
}

func TestExposureTokenIsGeneratedWhenNotPinned(t *testing.T) {
	h := phase3Handler(t)
	document := &appconfig.Document{}
	document.Spec.Exposure = &appconfig.Exposure{}

	token, err := h.exposureToken("web", document, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(token), 16)

	// An existing token is preserved so the public URL stays stable.
	again, err := h.exposureToken("web", document, token)
	require.NoError(t, err)
	assert.Equal(t, token, again)

	// A pinned token always wins.
	document.Spec.Exposure.Token = "pinned-token-value-0123456789ab"
	pinned, err := h.exposureToken("web", document, token)
	require.NoError(t, err)
	assert.Equal(t, "pinned-token-value-0123456789ab", pinned)
}

func TestTouchIdleAppOnlyActsWhenConfigured(t *testing.T) {
	h := phase3Handler(t)
	// No app state: touch is a no-op and must not panic.
	h.touchIdleApp("missing", 1)

	h.syncManager.ReplaceApp("web", &state.AppState{Backend: "static", IdleTimeoutSeconds: 30})
	h.touchIdleApp("web", 1)
}

func TestIdleTimeoutForReadsPersistedSeconds(t *testing.T) {
	h := phase3Handler(t)
	assert.Equal(t, time.Duration(0), h.idleTimeoutFor("missing"))
	h.syncManager.ReplaceApp("web", &state.AppState{IdleTimeoutSeconds: 45})
	assert.Equal(t, 45*time.Second, h.idleTimeoutFor("web"))
}

func intPtr(v int) *int { return &v }
