package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type StateNotifier interface {
	OnAppEvent(eventType string, appName string, config *ProcessConfig, pid int, reason string, startID string)
}

// AppInfoReader 应用信息只读接口（遵循LOD原则）
// 提供对应用状态的只读访问，防止调用者修改内部状态
type AppInfoReader interface {
	IsRunning() bool
	GetPID() int
	GetLogs(lines int) []LogEntry
	GetLogFile() string
	GetAppInfo() AppInfo
}

// MultiManager 管理多个应用的 Manager
// 遵循 KISS 原则：简单包装，不改变现有 Manager 行为
// 遵循 LOD 原则：只与 Manager 交互，不暴露内部细节
type MultiManager struct {
	mu            sync.RWMutex
	managers      map[string]*Manager
	appLocks      map[string]*sync.Mutex
	logDir        string
	stateNotifier StateNotifier // 状态通知器
}

// NewMultiManager 创建多应用管理器
// 遵循 DI 原则：通过参数注入依赖
func NewMultiManager(logDir string) *MultiManager {
	return &MultiManager{
		managers: make(map[string]*Manager),
		appLocks: make(map[string]*sync.Mutex),
		logDir:   logDir,
	}
}

// NewMultiManagerWithState 创建支持状态持久化的多应用管理器
func NewMultiManagerWithState(logDir string, notifier StateNotifier) *MultiManager {
	return &MultiManager{
		managers:      make(map[string]*Manager),
		appLocks:      make(map[string]*sync.Mutex),
		logDir:        logDir,
		stateNotifier: notifier,
	}
}

func (mm *MultiManager) appMutex(appName string) *sync.Mutex {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if lock, ok := mm.appLocks[appName]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	mm.appLocks[appName] = lock
	return lock
}

// SetStateNotifier 设置状态通知器
func (mm *MultiManager) SetStateNotifier(notifier StateNotifier) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.stateNotifier = notifier
}

// StartApp 启动指定应用
// 遵循 LOD：只暴露必要信息，内部 Manager 保持封装
func (mm *MultiManager) StartApp(ctx context.Context, appName string, config *ProcessConfig) error {
	if appName == "" {
		return fmt.Errorf("app name cannot be empty")
	}
	lock := mm.appMutex(appName)
	lock.Lock()
	defer lock.Unlock()

	mm.mu.Lock()
	existingManager, exists := mm.managers[appName]
	if exists && existingManager.IsRunning() {
		mm.mu.Unlock()
		return fmt.Errorf("app '%s' already running", appName)
	}
	if exists {
		delete(mm.managers, appName)
	}
	notifier := mm.stateNotifier
	logDir := mm.logDir
	mm.mu.Unlock()

	manager := NewManager(config, logDir, appName)
	if notifier != nil {
		manager.SetEventAdapter(&EventAdapter{
			AppName: appName,
			Notifier: &stateEventAdapter{
				appName:       appName,
				config:        config,
				stateNotifier: notifier,
			},
		})
	}
	if err := manager.StartWithConfig(config); err != nil {
		return fmt.Errorf("failed to start app '%s': %w", appName, err)
	}

	mm.mu.Lock()
	mm.managers[appName] = manager
	mm.mu.Unlock()
	return nil
}

// StopApp stops one application while holding only its lifecycle lock.
func (mm *MultiManager) StopApp(appName string) error {
	lock := mm.appMutex(appName)
	lock.Lock()
	defer lock.Unlock()

	mm.mu.RLock()
	manager, exists := mm.managers[appName]
	mm.mu.RUnlock()
	if !exists {
		return fmt.Errorf("app '%s' not found", appName)
	}
	if err := manager.Stop(); err != nil {
		return fmt.Errorf("failed to stop app '%s': %w", appName, err)
	}
	return nil
}

// RestartApp restarts one application while holding its lifecycle lock.
func (mm *MultiManager) RestartApp(appName string) error {
	lock := mm.appMutex(appName)
	lock.Lock()
	defer lock.Unlock()

	mm.mu.RLock()
	manager, exists := mm.managers[appName]
	mm.mu.RUnlock()
	if !exists {
		return fmt.Errorf("app '%s' not found", appName)
	}
	return manager.Restart()
}

// GetApp 获取应用管理器（只读访问）
// 遵循 LOD：返回只读接口，防止调用者修改内部状态
func (mm *MultiManager) GetApp(appName string) (AppInfoReader, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	manager, exists := mm.managers[appName]
	return manager, exists
}

// ListApps 列出所有应用名称
func (mm *MultiManager) ListApps() []string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	apps := make([]string, 0, len(mm.managers))
	for name := range mm.managers {
		apps = append(apps, name)
	}
	return apps
}

// GetAppInfo 获取应用信息（简化版，避免暴露 Manager）
func (mm *MultiManager) GetAppInfo(appName string) (AppInfo, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	manager, exists := mm.managers[appName]
	if !exists {
		return AppInfo{}, false
	}

	return manager.GetAppInfo(), true
}

