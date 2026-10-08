package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func newApplyRequest(t *testing.T, config, filename string, artifact []byte) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("config", config))
	part, err := writer.CreateFormFile("artifact", filename)
	require.NoError(t, err)
	_, err = part.Write(artifact)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/docs/apply", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func makeSiteZip(t *testing.T) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	file, err := writer.Create("index.html")
	require.NoError(t, err)
	_, err = file.Write([]byte("hello site"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestApplyStartsStaticSiteOnReturnedPort(t *testing.T) {
	h := NewHandler(t.TempDir(), t.TempDir(), t.TempDir())
	config := "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec:\n  artifact: ./site.zip\n  port: 0\n"
	request := newApplyRequest(t, config, "site.zip", makeSiteZip(t))
	recorder := httptest.NewRecorder()
	h.Apply(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Port int `json:"port"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(response.Data.Port) + "/")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "hello site", string(body))
}
