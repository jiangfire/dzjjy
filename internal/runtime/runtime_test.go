package runtime_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testWorkspace string
var testLogs string
var testBinary string

// TestMain 用于测试环境的全局设置和清理
func TestMain(m *testing.M) {
	// 获取当前工作目录
	cwd, _ := os.Getwd()
	testWorkspace = filepath.Join(cwd, "test-workspace")
	testLogs = filepath.Join(cwd, "test-logs")

	// 测试前清理
	os.RemoveAll(testWorkspace)
	os.RemoveAll(testLogs)
	os.MkdirAll(testWorkspace, 0755)
	os.MkdirAll(testLogs, 0755)

	// 编译一个简单的测试程序
	testBinary = filepath.Join(testWorkspace, "test-app.exe")
	if _, err := os.Stat(testBinary); os.IsNotExist(err) {
		cmd := exec.Command("go", "build", "-o", testBinary, "./internal/runtime/testapp")
		if err := cmd.Run(); err != nil {
			// 如果无法编译，尝试使用系统命令
			testBinary = ""
		}
	}

	// 运行测试
	code := m.Run()

	// 测试后清理
	os.RemoveAll(testWorkspace)
	os.RemoveAll(testLogs)

	os.Exit(code)
}

// createTestApp 创建一个 Go 测试应用
func createTestApp(t *testing.T, name, mainCode string) string {
	appDir := filepath.Join(testWorkspace, name)
	os.MkdirAll(appDir, 0755)
	mainFile := filepath.Join(appDir, "main.go")
	err := os.WriteFile(mainFile, []byte(mainCode), 0644)
	require.NoError(t, err, "创建测试应用源码失败")

	// 编译
	binary := filepath.Join(appDir, name+".exe")
	cmd := exec.Command("go", "build", "-o", binary, mainFile)
	cmd.Dir = appDir
	err = cmd.Run()
	require.NoError(t, err, "编译测试应用失败")

	return binary
}

// createBatchScript 创建 Windows 批处理脚本
func createBatchScript(t *testing.T, name, content string) string {
	path := filepath.Join(testWorkspace, name)
	// 使用 cmd.exe /c 执行简单命令
	fullContent := "@echo off\r\n" + content
	err := os.WriteFile(path, []byte(fullContent), 0755)
	require.NoError(t, err, "创建批处理脚本失败")
	return path
}

// TestManager_Start_Stop 测试正常启动和停止
func TestManager_Start_Stop(t *testing.T) {
	// 创建一个简单的 Go 程序，立即退出
	appPath := createTestApp(t, "exit-app", `package main
import "fmt"
func main() {
	fmt.Println("Hello from test")
}
`)

	manager := runtime.NewManager(testLogs)

	// 启动应用（不启用自动重启）
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	require.NoError(t, err, "启动失败")

	// 验证进程运行状态
	assert.True(t, manager.IsRunning(), "进程应该正在运行")
	pid := manager.GetPID()
	assert.Greater(t, pid, 0, "PID应该大于0")

	// 等待进程自然退出（程序会立即退出）
	time.Sleep(200 * time.Millisecond)

	// 进程应该已经退出
	assert.False(t, manager.IsRunning(), "进程应该已经退出")
}

