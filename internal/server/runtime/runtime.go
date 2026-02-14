package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jiangfire/dzjjy/pkg/api"
)

// EventNotifier 事件通知接口
type EventNotifier interface {
	Notify(eventType string, data map[string]interface{})
}

// EventAdapter 事件适配器（用于状态持久化）
type EventAdapter struct {
	AppName  string
	Notifier EventNotifier
}

// Notify 发送事件通知
func (e *EventAdapter) Notify(eventType string, data map[string]interface{}) {
	if e.Notifier != nil {
		e.Notifier.Notify(eventType, data)
	}
}

const (
	TypeExec    = "exec"    // 可执行程序
	TypeRuntime = "runtime" // 需要运行时的程序
)

// ProcessConfig 进程配置（使用pkg/api中的公共类型）
type ProcessConfig = api.ProcessConfig

// isValidExecutable 验证可执行文件路径是否安全
func isValidExecutable(path string) bool {
	// 禁止路径遍历
	if strings.Contains(path, "..") {
		return false
	}

	// 相对路径安全（将在 startProcess 中转换为绝对路径）
	if !filepath.IsAbs(path) {
		return true
	}

	// 绝对路径：阻止系统敏感目录
	dangerousPrefixes := []string{
		"/etc/", "/usr/", "/bin/", "/sbin/", "/sys/", "/proc/",
		"C:\\Windows\\", "C:\\System32\\", "C:\\Program Files\\",
	}
	for _, prefix := range dangerousPrefixes {
		if strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) {
			return false
		}
	}

	// 阻止路径遍历变种
	if strings.Contains(path, "/../") || strings.Contains(path, "\\..\\") {
		return false
	}

	return true
}

// AppInfo 应用信息（用于多应用管理）
type AppInfo struct {
	AppName      string
	Running      bool
	PID          int
	Type         string
	Executable   string
	Entry        string
	AutoRestart  bool
	RestartCount int
	Uptime       int64
}

// Manager 运行时管理器
type Manager struct {
	mu           sync.RWMutex
	cmd          *exec.Cmd
	config       *ProcessConfig
	ctx          context.Context
	cancel       context.CancelFunc
	restartCount int
	startTime    time.Time
	running      bool
	logger       *Logger
	logDir       string
	eventAdapter *EventAdapter // 事件通知适配器
	appName      string        // 应用名称（用于事件）
	monitorDone  chan struct{} // 监控goroutine完成信号
}

// NewManager 创建管理器
func NewManager(config *ProcessConfig, logDir, appName string) *Manager {
	return &Manager{
		config:  config,
		logDir:  logDir,
		appName: appName,
	}
}

// SetEventAdapter 设置事件通知器
func (m *Manager) SetEventAdapter(adapter *EventAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventAdapter = adapter
}

// SetAppName 设置应用名称
func (m *Manager) SetAppName(appName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appName = appName
}

// notify 发送事件通知（内部方法）
func (m *Manager) notify(eventType string, data map[string]interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.eventAdapter != nil {
		// 添加通用数据
		if data == nil {
			data = make(map[string]interface{})
		}
		data["app_name"] = m.appName
		data["timestamp"] = time.Now().Unix()
		m.eventAdapter.Notify(eventType, data)
	}
}

