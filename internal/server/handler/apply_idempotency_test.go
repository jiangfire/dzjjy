package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyProcessSameConfigIsIdempotent(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	artifactName, command, script := "app", "./app", "#!/bin/sh\nsleep 10\n"
	if runtime.GOOS == "windows" {
		artifactName, command, script = "app.bat", "./app.bat", "@echo off\nping -n 10 127.0.0.1 >nul\n"
	}
	config := fmt.Sprintf("apiVersion: dzjjy/v1\nkind: App\nmetadata: {name: app}\nspec:\n  artifact: %s.zip\n  command: [%s]\n", artifactName, command)
	artifact := makeIdempotencyZipFile(t, artifactName, script)
	apply := func() *httptest.ResponseRecorder {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		require.NoError(t, writer.WriteField("config", config))
		part, err := writer.CreateFormFile("artifact", "app.zip")
		require.NoError(t, err)
		_, err = part.Write(artifact)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/app/apply", body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		h.Apply(recorder, request)
		return recorder
	}
	first := apply()
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var firstResponse struct {
		Data struct {
			Generation uint64 `json:"generation"`
			PID        int    `json:"pid"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstResponse))
	second := apply()
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	var secondResponse struct {
		Data struct {
			Generation uint64 `json:"generation"`
			PID        int    `json:"pid"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondResponse))
	require.Equal(t, firstResponse.Data.Generation, secondResponse.Data.Generation)
	require.Equal(t, firstResponse.Data.PID, secondResponse.Data.PID)
	require.NoError(t, h.Shutdown())
	time.Sleep(20 * time.Millisecond)
}

func makeIdempotencyZipFile(t *testing.T, name, content string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	header := &zip.FileHeader{Name: name}
	header.SetMode(0o755)
	part, err := writer.CreateHeader(header)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
