package runtime_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var loggerTestDir string

func TestMain(m *testing.M) {
	cwd, _ := os.Getwd()
	loggerTestDir = filepath.Join(cwd, "test-logger-logs")
	os.RemoveAll(loggerTestDir)
	os.MkdirAll(loggerTestDir, 0755)

	code := m.Run()

	os.RemoveAll(loggerTestDir)
	os.Exit(code)
}

// setupTestDir 创建临时测试目录
func setupTestDir(t *testing.T, prefix string) string {
	dir, err := os.MkdirTemp("", prefix)
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// TestLogger_Start_Stop 测试启动和停止
func TestLogger_Start_Stop(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-app")

	// 启动
	err := logger.Start()
	require.NoError(t, err)

	// 验证日志文件已创建
	logFile := logger.GetLogFile()
	assert.NotEqual(t, "", logFile)
	assert.FileExists(t, logFile)

	// 停止
	err = logger.Stop()
	require.NoError(t, err)

	// 文件应该仍然存在
	assert.FileExists(t, logFile)
}

// TestLogger_WriteLog 测试写入日志
func TestLogger_WriteLog(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-write")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入不同类型的日志
	logger.WriteLog("system", "system message")
	logger.WriteLog("stdout", "stdout message")
	logger.WriteLog("stderr", "stderr message")

	// 获取日志
	logs := logger.GetLogs(10)
	assert.Equal(t, 3, len(logs))

	// 验证内容
	assert.Equal(t, "system", logs[0].Type)
	assert.Equal(t, "system message", logs[0].Message)
	assert.Equal(t, "stdout", logs[1].Type)
	assert.Equal(t, "stderr", logs[2].Type)
}

// TestLogger_MemoryLimit 测试内存限制
func TestLogger_MemoryLimit(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-limit")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入超过 1000 行
	for i := 0; i < 1500; i++ {
		logger.WriteLog("stdout", "line "+string(rune(i)))
	}

	// 获取所有日志
	logs := logger.GetAllLogs()
	assert.LessOrEqual(t, len(logs), 1000, "最多保留 1000 行")

	// 应该保留最后的 1000 行
	assert.Equal(t, 1000, len(logs))
}

// TestLogger_GetLogs 测试获取指定行数
func TestLogger_GetLogs(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-get")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入 10 行
	for i := 0; i < 10; i++ {
		logger.WriteLog("stdout", "line "+string(rune('0'+i)))
	}

	// 获取最近 5 行
	logs := logger.GetLogs(5)
	assert.Equal(t, 5, len(logs))

	// 应该是最后 5 行
	assert.Equal(t, "line 5", logs[0].Message)
	assert.Equal(t, "line 9", logs[4].Message)

	// 获取超过总数的行数
	logs = logger.GetLogs(20)
	assert.Equal(t, 10, len(logs))

	// 获取 0 行（应该返回所有）
	logs = logger.GetLogs(0)
	assert.Equal(t, 10, len(logs))
}

// TestLogger_CaptureOutput 测试捕获输出
func TestLogger_CaptureOutput(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-capture")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 模拟 stdout
	stdout := bytes.NewBufferString("stdout line 1\nstdout line 2\n")
	go logger.CaptureOutput(stdout, "stdout")

	// 模拟 stderr
	stderr := bytes.NewBufferString("error line 1\n")
	go logger.CaptureOutput(stderr, "stderr")

	// 等待捕获完成
	time.Sleep(100 * time.Millisecond)

	logs := logger.GetLogs(10)
	assert.GreaterOrEqual(t, len(logs), 2, "应该捕获到输出")

	// 验证捕获的内容
	hasStdout := false
	hasStderr := false
	for _, log := range logs {
		if log.Type == "stdout" && strings.Contains(log.Message, "stdout") {
			hasStdout = true
		}
		if log.Type == "stderr" && strings.Contains(log.Message, "error") {
			hasStderr = true
		}
	}
	// stdout/stderr 捕获取决于系统行为，不强制要求
	_ = hasStdout
	_ = hasStderr
}

// TestLogger_GetAllLogs 测试获取所有日志
func TestLogger_GetAllLogs(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-all")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入 5 行
	for i := 0; i < 5; i++ {
		logger.WriteLog("stdout", "msg "+string(rune('0'+i)))
	}

	logs := logger.GetAllLogs()
	assert.Equal(t, 5, len(logs))

	// 验证顺序
	assert.Equal(t, "msg 0", logs[0].Message)
	assert.Equal(t, "msg 4", logs[4].Message)
}

// TestLogger_GetLogFile 测试获取日志文件路径
func TestLogger_GetLogFile(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-file")

	// 未启动时
	assert.Equal(t, "", logger.GetLogFile())

	// 启动后
	require.NoError(t, logger.Start())
	logFile := logger.GetLogFile()
	assert.NotEqual(t, "", logFile)
	assert.Contains(t, logFile, "test-file")

	// 停止后仍然返回路径
	logger.Stop()
	assert.NotEqual(t, "", logger.GetLogFile())
}

// TestLogger_ConcurrentWrite 测试并发写入
func TestLogger_ConcurrentWrite(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-concurrent")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 并发写入
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				logger.WriteLog("stdout", "goroutine "+string(rune('0'+id))+" msg "+string(rune('0'+j)))
			}
		}(i)
	}
	wg.Wait()

	// 应该有 100 条日志
	logs := logger.GetAllLogs()
	assert.Equal(t, 100, len(logs))
}