// Start 启动应用
func (m *Manager) Start(appType, workDir, executable, entry, args string, autoRestart bool, maxRestarts int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("application is already running")
	}

	// 验证类型
	if appType != TypeExec && appType != TypeRuntime {
		return fmt.Errorf("invalid type: %s (must be 'exec' or 'runtime')", appType)
	}

	// 保存配置
	m.config = &ProcessConfig{
		Type:        appType,
		WorkDir:     workDir,
		Executable:  executable,
		Entry:       entry,
		Args:        args,
		AutoRestart: autoRestart,
		MaxRestarts: maxRestarts,
	}

	// 创建上下文
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.restartCount = 0
	m.running = true
	m.monitorDone = make(chan struct{})

	// 创建日志管理器（使用安全的文件名）
	safeExecutable := sanitizeFilename(executable)
	appName := fmt.Sprintf("%s-%s", appType, safeExecutable)
	m.logger = NewLogger(m.logDir, appName)
	if err := m.logger.Start(); err != nil {
		m.running = false
		return fmt.Errorf("failed to start logger: %w", err)
	}

	m.logger.WriteLog("system", fmt.Sprintf("Starting application: type=%s, executable=%s, entry=%s", appType, executable, entry))

	// 启动进程
	if err := m.startProcess(); err != nil {
		m.running = false
		if stopErr := m.logger.Stop(); stopErr != nil {
			slog.Warn("failed to stop logger", "error", stopErr)
		}
		return err
	}

	// 如果启用自动重启，启动监控协程
	if autoRestart {
		go m.monitor()
	} else {
		// 不启用自动重启时，也需要监控进程退出
		go m.waitProcess()
	}

	// 发送启动事件（在goroutine中，避免阻塞）
	go func() {
		pid := m.GetPID()
		logPath := m.GetLogFile()
		m.notify("start", map[string]interface{}{
			"pid":     pid,
			"logPath": logPath,
		})
	}()

	return nil
}

// startProcess 启动进程
func (m *Manager) startProcess() error {
	var cmd *exec.Cmd

	switch m.config.Type {
	case TypeExec:
		// 可执行程序：直接运行
		if m.config.Executable == "" {
			return fmt.Errorf("executable is required for exec type")
		}

		// 解析可执行文件路径
		// TypeExec 的可执行文件应该在工作目录中
		executablePath := m.config.Executable
		if !filepath.IsAbs(executablePath) {
			executablePath = filepath.Join(m.config.WorkDir, executablePath)
		}

		// 验证原始路径（防止注入）
		if !isValidExecutable(m.config.Executable) {
			return fmt.Errorf("invalid executable path: %s", m.config.Executable)
		}

		// 验证最终路径（防止遍历）
		if !isValidExecutable(executablePath) {
			return fmt.Errorf("invalid executable path after resolution: %s", executablePath)
		}

		if m.config.Args != "" {
			argList := strings.Fields(m.config.Args)
			cmd = exec.CommandContext(m.ctx, executablePath, argList...) // #nosec G204 - validated above
		} else {
			cmd = exec.CommandContext(m.ctx, executablePath) // #nosec G204 - validated above
		}

	case TypeRuntime:
		// 需要运行时：运行时命令 + 入口文件 + 应用参数
		// entry 可以包含多个参数，如 "-jar app.jar" 或 "-m module"
		if m.config.Executable == "" || m.config.Entry == "" {
			return fmt.Errorf("executable and entry are required for runtime type")
		}

		// TypeRuntime 的可执行文件（如 go, python）通常在 PATH 中
		// 直接使用原始可执行文件名，让系统 PATH 解析
		executablePath := m.config.Executable

		// 解析 entry（可能包含多个参数）
		entryArgs := strings.Fields(m.config.Entry)

		// 构建完整参数列表：entry参数 + 应用参数
		var cmdArgs []string
		cmdArgs = append(cmdArgs, entryArgs...)

		if m.config.Args != "" {
			argList := strings.Fields(m.config.Args)
			cmdArgs = append(cmdArgs, argList...)
		}

		// 验证可执行文件路径安全
		if !isValidExecutable(m.config.Executable) {
			return fmt.Errorf("invalid executable path: %s", m.config.Executable)
		}

		cmd = exec.CommandContext(m.ctx, executablePath, cmdArgs...) // #nosec G204 - validated above
	}

	cmd.Dir = m.config.WorkDir

	// 捕获标准输出和标准错误
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	m.cmd = cmd
	m.startTime = time.Now()

	slog.Info("process started",
		"pid", cmd.Process.Pid,
		"type", m.config.Type,
		"executable", m.config.Executable,
		"entry", m.config.Entry,
	)

	// 记录系统日志
	if m.logger != nil {
		m.logger.WriteLog("system", fmt.Sprintf("Process started: PID=%d", cmd.Process.Pid))

		// 启动日志捕获协程
		go m.logger.CaptureOutput(stdout, "stdout")
		go m.logger.CaptureOutput(stderr, "stderr")
	}

	return nil
}

