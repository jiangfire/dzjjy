package state

import (
	"github.com/jiangfire/dzjjy/internal/server/runtime"
)

// ManagerEventAdapter 将Manager事件转换为状态同步事件
type ManagerEventAdapter struct {
	syncManager *SyncManager
	appName     string
	config      *runtime.ProcessConfig
}

// NewManagerEventAdapter 创建事件适配器
func NewManagerEventAdapter(syncManager *SyncManager, appName string, config *runtime.ProcessConfig) *ManagerEventAdapter {
	return &ManagerEventAdapter{
		syncManager: syncManager,
		appName:     appName,
		config:      config,
	}
}

// Notify 实现runtime.EventNotifier接口
func (a *ManagerEventAdapter) Notify(eventType string, data map[string]interface{}) {
	// 转换Manager的事件为State事件
	var stateEvent AppEvent

	switch eventType {
	case "start":
		stateEvent = AppEvent{
			Type:    "start",
			AppName: a.appName,
			PID:     getInt(data, "pid"),
		}

	case "stop":
		stateEvent = AppEvent{
			Type:    "stop",
			AppName: a.appName,
			PID:     getInt(data, "pid"),
		}

	case "restart":
		stateEvent = AppEvent{
			Type:    "restart",
			AppName: a.appName,
			PID:     getInt(data, "new_pid"),
		}

	case "register":
		stateEvent = AppEvent{
			Type:    "register",
			AppName: a.appName,
			Config:  convertConfig(a.config),
		}

	case "config_change":
		stateEvent = AppEvent{
			Type:    "config_change",
			AppName: a.appName,
			Config:  convertConfig(a.config),
		}

	case "log_path":
		if logPath, ok := data["logPath"].(string); ok {
			stateEvent = AppEvent{
				Type:    "log_path",
				AppName: a.appName,
				Config: &ProcessConfig{
					WorkDir: logPath, // 临时使用WorkDir存储logPath
				},
			}
		}
	}

	if stateEvent.Type != "" {
		a.syncManager.OnAppEvent(stateEvent)
	}
}

// convertConfig 转换runtime.ProcessConfig到state.ProcessConfig
func convertConfig(config *runtime.ProcessConfig) *ProcessConfig {
	if config == nil {
		return nil
	}
	return &ProcessConfig{
		Type:        config.Type,
		WorkDir:     config.WorkDir,
		Executable:  config.Executable,
		Entry:       config.Entry,
		Args:        config.Args,
		AutoRestart: config.AutoRestart,
		MaxRestarts: config.MaxRestarts,
	}
}

// getInt 从map中安全地获取int值
func getInt(data map[string]interface{}, key string) int {
	if val, ok := data[key]; ok {
		if intVal, ok := val.(int); ok {
			return intVal
		}
	}
	return 0
}
