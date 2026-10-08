package deploy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyConfigPostsYAMLAndArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/apps/docs/apply", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		require.Contains(t, r.FormValue("config"), "kind: StaticSite")
		file, header, err := r.FormFile("artifact")
		require.NoError(t, err)
		defer file.Close()
		require.Equal(t, "site.zip", header.Filename)
		_, _ = w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	err := client.ApplyConfig("docs", []byte("apiVersion: dzjjy/v1\nkind: StaticSite\n"), "site.zip", strings.NewReader("zip-bytes"))
	require.NoError(t, err)
}
