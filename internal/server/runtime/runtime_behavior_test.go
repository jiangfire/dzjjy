package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedStateEvent struct {
	eventType string
	appName   string
	config    *ProcessConfig
	pid       int
}

type stateNotifierSpy struct {
	events []capturedStateEvent
}

func (s *stateNotifierSpy) OnAppEvent(eventType string, appName string, config *ProcessConfig, pid int) {
	var configCopy *ProcessConfig
	if config != nil {
		copyValue := *config
		configCopy = &copyValue
	}

	s.events = append(s.events, capturedStateEvent{
		eventType: eventType,
		appName:   appName,
		config:    configCopy,
		pid:       pid,
	})
}

type eventNotifierSpy struct {
	eventType string
	data      map[string]interface{}
}

func (s *eventNotifierSpy) Notify(eventType string, data map[string]interface{}) {
	dataCopy := make(map[string]interface{}, len(data))
	for key, value := range data {
		dataCopy[key] = value
	}

	s.eventType = eventType
	s.data = dataCopy
}

func TestStateEventAdapter_MapsRestartAndLogPath(t *testing.T) {
	notifier := &stateNotifierSpy{}
	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     filepath.Join("C:\\", "apps", "demo"),
		Executable:  "demo.exe",
		AutoRestart: true,
		MaxRestarts: 3,
	}

	adapter := &stateEventAdapter{
		appName:       "demo",
		config:        config,
		stateNotifier: notifier,
	}

	adapter.Notify("start", map[string]interface{}{
		"pid":     101,
		"logPath": filepath.Join("C:\\", "logs", "demo.log"),
	})
	adapter.Notify("restart", map[string]interface{}{
		"new_pid": 202,
	})
	adapter.Notify("config_change", nil)

	require.Len(t, notifier.events, 4)
	assert.Equal(t, "start", notifier.events[0].eventType)
	assert.Equal(t, 101, notifier.events[0].pid)
	assert.Equal(t, "demo", notifier.events[0].appName)

	require.NotNil(t, notifier.events[1].config)
	assert.Equal(t, "log_path", notifier.events[1].eventType)
	assert.Equal(t, filepath.Join("C:\\", "logs", "demo.log"), notifier.events[1].config.WorkDir)

	assert.Equal(t, "restart", notifier.events[2].eventType)
	assert.Equal(t, 202, notifier.events[2].pid)

	require.NotNil(t, notifier.events[3].config)
	assert.Equal(t, "config_change", notifier.events[3].eventType)
	assert.Equal(t, "demo.exe", notifier.events[3].config.Executable)
}

func TestManager_RestoreReadsLogsFromFileAndExposesConfig(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "demo.log")
	require.NoError(t, os.WriteFile(logPath, []byte("line 1\nline 2\nline 3\n"), 0o644))

	config := &ProcessConfig{
		Type:        TypeExec,
		WorkDir:     root,
		Executable:  "demo.exe",
		Args:        "--serve",
		AutoRestart: true,
		MaxRestarts: 5,
	}

	manager := NewManager(nil, root, "demo")
	startTime := time.Now().Add(-2 * time.Second).Unix()
	require.NoError(t, manager.Restore(config, os.Getpid(), startTime, 2, true, logPath))

	logs := manager.GetLogs(2)
	require.Len(t, logs, 2)
	assert.Equal(t, "system", logs[0].Type)
	assert.Equal(t, "line 2", logs[0].Message)
	assert.Equal(t, "line 3", logs[1].Message)

	assert.Equal(t, logPath, manager.GetLogFile())
	assert.Equal(t, os.Getpid(), manager.GetPID())

	appType, executable, entry, autoRestart, restartCount, uptime := manager.GetInfo()
	assert.Equal(t, TypeExec, appType)
	assert.Equal(t, "demo.exe", executable)
	assert.Equal(t, "", entry)
	assert.True(t, autoRestart)
	assert.Equal(t, 2, restartCount)
	assert.GreaterOrEqual(t, uptime, int64(1))

	cfgCopy := manager.GetConfig()
	require.NotNil(t, cfgCopy)
	cfgCopy.Executable = "mutated.exe"
	latestConfig := manager.GetConfig()
	require.NotNil(t, latestConfig)
	assert.Equal(t, "demo.exe", latestConfig.Executable)

	info := manager.GetAppInfo()
	assert.Equal(t, "demo", info.AppName)
	assert.True(t, info.Running)
	assert.Equal(t, os.Getpid(), info.PID)
	assert.Equal(t, 2, info.RestartCount)

	manager.SetAppName("renamed")
	assert.Equal(t, "renamed", manager.GetAppName())
}

func TestManagerNotifyAndEventAdapterForwarding(t *testing.T) {
	notifier := &eventNotifierSpy{}
	manager := NewManager(nil, t.TempDir(), "demo")
	manager.SetEventAdapter(&EventAdapter{Notifier: notifier})
	manager.SetAppName("renamed")

	manager.notify("start", map[string]interface{}{"pid": 303})

	assert.Equal(t, "start", notifier.eventType)
	assert.Equal(t, 303, notifier.data["pid"])
	assert.Equal(t, "renamed", notifier.data["app_name"])
	assert.Contains(t, notifier.data, "timestamp")
}

func TestMultiManager_RestoreAppsSnapshotAndConfigCopies(t *testing.T) {
	notifier := &stateNotifierSpy{}
	mmWithState := NewMultiManagerWithState(t.TempDir(), notifier)
	require.NotNil(t, mmWithState)

	logDir := t.TempDir()
	mm := NewMultiManager(logDir)
	mm.SetStateNotifier(notifier)

	apps := map[string]*ProcessConfig{
		"app1": {
			Type:        TypeExec,
			WorkDir:     filepath.Join(logDir, "app1"),
			Executable:  "app1.exe",
			Args:        "--one",
			AutoRestart: true,
			MaxRestarts: 3,
		},
		"app2": {
			Type:        TypeRuntime,
			WorkDir:     filepath.Join(logDir, "app2"),
			Executable:  "python",
			Entry:       "main.py",
			Args:        "--two",
			AutoRestart: false,
			MaxRestarts: 0,
		},
	}

	require.NoError(t, mm.RestoreApps(apps))
	assert.True(t, mm.HasApp("app1"))
	assert.True(t, mm.HasApp("app2"))

	snapshot := mm.GetStateSnapshot()
	require.Len(t, snapshot, 2)
	require.NotNil(t, snapshot["app1"])
	snapshot["app1"].Executable = "mutated.exe"

	info, exists := mm.GetAppInfo("app1")
	require.True(t, exists)
	assert.Equal(t, "app1.exe", info.Executable)

	configCopy, exists := mm.GetProcessConfig("app1")
	require.True(t, exists)
	configCopy.Args = "--mutated"

	freshConfig, exists := mm.GetProcessConfig("app1")
	require.True(t, exists)
	assert.Equal(t, "--one", freshConfig.Args)
}
