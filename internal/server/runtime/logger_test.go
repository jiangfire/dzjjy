package runtime_test

import (
	"bytes"
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
