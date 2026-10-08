package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestManager_StartInvalidTypeDoesNotLeakLock(t *testing.T) {
	manager := NewManager(nil, t.TempDir(), "invalid-type")
	require.Error(t, manager.Start("invalid", t.TempDir(), "ignored", "", "", false, 0))

	started := make(chan error, 1)
	go func() {
		started <- manager.Start(TypeExec, t.TempDir(), os.Args[0], "", "", false, 0)
	}()

	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("valid start blocked after invalid type")
	}
	require.NoError(t, manager.Stop())
}
