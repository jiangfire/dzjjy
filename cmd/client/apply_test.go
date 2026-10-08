package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveApplyArtifactUsesConfigDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "dzjjy.yaml")
	artifactPath := filepath.Join(root, "bundle.zip")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))
	require.NoError(t, os.WriteFile(artifactPath, []byte("zip"), 0o600))
	resolved, err := resolveArtifactPath(configPath, "./bundle.zip")
	require.NoError(t, err)
	require.Equal(t, artifactPath, resolved)
}
