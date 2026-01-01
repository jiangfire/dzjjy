package state

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"

	"log/slog"
)

// RestoreManager 恢复管理器
type RestoreManager struct {
	store       *StateStore
	syncManager *SyncManager
	log         *slog.Logger
}

// NewRestoreManager 创建恢复管理器
func NewRestoreManager(store *StateStore, syncManager *SyncManager) *RestoreManager {
	return &RestoreManager{
		store:       store,
		syncManager: syncManager,
		log:         slog.Default().With("module", "restore"),
	}
}

// Restore 执行恢复流程
func (rm *RestoreManager) Restore() error {
	// 加载状态文件
	stateFile, err := rm.store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if stateFile == nil {
		rm.log.Info("no state file found, first start")
		return nil
	}

	// 恢复每个应用
	for appName, appState := range stateFile.Data.Apps {
		if err := rm.restoreApp(appName, appState); err != nil {
			rm.log.Error("failed to restore app", "app", appName, "error", err)
		}
	}

	return nil
}

// restoreApp 恢复单个应用
func (rm *RestoreManager) restoreApp(appName string, state *AppState) error {
	rm.log.Info("restoring app", "app", appName, "pid", state.PID, "status", state.Status)

	// 1. PID验证
	if state.PID > 0 {
		if rm.isProcessRunning(state.PID) {
			return rm.recoverRunningApp(appName, state)
		}
		rm.log.Warn("process not running, cleaning up", "pid", state.PID)
	}

	// 2. 根据状态和配置决定是否重启
	if state.Status != StatusStopped && state.Config != nil && state.Config.AutoRestart {
		rm.log.Info("auto-restarting app", "app", appName)
		// 注意：这里只记录日志，实际重启由Handler在启动时处理
		// 我们更新状态为stopped，因为进程已不存在
		state.Status = StatusStopped
		state.PID = 0
		state.RestartCount = 0
	} else {
		// 已停止或不自动重启，保持状态
		state.Status = StatusStopped
		state.PID = 0
	}

	// 3. 更新到syncManager
	if state.Config != nil {
		rm.syncManager.OnAppEvent(AppEvent{
			Type:    "register",
			AppName: appName,
			Config:  state.Config,
		})
	}

	return nil
}

// isProcessRunning 检查进程是否正在运行
func (rm *RestoreManager) isProcessRunning(pid int) bool {
	// 在Windows上使用tasklist命令检查
	if isWindows() {
		return rm.isProcessRunningWindows(pid)
	}

	// 在Unix/Linux上使用syscall
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// 发送信号0检查进程是否存在
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return false
	}

	// 额外验证：检查进程名或命令行（可选）
	return true
}

// isProcessRunningWindows Windows平台进程检查
func (rm *RestoreManager) isProcessRunningWindows(pid int) bool {
	// 使用tasklist命令检查进程是否存在
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid)) // #nosec G204 - validated integer PID
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	outputStr := string(output)

	// tasklist输出格式:
	// 如果找到进程: "image.exe 12345 Console ..."
	// 如果没找到: "INFO: No tasks are running which match the specified criteria."

	// 使用正则匹配PID数字，更可靠
	// 匹配模式: 任意字符后跟空格+PID+空格+其他
	pidPattern := regexp.MustCompile(`\s` + strconv.Itoa(pid) + `\s`)

	// 检查是否包含匹配的PID，且不包含INFO信息
	if pidPattern.MatchString(outputStr) && !regexp.MustCompile(`INFO: No tasks`).MatchString(outputStr) {
		return true
	}

	return false
}

// isWindows 检查是否为Windows平台
func isWindows() bool {
	return os.PathSeparator == '\\'
}

// recoverRunningApp 恢复正在运行的应用
func (rm *RestoreManager) recoverRunningApp(appName string, state *AppState) error {
	rm.log.Info("recovering running app", "app", appName, "pid", state.PID)

	// 验证进程的实际状态
	// 尝试获取进程信息
	process, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// 在Unix上，发送信号0验证
	if !isWindows() {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return fmt.Errorf("process not responding: %w", err)
		}
	}

	// 恢复状态
	state.Status = StatusRunning

	// 更新到syncManager
	if state.Config != nil {
		rm.syncManager.OnAppEvent(AppEvent{
			Type:    "register",
			AppName: appName,
			Config:  state.Config,
		})
	}

	rm.syncManager.OnAppEvent(AppEvent{
		Type:    "start",
		AppName: appName,
		PID:     state.PID,
	})

	// 注意：我们不创建新的Manager来管理这个进程
	// 因为Manager需要有cmd引用才能监控
	// 这种情况下，进程会继续运行但不受Manager监控
	// 建议用户手动重启以获得完整的监控能力

	rm.log.Warn("app recovered but not monitored - consider restarting for full monitoring", "app", appName)

	return nil
}

// GetAppState 获取应用状态
func (rm *RestoreManager) GetAppState(appName string) (*AppState, error) {
	state := rm.syncManager.GetState()
	if appState, exists := state.Apps[appName]; exists {
		return appState, nil
	}
	return nil, fmt.Errorf("app %s not found", appName)
}

// ListApps 列出所有应用
func (rm *RestoreManager) ListApps() map[string]*AppState {
	state := rm.syncManager.GetState()
	return state.Apps
}

// Cleanup 清理无效状态
func (rm *RestoreManager) Cleanup() error {
	state := rm.syncManager.GetState()
	changed := false

	for appName, appState := range state.Apps {
		// 检查PID是否有效
		if appState.PID > 0 && !rm.isProcessRunning(appState.PID) {
			rm.log.Warn("cleaning up invalid state", "app", appName, "pid", appState.PID)

			// 通过事件更新状态，确保syncManager内部状态也被更新
			rm.syncManager.OnAppEvent(AppEvent{
				Type:    "stop",
				AppName: appName,
			})
			changed = true
		}
	}

	if changed {
		return rm.syncManager.ManualPersist()
	}

	return nil
}
