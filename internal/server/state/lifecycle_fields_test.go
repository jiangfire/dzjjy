package state

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppStatePersistsDesiredLifecycleMetadata(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	syncManager := NewSyncManager(store)
	defer syncManager.Close()
	syncManager.ReplaceState(&StateData{Apps: map[string]*AppState{
		"docs": {
			Status: StatusExpired, DesiredConfig: "kind: StaticSite", Generation: 3,
			Backend: "static", Port: 8081, ExpiresAt: 1700000000,
			TerminationReason: "ttl", ProcessStartID: "identity",
		},
	}})
	require.NoError(t, syncManager.ManualPersist())
	loaded, err := store.Load()
	require.NoError(t, err)
	state := loaded.Data.Apps["docs"]
	require.Equal(t, "kind: StaticSite", state.DesiredConfig)
	require.Equal(t, uint64(3), state.Generation)
	require.Equal(t, "static", state.Backend)
	require.Equal(t, 8081, state.Port)
	require.Equal(t, int64(1700000000), state.ExpiresAt)
	require.Equal(t, "ttl", state.TerminationReason)
	require.Equal(t, "identity", state.ProcessStartID)
}