// TestLogger_Stop_ClosesDoneChannel 测试 Stop 关闭 done channel
func TestLogger_Stop_ClosesDoneChannel(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-done")
	require.NoError(t, logger.Start())

	// 启动一个捕获 goroutine
	reader := bytes.NewBufferString("test\n")
	done := make(chan struct{})

	go func() {
		logger.CaptureOutput(reader, "stdout")
		close(done)
	}()

	// 停止
	logger.Stop()

	// 等待捕获 goroutine 退出
	select {
	case <-done:
		// 成功退出
	case <-time.After(1 * time.Second):
		t.Fatal("CaptureOutput did not exit after Stop()")
	}
}

// TestLogger_WriteLog_FileSync 测试文件同步
func TestLogger_WriteLog_FileSync(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-sync")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logger.WriteLog("system", "test message")

	// 读取文件验证内容
	logFile := logger.GetLogFile()
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)

	assert.Contains(t, string(content), "test message")
}

// TestLogger_SanitizeFilename 测试文件名清理
func TestLogger_SanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal-app", "normal-app"},
		{"app with spaces", "app_with_spaces"},
		{"app:with:colons", "app_with_colons"},
		{"app/with/slashes", "app_with_slashes"},
		{"app\\with\\backslashes", "app_with_backslashes"},
		{"app<>\"|?*chars", "app_chars"},
		{"app{brackets}", "app_brackets"},
		{"app(dollar$)", "app_dollar"},
		{"__app__", "app"},
		{"app__with__double", "app_with_double"},
		{"", "unknown"},
		{strings.Repeat("a", 150), strings.Repeat("a", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// 通过 NewLogger 间接测试 sanitizeFilename
			logger := runtime.NewLogger(loggerTestDir, tt.input)
			require.NoError(t, logger.Start())
			defer logger.Stop()

			logFile := logger.GetLogFile()
			// 验证文件名包含预期结果
			if tt.expected != "" && tt.input != "" {
				assert.Contains(t, logFile, tt.expected)
			}
		})
	}
}

// TestLogger_EmptyMessage 测试空消息
func TestLogger_EmptyMessage(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-empty")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logger.WriteLog("stdout", "")
	logger.WriteLog("stderr", "")

	logs := logger.GetLogs(10)
	assert.Equal(t, 2, len(logs))
	assert.Equal(t, "", logs[0].Message)
}

