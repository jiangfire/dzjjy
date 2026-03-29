package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	serverruntime "github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/jiangfire/dzjjy/pkg/api"
)

type infiniteZeroReader struct{}

func (infiniteZeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	workDir := filepath.Join(root, "work")
	logDir := filepath.Join(root, "logs")

	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	return NewHandler(uploadDir, workDir, logDir)
}

func newMultipartRequest(t *testing.T, target string, fields map[string]string, filename string, content []byte) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}

	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func newOversizedMultipartRequest(t *testing.T, target string, size int64) (*http.Request, chan struct{}) {
	t.Helper()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	done := make(chan struct{})

	req := httptest.NewRequest(http.MethodPost, target, pr)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	go func() {
		defer close(done)
		defer func() { _ = pw.Close() }()

		_ = writer.WriteField("type", serverruntime.TypeExec)
		_ = writer.WriteField("executable", "app.exe")

		part, err := writer.CreateFormFile("file", "payload.bin")
		if err == nil {
			_, _ = io.CopyN(part, infiniteZeroReader{}, size)
		}

		_ = writer.Close()
	}()

	return req, done
}

func decodeResponse(t *testing.T, rr *httptest.ResponseRecorder) api.Response {
	t.Helper()

	var resp api.Response
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	return resp
}

func TestValidateAppName_Boundaries(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		wantErr bool
	}{
		{name: "empty", appName: "", wantErr: true},
		{name: "max length", appName: strings.Repeat("a", 64)},
		{name: "too long", appName: strings.Repeat("a", 65), wantErr: true},
		{name: "single dot", appName: ".", wantErr: true},
		{name: "double dot", appName: "..", wantErr: true},
		{name: "contains slash", appName: "app/name", wantErr: true},
		{name: "contains parent dir", appName: "app..name", wantErr: true},
		{name: "unicode", appName: "应用", wantErr: true},
		{name: "valid punctuation", appName: "App_01-prod.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppName(tt.appName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateFilename_Boundaries(t *testing.T) {
	h := newTestHandler(t)

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{name: "empty", filename: "", wantErr: true},
		{name: "regular", filename: "app.exe"},
		{name: "max length", filename: strings.Repeat("a", 255)},
		{name: "too long", filename: strings.Repeat("a", 256), wantErr: true},
		{name: "invalid chars", filename: `bad:name.exe`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.validateFilename(tt.filename)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateAppConfig_Boundaries(t *testing.T) {
	h := newTestHandler(t)

	tests := []struct {
		name        string
		appType     string
		executable  string
		entry       string
		maxRestarts int
		wantErr     string
	}{
		{name: "missing type", appType: "", executable: "app.exe", wantErr: "type and executable are required"},
		{name: "missing executable", appType: serverruntime.TypeExec, executable: "", wantErr: "type and executable are required"},
		{name: "invalid type", appType: "shell", executable: "app.exe", wantErr: "invalid type"},
		{name: "blank executable", appType: serverruntime.TypeExec, executable: "   ", wantErr: "executable cannot be empty"},
		{name: "invalid executable chars", appType: serverruntime.TypeExec, executable: "app;rm", wantErr: "executable contains invalid characters"},
		{name: "negative restart count", appType: serverruntime.TypeExec, executable: "app.exe", maxRestarts: -1, wantErr: "max_restarts cannot be negative"},
		{name: "runtime missing entry", appType: serverruntime.TypeRuntime, executable: "python", wantErr: "entry is required for runtime type"},
		{name: "runtime valid", appType: serverruntime.TypeRuntime, executable: "python", entry: "app.py"},
		{name: "exec valid", appType: serverruntime.TypeExec, executable: "app.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.validateAppConfig(tt.appType, tt.executable, tt.entry, tt.maxRestarts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPrepareWorkDir_FailsWhenRootIsFile(t *testing.T) {
	root := t.TempDir()
	workRoot := filepath.Join(root, "work-root")
	require.NoError(t, os.WriteFile(workRoot, []byte("not a dir"), 0o644))

	h := NewHandler(filepath.Join(root, "uploads"), workRoot, filepath.Join(root, "logs"))
	err := h.prepareWorkDir("app")

	require.Error(t, err)
	// Platform-specific errors:
	// - Linux: os.RemoveAll fails with "not a directory" → "failed to clean work dir"
	// - Windows: os.RemoveAll succeeds, os.MkdirAll fails → "failed to create work dir"
	errMsg := err.Error()
	assertTrue := assert.True(t,
		strings.Contains(errMsg, "failed to clean work dir") || strings.Contains(errMsg, "failed to create work dir"),
		"error should mention clean or create, got: %s", errMsg)
	_ = assertTrue // suppress unused warning
}

func TestHandleFileUpload_MissingFileReturnsError(t *testing.T) {
	h := newTestHandler(t)
	require.NoError(t, h.prepareWorkDir("app"))

	req := newMultipartRequest(t, "/api/v1/apps/app/deploy", map[string]string{
		"type":       serverruntime.TypeExec,
		"executable": "app.exe",
	}, "", nil)

	err := h.handleFileUpload(req, "app")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get file")
}

func TestHandleFileUpload_ArchiveExtractionFailureLeavesEmptyWorkDir(t *testing.T) {
	h := newTestHandler(t)
	require.NoError(t, h.prepareWorkDir("app"))

	req := newMultipartRequest(t, "/api/v1/apps/app/deploy", map[string]string{
		"type":       serverruntime.TypeExec,
		"executable": "app.exe",
	}, "broken.zip", []byte("not-a-zip"))

	err := h.handleFileUpload(req, "app")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract archive")

	appWorkDir := filepath.Join(h.workDir, "app")
	entries, readErr := os.ReadDir(appWorkDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestHandleFileUpload_FailsWhenUploadRootIsFile(t *testing.T) {
	root := t.TempDir()
	uploadRoot := filepath.Join(root, "uploads-root")
	require.NoError(t, os.WriteFile(uploadRoot, []byte("not a dir"), 0o644))

	workDir := filepath.Join(root, "work")
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	h := NewHandler(uploadRoot, workDir, logDir)
	require.NoError(t, h.prepareWorkDir("app"))

	req := newMultipartRequest(t, "/api/v1/apps/app/deploy", map[string]string{
		"type":       serverruntime.TypeExec,
		"executable": "app.exe",
	}, "app.exe", []byte("payload"))

	err := h.handleFileUpload(req, "app")

	require.Error(t, err)
	// Platform-specific errors:
	// - Linux: os.RemoveAll fails with "not a directory" → "failed to clean upload dir"
	// - Windows: os.RemoveAll succeeds, os.MkdirAll fails → "failed to create upload dir"
	errMsg := err.Error()
	assertTrue := assert.True(t,
		strings.Contains(errMsg, "failed to clean upload dir") || strings.Contains(errMsg, "failed to create upload dir"),
		"error should mention clean or create, got: %s", errMsg)
	_ = assertTrue // suppress unused warning
}

func TestCopyFileBetweenRoots_RejectsEscapePath(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	outsideRoot := t.TempDir()
	outsideFile := filepath.Join(outsideRoot, "outside.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0o644))

	err := copyFileBetweenRoots(srcRoot, outsideFile, dstRoot, filepath.Join(dstRoot, "copied.txt"))

	require.Error(t, err)
}

func TestExtractDeployParams_RejectsOversizedBody(t *testing.T) {
	h := newTestHandler(t)
	req, done := newOversizedMultipartRequest(t, "/api/v1/apps/large/deploy", (110<<20)+1)
	recorder := httptest.NewRecorder()

	_, _, err := h.extractDeployParams(recorder, req)
	require.NoError(t, req.Body.Close())
	<-done

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse form")
	assert.Contains(t, strings.ToLower(err.Error()), "too large")
}

func TestLogs_InvalidLinesFallsBackToDefault(t *testing.T) {
	h := newTestHandler(t)
	config := &serverruntime.ProcessConfig{
		Type:       serverruntime.TypeExec,
		WorkDir:    t.TempDir(),
		Executable: "app.exe",
	}
	require.NoError(t, h.multiManager.RestoreApp("app", config, 0, 0, 0, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/app/logs?lines=invalid", nil)
	recorder := httptest.NewRecorder()

	h.Logs(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeResponse(t, recorder)
	assert.True(t, resp.Success)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 0, data["count"])
}

func TestRemoveApp_MissingAppIsIdempotent(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apps/missing/remove", nil)
	recorder := httptest.NewRecorder()

	h.RemoveApp(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeResponse(t, recorder)
	assert.True(t, resp.Success)
	assert.Equal(t, "application removed", resp.Message)
}
