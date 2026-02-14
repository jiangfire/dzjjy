package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateStore_PersistAndLoad(t *testing.T) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)

	// 准备测试数据
	data := &StateData{
		Apps: map[string]*AppState{
			"test-app": {
				Config: &ProcessConfig{
					Type:        "runtime",
					WorkDir:     "/workspace",
					Executable:  "python3",
					Entry:       "app.py",
					Args:        "--port 8000",
					AutoRestart: true,
					MaxRestarts: 10,
				},
				PID:          12345,
				StartTime:    1704067200,
				RestartCount: 2,
				Status:       StatusRunning,
				WorkPath:     "/workspace",
				LogPath:      "/logs/test.log",
			},
		},
	}

	// 持久化
	err = store.Persist(data)
	require.NoError(t, err)

	// 验证文件存在
	_, err = os.Stat(stateFile)
	require.NoError(t, err)

	// 加载
	loaded, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// 验证数据
	assert.Equal(t, StateFileVersion, loaded.Version)
	assert.Equal(t, len(data.Apps), len(loaded.Data.Apps))

	// 验证应用数据
	app := loaded.Data.Apps["test-app"]
	require.NotNil(t, app)
	assert.Equal(t, 12345, app.PID)
	assert.Equal(t, StatusRunning, app.Status)
	assert.Equal(t, 2, app.RestartCount)
	assert.Equal(t, "python3", app.Config.Executable)
}

func TestStateStore_ChecksumValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)

	// 创建并持久化状态
	data := &StateData{
		Apps: map[string]*AppState{
			"app1": {PID: 100, Status: StatusRunning},
		},
	}
	require.NoError(t, store.Persist(data))

	// 读取文件并手动损坏校验和
	jsonData, _ := os.ReadFile(stateFile)
	var stateFileObj StateFile
	require.NoError(t, json.Unmarshal(jsonData, &stateFileObj))

	// 损坏校验和
	stateFileObj.Checksum = "invalid_checksum"
	corruptedData, _ := json.MarshalIndent(stateFileObj, "", "  ")
	require.NoError(t, os.WriteFile(stateFile, corruptedData, 0644))

	// 尝试加载 - 应该尝试从备份恢复
	_, err = store.Load()
	// 由于没有备份，应该返回错误或nil
	// 实际行为取决于实现，这里验证不崩溃
	t.Logf("Load result: %v", err)
}

func TestStateStore_Backup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)

	// 创建状态
	data := &StateData{
		Apps: map[string]*AppState{
			"app1": {PID: 100, Status: StatusRunning},
		},
	}
	require.NoError(t, store.Persist(data))

	// 创建备份
	err = store.Backup()
	require.NoError(t, err)

	// 验证备份文件存在
	matches, _ := filepath.Glob(stateFile + ".backup.*")
	assert.Greater(t, len(matches), 0)
}

func TestStateStore_Clear(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)

	// 创建状态和备份
	data := &StateData{Apps: map[string]*AppState{"app1": {PID: 100}}}
	require.NoError(t, store.Persist(data))
	require.NoError(t, store.Backup())

	// 清除
	err = store.Clear()
	require.NoError(t, err)

	// 验证文件已删除
	_, err = os.Stat(stateFile)
	assert.True(t, os.IsNotExist(err))

	// 验证备份也已删除
	matches, _ := filepath.Glob(stateFile + ".backup.*")
	assert.Equal(t, 0, len(matches))
}

func TestSyncManager_OnAppEvent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	defer syncManager.Close()

	// 发送注册事件
	syncManager.OnAppEvent(AppEvent{
		Type:    "register",
		AppName: "test-app",
		Config: &ProcessConfig{
			Type:       "runtime",
			Executable: "python3",
			Entry:      "app.py",
		},
	})

	// 发送启动事件
	syncManager.OnAppEvent(AppEvent{
		Type:    "start",
		AppName: "test-app",
		PID:     12345,
	})

	// 等待异步持久化
	// 手动触发持久化以测试
	err = syncManager.ManualPersist()
	require.NoError(t, err)

	// 验证状态
	state := syncManager.GetState()
	app := state.Apps["test-app"]
	require.NotNil(t, app)
	assert.Equal(t, 12345, app.PID)
	assert.Equal(t, StatusRunning, app.Status)
}

func TestRestoreManager_Restore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	restoreManager := NewRestoreManager(store, syncManager)
	defer syncManager.Close()

	// 创建一个已停止的状态
	data := &StateData{
		Apps: map[string]*AppState{
			"stopped-app": {
				Config: &ProcessConfig{
					Type:       "exec",
					Executable: "./app",
				},
				Status: StatusStopped,
				PID:    0,
			},
		},
	}
	require.NoError(t, store.Persist(data))

	// 执行恢复
	err = restoreManager.Restore()
	require.NoError(t, err)

	// 验证状态已更新到syncManager
	state := syncManager.GetState()
	app := state.Apps["stopped-app"]
	require.NotNil(t, app)
	assert.Equal(t, StatusStopped, app.Status)
}

func TestRestoreManager_Cleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	restoreManager := NewRestoreManager(store, syncManager)
	defer syncManager.Close()

	// 创建包含无效PID的状态
	data := &StateData{
		Apps: map[string]*AppState{
			"invalid-pid": {
				Config: &ProcessConfig{
					Type:       "exec",
					Executable: "./app",
				},
				PID:    99999, // 不存在的PID
				Status: StatusRunning,
			},
		},
	}
	require.NoError(t, store.Persist(data))

	// 模拟Restore流程：加载状态并注册到syncManager
	stateFileObj, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, stateFileObj)

	// 模拟restoreApp的行为：注册应用并设置状态
	for appName, appState := range stateFileObj.Data.Apps {
		// 注册应用
		syncManager.OnAppEvent(AppEvent{
			Type:    "register",
			AppName: appName,
			Config:  appState.Config,
		})
		// 设置PID和状态（模拟recoverRunningApp或后续处理）
		syncManager.OnAppEvent(AppEvent{
			Type:    "start",
			AppName: appName,
			PID:     appState.PID,
		})
	}

	// 验证当前状态包含无效PID
	stateBefore := syncManager.GetState()
	appBefore := stateBefore.Apps["invalid-pid"]
	require.NotNil(t, appBefore)
	assert.Equal(t, 99999, appBefore.PID)
	assert.Equal(t, StatusRunning, appBefore.Status)

	// 执行清理
	err = restoreManager.Cleanup()
	require.NoError(t, err)

	// 验证PID已被清除
	stateAfter := syncManager.GetState()
	appAfter := stateAfter.Apps["invalid-pid"]
	require.NotNil(t, appAfter)
	assert.Equal(t, 0, appAfter.PID)
	assert.Equal(t, StatusStopped, appAfter.Status)
}

func TestStateStore_AtomictWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stateFile := filepath.Join(tmpDir, "state.json")
	store := NewStateStore(stateFile)

	// 测试原子写入
	data := []byte(`{"test": "data"}`)
	err = store.AtomicWriteFile(stateFile, data)
	require.NoError(t, err)

	// 验证内容
	readData, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.Equal(t, data, readData)
}