// monitor 监控进程并自动重启
func (m *Manager) monitor() {
	defer close(m.monitorDone) // 通知监控已退出
	for {
		select {
		case <-m.ctx.Done():
			// 收到停止信号，退出监控
			return
		default:
			// 在锁外获取当前状态，避免长时间持有锁
			m.mu.RLock()
			cmd := m.cmd
			running := m.running
			config := m.config
			m.mu.RUnlock()

			// 检查是否应该继续监控
			if !running || cmd == nil || cmd.Process == nil {
				return
			}

			// 等待进程退出（阻塞操作，不持有锁）
			err := cmd.Wait()

			// 进程已退出，重新获取锁检查状态
			m.mu.Lock()

			// 检查是否已经被手动停止
			if !m.running {
				m.mu.Unlock()
				return
			}

			// 检查是否需要重启
			if config.MaxRestarts > 0 && m.restartCount >= config.MaxRestarts {
				slog.Warn("process reached max restart limit",
					"max_restarts", config.MaxRestarts,
					"executable", config.Executable,
				)
				if m.logger != nil {
					msg := fmt.Sprintf("Process exited and reached max restart limit (%d)", config.MaxRestarts)
					m.logger.WriteLog("system", msg)
				}
				m.running = false
				m.mu.Unlock()
				return
			}

			// 进程异常退出，准备重启
			m.restartCount++
			slog.Warn("process exited, restarting",
				"error", err,
				"attempt", m.restartCount,
				"executable", config.Executable,
			)
			if m.logger != nil {
				msg := fmt.Sprintf("Process exited with error: %v, restarting... (attempt %d)", err, m.restartCount)
				m.logger.WriteLog("system", msg)
			}

			// 等待一小段时间再重启，避免快速失败循环
			time.Sleep(1 * time.Second)

			// 重启进程（在锁保护下）
			if err := m.startProcess(); err != nil {
				slog.Error("failed to restart process",
					"error", err,
					"executable", config.Executable,
				)
				if m.logger != nil {
					msg := fmt.Sprintf("Failed to restart process: %v", err)
					m.logger.WriteLog("system", msg)
				}
				m.running = false
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
		}
	}
}

// waitProcess 等待进程退出（不重启）
func (m *Manager) waitProcess() {
	defer close(m.monitorDone) // 通知监控已退出

	// 在锁外获取命令引用
	m.mu.RLock()
	cmd := m.cmd
	m.mu.RUnlock()

	if cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		// 等待进程退出（阻塞操作，不持有锁）
		if err := cmd.Wait(); err != nil {
			slog.Warn("process wait error", "pid", pid, "error", err)
		}

		// 进程已退出，更新状态
		m.mu.Lock()
		m.running = false
		slog.Info("process exited", "pid", pid)
		m.mu.Unlock()
	}
}

// Stop 停止应用
func (m *Manager) Stop() error {
	m.mu.Lock()

	if !m.running {
		m.mu.Unlock()
		return fmt.Errorf("no running application")
	}

	// 标记为停止状态
	m.running = false

	// 取消上下文，停止监控
	if m.cancel != nil {
		m.cancel()
	}

	// 杀死进程
	var pid int
	if m.cmd != nil && m.cmd.Process != nil {
		pid = m.cmd.Process.Pid
		if err := m.cmd.Process.Kill(); err != nil {
			// 进程可能已退出：Windows 上有时会返回 "Access is denied"
			isProcessDone := errors.Is(err, os.ErrProcessDone)
			isWindowsAccessDenied := os.PathSeparator == '\\' &&
				strings.Contains(strings.ToLower(err.Error()), "access is denied")
			if !isProcessDone && !isWindowsAccessDenied {
				m.mu.Unlock()
				return fmt.Errorf("failed to kill process: %w", err)
			}
			slog.Warn("process already exited before kill completed", "pid", pid, "error", err)
		}
		slog.Info("process stopped", "pid", pid)
		if m.logger != nil {
			msg := fmt.Sprintf("Process stopped: PID=%d", pid)
			m.logger.WriteLog("system", msg)
		}
	}

	// 停止日志收集
	if m.logger != nil {
		if err := m.logger.Stop(); err != nil {
			slog.Warn("failed to stop logger", "error", err)
		}
	}

	m.cmd = nil

	// 获取 monitorDone 引用（在锁保护下）
	monitorDone := m.monitorDone
	m.mu.Unlock()

	// 等待监控goroutine完成（避免死锁）
	// 注意：这里在锁外等待，避免死锁
	if monitorDone != nil {
		<-monitorDone
	}

	// 发送停止事件
	m.notify("stop", map[string]interface{}{
		"pid": pid,
	})

	return nil
}