// RemoveApp removes an application from the in-memory registry.
func (mm *MultiManager) RemoveApp(appName string) error {
	lock := mm.appMutex(appName)
	lock.Lock()
	defer lock.Unlock()

	mm.mu.Lock()
	defer mm.mu.Unlock()
	if _, exists := mm.managers[appName]; !exists {
		return fmt.Errorf("app '%s' not found", appName)
	}
	delete(mm.managers, appName)
	return nil
}

// RestoreApp restores persisted process metadata into the manager.
func (mm *MultiManager) RestoreApp(appName string, config *ProcessConfig, pid int, startTime int64, restartCount int, running bool, logPath string, identityVerified bool) error {
	if appName == "" {
		return fmt.Errorf("app name cannot be empty")
	}
	lock := mm.appMutex(appName)
	lock.Lock()
	defer lock.Unlock()

	mm.mu.RLock()
	logDir := mm.logDir
	notifier := mm.stateNotifier
	mm.mu.RUnlock()
	manager := NewManager(config, logDir, appName)
	if notifier != nil {
		manager.SetEventAdapter(&EventAdapter{
			AppName:  appName,
			Notifier: &stateEventAdapter{appName: appName, config: config, stateNotifier: notifier},
		})
	}
	if err := manager.Restore(config, pid, startTime, restartCount, running, logPath, identityVerified); err != nil {
		return fmt.Errorf("failed to restore app '%s': %w", appName, err)
	}
	mm.mu.Lock()
	mm.managers[appName] = manager
	mm.mu.Unlock()
	return nil
}

// StopAll stops all registered applications for graceful shutdown.
func (mm *MultiManager) StopAll() error {
	mm.mu.RLock()
	managers := make(map[string]*Manager, len(mm.managers))
	for name, manager := range mm.managers {
		managers[name] = manager
	}
	mm.mu.RUnlock()

	var errs []error
	for name, manager := range managers {
		lock := mm.appMutex(name)
		lock.Lock()
		if manager.IsRunning() {
			if err := manager.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("stop app '%s': %w", name, err))
			}
		}
		mm.mu.Lock()
		if mm.managers[name] == manager {
			delete(mm.managers, name)
		}
		mm.mu.Unlock()
		lock.Unlock()
	}
	return errors.Join(errs...)
}

// GetStateSnapshot returns a snapshot of all application states.
func (mm *MultiManager) GetStateSnapshot() map[string]*AppInfo {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	snapshot := make(map[string]*AppInfo)
	for appName, manager := range mm.managers {
		info := manager.GetAppInfo()
		snapshot[appName] = &info
	}
	return snapshot
}

// RestoreApps 从状态恢复应用（不启动，只注册）
func (mm *MultiManager) RestoreApps(apps map[string]*ProcessConfig) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for appName, config := range apps {
		if _, exists := mm.managers[appName]; exists {
			// 已存在，跳过
			continue
		}

		// 创建 Manager 但不启动
		manager := NewManager(config, mm.logDir, appName)
		mm.managers[appName] = manager
	}

	return nil
}

// HasApp 检查应用是否存在
func (mm *MultiManager) HasApp(appName string) bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	_, exists := mm.managers[appName]
	return exists
}

// GetProcessConfig 获取应用的配置
func (mm *MultiManager) GetProcessConfig(appName string) (*ProcessConfig, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	manager, exists := mm.managers[appName]
	if !exists {
		return nil, false
	}

	// Manager 内部有 config 字段，但未导出，需要通过 GetInfo 获取
	// 这里我们返回一个副本
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	if manager.config == nil {
		return nil, false
	}

	// 深拷贝配置
	config := &ProcessConfig{
		Type: manager.config.Type, WorkDir: manager.config.WorkDir, Executable: manager.config.Executable,
		Entry: manager.config.Entry, Args: manager.config.Args,
		Command:     append([]string(nil), manager.config.Command...),
		AutoRestart: manager.config.AutoRestart, MaxRestarts: manager.config.MaxRestarts,
	}
	return config, true
}

// stateEventAdapter 将 MultiManager 的状态通知转换为 state 包的事件
type stateEventAdapter struct {
	appName       string
	config        *ProcessConfig
	stateNotifier StateNotifier
}

func (a *stateEventAdapter) Notify(eventType string, data map[string]interface{}) {
	if a.stateNotifier == nil {
		return
	}

	pid := extractInt(data, "pid")
	if eventType == "restart" {
		if newPID := extractInt(data, "new_pid"); newPID != 0 {
			pid = newPID
		}
	}
	reason, _ := data["reason"].(string)
	startID, _ := data["start_id"].(string)

	a.stateNotifier.OnAppEvent(eventType, a.appName, a.config, pid, reason, startID)

	if logPath, ok := data["logPath"].(string); ok && logPath != "" {
		a.stateNotifier.OnAppEvent("log_path", a.appName, &ProcessConfig{
			WorkDir: logPath,
		}, 0, "", "")
	}
}

func extractInt(data map[string]interface{}, key string) int {
	if data == nil {
		return 0
	}
	if value, ok := data[key].(int); ok {
		return value
	}
	return 0
}
