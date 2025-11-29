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
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipFile(f, destDir); err != nil {
			return err
		}
	}

	slog.Info("zip extraction completed", "files", len(r.File))
	return nil
}

// extractZipFile 解压单个 ZIP 文件
func extractZipFile(f *zip.File, destDir string) error {
	// 构建目标路径
	destPath := filepath.Join(destDir, f.Name)

	// 防止路径穿越攻击
	if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path: %s", f.Name)
	}

	// 如果是目录
	if f.FileInfo().IsDir() {
		return os.MkdirAll(destPath, f.Mode())
	}

	// 创建父目录
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建文件
	destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer destFile.Close()

	// 打开源文件
	srcFile, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer srcFile.Close()

	// 复制内容
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// extractTarGz 解压 .tar.gz 文件
func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open tar.gz: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	return extractTarReader(tar.NewReader(gzr), destDir)
}

// extractTar 解压 .tar 文件
func extractTar(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open tar: %w", err)
	}
	defer file.Close()

	return extractTarReader(tar.NewReader(file), destDir)
}

// extractTarReader 从 tar.Reader 解压文件
func extractTarReader(tr *tar.Reader, destDir string) error {
	count := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// 构建目标路径
		destPath := filepath.Join(destDir, header.Name)

		// 防止路径穿越攻击
		if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			slog.Warn("skipping invalid path", "path", header.Name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// 创建目录
			if err := os.MkdirAll(destPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// 创建父目录
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// 创建文件
			destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			// 复制内容
			if _, err := io.Copy(destFile, tr); err != nil {
				destFile.Close()
				return fmt.Errorf("failed to copy file: %w", err)
			}
			destFile.Close()
			count++

		default:
			slog.Warn("skipping unsupported file type",
				"name", header.Name,
				"type", header.Typeflag,
			)
		}
	}

	slog.Info("tar extraction completed", "files", count)
	return nil
}

// extractGzip 解压单个 .gz 文件
func extractGzip(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open gzip: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// 目标文件名（去掉 .gz 扩展名）
	destPath := filepath.Join(destDir, strings.TrimSuffix(filepath.Base(archivePath), ".gz"))

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, gzr); err != nil {
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
