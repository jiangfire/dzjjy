package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/stretchr/testify/require"
)

// liveSleeperCommand returns a cross-platform command that stays alive long
// enough for lifecycle assertions without needing any artifact files.
func liveSleeperCommand() []string {
	if goruntime.GOOS == "windows" {
		return []string{"ping", "-n", "30", "127.0.0.1"}
	}
	return []string{"sleep", "30"}
}

func applyRequestForTest(t *testing.T, config string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/live/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request, httptest.NewRecorder()
}

func applyResponseData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Data
}

func configForCommand(command []string, ttl string) string {
	spec := map[string]any{"command": command}
	if ttl != "" {
		spec["lifecycle"] = map[string]any{"ttl": ttl}
	}
	document := map[string]any{
		"apiVersion": "dzjjy/v1", "kind": "App",
		"metadata": map[string]any{"name": "live"},
		"spec":     spec,
	}
	encoded, _ := json.Marshal(document)
	return string(encoded)
}

func TestApplyProcessChangedConfigWhileRunningReturnsConflict(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	defer func() { _ = h.Shutdown() }()

	req, rec := applyRequestForTest(t, configForCommand(liveSleeperCommand(), ""))
	h.Apply(rec, req)
	first := applyResponseData(t, rec)
	require.Equal(t, float64(1), first["generation"])

	// 运行中改配置：必须明确冲突，而不是静默替换。
	var changed []string
	if goruntime.GOOS == "windows" {
		changed = []string{"ping", "-n", "31", "127.0.0.1"}
	} else {
		changed = []string{"sleep", "31"}
	}
	req, rec = applyRequestForTest(t, configForCommand(changed, ""))
	h.Apply(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	app := h.GetStateSnapshot()["live"]
	require.NotNil(t, app)
	require.Equal(t, state.StatusRunning, app.Status)
}

func TestApplyProcessGenerationIncrementsOnRedeploy(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	defer func() { _ = h.Shutdown() }()

	req, rec := applyRequestForTest(t, configForCommand(liveSleeperCommand(), ""))
	h.Apply(rec, req)
	first := applyResponseData(t, rec)
	require.Equal(t, float64(1), first["generation"])

	stopRec := httptest.NewRecorder()
	h.Stop(stopRec, httptest.NewRequest(http.MethodPost, "/api/v1/apps/live/stop", nil))
	require.Equal(t, http.StatusOK, stopRec.Code)

	// 停止后重新 apply（同配置）：期望新 generation 替换旧代际。
	req, rec = applyRequestForTest(t, configForCommand(liveSleeperCommand(), ""))
	h.Apply(rec, req)
	second := applyResponseData(t, rec)
	require.Equal(t, float64(2), second["generation"])
}

func TestRestartCreatesNewGenerationAndResetsTTL(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	defer func() { _ = h.Shutdown() }()

	req, rec := applyRequestForTest(t, configForCommand(liveSleeperCommand(), "2s"))
	h.Apply(rec, req)
	first := applyResponseData(t, rec)
	require.Equal(t, float64(1), first["generation"])
	firstExpiry, ok := first["expires_at"].(float64)
	require.True(t, ok)
	require.Greater(t, firstExpiry, float64(0))

	time.Sleep(150 * time.Millisecond)
	restartStart := time.Now().Unix()
	restartRec := httptest.NewRecorder()
	h.Restart(restartRec, httptest.NewRequest(http.MethodPost, "/api/v1/apps/live/restart", nil))
	require.Equal(t, http.StatusOK, restartRec.Code, restartRec.Body.String())

	app := h.GetStateSnapshot()["live"]
	require.NotNil(t, app)
	require.Equal(t, uint64(2), app.Generation, "manual restart must create a new generation")
	require.Equal(t, state.StatusRunning, app.Status)
	// 新到期时间必须从重启时刻重新计算：不早于 restartStart+TTL，也不超过重启耗时+TTL 的合理范围。
	require.GreaterOrEqual(t, app.ExpiresAt, restartStart+2, "manual restart must recompute the TTL deadline")
	require.LessOrEqual(t, app.ExpiresAt, time.Now().Add(3*time.Second).Unix())
}

func TestStaticStopRemovesSiteDirectory(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	defer func() { _ = h.Shutdown() }()

	applyRec := httptest.NewRecorder()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec: {artifact: index.html}\n"))
	part, err := writer.CreateFormFile("artifact", "index.html")
	require.NoError(t, err)
	_, err = part.Write([]byte("<html>ok</html>"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	h.Apply(applyRec, request)
	require.Equal(t, http.StatusOK, applyRec.Code, applyRec.Body.String())

	siteRoot := h.GetStateSnapshot()["docs"].WorkPath
	require.NotEmpty(t, siteRoot)
	_, err = os.Stat(siteRoot)
	require.NoError(t, err, "site directory must exist while running")

	stopRec := httptest.NewRecorder()
	h.Stop(stopRec, httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/stop", nil))
	require.Equal(t, http.StatusOK, stopRec.Code)

	_, err = os.Stat(siteRoot)
	require.True(t, os.IsNotExist(err), "manual stop must clean up the site directory")
}

func TestRestoreProcessExpiredAppIsNotRevived(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	work := filepath.Join(root, "work")
	h := NewHandlerWithState(filepath.Join(root, "uploads"), work, filepath.Join(root, "logs"), stateFile)
	defer func() { _ = h.Shutdown() }()

	// 已过期且声明 auto-restart 的应用：恢复后必须保持 expired，不被自动重启复活。
	expired := &state.AppState{
		Config:            &state.ProcessConfig{Type: "exec", WorkDir: work, Executable: "app", AutoRestart: true},
		Status:            state.StatusExpired,
		TerminationReason: "ttl",
		Backend:           "process",
	}
	require.NoError(t, state.NewStateStore(stateFile).Persist(&state.StateData{Apps: map[string]*state.AppState{"gone": expired}}))

	require.NoError(t, h.RestoreState())

	app := h.GetStateSnapshot()["gone"]
	require.NotNil(t, app)
	require.Equal(t, state.StatusExpired, app.Status)
	require.Equal(t, 0, app.PID)
}

func TestRestoreProcessTTLFiresAfterServerRestart(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	work := filepath.Join(root, "work")
	h := NewHandlerWithState(filepath.Join(root, "uploads"), work, filepath.Join(root, "logs"), stateFile)
	defer func() { _ = h.Shutdown() }()

	// 运行中的应用带未到期 TTL：恢复后调度器必须继续接管并在到期时清理。
	// 用本测试进程作为被恢复的 PID（身份未持久化 → 保守接管，不允许 kill）。
	running := &state.AppState{
		Config:     &state.ProcessConfig{Type: "exec", WorkDir: work, Executable: "app"},
		Status:     state.StatusRunning,
		PID:        os.Getpid(),
		Backend:    "process",
		Generation: 3,
		ExpiresAt:  time.Now().Add(2 * time.Second).Unix(),
	}
	require.NoError(t, state.NewStateStore(stateFile).Persist(&state.StateData{Apps: map[string]*state.AppState{"ttled": running}}))

	require.NoError(t, h.RestoreState())

	app := h.GetStateSnapshot()["ttled"]
	require.NotNil(t, app)
	require.Equal(t, state.StatusRunning, app.Status)

	require.Eventually(t, func() bool {
		app := h.GetStateSnapshot()["ttled"]
		return app != nil && app.Status == state.StatusExpired && app.TerminationReason == "ttl"
	}, 6*time.Second, 20*time.Millisecond, "TTL must keep working across a server restart")
}