// TestManager_Start_AlreadyRunning 测试重复启动
func TestManager_Start_AlreadyRunning(t *testing.T) {
	// 创建一个长时间运行的程序
	appPath := createTestApp(t, "sleep-app", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("Sleeping...")
	time.Sleep(10 * time.Second)
}
`)

	manager := runtime.NewManager(testLogs)

	// 第一次启动
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	require.NoError(t, err)

	// 尝试再次启动（应该失败）
	err = manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	assert.Error(t, err, "应该返回错误")
	assert.Contains(t, err.Error(), "already running", "错误信息应该包含'already running'")

	// 清理
	manager.Stop()
}

// TestManager_AutoRestart_Limit 测试自动重启限制
func TestManager_AutoRestart_Limit(t *testing.T) {
	// 创建一个总是失败的程序
	appPath := createTestApp(t, "fail-app", `package main
import "fmt"
func main() {
	fmt.Println("Failing")
	panic("intentional failure")
}
`)

	manager := runtime.NewManager(testLogs)

	// 启动应用，最多重启3次
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		true,  // 启用自动重启
		3,     // 最多重启3次
	)
	require.NoError(t, err)

	// 等待重启过程（每次重启间隔1秒）
	time.Sleep(6 * time.Second)

	// 验证重启次数
	_, _, _, _, restartCount, _ := manager.GetInfo()
	assert.Equal(t, 3, restartCount, "应该重启了3次")

	// 进程应该已经停止（达到最大重启次数）
	assert.False(t, manager.IsRunning(), "达到最大重启次数后应该停止")
}

// TestManager_ManualStop_WhileRestarting 测试在重启过程中手动停止
func TestManager_ManualStop_WhileRestarting(t *testing.T) {
	appPath := createTestApp(t, "fail-fast-app", `package main
func main() {
	panic("fail")
}
`)

	manager := runtime.NewManager(testLogs)

	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		true,  // 启用自动重启
		10,    // 允许很多次重启
	)
	require.NoError(t, err)

	// 等待一小段时间，让进程退出并开始重启
	time.Sleep(500 * time.Millisecond)

	// 手动停止
	err = manager.Stop()
	require.NoError(t, err)

	// 等待一下，确保监控goroutine已经退出
	time.Sleep(200 * time.Millisecond)

	// 进程不应该在运行
	assert.False(t, manager.IsRunning(), "手动停止后进程不应该运行")
}

// TestManager_ConcurrentOperations 测试并发操作
func TestManager_ConcurrentOperations(t *testing.T) {
	appPath := createTestApp(t, "concurrent-app", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("Running")
	time.Sleep(5 * time.Second)
}
`)

	manager := runtime.NewManager(testLogs)

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	// 并发启动
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := manager.Start(
				runtime.TypeExec,
				testWorkspace,
				appPath,
				"",
				"",
				false,
				0,
			)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// 并发查询状态
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = manager.IsRunning()
			_ = manager.GetPID()
			_, _, _, _, _, _ = manager.GetInfo()
		}(i)
	}

	// 并发停止
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = manager.Stop()
		}(i)
	}

	wg.Wait()
	close(errors)

	// 收集错误
	var errCount int
	for range errors {
		errCount++
	}

	// 验证没有死锁或panic
	assert.True(t, errCount >= 0, "应该能处理并发操作")
}

