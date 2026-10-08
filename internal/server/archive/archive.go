package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Limits bounds both archive metadata and the bytes actually expanded.
type Limits struct {
	MaxFileSize  int64
	MaxTotalSize int64
	MaxFiles     int
}

var DefaultLimits = Limits{
	MaxFileSize:  100 << 20,
	MaxTotalSize: 1 << 30,
	MaxFiles:     10000,
}

type extractionState struct {
	limits Limits
	files  int
	total  int64
}

func (s *extractionState) reserveFile(size int64) error {
	if size < 0 {
		return fmt.Errorf("invalid archive entry size: %d", size)
	}
	if s.limits.MaxFiles > 0 && s.files >= s.limits.MaxFiles {
		return fmt.Errorf("file count limit exceeded: maximum %d files", s.limits.MaxFiles)
	}
	if s.limits.MaxFileSize > 0 && size > s.limits.MaxFileSize {
		return fmt.Errorf("file size limit exceeded: maximum %d bytes", s.limits.MaxFileSize)
	}
	if s.limits.MaxTotalSize > 0 && size > s.limits.MaxTotalSize-s.total {
		return fmt.Errorf("total expanded size limit exceeded: maximum %d bytes", s.limits.MaxTotalSize)
	}
	return nil
}

func (s *extractionState) addFile(size int64) error {
	if err := s.reserveFile(size); err != nil {
		return err
	}
	s.files++
	s.total += size
	return nil
}

// safeExtractPath validates an archive entry and returns a path inside destDir.
func safeExtractPath(destDir, entryPath string) (string, error) {
	if entryPath == "" || filepath.IsAbs(entryPath) {
		return "", fmt.Errorf("invalid file path: %s (path traversal detected)", entryPath)
	}

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for dest dir: %w", err)
	}
	destPath := filepath.Join(destDir, filepath.FromSlash(entryPath))
	absDestPath, err := filepath.Abs(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}
	if absDestPath != absDestDir && !strings.HasPrefix(absDestPath, absDestDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid file path: %s (path traversal detected)", entryPath)
	}

	// Reject symlinks in the existing destination path and its parents.
	current := absDestPath
	for current != absDestDir {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symbolic links not allowed: %s", entryPath)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("failed to inspect destination path: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return destPath, nil
}

// Extract extracts an archive using conservative default limits.
func Extract(archivePath, destDir string) error {
	return ExtractWithLimits(archivePath, destDir, DefaultLimits)
}

// ExtractWithLimits extracts an archive while enforcing per-file, total-size,
// and file-count limits. A limit of zero disables that individual limit.
func ExtractWithLimits(archivePath, destDir string, limits Limits) error {
	ext := strings.ToLower(filepath.Ext(archivePath))
	state := &extractionState{limits: limits}
	slog.Info("extracting archive", "file", archivePath, "dest", destDir, "type", ext)

	switch ext {
	case ".zip":
		return extractZip(archivePath, destDir, state)
	case ".gz":
		if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
			return extractTarGz(archivePath, destDir, state)
		}
		return extractGzip(archivePath, destDir, state)
	case ".tar":
		return extractTar(archivePath, destDir, state)
	default:
		return fmt.Errorf("unsupported archive format: %s", ext)
	}
}

func extractZip(archivePath, destDir string, state *extractionState) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, entry := range r.File {
		destPath, err := safeExtractPath(destDir, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}
		if err := state.reserveFile(int64(entry.UncompressedSize64)); err != nil {
			return fmt.Errorf("archive entry %q: %w", entry.Name, err)
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		file, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 - path validated above
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		src, err := entry.Open()
		if err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to open zip file: %w", err)
		}
		n, copyErr := copyLimited(file, src, state.limits.MaxFileSize)
		_ = src.Close()
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("archive entry %q: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close file: %w", closeErr)
		}
		if err := state.addFile(n); err != nil {
			return fmt.Errorf("archive entry %q: %w", entry.Name, err)
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string, state *extractionState) error {
	file, err := os.Open(archivePath) // #nosec G304 - caller supplied archive path
	if err != nil {
		return fmt.Errorf("failed to open tar.gz: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = reader.Close() }()
	return extractTarReader(tar.NewReader(reader), destDir, state)
}

func extractTar(archivePath, destDir string, state *extractionState) error {
	file, err := os.Open(archivePath) // #nosec G304 - caller supplied archive path
	if err != nil {
		return fmt.Errorf("failed to open tar: %w", err)
	}
	defer func() { _ = file.Close() }()
	return extractTarReader(tar.NewReader(file), destDir, state)
}

func extractTarReader(reader *tar.Reader, destDir string, state *extractionState) error {
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}
		destPath, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := state.reserveFile(header.Size); err != nil {
				return fmt.Errorf("archive entry %q: %w", header.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			file, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 - path validated above
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			n, copyErr := copyLimited(file, reader, state.limits.MaxFileSize)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("archive entry %q: %w", header.Name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("failed to close file: %w", closeErr)
			}
			if n != header.Size {
				return fmt.Errorf("archive entry %q: truncated content: expected %d bytes, read %d", header.Name, header.Size, n)
			}
			if err := state.addFile(n); err != nil {
				return fmt.Errorf("archive entry %q: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported archive entry type %q: %s", header.Typeflag, header.Name)
		}
	}
}

func extractGzip(archivePath, destDir string, state *extractionState) error {
	file, err := os.Open(archivePath) // #nosec G304 - caller supplied archive path
	if err != nil {
		return fmt.Errorf("failed to open gzip: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	entryPath := strings.TrimSuffix(filepath.Base(archivePath), ".gz")
	destPath, err := safeExtractPath(destDir, entryPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if state.limits.MaxFiles > 0 && state.limits.MaxFiles < 1 {
		return fmt.Errorf("file count limit exceeded: maximum %d files", state.limits.MaxFiles)
	}
	fileOut, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 - path validated above
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	n, copyErr := copyLimited(fileOut, reader, state.limits.MaxFileSize)
	closeErr := fileOut.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close file: %w", closeErr)
	}
	if err := state.addFile(n); err != nil {
		return err
	}
	return nil
}

func copyLimited(dst io.Writer, src io.Reader, max int64) (int64, error) {
	if max <= 0 {
		return io.Copy(dst, src)
	}
	limited := io.LimitReader(src, max+1)
	n, err := io.Copy(dst, limited)
	if err != nil {
		return n, fmt.Errorf("failed to copy file: %w", err)
	}
	if n > max {
		return n, fmt.Errorf("file size limit exceeded: maximum %d bytes", max)
	}
	return n, nil
}

// IsArchive reports whether filename uses a supported archive suffix.
func IsArchive(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".zip", ".tar", ".gz":
		return true
	default:
		return false
	}
}