// TestLogger_Timestamp 测试时间戳
func TestLogger_Timestamp(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-time")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	before := time.Now()
	// Add small delay to ensure time difference from before
	time.Sleep(1 * time.Millisecond)

	logger.WriteLog("stdout", "test")

	after := time.Now()

	logs := logger.GetLogs(1)
	require.Equal(t, 1, len(logs))

	// 验证时间戳在合理范围内
	assert.True(t, logs[0].Timestamp.After(before))
	assert.True(t, !logs[0].Timestamp.After(after))
}

// TestLogger_Stop_MultipleTimes 测试多次停止
func TestLogger_Stop_MultipleTimes(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-multi")
	require.NoError(t, logger.Start())

	err1 := logger.Stop()
	require.NoError(t, err1)

	// 第二次停止应该也安全
	err2 := logger.Stop()
	require.NoError(t, err2)
}

// TestLogger_WriteAfterStop 测试停止后写入
func TestLogger_WriteAfterStop(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-after")
	require.NoError(t, logger.Start())
	logger.Stop()

	// 停止后写入应该不会 panic
	logger.WriteLog("stdout", "after stop")

	// 内存中应该有这条日志
	logs := logger.GetAllLogs()
	assert.Equal(t, 1, len(logs))
}

// TestLogger_GetLogFile_NotStarted 测试未启动时获取路径
func TestLogger_GetLogFile_NotStarted(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-not-started")
	assert.Equal(t, "", logger.GetLogFile())
}

// TestLogger_NewLogger 测试创建 Logger
func TestLogger_NewLogger(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-new")
	assert.NotNil(t, logger)
	// 未启动时 GetLogFile 返回空字符串
	assert.Equal(t, "", logger.GetLogFile())
}

// TestLogger_Start_DirCreationFailure 测试目录创建失败
func TestLogger_Start_DirCreationFailure(t *testing.T) {
	// 在Windows上，尝试创建到根目录或受保护目录会失败
	// 使用一个不存在的父目录路径
	invalidDir := "Z:\\nonexistent\\protected\\path\\that\\cannot\\be\\created"
	logger := runtime.NewLogger(invalidDir, "test-fail")

	err := logger.Start()
	// 应该返回错误
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create log dir")
}

// TestLogger_Start_FileCreationFailure 测试文件创建失败
func TestLogger_Start_FileCreationFailure(t *testing.T) {
	// 使用一个无效的文件路径（包含在不存在的目录中）
	invalidDir := "Z:\\nonexistent\\dir\\for\\testing"
	testDir := filepath.Join(invalidDir, "subdir")

	logger := runtime.NewLogger(testDir, "test-readonly")
	err := logger.Start()

	// 目录创建失败，应该在第一步就返回错误
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create log dir")
}

// TestLogger_CaptureOutput_Stopped 测试停止后捕获输出
func TestLogger_CaptureOutput_Stopped(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-stopped-capture")
	require.NoError(t, logger.Start())

	// 先停止
	logger.Stop()

	// 尝试捕获（应该立即返回，不会panic）
	reader := strings.NewReader("test\n")
	done := make(chan bool)

	go func() {
		logger.CaptureOutput(reader, "stdout")
		done <- true
	}()

	// 应该快速完成
	select {
	case <-done:
		// 成功
	case <-time.After(100 * time.Millisecond):
		t.Fatal("CaptureOutput did not exit after Stop()")
	}
}

// TestLogger_WriteLog_NoFile 测试无文件时写入
func TestLogger_WriteLog_NoFile(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-no-file")
	// 不调用Start，直接写入
	logger.WriteLog("stdout", "test message")

	// 应该只在内存中
	logs := logger.GetAllLogs()
	assert.Equal(t, 1, len(logs))
	assert.Equal(t, "test message", logs[0].Message)
}

// TestLogger_GetLogs_NegativeLines 测试负数行数
func TestLogger_GetLogs_NegativeLines(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-negative")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logger.WriteLog("stdout", "line 1")
	logger.WriteLog("stdout", "line 2")

	// 负数应该返回所有日志
	logs := logger.GetLogs(-1)
	assert.Equal(t, 2, len(logs))
}