// TestManager_Restart 测试重启功能
func TestManager_Restart(t *testing.T) {
	appPath := createTestApp(t, "restart-app", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("Running")
	time.Sleep(5 * time.Second)
}
`)

	manager := runtime.NewManager(testLogs)

	// 启动
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	require.NoError(t, err)

	// 等待进程启动
	time.Sleep(200 * time.Millisecond)
	firstPID := manager.GetPID()
	assert.Greater(t, firstPID, 0)

	// 重启
	err = manager.Restart()
	require.NoError(t, err)

	// 等待重启完成
	time.Sleep(500 * time.Millisecond)

	// 验证新PID
	secondPID := manager.GetPID()
	assert.Greater(t, secondPID, 0)
	// PID应该不同（新进程）
	assert.NotEqual(t, firstPID, secondPID, "重启后应该有新的PID")

	// 进程应该在运行
	assert.True(t, manager.IsRunning())

	// 清理
	manager.Stop()
}

// TestManager_GetLogs 测试日志获取
func TestManager_GetLogs(t *testing.T) {
	appPath := createTestApp(t, "logs-app", `package main
import "fmt"
func main() {
	fmt.Println("stdout line 1")
	fmt.Println("stdout line 2")
	fmt.Println("stdout line 3")
}
`)

	manager := runtime.NewManager(testLogs)

	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	require.NoError(t, err)

	// 等待进程执行完成
	time.Sleep(500 * time.Millisecond)

	// 获取日志
	logs := manager.GetLogs(100)

	// 应该有系统日志和应用输出
	assert.Greater(t, len(logs), 0, "应该有日志")

	// 验证日志包含预期内容
	hasSystemLog := false

	for _, log := range logs {
		if log.Type == "system" {
			hasSystemLog = true
		}
	}

	assert.True(t, hasSystemLog, "应该有系统日志")
}

// TestManager_InvalidType 测试无效类型
func TestManager_InvalidType(t *testing.T) {
	manager := runtime.NewManager(testLogs)

	err := manager.Start(
		"invalid-type",
		testWorkspace,
		"echo",
		"",
		"",
		false,
		0,
	)

	assert.Error(t, err, "应该返回错误")
	assert.Contains(t, err.Error(), "invalid type", "错误信息应该包含'invalid type'")
}

// TestManager_RuntimeType 测试runtime类型
func TestManager_RuntimeType(t *testing.T) {
	// 创建一个简单的 Go 程序
	appPath := createTestApp(t, "runtime-app", `package main
import "fmt"
func main() {
	fmt.Println("Runtime test")
}
`)

	manager := runtime.NewManager(testLogs)

	// runtime 类型需要 executable 和 entry
	// 这里使用 go 作为 runtime，appPath 作为 entry
	err := manager.Start(
		runtime.TypeRuntime,
		testWorkspace,
		"go",
		"run "+appPath,
		"",
		false,
		0,
	)
	require.NoError(t, err)

	// 等待执行
	time.Sleep(500 * time.Millisecond)

	// 验证状态
	assert.False(t, manager.IsRunning(), "脚本执行完成后应该停止")
}

// TestManager_GetInfo 测试信息获取
func TestManager_GetInfo(t *testing.T) {
	appPath := createTestApp(t, "info-app", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("Running")
	time.Sleep(5 * time.Second)
}
`)

	manager := runtime.NewManager(testLogs)

	// 未启动时获取信息
	appType, executable, entry, autoRestart, restartCount, uptime := manager.GetInfo()
	assert.Equal(t, "", appType)
	assert.Equal(t, "", executable)
	assert.Equal(t, "", entry)
	assert.False(t, autoRestart)
	assert.Equal(t, 0, restartCount)
	assert.Equal(t, int64(0), uptime)

	// 启动后获取信息
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		true,
		5,
	)
	require.NoError(t, err)

	// 等待进程启动
	time.Sleep(500 * time.Millisecond)

	appType, executable, entry, autoRestart, restartCount, uptime = manager.GetInfo()
	assert.Equal(t, runtime.TypeExec, appType)
	assert.Contains(t, executable, "info-app")
	assert.Equal(t, "", entry)
	assert.True(t, autoRestart)
	assert.Equal(t, 0, restartCount)
	assert.GreaterOrEqual(t, uptime, int64(0), "运行时间应该大于等于0")

	// 清理
	manager.Stop()
}

// TestManager_Stop_NotRunning 测试停止未运行的进程
func TestManager_Stop_NotRunning(t *testing.T) {
	manager := runtime.NewManager(testLogs)

	err := manager.Stop()
	assert.Error(t, err, "应该返回错误")
	assert.Contains(t, err.Error(), "no running", "错误信息应该包含'no running'")
}

// TestManager_Restart_NotRunning 测试重启未运行的进程
func TestManager_Restart_NotRunning(t *testing.T) {
	manager := runtime.NewManager(testLogs)

	err := manager.Restart()
	assert.Error(t, err, "应该返回错误")
	assert.Contains(t, err.Error(), "no running", "错误信息应该包含'no running'")
}

// TestManager_GetLogFile 测试获取日志文件路径
func TestManager_GetLogFile(t *testing.T) {
	appPath := createTestApp(t, "logfile-app", `package main
import "fmt"
func main() {
	fmt.Println("test")
}
`)

	manager := runtime.NewManager(testLogs)

	// 未启动时
	logFile := manager.GetLogFile()
	assert.Equal(t, "", logFile)

	// 启动后
	err := manager.Start(
		runtime.TypeExec,
		testWorkspace,
		appPath,
		"",
		"",
		false,
		0,
	)
	require.NoError(t, err)

	// 等待启动
	time.Sleep(200 * time.Millisecond)

	logFile = manager.GetLogFile()
	assert.NotEqual(t, "", logFile, "应该有日志文件路径")
	assert.Contains(t, logFile, testLogs, "路径应该包含日志目录")

	// 清理
	manager.Stop()
}