// Restart 重启应用
func (m *Manager) Restart() error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return fmt.Errorf("no running application")
	}

	// 在锁内获取配置和PID
	config := m.config
	oldPID := 0
	if m.cmd != nil && m.cmd.Process != nil {
		oldPID = m.cmd.Process.Pid
	}
	m.mu.Unlock()

	// 停止当前进程
	if err := m.Stop(); err != nil {
		return err
	}

	// 等待一小段时间
	time.Sleep(500 * time.Millisecond)

	// 重新启动
	err := m.Start(
		config.Type,
		config.WorkDir,
		config.Executable,
		config.Entry,
		config.Args,
		config.AutoRestart,
		config.MaxRestarts,
	)

	// 发送重启事件
	if err == nil {
		newPID := m.GetPID()
		m.notify("restart", map[string]interface{}{
			"old_pid": oldPID,
			"new_pid": newPID,
		})
	}

	return err
}

// IsRunning 检查是否运行中
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetPID 获取进程ID
func (m *Manager) GetPID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}

// GetInfo 获取运行信息
func (m *Manager) GetInfo() (appType, executable, entry string, autoRestart bool, restartCount int, uptime int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config != nil {
		// 只有在 startTime 被设置后才计算 uptime
		if !m.startTime.IsZero() {
			uptime = int64(time.Since(m.startTime).Seconds())
		}
		return m.config.Type, m.config.Executable, m.config.Entry, m.config.AutoRestart, m.restartCount, uptime
	}
	return "", "", "", false, 0, 0
}

// GetConfig 获取配置信息（用于状态持久化）
func (m *Manager) GetConfig() *ProcessConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config == nil {
		return nil
	}

	// 返回配置的副本
	return &ProcessConfig{
		Type:        m.config.Type,
		WorkDir:     m.config.WorkDir,
		Executable:  m.config.Executable,
		Entry:       m.config.Entry,
		Args:        m.config.Args,
		AutoRestart: m.config.AutoRestart,
		MaxRestarts: m.config.MaxRestarts,
	}
}

// GetAppName 获取应用名称
func (m *Manager) GetAppName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.appName
}

// GetAppInfo 获取应用信息（结构化返回）
func (m *Manager) GetAppInfo() AppInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := AppInfo{
		AppName:      m.appName,
		Running:      m.running,
		PID:          0,
		AutoRestart:  false,
		RestartCount: m.restartCount,
		Uptime:       0,
	}

	if m.cmd != nil && m.cmd.Process != nil {
		info.PID = m.cmd.Process.Pid
	}

	if m.config != nil {
		info.Type = m.config.Type
		info.Executable = m.config.Executable
		info.Entry = m.config.Entry
		info.AutoRestart = m.config.AutoRestart
	}

	if m.running && !m.startTime.IsZero() {
		info.Uptime = int64(time.Since(m.startTime).Seconds())
	}

	return info
}

// GetLogs 获取日志
func (m *Manager) GetLogs(lines int) []LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.logger != nil {
		return m.logger.GetLogs(lines)
	}
	return []LogEntry{}
}

// GetLogFile 获取日志文件路径
func (m *Manager) GetLogFile() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.logger != nil {
		return m.logger.GetLogFile()
	}
	return ""
}

// 确保 Manager 实现了 AppInfoReader 接口
var _ AppInfoReader = (*Manager)(nil)
