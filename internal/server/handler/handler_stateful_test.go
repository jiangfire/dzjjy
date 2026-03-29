package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/handler"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/pkg/api"
)

func TestDeploy_InvalidMaxRestartsRejected(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "work")
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	h := handler.NewHandler(uploadDir, workDir, logDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/demo/deploy":
			h.Deploy(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("type", "exec"))
	require.NoError(t, writer.WriteField("executable", "demo.exe"))
	require.NoError(t, writer.WriteField("max_restarts", "1oops"))
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/apps/demo/deploy", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	result := decodeHTTPResponse(t, resp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "invalid max_restarts")
}

func TestStartRestartAndRemove_KeepStateSnapshotConsistent(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "workspace")
	logDir := filepath.Join(root, "logs")
	stateFile := filepath.Join(root, "state.json")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	h := handler.NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/demo/start":
			h.Start(w, r)
		case "/api/v1/apps/demo/status":
			h.Status(w, r)
		case "/api/v1/apps/demo/restart":
			h.Restart(w, r)
		case "/api/v1/apps/demo/remove":
			h.RemoveApp(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executable := createTestApp(t, root, "demo-live", `package main
import "time"
func main() {
	time.Sleep(30 * time.Second)
}
`)

	startAppViaAPI(t, server.URL, "demo", executable, true, 4)
	initialStatus := getStatus(t, server.URL, "demo")
	require.True(t, initialStatus.Running)
	require.Greater(t, initialStatus.PID, 0)

	postAndExpectSuccess(t, http.MethodPost, server.URL+"/api/v1/apps/demo/restart")

	require.Eventually(t, func() bool {
		status := getStatus(t, server.URL, "demo")
		return status.Running && status.PID > 0 && status.PID != initialStatus.PID
	}, 5*time.Second, 100*time.Millisecond)

	restartedStatus := getStatus(t, server.URL, "demo")
	snapshot := h.GetStateSnapshot()
	appState := snapshot["demo"]
	require.NotNil(t, appState)
	assert.Equal(t, state.StatusRunning, appState.Status)
	assert.Equal(t, restartedStatus.PID, appState.PID)
	assert.Equal(t, 1, appState.RestartCount)
	assert.NotEmpty(t, appState.LogPath)

	postAndExpectSuccess(t, http.MethodDelete, server.URL+"/api/v1/apps/demo/remove")
	assert.NotContains(t, h.GetStateSnapshot(), "demo")
}

func TestStart_ShortLivedProcessTransitionsToStoppedState(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "workspace")
	logDir := filepath.Join(root, "logs")
	stateFile := filepath.Join(root, "state.json")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	h := handler.NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/demo/start":
			h.Start(w, r)
		case "/api/v1/apps/demo/status":
			h.Status(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executable := createTestApp(t, root, "demo-short", `package main
import "time"
func main() {
	time.Sleep(200 * time.Millisecond)
}
`)

	startAppViaAPI(t, server.URL, "demo", executable, false, 0)

	require.Eventually(t, func() bool {
		status := getStatus(t, server.URL, "demo")
		if status.Running || status.PID != 0 {
			return false
		}

		appState := h.GetStateSnapshot()["demo"]
		return appState != nil && appState.Status == state.StatusStopped && appState.PID == 0
	}, 5*time.Second, 100*time.Millisecond)
}

func TestRestoreState_OnlyRestartsAutoRestartApps(t *testing.T) {
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "workspace")
	logDir := filepath.Join(root, "logs")
	stateFile := filepath.Join(root, "state.json")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	executable := createTestApp(t, root, "restore-live", `package main
import "time"
func main() {
	time.Sleep(30 * time.Second)
}
`)

	autoWorkDir := filepath.Join(workDir, "auto")
	manualWorkDir := filepath.Join(workDir, "manual")
	require.NoError(t, os.MkdirAll(autoWorkDir, 0o755))
	require.NoError(t, os.MkdirAll(manualWorkDir, 0o755))

	store := state.NewStateStore(stateFile)
	require.NoError(t, store.Persist(&state.StateData{
		Apps: map[string]*state.AppState{
			"auto": {
				Config: &state.ProcessConfig{
					Type:        "exec",
					WorkDir:     autoWorkDir,
					Executable:  executable,
					AutoRestart: true,
					MaxRestarts: 2,
				},
				Status: state.StatusStopped,
			},
			"manual": {
				Config: &state.ProcessConfig{
					Type:        "exec",
					WorkDir:     manualWorkDir,
					Executable:  executable,
					AutoRestart: false,
					MaxRestarts: 0,
				},
				Status: state.StatusStopped,
			},
		},
	}))

	h := handler.NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	require.NoError(t, h.RestoreState())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/auto/status":
			h.Status(w, r)
		case "/api/v1/apps/manual/status":
			h.Status(w, r)
		case "/api/v1/apps/auto/stop":
			h.Stop(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.Eventually(t, func() bool {
		return getStatus(t, server.URL, "auto").Running
	}, 5*time.Second, 100*time.Millisecond)

	autoStatus := getStatus(t, server.URL, "auto")
	manualStatus := getStatus(t, server.URL, "manual")
	assert.True(t, autoStatus.Running)
	assert.False(t, manualStatus.Running)

	snapshot := h.GetStateSnapshot()
	require.NotNil(t, snapshot["auto"])
	require.NotNil(t, snapshot["manual"])
	assert.Equal(t, state.StatusRunning, snapshot["auto"].Status)
	assert.Equal(t, state.StatusStopped, snapshot["manual"].Status)

	stopApp(t, server.URL, "auto")
}

func startAppViaAPI(t *testing.T, serverURL, appName, executable string, autoRestart bool, maxRestarts int) {
	t.Helper()

	body, err := json.Marshal(api.DeployRequest{
		Type:        "exec",
		Executable:  executable,
		AutoRestart: autoRestart,
		MaxRestarts: maxRestarts,
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/v1/apps/"+appName+"/start", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	result := decodeHTTPResponse(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, result.Message)
	require.True(t, result.Success, result.Message)
}

func postAndExpectSuccess(t *testing.T, method, url string) {
	t.Helper()

	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	result := decodeHTTPResponse(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, result.Message)
	require.True(t, result.Success, result.Message)
}

func decodeHTTPResponse(t *testing.T, resp *http.Response) api.Response {
	t.Helper()

	var result api.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}
