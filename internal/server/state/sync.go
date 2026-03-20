package state

import (
	"sync"
	"time"

	"log/slog"
)

// AppEvent 应用事件
type AppEvent struct {
	Type      string         `json:"type"`      // 事件类型
	AppName   string         `json:"app_name"`  // 应用名称
	Config    *ProcessConfig `json:"config"`    // 配置（用于register/config_change）
	PID       int            `json:"pid"`       // 进程ID（用于start/restart）
	Timestamp int64          `json:"timestamp"` // 时间戳
}

// SyncManager 状态同步管理器
type SyncManager struct {
	store       *StateStore
	state       *StateData
	mu          sync.RWMutex
	persistChan chan AppEvent
	done        chan struct{}
	log         *slog.Logger
}

// NewSyncManager 创建同步管理器
func NewSyncManager(store *StateStore) *SyncManager {
	sm := &SyncManager{
		store:       store,
		state:       &StateData{Apps: make(map[string]*AppState)},
		persistChan: make(chan AppEvent, 100), // 缓冲100个事件
		done:        make(chan struct{}),
		log:         slog.Default().With("module", "state-sync"),
	}

	// 启动后台同步循环
	go sm.syncLoop()

	return sm
}

// OnAppEvent 应用事件处理（事件驱动）
func (sm *SyncManager) OnAppEvent(event AppEvent) {
	event.Timestamp = time.Now().Unix()

	// 更新内存状态
	sm.updateAppState(event)

	// 发送到持久化队列
	select {
	case sm.persistChan <- event:
		// 成功入队
	default:
		sm.log.Warn("persist channel full, dropping event", "event", event.Type)
	}
}

// updateAppState 更新内存中的应用状态
func (sm *SyncManager) updateAppState(event AppEvent) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	appName := event.AppName
	appState, exists := sm.state.Apps[appName]

	if !exists {
		appState = &AppState{}
		sm.state.Apps[appName] = appState
	}

	switch event.Type {
	case "register":
		appState.Config = event.Config
		appState.WorkPath = event.Config.WorkDir
		appState.Status = StatusStopped

	case "config_change":
		appState.Config = event.Config
		appState.WorkPath = event.Config.WorkDir

	case "start":
		appState.PID = event.PID
		appState.StartTime = event.Timestamp
		appState.Status = StatusRunning
		// 重启计数在restart事件中更新

	case "stop":
		appState.Status = StatusStopped
		appState.PID = 0

	case "restart":
		appState.PID = event.PID
		appState.StartTime = event.Timestamp
		appState.RestartCount++
		appState.Status = StatusRunning

	case "failed":
		appState.Status = StatusFailed
		appState.PID = 0

	case "log_path":
		// 单独更新日志路径
		if event.Config != nil {
			appState.LogPath = event.Config.WorkDir // 临时使用
		}
	}
}

// syncLoop 后台持久化循环
func (sm *SyncManager) syncLoop() {
	// 延迟持久化定时器（100ms延迟，批量写入）
	var persistTimer *time.Timer
	var timerC <-chan time.Time
	var pendingEvents []AppEvent

	for {
		select {
		case event := <-sm.persistChan:
			// 收到事件，加入待处理列表
			pendingEvents = append(pendingEvents, event)

			// 启动延迟计时器（如果未启动）
			if persistTimer == nil {
				persistTimer = time.NewTimer(100 * time.Millisecond)
				timerC = persistTimer.C
			}

		case <-timerC:
			if len(pendingEvents) > 0 {
				sm.mu.RLock()
				stateCopy := sm.copyState()
				sm.mu.RUnlock()

				if err := sm.store.Persist(stateCopy); err != nil {
					sm.log.Error("failed to persist state", "error", err)
				} else {
					sm.log.Debug("state persisted", "events", len(pendingEvents))
				}

				pendingEvents = nil
			}

			persistTimer = nil
			timerC = nil

		case <-sm.done:
			// 关闭前执行最后一次持久化
			if len(pendingEvents) > 0 {
				sm.mu.RLock()
				stateCopy := sm.copyState()
				sm.mu.RUnlock()
				if err := sm.store.Persist(stateCopy); err != nil {
					slog.Warn("failed to persist state on close", "error", err)
				}
			}
			return
		}
	}
}

// copyState 创建状态的深拷贝
func (sm *SyncManager) copyState() *StateData {
	return cloneStateData(sm.state)
}

func cloneStateData(data *StateData) *StateData {
	copy := &StateData{
		Apps: make(map[string]*AppState),
	}

	if data == nil {
		return copy
	}

	for name, appState := range data.Apps {
		// 深拷贝配置
		var config *ProcessConfig
		if appState.Config != nil {
			config = &ProcessConfig{
				Type:        appState.Config.Type,
				WorkDir:     appState.Config.WorkDir,
				Executable:  appState.Config.Executable,
				Entry:       appState.Config.Entry,
				Args:        appState.Config.Args,
				AutoRestart: appState.Config.AutoRestart,
				MaxRestarts: appState.Config.MaxRestarts,
			}
		}

		// 深拷贝应用状态
		copy.Apps[name] = &AppState{
			Config:       config,
			PID:          appState.PID,
			StartTime:    appState.StartTime,
			RestartCount: appState.RestartCount,
			Status:       appState.Status,
			WorkPath:     appState.WorkPath,
			LogPath:      appState.LogPath,
		}
	}

	return copy
}

// GetState 获取当前状态（用于恢复）
func (sm *SyncManager) GetState() *StateData {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.copyState()
}

// ReplaceState 用于恢复流程，将外部状态完整写回内存。
func (sm *SyncManager) ReplaceState(data *StateData) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state = cloneStateData(data)
}

// UpdateLogPath 更新日志路径
func (sm *SyncManager) UpdateLogPath(appName, logPath string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if appState, exists := sm.state.Apps[appName]; exists {
		appState.LogPath = logPath
	}
}

// Close 关闭同步管理器
func (sm *SyncManager) Close() {
	close(sm.done)
}

// ManualPersist 手动触发持久化（用于紧急保存）
func (sm *SyncManager) ManualPersist() error {
	sm.mu.RLock()
	stateCopy := sm.copyState()
	sm.mu.RUnlock()
	return sm.store.Persist(stateCopy)
}
