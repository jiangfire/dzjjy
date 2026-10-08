package handler

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyStaticSiteSameConfigIsIdempotent(t *testing.T) {
	root := t.TempDir()
	h := NewHandlerWithState(filepath.Join(root, "uploads"), filepath.Join(root, "work"), filepath.Join(root, "logs"), filepath.Join(root, "state.json"))
	config := "apiVersion: dzjjy/v1\nkind: StaticSite\nmetadata: {name: docs}\nspec:\n  artifact: site.zip\n  port: 0\n"
	apply := func() struct {
		Port       int    `json:"port"`
		Generation uint64 `json:"generation"`
	} {
		recorder := httptest.NewRecorder()
		h.Apply(recorder, newApplyRequest(t, config, "site.zip", makeSiteZip(t)))
		require.Equal(t, 200, recorder.Code, recorder.Body.String())
		var response struct {
			Data struct {
				Port       int    `json:"port"`
				Generation uint64 `json:"generation"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Data
	}
	first := apply()
	second := apply()
	require.Equal(t, first, second)
	require.NoError(t, h.Shutdown())
}