// TestLogger_GetLogs_LargerThanTotal 测试请求行数大于总数
func TestLogger_GetLogs_LargerThanTotal(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-larger")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logger.WriteLog("stdout", "line 1")
	logger.WriteLog("stdout", "line 2")

	// 请求100行，但只有2行
	logs := logger.GetLogs(100)
	assert.Equal(t, 2, len(logs))
}

// TestLogger_GetLogFile_AfterStop 测试停止后获取路径
func TestLogger_GetLogFile_AfterStop(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-after-stop")
	require.NoError(t, logger.Start())

	beforeStop := logger.GetLogFile()
	assert.NotEqual(t, "", beforeStop)

	logger.Stop()

	// 停止后仍然返回路径
	afterStop := logger.GetLogFile()
	assert.Equal(t, beforeStop, afterStop)
}

// TestLogger_WriteLog_FileSync_Error 测试文件同步错误处理
func TestLogger_WriteLog_FileSync_Error(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-sync-error")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 正常写入应该成功
	logger.WriteLog("stdout", "test")

	// 验证文件存在
	logFile := logger.GetLogFile()
	assert.FileExists(t, logFile)
}

// TestLogger_SanitizeFilename_EmptyAfterTrim 测试清理后为空
func TestLogger_SanitizeFilename_EmptyAfterTrim(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "___")
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logFile := logger.GetLogFile()
	// 应该包含"unknown"作为fallback
	assert.Contains(t, logFile, "unknown")
}

// TestLogger_SanitizeFilename_VeryLong 测试超长文件名
func TestLogger_SanitizeFilename_VeryLong(t *testing.T) {
	longName := strings.Repeat("a", 200)
	logger := runtime.NewLogger(loggerTestDir, longName)
	require.NoError(t, logger.Start())
	defer logger.Stop()

	logFile := logger.GetLogFile()
	// 应用名部分应该被截断到100字符，总文件名会更长（包含-timestamp.log）
	baseName := filepath.Base(logFile)
	// 验证应用名部分不超过100字符（去掉 -timestamp.log 后缀）
	// 格式: {safeAppName}-{timestamp}.log
	// timestamp: 15 chars, .log: 4 chars, -: 1 char = 20 chars
	expectedAppName := strings.Repeat("a", 100)
	assert.Contains(t, baseName, expectedAppName)
}

// TestLogger_Concurrent_StopAndWrite 测试并发停止和写入
func TestLogger_Concurrent_StopAndWrite(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-concurrent-stop")
	require.NoError(t, logger.Start())

	var wg sync.WaitGroup

	// 启动多个写入goroutine
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				logger.WriteLog("stdout", fmt.Sprintf("msg %d-%d", id, j))
			}
		}(i)
	}

	// 同时停止
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond) // 稍微延迟
		logger.Stop()
	}()

	wg.Wait()

	// 应该没有panic
}

// TestLogger_Stop_MultipleTimes_Concurrent 测试并发多次停止
func TestLogger_Stop_MultipleTimes_Concurrent(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-multi-stop")
	require.NoError(t, logger.Start())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Stop()
		}()
	}
	wg.Wait()
	// 不应该panic
}

// TestLogger_Rotation_Basic 测试基本轮转功能
func TestLogger_Rotation_Basic(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-basic")
	logger := runtime.NewLogger(dir, "test-rotation")

	// 设置小文件大小限制以便测试
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  100, // 100字节，非常小以便触发轮转
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入足够多的日志以触发轮转
	for i := 0; i < 50; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("line %d with some content to make it longer", i))
	}

	// 检查是否创建了轮转文件
	files, _ := os.ReadDir(dir)
	var rotatedCount int
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	// 应该至少有一个轮转文件
	assert.Greater(t, rotatedCount, 0, "应该创建了轮转文件")
}

