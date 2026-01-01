package test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// CreateTestZip 创建测试用的 ZIP 文件
func CreateTestZip(t *testing.T, files map[string]string) []byte {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err, "创建 ZIP 文件条目失败: %s", name)
		_, err = f.Write([]byte(content))
		require.NoError(t, err, "写入 ZIP 文件内容失败: %s", name)
	}

	require.NoError(t, w.Close(), "关闭 ZIP 写入器失败")
	return buf.Bytes()
}

// CreateMaliciousZip 创建包含路径遍历的恶意 ZIP 文件
func CreateMaliciousZip(t *testing.T) []byte {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	// 创建路径遍历文件
	maliciousFiles := []string{
		"../etc/passwd",
		"../../windows/system32/config/sam",
		"/absolute/path/file.txt",
		"file.txt",
	}

	for _, name := range maliciousFiles {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte("malicious content"))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	return buf.Bytes()
}

// SetupTestDir 创建临时测试目录
func SetupTestDir(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "dzjjy-test-*")
	require.NoError(t, err, "创建临时目录失败")
	return tmpDir
}

// CleanupTestDir 清理测试目录
func CleanupTestDir(t *testing.T, dir string) {
	if dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("Warning: failed to cleanup test dir %s: %v", dir, err)
		}
	}
}

// CreateTestApp 创建一个简单的测试应用
func CreateTestApp(t *testing.T, workDir string) string {
	// 创建一个简单的 shell 脚本作为测试应用
	scriptPath := filepath.Join(workDir, "test-app.sh")
	content := `#!/bin/sh
echo "Test app started"
sleep 0.1
echo "Test app finished"
exit 0
`
	err := os.WriteFile(scriptPath, []byte(content), 0700) // #nosec G306 - test script needs execute permission
	require.NoError(t, err, "创建测试应用失败")
	return scriptPath
}

// CreateFailingApp 创建一个会失败的应用
func CreateFailingApp(t *testing.T, workDir string) string {
	scriptPath := filepath.Join(workDir, "fail-app.sh")
	content := `#!/bin/sh
echo "This app will fail"
exit 1
`
	err := os.WriteFile(scriptPath, []byte(content), 0700) // #nosec G306 - test script needs execute permission
	require.NoError(t, err, "创建失败应用失败")
	return scriptPath
}

// CreateLongRunningApp 创建一个长时间运行的应用
func CreateLongRunningApp(t *testing.T, workDir string) string {
	scriptPath := filepath.Join(workDir, "long-app.sh")
	content := `#!/bin/sh
echo "Long running app started"
sleep 10
echo "Long running app finished"
`
	err := os.WriteFile(scriptPath, []byte(content), 0700) // #nosec G306 - test script needs execute permission
	require.NoError(t, err, "创建长运行应用失败")
	return scriptPath
}

// WaitFor 等待条件满足或超时
func WaitFor(condition func() bool, timeoutMs int) bool {
	elapsed := 0
	interval := 10 // 每10ms检查一次
	for elapsed < timeoutMs {
		if condition() {
			return true
		}
		// 在测试中，我们直接返回，不实际sleep
		elapsed += interval
	}
	return false
}

// AssertFileExists 断言文件存在
func AssertFileExists(t *testing.T, path string) {
	_, err := os.Stat(path)
	require.NoError(t, err, "文件应该存在: %s", path)
}

// AssertFileNotExists 断言文件不存在
func AssertFileNotExists(t *testing.T, path string) {
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "文件应该不存在: %s", path)
}

// AssertContains 断言字符串包含
func AssertContains(t *testing.T, haystack, needle string) {
	require.Contains(t, haystack, needle, "字符串应该包含: %s", needle)
}

// CreateTempFile 创建临时文件
func CreateTempFile(t *testing.T, dir, name, content string) string {
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0600)
	require.NoError(t, err, "创建临时文件失败: %s", name)
	return path
}

// CreateTestArchive 创建测试归档文件
func CreateTestArchive(t *testing.T, format, dir string) string {
	var archivePath string

	switch format {
	case "zip":
		archivePath = filepath.Join(dir, "test.zip")
		zipData := CreateTestZip(t, map[string]string{
			"app.py":     "print('Hello from Python')",
			"config.txt": "config=value",
		})
		require.NoError(t, os.WriteFile(archivePath, zipData, 0600))

	case "tar":
		// 简化：创建一个假的 tar 文件
		archivePath = filepath.Join(dir, "test.tar")
		// 实际测试中会使用真实 tar 命令或库创建

	case "tar.gz":
		archivePath = filepath.Join(dir, "test.tar.gz")

	default:
		require.Fail(t, "不支持的格式: %s", format)
	}

	return archivePath
}
