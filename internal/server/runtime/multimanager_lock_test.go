package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiManagerLifecycleLocksArePerApp(t *testing.T) {
	manager := NewMultiManager(t.TempDir())
	first := manager.appMutex("one")
	second := manager.appMutex("two")

	require.Same(t, first, manager.appMutex("one"))
	require.NotSame(t, first, second)
}