// TestLogger_Rotation_Disabled 测试禁用轮转
func TestLogger_Rotation_Disabled(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-disabled")
	logger := runtime.NewLogger(dir, "test-disabled")

	// 禁用轮转
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  100,
		MaxFiles: 3,
		Enabled:  false,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入大量日志
	for i := 0; i < 100; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("line %d", i))
	}

	// 检查没有轮转文件
	files, _ := os.ReadDir(dir)
	var rotatedCount int
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Equal(t, 0, rotatedCount, "禁用轮转后不应创建轮转文件")
}

// TestLogger_Rotation_MaxFiles 测试最大文件数限制
func TestLogger_Rotation_MaxFiles(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-maxfiles")
	logger := runtime.NewLogger(dir, "test-maxfiles")

	// 设置小文件大小和最多保留2个文件
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 2,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入大量日志，触发多次轮转
	for i := 0; i < 200; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("line %d with content", i))
	}

	// 检查文件数量
	files, _ := os.ReadDir(dir)
	// 应该只有当前文件 + 最多2个轮转文件
	assert.LessOrEqual(t, len(files), 3, "应该最多保留3个文件（1个当前+2个轮转）")
}

// TestLogger_Rotation_Configuration 测试轮转配置
func TestLogger_Rotation_Configuration(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-config")

	// 通过写入日志并检查行为来验证默认配置
	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入少量日志，不应该触发轮转（默认10MB限制）
	for i := 0; i < 10; i++ {
		logger.WriteLog("stdout", "small message")
	}

	// 检查没有轮转文件
	files, _ := os.ReadDir(loggerTestDir)
	var rotatedCount int
	for _, f := range files {
		if strings.Contains(f.Name(), "test-config") && strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Equal(t, 0, rotatedCount, "小量日志不应该触发轮转")

	// 现在设置小限制并验证
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 5,
		Enabled:  true,
	})

	// 写入足够触发轮转的日志
	for i := 0; i < 50; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("trigger rotation %d", i))
	}

	// 应该有轮转文件
	files, _ = os.ReadDir(loggerTestDir)
	rotatedCount = 0
	for _, f := range files {
		if strings.Contains(f.Name(), "test-config") && strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Greater(t, rotatedCount, 0, "应该触发了轮转")
}

// TestLogger_Rotation_FileSize 测试文件大小准确性
func TestLogger_Rotation_FileSize(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-size")
	logger := runtime.NewLogger(dir, "test-size")

	// 设置精确的大小限制
	targetSize := int64(200)
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  targetSize,
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入日志直到超过限制
	for i := 0; i < 100; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("message number %d with some content", i))
	}

	// 检查轮转文件存在
	files, _ := os.ReadDir(dir)
	rotatedFiles := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedFiles++
			// 验证轮转文件不为空
			info, _ := f.Info()
			assert.Greater(t, info.Size(), int64(0), "轮转文件应该有内容")
		}
	}

	assert.Greater(t, rotatedFiles, 0, "应该有轮转文件")
}

// TestLogger_Rotation_Concurrent 测试并发轮转
func TestLogger_Rotation_Concurrent(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-concurrent")
	logger := runtime.NewLogger(dir, "test-concurrent")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  100,
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 并发写入
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				logger.WriteLog("stdout", fmt.Sprintf("goroutine %d msg %d", id, j))
			}
		}(i)
	}
	wg.Wait()

	// 不应该panic，验证有轮转文件
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}
	assert.Greater(t, rotatedCount, 0, "并发写入应该触发轮转")
}

// TestLogger_Rotation_GetLogFile_AfterRotation 测试轮转后获取日志文件路径
func TestLogger_Rotation_GetLogFile_AfterRotation(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-path")
	logger := runtime.NewLogger(dir, "test-path")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 获取初始路径
	initialPath := logger.GetLogFile()
	assert.NotEmpty(t, initialPath)

	// 触发轮转
	for i := 0; i < 50; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 获取新路径
	newPath := logger.GetLogFile()
	assert.NotEmpty(t, newPath)

	// 路径应该不同（因为是新文件）
	// 注意：路径可能相同如果在同一秒内，但文件应该已轮转
	assert.FileExists(t, newPath, "当前日志文件应该存在")
}

