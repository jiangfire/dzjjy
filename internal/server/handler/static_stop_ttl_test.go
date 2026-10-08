package handler

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManualStopStaticSiteRemainsStoppedPastTTL(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	config := "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec:\n  artifact: site.zip\n  lifecycle: {ttl: 300ms}\n"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	part, err := writer.CreateFormFile("artifact", "site.zip")
	require.NoError(t, err)
	zipWriter := zip.NewWriter(part)
	index, err := zipWriter.Create("index.html")
	require.NoError(t, err)
	_, err = index.Write([]byte("site"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	applyResponse := httptest.NewRecorder()
	h.Apply(applyResponse, request)
	require.Equal(t, http.StatusOK, applyResponse.Code)
	stopResponse := httptest.NewRecorder()
	h.Stop(stopResponse, httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/stop", nil))
	require.Equal(t, http.StatusOK, stopResponse.Code)
	time.Sleep(400 * time.Millisecond)
	var app = h.GetStateSnapshot()["docs"]
	require.Equal(t, "stopped", app.Status)
	require.Equal(t, "manual_stop", app.TerminationReason)
	require.NoError(t, h.Shutdown())
}
