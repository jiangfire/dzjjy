package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/stretchr/testify/require"
)

func TestApplyStaticSiteTTLExpiresGeneration(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(t.TempDir(), t.TempDir(), t.TempDir(), root+"/state.json")
	config := "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: expiring}\nspec:\n  artifact: site.zip\n  lifecycle: {ttl: 100ms}\n"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	part, err := writer.CreateFormFile("artifact", "site.zip")
	require.NoError(t, err)
	zipWriter := zip.NewWriter(part)
	index, err := zipWriter.Create("index.html")
	require.NoError(t, err)
	_, err = index.Write([]byte("temporary"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/expiring/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	h.Apply(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Port int `json:"port"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Eventually(t, func() bool {
		app := h.GetStateSnapshot()["expiring"]
		return app != nil && app.Status == state.StatusExpired
	}, time.Second, 10*time.Millisecond)
	_, err = http.Get("http://127.0.0.1:" + strconv.Itoa(response.Data.Port) + "/")
	require.Error(t, err)
	require.NoError(t, h.Shutdown())
}