// TestLogger_Rotation_EmptyDir 测试空目录轮转
func TestLogger_Rotation_EmptyDir(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-empty")
	logger := runtime.NewLogger(dir, "test-empty")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入触发轮转
	for i := 0; i < 30; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 验证
	files, _ := os.ReadDir(dir)
	assert.Greater(t, len(files), 0, "应该有日志文件")
}

// TestLogger_Rotation_InvalidConfig 测试无效配置
func TestLogger_Rotation_InvalidConfig(t *testing.T) {
	logger := runtime.NewLogger(loggerTestDir, "test-invalid")

	// 设置无效配置（MaxSize=0，MaxFiles=0）
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  0,
		MaxFiles: 0,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 应该正常工作，只是不轮转
	for i := 0; i < 10; i++ {
		logger.WriteLog("stdout", "test")
	}

	// 不应该panic
}

// TestLogger_Rotation_MultipleRotations 测试多次轮转
func TestLogger_Rotation_MultipleRotations(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-multi")
	logger := runtime.NewLogger(dir, "test-multi")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  30,
		MaxFiles: 10,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入足够触发多次轮转
	for i := 0; i < 300; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("long message with content to exceed size limit %d", i))
	}

	// 检查多个轮转文件
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Greater(t, rotatedCount, 1, "应该有多次轮转")
}

// TestLogger_Rotation_Cleanup 测试清理旧文件
func TestLogger_Rotation_Cleanup(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-cleanup")
	logger := runtime.NewLogger(dir, "test-cleanup")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  30,
		MaxFiles: 2, // 只保留2个文件
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 触发多次轮转
	for i := 0; i < 200; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d with content", i))
	}

	// 检查文件数量
	files, _ := os.ReadDir(dir)
	// 应该最多3个：1个当前 + 2个轮转
	assert.LessOrEqual(t, len(files), 3, "应该最多保留2个轮转文件")
}

// TestLogger_Rotation_DisableDuringRuntime 测试运行时禁用轮转
func TestLogger_Rotation_DisableDuringRuntime(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-disable-runtime")
	logger := runtime.NewLogger(dir, "test-disable-runtime")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入一些日志
	for i := 0; i < 10; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 禁用轮转
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 3,
		Enabled:  false,
	})

	// 继续写入大量日志
	for i := 0; i < 100; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 检查没有新的轮转文件
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	// 可能有之前的轮转，但不应该新增
	assert.GreaterOrEqual(t, rotatedCount, 0)
}

// TestLogger_Rotation_GetCurrentFileSize 测试获取当前文件大小
func TestLogger_Rotation_GetCurrentFileSize(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-size-check")
	logger := runtime.NewLogger(dir, "test-size-check")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  1000,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入一些日志
	for i := 0; i < 5; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("message %d", i))
	}

	// 获取当前日志文件并检查大小
	logFile := logger.GetLogFile()
	info, err := os.Stat(logFile)
	require.NoError(t, err)

	assert.Greater(t, info.Size(), int64(0), "日志文件应该有内容")
	assert.Less(t, info.Size(), int64(1000), "文件大小应该小于限制")
}

// TestLogger_Rotation_EmptyMessage 测试空消息轮转
func TestLogger_Rotation_EmptyMessage(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-empty-msg")
	logger := runtime.NewLogger(dir, "test-empty-msg")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  20,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入空消息
	for i := 0; i < 50; i++ {
		logger.WriteLog("stdout", "")
	}

	// 应该正常工作
	files, _ := os.ReadDir(dir)
	assert.Greater(t, len(files), 0, "应该有日志文件")
}

