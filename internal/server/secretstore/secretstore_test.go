package secretstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/secretstore"
)

func TestResolveReturnsConfiguredValue(t *testing.T) {
	store := secretstore.New(map[string]string{"web/api-key": "s3cret"})
	value, ok := store.Resolve("web/api-key")
	require.True(t, ok)
	assert.Equal(t, "s3cret", value)

	_, ok = store.Resolve("missing")
	assert.False(t, ok)
	assert.True(t, store.Has("web/api-key"))
	assert.False(t, store.Has("missing"))
}

func TestKeysAreSorted(t *testing.T) {
	store := secretstore.New(map[string]string{"b": "2", "a": "1", "c": "3"})
	assert.Equal(t, []string{"a", "b", "c"}, store.Keys())
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	original := secretstore.New(map[string]string{"web/api-key": "s3cret", "db/password": "hunter2"})
	require.NoError(t, original.Save(path, "passphrase"))

	loaded, err := secretstore.Load(path, "passphrase")
	require.NoError(t, err)
	value, ok := loaded.Resolve("web/api-key")
	require.True(t, ok)
	assert.Equal(t, "s3cret", value)
}

func TestSaveDoesNotWritePlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	store := secretstore.New(map[string]string{"web/api-key": "plaintext-marker"})
	require.NoError(t, store.Save(path, "passphrase"))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "plaintext-marker", "secret values must never be persisted in clear text")
}

func TestLoadRejectsWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	require.NoError(t, secretstore.New(map[string]string{"k": "v"}).Save(path, "correct"))

	_, err := secretstore.Load(path, "wrong")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt")
}

func TestSaveRequiresPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	err := secretstore.New(map[string]string{"k": "v"}).Save(path, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "passphrase is required")
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))
	_, err := secretstore.Load(path, "passphrase")
	require.Error(t, err)
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "secrets.json")
	require.NoError(t, secretstore.New(map[string]string{"k": "v"}).Save(path, "passphrase"))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestNewFromEnvUsesLookupPolicy(t *testing.T) {
	t.Setenv("DZJJY_TEST_SECRET_ONE", "value-one")
	t.Setenv("DZJJY_TEST_SECRET_TWO", "value-two")
	t.Setenv("DZJJY_TEST_IGNORED", "nope")

	store := secretstore.NewFromEnv(func(name string) (string, bool) {
		key, ok := strings.CutPrefix(name, "DZJJY_TEST_SECRET_")
		if !ok {
			return "", false
		}
		return strings.ToLower(key), true
	})

	value, ok := store.Resolve("one")
	require.True(t, ok)
	assert.Equal(t, "value-one", value)

	value, ok = store.Resolve("two")
	require.True(t, ok)
	assert.Equal(t, "value-two", value)

	_, ok = store.Resolve("ignored")
	assert.False(t, ok, "unmapped environment variables must not be captured")
}
