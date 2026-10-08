package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/stretchr/testify/require"
)

func TestApplyProcessPreservesCommandArguments(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	h := NewHandlerWithState(filepath.Join(root, "uploads"), workDir, filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	marker := filepath.Join(workDir, "app", "result.txt")
	name, script, command := processArtifactForTest(marker)
	configBytes, err := json.Marshal(struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Artifact string   `json:"artifact"`
			Command  []string `json:"command"`
		} `json:"spec"`
	}{APIVersion: "dzjjy/v1", Kind: "App", Metadata: struct {
		Name string `json:"name"`
	}{Name: "app"}, Spec: struct {
		Artifact string   `json:"artifact"`
		Command  []string `json:"command"`
	}{Artifact: "app.zip", Command: command}})
	require.NoError(t, err)
	config := string(configBytes)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	part, err := writer.CreateFormFile("artifact", "app.zip")
	require.NoError(t, err)
	_, err = part.Write(makeZipFile(t, name, script))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/app/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	h.Apply(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Data struct {
			Generation uint64 `json:"generation"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, uint64(1), response.Data.Generation)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && strings.TrimSpace(string(data)) == "hello world"
	}, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		stateValue := h.GetStateSnapshot()["app"]
		return stateValue != nil && stateValue.Status == state.StatusStopped
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, h.Shutdown())
}

func processArtifactForTest(marker string) (string, string, []string) {
	if runtime.GOOS == "windows" {
		return "app.bat", "@echo off\necho hello world > result.txt\n", []string{"./app.bat"}
	}
	return "app", "#!/bin/sh\nprintf '%s' \"$1\" > result.txt\n", []string{"./app", "hello world"}
}

func makeZipFile(t *testing.T, name, content string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o755)
	part, err := writer.CreateHeader(header)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