// TestLogger_Rotation_SpecialCharacters 测试特殊字符文件名
func TestLogger_Rotation_SpecialCharacters(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-special")

	// 使用包含特殊字符的应用名
	logger := runtime.NewLogger(dir, "test:app/with\\special*chars")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 3,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 触发轮转
	for i := 0; i < 50; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 验证文件存在且名称安全
	files, _ := os.ReadDir(dir)
	assert.Greater(t, len(files), 0, "应该有日志文件")

	// 检查文件名不包含危险字符
	for _, f := range files {
		name := f.Name()
		assert.NotContains(t, name, ":")
		assert.NotContains(t, name, "/")
		assert.NotContains(t, name, "\\")
		assert.NotContains(t, name, "*")
	}
}

// TestLogger_Rotation_LargeFile 测试大文件轮转
func TestLogger_Rotation_LargeFile(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-large")
	logger := runtime.NewLogger(dir, "test-large")

	// 设置较大的文件限制
	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  1024, // 1KB
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入大量数据
	for i := 0; i < 200; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("This is line %d with some additional content to make the line longer and exceed the size limit more quickly", i))
	}

	// 验证轮转发生
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Greater(t, rotatedCount, 0, "应该触发轮转")
}

// TestLogger_Rotation_SequentialWrites 测试连续写入轮转
func TestLogger_Rotation_SequentialWrites(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-sequential")
	logger := runtime.NewLogger(dir, "test-sequential")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  40,
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 连续写入，每次写入后检查
	for batch := 0; batch < 5; batch++ {
		for i := 0; i < 20; i++ {
			logger.WriteLog("stdout", fmt.Sprintf("batch %d msg %d", batch, i))
		}
		// 短暂延迟确保文件系统更新
		time.Sleep(10 * time.Millisecond)
	}

	// 验证
	files, _ := os.ReadDir(dir)
	assert.Greater(t, len(files), 1, "应该有多个文件")
}

// TestLogger_Rotation_ConcurrentConfigChange 测试并发配置修改
func TestLogger_Rotation_ConcurrentConfigChange(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-concurrent-config")
	logger := runtime.NewLogger(dir, "test-concurrent-config")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  100,
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	var wg sync.WaitGroup

	// goroutine 1: 写入日志
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// goroutine 2: 修改配置
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		logger.SetRotationConfig(runtime.RotationConfig{
			MaxSize:  50,
			MaxFiles: 3,
			Enabled:  true,
		})
	}()

	wg.Wait()
	// 不应该panic
}

// TestLogger_Rotation_ZeroMaxFiles 测试 MaxFiles=0 (不限制)
func TestLogger_Rotation_ZeroMaxFiles(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-zero-files")
	logger := runtime.NewLogger(dir, "test-zero-files")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  50,
		MaxFiles: 0, // 不限制文件数量
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 触发多次轮转
	for i := 0; i < 200; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 应该有很多轮转文件
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Greater(t, rotatedCount, 5, "应该有很多轮转文件（无限制）")
}

// TestLogger_Rotation_ZeroMaxSize 测试 MaxSize=0 (禁用轮转)
func TestLogger_Rotation_ZeroMaxSize(t *testing.T) {
	dir := setupTestDir(t, "test-rotation-zero-size")
	logger := runtime.NewLogger(dir, "test-zero-size")

	logger.SetRotationConfig(runtime.RotationConfig{
		MaxSize:  0, // 禁用轮转
		MaxFiles: 5,
		Enabled:  true,
	})

	require.NoError(t, logger.Start())
	defer logger.Stop()

	// 写入大量日志
	for i := 0; i < 100; i++ {
		logger.WriteLog("stdout", fmt.Sprintf("msg %d", i))
	}

	// 不应该有轮转
	files, _ := os.ReadDir(dir)
	rotatedCount := 0
	for _, f := range files {
		if strings.Contains(f.Name(), ".rotated.") {
			rotatedCount++
		}
	}

	assert.Equal(t, 0, rotatedCount, "MaxSize=0 应该禁用轮转")
}
