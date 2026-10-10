package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyExposedSite applies a StaticSite with spec.exposure and returns the
// public path the server handed back.
func applyExposedSite(t *testing.T, h *Handler, name string, extraSpec string) string {
	t.Helper()
	config := fmt.Sprintf(
		"apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: %s}\nspec:\n  artifact: site.zip\n  port: 0\n%s",
		name, extraSpec)
	recorder := httptest.NewRecorder()
	h.Apply(recorder, newApplyRequest(t, config, "site.zip", makeSiteZip(t)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		Data struct {
			PublicPath string `json:"public_path"`
			Port       int    `json:"port"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.PublicPath, "an exposed site must report its public path")
	require.NotZero(t, response.Data.Port)
	return response.Data.PublicPath
}

// tokenOf strips the /_dzjjy/ prefix and trailing slash from a public path.
func tokenOf(publicPath string) string {
	return publicPath[len("/_dzjjy/") : len(publicPath)-1]
}

// TestExposedSiteIsServedThroughTokenPath is the end-to-end contract of
// spec.exposure: apply publishes the site under an unguessable token path and
// a real HTTP request to that path returns the site content.
func TestExposedSiteIsServedThroughTokenPath(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { _ = h.Shutdown() })

	publicPath := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	assert.Contains(t, publicPath, "/_dzjjy/")

	server, err := h.ensureProxy()
	require.NoError(t, err)

	target := fmt.Sprintf("http://127.0.0.1:%d/_dzjjy/%s/index.html", server.Port(), tokenOf(publicPath)) // #nosec G107 -- loopback proxy built by the test
	response, err := http.Get(target)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "hello site", string(body))
}

// TestExposedSiteRejectsWrongToken proves the token is the access control: a
// different token must not serve the site.
func TestExposedSiteRejectsWrongToken(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { _ = h.Shutdown() })

	applyExposedSite(t, h, "docs", "  exposure: {}\n")
	server, err := h.ensureProxy()
	require.NoError(t, err)

	target := fmt.Sprintf("http://127.0.0.1:%d/_dzjjy/%s/index.html", // #nosec G107 -- loopback proxy built by the test
		server.Port(), "ffffffffffffffffffffffffffffffff")
	response, err := http.Get(target)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
}

// TestExposedSiteWorksWithoutStatePersistence guards the apply path against a
// nil sync manager: NewHandler installs no state store, and reading app state
// while reusing a token must not dereference it.
func TestExposedSiteWorksWithoutStatePersistence(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { _ = h.Shutdown() })
	require.Nil(t, h.syncManager)

	// Applies twice so both the fresh-token and the reuse-token branches run.
	first := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	second := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
}

// TestExposedSiteKeepsTokenStableAcrossReapply proves the public URL does not
// rotate when the same site is re-applied.
func TestExposedSiteKeepsTokenStableAcrossReapply(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	t.Cleanup(func() { _ = h.Shutdown() })

	first := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	second := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	assert.Equal(t, first, second, "re-applying must not invalidate an already published URL")
}

// TestPinnedExposureTokenIsHonoured verifies spec.exposure.token wins over the
// generated token so an operator can hand out a stable URL.
func TestPinnedExposureTokenIsHonoured(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { _ = h.Shutdown() })

	const pinned = "0123456789abcdef0123456789abcdef"
	publicPath := applyExposedSite(t, h, "docs", "  exposure: {token: "+pinned+"}\n")
	assert.Equal(t, "/_dzjjy/"+pinned+"/", publicPath)

	server, err := h.ensureProxy()
	require.NoError(t, err)
	_, found := server.Lookup(pinned)
	assert.True(t, found)
}

// TestIdleSuspendedSiteResumesOnRequest is the idle-suspend contract end to
// end: after the app is suspended, a request through the token path revives it
// and still serves content.
func TestIdleSuspendedSiteResumesOnRequest(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { _ = h.Shutdown() })

	publicPath := applyExposedSite(t, h, "docs", "  exposure: {}\n  lifecycle: {idleTimeout: 1h}\n")
	server, err := h.ensureProxy()
	require.NoError(t, err)
	target := fmt.Sprintf("http://127.0.0.1:%d/_dzjjy/%s/index.html", server.Port(), tokenOf(publicPath)) // #nosec G107 -- loopback proxy built by the test

	get := func() (int, string) {
		response, err := http.Get(target) // #nosec G107 -- loopback proxy built by the test
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return response.StatusCode, string(body)
	}

	status, body := get()
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "hello site", body)

	// Suspend it exactly the way the idle tracker does.
	require.NoError(t, h.suspendIdleApp("docs", 1))
	_, suspended := h.sites["docs"]
	assert.False(t, suspended, "a suspended site must no longer hold a listener")

	// The next request must revive the site rather than fail.
	status, body = get()
	assert.Equal(t, http.StatusOK, status, "an idle-suspended site must resume on request")
	assert.Equal(t, "hello site", body)
}

// TestExposureTokenSurvivesRestart proves a restarted server republishes the
// same token, so a shared URL keeps working across deploys.
func TestExposureTokenSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "work")
	logDir := filepath.Join(root, "logs")
	stateFile := filepath.Join(root, "state.json")

	h := NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	publicPath := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	require.NoError(t, h.Shutdown())

	// A fresh handler restores from the same state file.
	h2 := NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	t.Cleanup(func() { _ = h2.Shutdown() })
	require.NoError(t, h2.RestoreState())

	app := h2.syncManager.GetState().Apps["docs"]
	require.NotNil(t, app)
	assert.Equal(t, publicPath[:len(publicPath)-1], app.ExposureToken)

	h2.proxyMu.Lock()
	proxyServer := h2.proxyServer
	h2.proxyMu.Unlock()
	require.NotNil(t, proxyServer, "restore must republish the token with the proxy")
	_, found := proxyServer.Lookup(tokenOf(publicPath))
	assert.True(t, found, "a restored site must be reachable at the same public path")
}

// TestIdleWindowIsRebuiltAfterRestart checks the idle schedule survives a
// restart, otherwise a restarted app would never suspend again.
func TestIdleWindowIsRebuiltAfterRestart(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "work")
	logDir := filepath.Join(root, "logs")
	stateFile := filepath.Join(root, "state.json")

	h := NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	applyExposedSite(t, h, "docs", "  exposure: {}\n  lifecycle: {idleTimeout: 1h}\n")
	require.NoError(t, h.Shutdown())

	h2 := NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	t.Cleanup(func() { _ = h2.Shutdown() })
	require.NoError(t, h2.RestoreState())

	app := h2.syncManager.GetState().Apps["docs"]
	require.NotNil(t, app)
	assert.Equal(t, int(time.Hour.Seconds()), app.IdleTimeoutSeconds)
	assert.Greater(t, h2.idleTimeoutFor("docs"), time.Duration(0))
}

// TestStopRemovesExposure proves a stopped site stops being publicly reachable.
func TestStopRemovesExposure(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	t.Cleanup(func() { _ = h.Shutdown() })

	publicPath := applyExposedSite(t, h, "docs", "  exposure: {}\n")
	server, err := h.ensureProxy()
	require.NoError(t, err)

	_, ok := server.Lookup(tokenOf(publicPath))
	require.True(t, ok)

	stopRecorder := httptest.NewRecorder()
	h.Stop(stopRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/stop", nil))
	require.Equal(t, http.StatusOK, stopRecorder.Code, stopRecorder.Body.String())

	_, ok = server.Lookup(tokenOf(publicPath))
	assert.False(t, ok, "a stopped site must no longer be published")
}
