package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreStateRestartsStaticSiteAndPreservesTTL(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	h1 := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), statePath)
	config := "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec:\n  artifact: site.zip\n  port: 0\n"
	recorder := httptest.NewRecorder()
	h1.Apply(recorder, newApplyRequest(t, config, "site.zip", makeSiteZip(t)))
	require.Equal(t, http.StatusOK, recorder.Code)
	var first struct {
		Data struct {
			Port int `json:"port"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &first))
	require.NoError(t, h1.Shutdown())

	h2 := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), statePath)
	require.NoError(t, h2.RestoreState())
	statusRecorder := httptest.NewRecorder()
	h2.Status(statusRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/apps/docs/status", nil))
	require.Contains(t, statusRecorder.Body.String(), `"running":true`)
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(first.Data.Port) + "/")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, h2.Shutdown())
}
