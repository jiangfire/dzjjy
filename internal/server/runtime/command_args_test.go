package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStartWithConfigPreservesCommandArgumentBoundaries(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker.txt")
	config := &ProcessConfig{
		Type: TypeExec, WorkDir: root,
		Command: []string{os.Args[0], "-test.run=^TestCommandArgumentMarker$", "hello world", marker},
	}
	manager := NewManager(config, root, "args")
	require.NoError(t, manager.StartWithConfig(config))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && string(data) == "ok"
	}, time.Second, 10*time.Millisecond)
}

func TestCommandArgumentMarker(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-2] == "hello world" {
		_ = os.WriteFile(os.Args[len(os.Args)-1], []byte("ok"), 0o600)
	}
}
