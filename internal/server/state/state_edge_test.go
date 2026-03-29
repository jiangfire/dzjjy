package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleStateData() *StateData {
	return &StateData{
		Apps: map[string]*AppState{
			"app": {
				Config: &ProcessConfig{
					Type:        "exec",
					WorkDir:     "/workspace/app",
					Executable:  "app.exe",
					Entry:       "",
					Args:        "--port 8080",
					AutoRestart: true,
					MaxRestarts: 3,
				},
				PID:          1234,
				StartTime:    1704067200,
				RestartCount: 1,
				Status:       StatusRunning,
				WorkPath:     "/workspace/app",
				LogPath:      "/logs/app.log",
			},
		},
	}
}

func corruptChecksum(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var stateFile StateFile
	require.NoError(t, json.Unmarshal(data, &stateFile))
	stateFile.Checksum = "corrupted"

	encoded, err := json.MarshalIndent(stateFile, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
}

func TestStateStore_Load_NoStateFile(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	loaded, err := store.Load()

	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestStateStore_Persist_FailsWhenLockExists(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)
	require.NoError(t, os.WriteFile(stateFile+".lock", []byte("locked"), 0o600))

	err := store.Persist(sampleStateData())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock")
}

func TestStateStore_Load_RestoresFromBackup(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)

	require.NoError(t, store.Persist(sampleStateData()))
	require.NoError(t, store.Persist(sampleStateData()))
	corruptChecksum(t, stateFile)

	loaded, err := store.Load()

	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Contains(t, loaded.Data.Apps, "app")

	reloaded, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Contains(t, reloaded.Data.Apps, "app")
}

func TestStateStore_Load_ErrorsWhenBackupAlsoCorrupted(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)

	require.NoError(t, store.Persist(sampleStateData()))
	require.NoError(t, store.Persist(sampleStateData()))
	corruptChecksum(t, stateFile)

	matches, err := filepath.Glob(stateFile + ".backup.*")
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	corruptChecksum(t, matches[len(matches)-1])

	loaded, err := store.Load()

	require.Error(t, err)
	assert.Nil(t, loaded)
	assert.Contains(t, err.Error(), "backup file is also corrupted")
}

func TestStateStore_AtomicWriteFile_MissingParentDir(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	target := filepath.Join(t.TempDir(), "missing", "state.json")

	err := store.AtomicWriteFile(target, []byte(`{"ok":true}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write temp file")
}

func TestStateStore_ExistsReflectsPersistAndClear(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)

	assert.False(t, store.Exists())

	require.NoError(t, store.Persist(sampleStateData()))
	assert.True(t, store.Exists())

	require.NoError(t, store.Clear())
	assert.False(t, store.Exists())
}

func TestSyncManager_Close_PersistsCurrentState(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)

	syncManager.OnAppEvent(AppEvent{
		Type:    "register",
		AppName: "app",
		Config:  sampleStateData().Apps["app"].Config,
	})
	syncManager.UpdateLogPath("app", "/logs/app.log")
	syncManager.Close()

	require.Eventually(t, func() bool {
		loaded, err := store.Load()
		if err != nil || loaded == nil {
			return false
		}
		app := loaded.Data.Apps["app"]
		return app != nil && app.LogPath == "/logs/app.log"
	}, 2*time.Second, 20*time.Millisecond)
}

func TestSyncManager_ConcurrentEvents_MultipleApps(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	defer syncManager.Close()

	const appCount = 32
	var wg sync.WaitGroup

	for i := 0; i < appCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			appName := fmt.Sprintf("app-%02d", idx)
			syncManager.OnAppEvent(AppEvent{
				Type:    "register",
				AppName: appName,
				Config: &ProcessConfig{
					Type:        "exec",
					WorkDir:     filepath.Join(root, appName),
					Executable:  "app.exe",
					AutoRestart: idx%2 == 0,
					MaxRestarts: idx,
				},
			})
			syncManager.OnAppEvent(AppEvent{
				Type:    "start",
				AppName: appName,
				PID:     1000 + idx,
			})
		}(i)
	}

	wg.Wait()
	require.NoError(t, syncManager.ManualPersist())

	currentState := syncManager.GetState()
	require.Len(t, currentState.Apps, appCount)

	for i := 0; i < appCount; i++ {
		appName := fmt.Sprintf("app-%02d", i)
		app := currentState.Apps[appName]
		require.NotNil(t, app)
		assert.Equal(t, 1000+i, app.PID)
		assert.Equal(t, StatusRunning, app.Status)
	}
}
