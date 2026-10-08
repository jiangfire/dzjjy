package staticserve

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSiteServesFilesFallbackAndClosesListener(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "asset.txt"), []byte("asset"), 0o600))
	site, err := Start("127.0.0.1", root, 0)
	require.NoError(t, err)
	port := site.Port()
	defer func() { _ = site.Close() }()

	for path, expected := range map[string]string{"/": "index", "/asset.txt": "asset", "/client/route": "index"} {
		response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + path)
		require.NoError(t, err)
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		require.NoError(t, readErr)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Equal(t, expected, string(body))
	}
	require.NoError(t, site.Close())
	require.Eventually(t, func() bool {
		_, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
		return err != nil
	}, time.Second, 10*time.Millisecond)
}

func TestSiteRejectsTraversalAndPortConflict(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o600))
	first, err := Start("127.0.0.1", root, 0)
	require.NoError(t, err)
	defer func() { _ = first.Close() }()
	_, err = Start("127.0.0.1", root, first.Port())
	require.Error(t, err)

	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(first.Port()) + "/%2e%2e/secret")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.NotEqual(t, http.StatusOK, response.StatusCode)

}

func TestSiteRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o600))
	if err := os.Symlink(outside, filepath.Join(root, "leak.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	site, err := Start("127.0.0.1", root, 0)
	require.NoError(t, err)
	defer func() { _ = site.Close() }()
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(site.Port()) + "/leak.txt")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.NotEqual(t, http.StatusOK, response.StatusCode)
}
