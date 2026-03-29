package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	serverruntime "github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerEventAdapter_MapsRuntimeEvents(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(filepath.Join(root, "state.json"))
	syncManager := NewSyncManager(store)
	defer syncManager.Close()

	config := &serverruntime.ProcessConfig{
		Type:        serverruntime.TypeExec,
		WorkDir:     filepath.Join(root, "work"),
		Executable:  "demo.exe",
		Args:        "--port 8080",
		AutoRestart: true,
		MaxRestarts: 3,
	}

	adapter := NewManagerEventAdapter(syncManager, "demo", config)
	adapter.Notify("register", nil)

	config.Args = "--port 9090"
	adapter.Notify("config_change", nil)
	adapter.Notify("start", map[string]interface{}{"pid": 101})
	adapter.Notify("log_path", map[string]interface{}{"logPath": filepath.Join(root, "logs", "demo.log")})
	adapter.Notify("restart", map[string]interface{}{"new_pid": 202})

	app := syncManager.GetState().Apps["demo"]
	require.NotNil(t, app)
	require.NotNil(t, app.Config)
	assert.Equal(t, "--port 9090", app.Config.Args)
	assert.Equal(t, 202, app.PID)
	assert.Equal(t, 1, app.RestartCount)
	assert.Equal(t, StatusRunning, app.Status)
	assert.Equal(t, filepath.Join(root, "logs", "demo.log"), app.LogPath)
}

func TestSyncManager_StateTransitionsAndDeregister(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(filepath.Join(root, "state.json"))
	syncManager := NewSyncManager(store)
	defer syncManager.Close()

	initialConfig := &ProcessConfig{
		Type:        "exec",
		WorkDir:     filepath.Join(root, "app"),
		Executable:  "demo.exe",
		AutoRestart: true,
		MaxRestarts: 2,
	}
	updatedConfig := &ProcessConfig{
		Type:        "exec",
		WorkDir:     filepath.Join(root, "app"),
		Executable:  "demo-v2.exe",
		Args:        "--debug",
		AutoRestart: false,
		MaxRestarts: 0,
	}

	syncManager.OnAppEvent(AppEvent{Type: "register", AppName: "demo", Config: initialConfig})
	syncManager.OnAppEvent(AppEvent{Type: "config_change", AppName: "demo", Config: updatedConfig})
	syncManager.OnAppEvent(AppEvent{Type: "start", AppName: "demo", PID: 321})

	runningState := syncManager.GetState().Apps["demo"]
	require.NotNil(t, runningState)
	require.NotNil(t, runningState.Config)
	assert.Equal(t, "demo-v2.exe", runningState.Config.Executable)
	assert.Equal(t, "--debug", runningState.Config.Args)
	assert.Equal(t, 321, runningState.PID)
	assert.Equal(t, StatusRunning, runningState.Status)

	syncManager.OnAppEvent(AppEvent{Type: "failed", AppName: "demo"})
	failedState := syncManager.GetState().Apps["demo"]
	require.NotNil(t, failedState)
	assert.Equal(t, StatusFailed, failedState.Status)
	assert.Equal(t, 0, failedState.PID)

	syncManager.OnAppEvent(AppEvent{Type: "stop", AppName: "demo"})
	stoppedState := syncManager.GetState().Apps["demo"]
	require.NotNil(t, stoppedState)
	assert.Equal(t, StatusStopped, stoppedState.Status)
	assert.Equal(t, 0, stoppedState.PID)

	syncManager.OnAppEvent(AppEvent{Type: "deregister", AppName: "demo"})
	assert.NotContains(t, syncManager.GetState().Apps, "demo")
}

func TestRestoreManager_RecoverRunningAppPreservesMetadataAndLookup(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	defer syncManager.Close()

	cmd := startRestorableProcess(t)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	expectedStartTime := time.Now().Add(-3 * time.Second).Unix()
	appState := &AppState{
		Config: &ProcessConfig{
			Type:        "exec",
			WorkDir:     workDir,
			Executable:  "demo.exe",
			AutoRestart: true,
			MaxRestarts: 5,
		},
		PID:          cmd.Process.Pid,
		StartTime:    expectedStartTime,
		RestartCount: 4,
		Status:       StatusRunning,
	}

	restoreManager := NewRestoreManager(store, syncManager)
	require.NoError(t, restoreManager.recoverRunningApp("self", appState))

	loadedAppState, err := restoreManager.GetAppState("self")
	require.NoError(t, err)
	require.NotNil(t, loadedAppState)
	assert.Equal(t, StatusRunning, loadedAppState.Status)
	assert.Equal(t, cmd.Process.Pid, loadedAppState.PID)
	assert.GreaterOrEqual(t, loadedAppState.StartTime, expectedStartTime)

	apps := restoreManager.ListApps()
	require.Contains(t, apps, "self")
	assert.Equal(t, cmd.Process.Pid, apps["self"].PID)
}

func BenchmarkStateStorePersist100Apps(b *testing.B) {
	root := b.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)
	data := benchmarkStateData(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Persist(data); err != nil {
			b.Fatalf("persist failed: %v", err)
		}

		b.StopTimer()
		matches, err := filepath.Glob(stateFile + ".backup.*")
		if err != nil {
			b.Fatalf("glob backups failed: %v", err)
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				b.Fatalf("remove backup failed: %v", err)
			}
		}
		b.StartTimer()
	}
}

func benchmarkStateData(appCount int) *StateData {
	apps := make(map[string]*AppState, appCount)
	for i := 0; i < appCount; i++ {
		appName := fmt.Sprintf("app-%03d", i)
		apps[appName] = &AppState{
			Config: &ProcessConfig{
				Type:        "exec",
				WorkDir:     filepath.Join("C:\\bench", appName),
				Executable:  "demo.exe",
				Args:        "--bench",
				AutoRestart: i%2 == 0,
				MaxRestarts: 5,
			},
			PID:          1000 + i,
			StartTime:    1704067200,
			RestartCount: i % 3,
			Status:       StatusRunning,
			WorkPath:     filepath.Join("C:\\bench", appName),
			LogPath:      filepath.Join("C:\\bench", "logs", appName+".log"),
		}
	}

	return &StateData{Apps: apps}
}

func startRestorableProcess(t *testing.T) *exec.Cmd {
	t.Helper()

	var cmd *exec.Cmd
	if isWindows() {
		cmd = exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 10")
	} else {
		cmd = exec.Command("sh", "-c", "sleep 10")
	}

	require.NoError(t, cmd.Start())
	return cmd
}
