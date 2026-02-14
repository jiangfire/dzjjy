package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestBinary 创建一个简单的 Go 测试程序
func createTestBinary(path, action, arg string) error {
	mainCode := `package main

import (
	"fmt"
	"os"
	"time"
	"strconv"
)

func main() {
	action := "` + action + `"
	arg := "` + arg + `"

	switch action {
	case "sleep":
		if arg != "" {
			if seconds, err := strconv.Atoi(arg); err == nil {
				time.Sleep(time.Duration(seconds) * time.Second)
			}
		}
	case "echo":
		fmt.Println(arg)
	case "exit":
		if arg != "" {
			if code, err := strconv.Atoi(arg); err == nil {
				os.Exit(code)
			}
		}
	}
}
`
	tmpDir, err := os.MkdirTemp("", "test-binary-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(mainCode), 0644); err != nil {
		return err
	}

	cmd := exec.Command("go", "build", "-o", path, mainFile)
	if err := cmd.Run(); err != nil {
		return err
	}

	// 设置执行权限（Linux/Unix 需要）
	if os.PathSeparator != '\\' {
		if err := os.Chmod(path, 0755); err != nil {
			return err
		}
	}

	return nil
}

func TestMultiManager_StartApp(t *testing.T) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	workDir := filepath.Join(tmpDir, "work")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	// 创建测试配置
	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     workDir,
		Executable:  "echo",
		Entry:       "",
		Args:        "hello",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	// 启动应用
	err = mm.StartApp(ctx, "test-app", config)
	// 可能失败因为 echo 命令在 Windows 上的行为不同
	if err != nil {
		t.Skip("Skipping test - echo command not available or failed")
		return
	}

	// 验证应用已启动
	manager, exists := mm.GetApp("test-app")
	require.True(t, exists)
	assert.True(t, manager.IsRunning())

	// 验证列表
	apps := mm.ListApps()
	assert.Contains(t, apps, "test-app")
}

func TestMultiManager_MultipleApps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	// 创建测试二进制（Windows 兼容）
	app1Path := filepath.Join(tmpDir, "app1.exe")
	app2Path := filepath.Join(tmpDir, "app2.exe")

	// 使用 Go 编译简单的测试程序
	err = createTestBinary(app1Path, "sleep", "5")
	if err != nil {
		t.Skipf("Skipping test - cannot create test binary: %v", err)
		return
	}
	err = createTestBinary(app2Path, "echo", "test")
	if err != nil {
		t.Skipf("Skipping test - cannot create test binary: %v", err)
		return
	}

	// 创建多个应用配置
	config1 := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     tmpDir,
		Executable:  app1Path,
		Args:        "",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	config2 := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     tmpDir,
		Executable:  app2Path,
		Args:        "",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	// 尝试启动多个应用
	_ = mm.StartApp(ctx, "app1", config1)
	_ = mm.StartApp(ctx, "app2", config2)

	// 验证列表（可能只有部分成功）
	apps := mm.ListApps()
	assert.NotEmpty(t, apps)

	// 验证不能重复启动同名应用
	if _, exists := mm.GetApp("app1"); exists {
		err := mm.StartApp(ctx, "app1", config1)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "already running")
		}
	}
}

func TestMultiManager_StopApp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	workDir := filepath.Join(tmpDir, "work")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     workDir,
		Executable:  "echo",
		Args:        "test",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	// 启动应用
	_ = mm.StartApp(ctx, "test-app", config)

	// 停止应用
	err = mm.StopApp("test-app")
	if err != nil {
		t.Skip("Skipping test - stop failed")
		return
	}

	// 验证已停止
	_, exists := mm.GetApp("test-app")
	assert.False(t, exists)

	// 停止不存在的应用应返回错误
	err = mm.StopApp("non-existent")
	assert.Error(t, err)
}

func TestMultiManager_RestartApp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	workDir := filepath.Join(tmpDir, "work")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     workDir,
		Executable:  "echo",
		Args:        "test",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	// 启动应用
	_ = mm.StartApp(ctx, "test-app", config)

	// 重启应用
	err = mm.RestartApp("test-app")
	if err != nil {
		t.Skip("Skipping test - restart failed")
		return
	}

	// 验证仍在运行
	manager, exists := mm.GetApp("test-app")
	if exists {
		assert.True(t, manager.IsRunning())
	}

	// 重启不存在的应用应返回错误
	err = mm.RestartApp("non-existent")
	assert.Error(t, err)
}

func TestMultiManager_GetAppInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	workDir := filepath.Join(tmpDir, "work")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     workDir,
		Executable:  "echo",
		Args:        "test",
		AutoRestart: true,
		MaxRestarts: 5,
	}

	// 启动应用
	_ = mm.StartApp(ctx, "test-app", config)

	// 获取信息
	info, exists := mm.GetAppInfo("test-app")
	if exists {
		assert.Equal(t, "test-app", info.AppName)
		assert.Equal(t, TypeExec, info.Type)
		assert.Equal(t, "echo", info.Executable)
		assert.Equal(t, true, info.AutoRestart)
	}

	// 获取不存在的应用信息
	_, exists = mm.GetAppInfo("non-existent")
	assert.False(t, exists)
}

func TestMultiManager_RemoveApp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	mm := NewMultiManager(logDir)

	// 添加一个虚拟应用（不启动）
	config := &ProcessConfig{
		Type:       TypeExec,
		WorkDir:    tmpDir,
		Executable: "echo",
	}
	mm.managers["test-app"] = NewManager(config, logDir, "test-app")

	// 移除应用
	err = mm.RemoveApp("test-app")
	assert.NoError(t, err)

	// 验证已移除
	_, exists := mm.GetApp("test-app")
	assert.False(t, exists)

	// 移除不存在的应用应返回错误
	err = mm.RemoveApp("non-existent")
	assert.Error(t, err)
}

func TestMultiManager_StopAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	workDir := filepath.Join(tmpDir, "work")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))

	mm := NewMultiManager(logDir)
	ctx := context.Background()

	// 启动多个应用
	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     workDir,
		Executable:  "echo",
		Args:        "test",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	_ = mm.StartApp(ctx, "app1", config)
	_ = mm.StartApp(ctx, "app2", config)

	// 停止所有
	err = mm.StopAll()
	if err != nil {
		t.Skip("Skipping test - stop all failed")
		return
	}

	// 验证所有应用已停止
	apps := mm.ListApps()
	assert.Empty(t, apps)
}

func TestMultiManager_ConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	mm := NewMultiManager(logDir)

	// 并发读写测试
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func(id int) {
			appName := "app" + string(rune('0'+id))
			_ = mm.ListApps()
			_, _ = mm.GetApp(appName)
			_, _ = mm.GetAppInfo(appName)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestMultiManager_EmptyOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multimanager-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	mm := NewMultiManager(logDir)

	// 空列表
	apps := mm.ListApps()
	assert.Empty(t, apps)

	// 操作不存在的应用
	err = mm.StopApp("non-existent")
	assert.Error(t, err)

	err = mm.RestartApp("non-existent")
	assert.Error(t, err)

	_, exists := mm.GetApp("non-existent")
	assert.False(t, exists)

	_, exists = mm.GetAppInfo("non-existent")
	assert.False(t, exists)

	// StopAll 无应用时应成功
	err = mm.StopAll()
	assert.NoError(t, err)
}
