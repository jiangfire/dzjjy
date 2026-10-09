package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// collectedNotifier records every event with its reason for terminal-state assertions.
type collectedNotifier struct {
	mu     sync.Mutex
	events []string
	reason map[string]string
}

func (c *collectedNotifier) Notify(eventType string, data map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, eventType)
	if c.reason == nil {
		c.reason = make(map[string]string)
	}
	if reason, ok := data["reason"].(string); ok {
		c.reason[eventType] = reason
	}
}

func (c *collectedNotifier) hasEvent(eventType, reason string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason[eventType] == reason
}

// TestFailingProcessHelper exits with a non-zero status; it only runs when
// re-executed by the manager as the child process.
func TestFailingProcessHelper(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "fail-marker" {
		os.Exit(1)
	}
}

// TestLongRunningMarkerHelper writes a marker and then stays alive so restart
// behavior can be observed.
func TestLongRunningMarkerHelper(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "live-marker" {
		_ = os.WriteFile(os.Args[len(os.Args)-2], []byte("ok"), 0o600)
		time.Sleep(30 * time.Second)
	}
}

func TestMonitorMaxRestartsEmitsFailedTerminalEvent(t *testing.T) {
	root := t.TempDir()
	config := &ProcessConfig{
		Type: TypeExec, WorkDir: root,
		Command:     []string{os.Args[0], "-test.run=^TestFailingProcessHelper$", "fail-marker"},
		AutoRestart: true, MaxRestarts: 1,
	}
	manager := NewManager(config, root, "max-restart")
	notifier := &collectedNotifier{}
	manager.SetEventAdapter(&EventAdapter{AppName: "max-restart", Notifier: notifier})
	require.NoError(t, manager.StartWithConfig(config))

	require.Eventually(t, func() bool {
		return notifier.hasEvent("failed", "max_restart")
	}, 10*time.Second, 50*time.Millisecond, "max restart limit must emit a failed terminal event")

	require.False(t, manager.IsRunning())
	require.Equal(t, 0, manager.GetPID())
}

func TestRestartPreservesCommandArguments(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker.txt")
	config := &ProcessConfig{
		Type: TypeExec, WorkDir: root,
		Command: []string{os.Args[0], "-test.run=^TestLongRunningMarkerHelper$", marker, "live-marker"},
	}
	manager := NewManager(config, root, "restart-args")
	require.NoError(t, manager.StartWithConfig(config))
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && string(data) == "ok"
	}, 5*time.Second, 20*time.Millisecond)

	// 删除 marker 后重启：新进程只有携带完整参数数组才会重新写出 marker。
	require.NoError(t, os.Remove(marker))
	require.NoError(t, manager.Restart())
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && string(data) == "ok"
	}, 5*time.Second, 20*time.Millisecond, "restart must keep the command argument array")

	require.NoError(t, manager.Stop())
}

func TestStopRefusesToKillUnverifiedRecoveredProcess(t *testing.T) {
	// 以本测试进程作为"身份未验证的恢复进程"：Stop 若误杀，测试进程会直接终止。
	root := t.TempDir()
	config := &ProcessConfig{Type: TypeExec, WorkDir: root, Executable: "recovered"}
	manager := NewManager(config, root, "unverified")
	require.NoError(t, manager.Restore(config, os.Getpid(), time.Now().Unix(), 0, true, "", false))
	require.True(t, manager.IsRunning())

	require.NoError(t, manager.Stop())
	// 到达这里即证明自身进程未被杀死。
	require.Equal(t, os.Getpid(), os.Getpid())
}

func TestStopKillsVerifiedRecoveredProcess(t *testing.T) {
	root := t.TempDir()
	// 正控制组：身份验证通过的恢复进程可以被停止。
	helper := exec.Command(os.Args[0], "-test.run=^TestLongRunningMarkerHelper$", filepath.Join(root, "m.txt"), "live-marker")
	require.NoError(t, helper.Start())
	helperDone := make(chan struct{})
	go func() { _ = helper.Wait(); close(helperDone) }()

	config := &ProcessConfig{Type: TypeExec, WorkDir: root, Executable: "recovered"}
	manager := NewManager(config, root, "verified")
	require.NoError(t, manager.Restore(config, helper.Process.Pid, time.Now().Unix(), 0, true, "", true))
	require.True(t, manager.IsRunning())

	require.NoError(t, manager.Stop())
	select {
	case <-helperDone:
	case <-time.After(3 * time.Second):
		t.Fatal("verified recovered process must be terminated by stop")
	}
}
