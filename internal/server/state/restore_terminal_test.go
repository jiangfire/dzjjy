package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func persistAppsForRestore(t *testing.T, apps map[string]*AppState) (*RestoreManager, *SyncManager) {
	t.Helper()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	store := NewStateStore(stateFile)
	syncManager := NewSyncManager(store)
	t.Cleanup(func() { syncManager.Close() })
	restoreManager := NewRestoreManager(store, syncManager)
	require.NoError(t, store.Persist(&StateData{Apps: apps}))
	return restoreManager, syncManager
}

func TestRestorePreservesExpiredTerminalStatus(t *testing.T) {
	restoreManager, syncManager := persistAppsForRestore(t, map[string]*AppState{
		"expired-app": {
			Config: &ProcessConfig{Type: "exec", Executable: "./app", AutoRestart: true},
			Status: StatusExpired, PID: 123, TerminationReason: "ttl",
			Backend: "process",
		},
	})

	require.NoError(t, restoreManager.Restore())

	app := syncManager.GetState().Apps["expired-app"]
	require.NotNil(t, app)
	assert.Equal(t, StatusExpired, app.Status, "expired is terminal and must survive restore")
	assert.Equal(t, 0, app.PID)
	assert.Equal(t, "ttl", app.TerminationReason)
}

func TestRestoreDoesNotAdoptReusedPID(t *testing.T) {
	// PID 存活但启动身份与持久化值不符：视为 PID 已被复用，绝不接管。
	restoreManager, syncManager := persistAppsForRestore(t, map[string]*AppState{
		"hijacked": {
			Config: &ProcessConfig{Type: "exec", Executable: "./app", AutoRestart: false},
			Status: StatusRunning, PID: os.Getpid(), ProcessStartID: "definitely-not-this-process",
			Backend: "process",
		},
	})

	require.NoError(t, restoreManager.Restore())

	app := syncManager.GetState().Apps["hijacked"]
	require.NotNil(t, app)
	assert.Equal(t, StatusStopped, app.Status, "a reused PID must not be adopted as running")
	assert.Equal(t, 0, app.PID)
	assert.False(t, app.IdentityVerified)
}

func TestRestoreAdoptsWithoutKillAuthorityWhenIdentityMissing(t *testing.T) {
	restoreManager, syncManager := persistAppsForRestore(t, map[string]*AppState{
		"legacy": {
			Config: &ProcessConfig{Type: "exec", Executable: "./app"},
			Status: StatusRunning, PID: os.Getpid(),
			Backend: "process",
		},
	})

	require.NoError(t, restoreManager.Restore())

	app := syncManager.GetState().Apps["legacy"]
	require.NotNil(t, app)
	assert.Equal(t, StatusRunning, app.Status)
	assert.False(t, app.IdentityVerified, "legacy state without identity must adopt without kill authority")
}
