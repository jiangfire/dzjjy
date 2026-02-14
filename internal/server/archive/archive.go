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

// safeExtractPath 验证并返回安全的解压路径
func safeExtractPath(destDir, entryPath string) (string, error) {
	// 获取目标目录的绝对路径
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for dest dir: %w", err)
	}

	// 构建完整的目标路径
	destPath := filepath.Join(destDir, entryPath)

	// 获取目标路径的绝对路径
	absDestPath, err := filepath.Abs(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// 验证目标路径在目标目录内
	if !strings.HasPrefix(absDestPath, absDestDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid file path: %s (path traversal detected)", entryPath)
	}

	// 检查符号链接（防止通过符号链接逃逸）
	if info, err := os.Lstat(destPath); err == nil && info.Mode()&os.ModeSymlink != 0 { // #nosec G703 - destPath is validated to stay under destDir
		return "", fmt.Errorf("symbolic links not allowed: %s", entryPath)
	}

	return destPath, nil
}

// Extract 解压缩文件到指定目录
func Extract(archivePath, destDir string) error {
	ext := strings.ToLower(filepath.Ext(archivePath))

	slog.Info("extracting archive",
		"file", archivePath,
		"dest", destDir,
		"type", ext,
	)

	switch ext {
	case ".zip":
		return extractZip(archivePath, destDir)
	case ".gz":
		// 检查是否是 .tar.gz
		if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
			return extractTarGz(archivePath, destDir)
		}
		return extractGzip(archivePath, destDir)
	case ".tar":
		return extractTar(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", ext)
	}
}

// extractZip 解压 ZIP 文件
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	var invalidPaths []string
	successCount := 0

	for _, f := range r.File {
		err := extractZipFile(f, destDir)
		if err != nil {
			// 检查是否是路径遍历错误
			if strings.Contains(err.Error(), "path traversal") {
				invalidPaths = append(invalidPaths, f.Name)
				slog.Warn("skipping invalid path", "path", f.Name, "error", err)
				continue
			}
			return err
		}
		successCount++
	}

	// 如果有无效路径，返回警告信息
	if len(invalidPaths) > 0 {
		return fmt.Errorf("extraction completed with %d invalid paths (path traversal detected): %v", len(invalidPaths), invalidPaths)
	}

	slog.Info("zip extraction completed", "files", successCount)
	return nil
}

// extractZipFile 解压单个 ZIP 文件
func extractZipFile(f *zip.File, destDir string) error {
	// 使用安全路径验证
	destPath, err := safeExtractPath(destDir, f.Name)
	if err != nil {
		return err
	}

	// 如果是目录
	if f.FileInfo().IsDir() {
		return os.MkdirAll(destPath, f.Mode())
	}

	// 创建父目录
	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建文件
	destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode()) // #nosec G304 - path validated by safeExtractPath
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	// 打开源文件
	srcFile, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// 复制内容（限制大小防止解压炸弹）
	limitedReader := io.LimitReader(srcFile, 100*1024*1024)     // 100MB limit
	if _, err := io.Copy(destFile, limitedReader); err != nil { // #nosec G110 - limited to 100MB
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// extractTarGz 解压 .tar.gz 文件
func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath) // #nosec G304 - archivePath is validated by Extract function
	if err != nil {
		return fmt.Errorf("failed to open tar.gz: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	return extractTarReader(tar.NewReader(gzr), destDir)
}

// extractTar 解压 .tar 文件
func extractTar(archivePath, destDir string) error {
	file, err := os.Open(archivePath) // #nosec G304 - archivePath is validated by Extract function
	if err != nil {
		return fmt.Errorf("failed to open tar: %w", err)
	}
	defer func() { _ = file.Close() }()

	return extractTarReader(tar.NewReader(file), destDir)
}

// extractTarReader 从 tar.Reader 解压文件
func extractTarReader(tr *tar.Reader, destDir string) error {
	count := 0
	var invalidPaths []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// 使用安全路径验证
		destPath, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			invalidPaths = append(invalidPaths, header.Name)
			slog.Warn("skipping invalid path", "path", header.Name, "error", err) // #nosec G706 - archive entry names are operational diagnostics
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// 创建目录
			mode := header.Mode
			if mode < 0 || mode > 07777 {
				return fmt.Errorf("invalid file mode: %d", mode)
			}
			if err := os.MkdirAll(destPath, os.FileMode(mode)); err != nil { // #nosec G703 - destPath is validated by safeExtractPath
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// 创建父目录
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil { // #nosec G703 - destPath is validated by safeExtractPath
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// 创建文件
			mode := header.Mode
			if mode < 0 || mode > 07777 {
				return fmt.Errorf("invalid file mode: %d", mode)
			}
			destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(mode)) // #nosec G304,G703 - path validated by safeExtractPath
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			// 复制内容（限制大小防止解压炸弹）
			limitedReader := io.LimitReader(tr, 100*1024*1024)          // 100MB limit
			if _, err := io.Copy(destFile, limitedReader); err != nil { // #nosec G110 - limited to 100MB
				_ = destFile.Close() // ignore close error in error path
				return fmt.Errorf("failed to copy file: %w", err)
			}
			if err := destFile.Close(); err != nil {
				return fmt.Errorf("failed to close file: %w", err)
			}
			count++

		default:
			// #nosec G706 - archive entry names are operational diagnostics
			slog.Warn("skipping unsupported file type",
				"name", header.Name,
				"type", header.Typeflag,
			)
		}
	}

	// 如果有无效路径，返回警告信息
	if len(invalidPaths) > 0 {
		return fmt.Errorf("extraction completed with %d invalid paths (path traversal detected): %v", len(invalidPaths), invalidPaths)
	}

	slog.Info("tar extraction completed", "files", count)
	return nil
}

// extractGzip 解压单个 .gz 文件
func extractGzip(archivePath, destDir string) error {
	file, err := os.Open(archivePath) // #nosec G304 - archivePath is validated by Extract function
	if err != nil {
		return fmt.Errorf("failed to open gzip: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	// 目标文件名（去掉 .gz 扩展名）
	entryPath := strings.TrimSuffix(filepath.Base(archivePath), ".gz")

	// 使用安全路径验证
	destPath, err := safeExtractPath(destDir, entryPath)
	if err != nil {
		return err
	}

	destFile, err := os.Create(destPath) // #nosec G304 - path validated by safeExtractPath
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	// 复制内容（限制大小防止解压炸弹）
	limitedReader := io.LimitReader(gzr, 100*1024*1024)         // 100MB limit
	if _, err := io.Copy(destFile, limitedReader); err != nil { // #nosec G110 - limited to 100MB
		return fmt.Errorf("failed to copy file: %w", err)
	}

	slog.Info("gzip extraction completed", "file", destPath)
	return nil
}

// IsArchive 检查文件是否是支持的压缩格式
func IsArchive(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".zip", ".tar", ".gz":
		return true
	default:
		return false
	}
}
