package archive_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiangfire/dzjjy/internal/server/archive"
	"github.com/stretchr/testify/require"
)

func createLimitedZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "limited.zip")
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
	return path
}

func TestExtractWithLimitsRejectsOversizedFile(t *testing.T) {
	archivePath := createLimitedZip(t, map[string]string{"large.txt": "123456789"})
	err := archive.ExtractWithLimits(archivePath, t.TempDir(), archive.Limits{MaxFileSize: 4, MaxTotalSize: 100, MaxFiles: 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "file size limit")
}

func TestExtractWithLimitsRejectsTooManyFiles(t *testing.T) {
	archivePath := createLimitedZip(t, map[string]string{"one.txt": "1", "two.txt": "2"})
	err := archive.ExtractWithLimits(archivePath, t.TempDir(), archive.Limits{MaxFileSize: 10, MaxTotalSize: 100, MaxFiles: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "file count limit")
}

func TestExtractWithLimitsRejectsExpandedTotal(t *testing.T) {
	archivePath := createLimitedZip(t, map[string]string{"one.txt": "1234", "two.txt": "5678"})
	err := archive.ExtractWithLimits(archivePath, t.TempDir(), archive.Limits{MaxFileSize: 10, MaxTotalSize: 5, MaxFiles: 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "total expanded size limit")
}
