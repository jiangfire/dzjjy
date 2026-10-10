package state_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/state"
)

func newTestSyncManager(t *testing.T) *state.SyncManager {
	t.Helper()
	store := state.NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	manager := state.NewSyncManager(store)
	t.Cleanup(manager.Close)
	return manager
}

// TestClonePreservesPhase3Fields guards the field-by-field clone in
// cloneStateData: any AppState field that is not copied there disappears from
// every clone, so a newly added field would be silently dropped in
// production even while its own unit tests pass.
func TestClonePreservesPhase3Fields(t *testing.T) {
	manager := newTestSyncManager(t)

	original := &state.AppState{
		Backend:            "docker",
		Generation:         4,
		Status:             state.StatusRunning,
		Isolator:           "runsc",
		ExposureToken:      "/_dzjjy/0123456789abcdef0123456789abcdef",
		IdleTimeoutSeconds: 300,
		Suspended:          true,
		LastActivityUnix:   1700000000,
		PublishedPorts: []state.PublishedPort{
			{Container: 8000, Host: 49153},
			{Container: 9000, Host: 49154},
		},
	}
	manager.ReplaceApp("web", original)

	cloned := manager.GetState().Apps["web"]
	require.NotNil(t, cloned)
	assert.Equal(t, "runsc", cloned.Isolator)
	assert.Equal(t, "/_dzjjy/0123456789abcdef0123456789abcdef", cloned.ExposureToken)
	assert.Equal(t, 300, cloned.IdleTimeoutSeconds)
	assert.True(t, cloned.Suspended)
	assert.Equal(t, int64(1700000000), cloned.LastActivityUnix)
	assert.Equal(t, original.PublishedPorts, cloned.PublishedPorts)
}

// TestCloneDoesNotAliasPublishedPorts proves the copy is deep: mutating the
// clone must not affect the stored state.
func TestCloneDoesNotAliasPublishedPorts(t *testing.T) {
	manager := newTestSyncManager(t)
	manager.ReplaceApp("web", &state.AppState{
		PublishedPorts: []state.PublishedPort{{Container: 8000, Host: 49153}},
	})

	cloned := manager.GetState().Apps["web"]
	require.Len(t, cloned.PublishedPorts, 1)
	cloned.PublishedPorts[0].Host = 1

	fresh := manager.GetState().Apps["web"]
	require.Len(t, fresh.PublishedPorts, 1)
	assert.Equal(t, 49153, fresh.PublishedPorts[0].Host, "the clone must not alias stored state")
}

// TestPhase3FieldsSurviveJSONRoundTrip confirms the new fields are persisted,
// which is what lets a restarted server resume an idle app or re-publish a
// token URL.
func TestPhase3FieldsSurviveJSONRoundTrip(t *testing.T) {
	original := state.AppState{
		Backend:            "static",
		Isolator:           "runsc",
		ExposureToken:      "/_dzjjy/abc",
		IdleTimeoutSeconds: 120,
		Suspended:          true,
		LastActivityUnix:   42,
		PublishedPorts:     []state.PublishedPort{{Container: 80, Host: 8080}},
	}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded state.AppState
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, original.Isolator, decoded.Isolator)
	assert.Equal(t, original.ExposureToken, decoded.ExposureToken)
	assert.Equal(t, original.IdleTimeoutSeconds, decoded.IdleTimeoutSeconds)
	assert.Equal(t, original.Suspended, decoded.Suspended)
	assert.Equal(t, original.LastActivityUnix, decoded.LastActivityUnix)
	assert.Equal(t, original.PublishedPorts, decoded.PublishedPorts)
}

// TestIdentityVerifiedIsNotPersisted keeps the restore-only flag out of the
// state file, matching its json:"-" contract.
func TestIdentityVerifiedIsNotPersisted(t *testing.T) {
	encoded, err := json.Marshal(state.AppState{IdentityVerified: true})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "IdentityVerified")
}
