package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jiangfire/dzjjy/internal/server/dockerbackend"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/stretchr/testify/require"
)

type handlerDockerRunner struct{ calls [][]string }

func (r *handlerDockerRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	switch args[0] {
	case "info":
		return []byte("linux"), nil
	case "create":
		return []byte("container-1"), nil
	case "inspect":
		return []byte("true"), nil
	case "stop":
		return nil, nil
	default:
		return nil, nil
	}
}

func TestApplyDockerUsesManagedTemplateAndStopCleansContainer(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(root+"/uploads", root+"/work", root+"/logs", root+"/state.json")
	runner := &handlerDockerRunner{}
	h.dockerEngine = dockerbackend.NewEngineWithRunner(runner)
	config := "apiVersion: dzjjy/v1\nkind: App\nmetadata: {name: docker-app}\nspec:\n  artifact: app.zip\n  runtime: {type: python, version: \"3.12\"}\n  command: [python, app.py]\n  sandbox: {type: docker}\n"
	archiveBytes := func() []byte {
		buf := &bytes.Buffer{}
		writer := zip.NewWriter(buf)
		file, err := writer.Create("app.py")
		require.NoError(t, err)
		_, err = file.Write([]byte("print('ok')"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		return buf.Bytes()
	}()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	part, err := writer.CreateFormFile("artifact", "app.zip")
	require.NoError(t, err)
	_, err = part.Write(archiveBytes)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/docker-app/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	h.Apply(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var create string
	for _, call := range runner.calls {
		if call[0] == "create" {
			create = strings.Join(call, " ")
		}
	}
	require.Contains(t, create, "python:3.12-slim")
	require.Contains(t, create, "dzjjy.managed=true")
	require.Contains(t, create, "target=/workspace,readonly")
	appState := h.GetStateSnapshot()["docker-app"]
	require.Equal(t, "docker", appState.Backend)
	require.Equal(t, "container-1", appState.ContainerID)

	stopResponse := httptest.NewRecorder()
	h.Stop(stopResponse, httptest.NewRequest(http.MethodPost, "/api/v1/apps/docker-app/stop", nil))
	require.Equal(t, http.StatusOK, stopResponse.Code)
	var stopCalls []string
	for _, call := range runner.calls {
		if call[0] == "inspect" || call[0] == "stop" || call[0] == "rm" {
			stopCalls = append(stopCalls, call[0])
		}
	}
	require.Equal(t, []string{"inspect", "stop", "rm"}, stopCalls)
	require.Equal(t, state.StatusStopped, h.GetStateSnapshot()["docker-app"].Status)
	require.NoError(t, h.Shutdown())
}
