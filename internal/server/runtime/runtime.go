package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	TypeExec    = "exec"    // 可执行程序
	TypeRuntime = "runtime" // 需要运行时的程序
)

// ProcessConfig 进程配置
type ProcessConfig struct {
	Type        string
	WorkDir     string
	Executable  string
	Entry       string
	Args        string
	AutoRestart bool
	MaxRestarts int
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
}

// NewManager 创建管理器
func NewManager(logDir string) *Manager {
	return &Manager{
		logDir: logDir,
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
		m.logger.Stop()
		return err
	}

	// 如果启用自动重启，启动监控协程
	if autoRestart {
		go m.monitor()
	} else {
		// 不启用自动重启时，也需要监控进程退出
		go m.waitProcess()
	}

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
		if m.config.Args != "" {
			argList := strings.Fields(m.config.Args)
			cmd = exec.CommandContext(m.ctx, m.config.Executable, argList...)
		} else {
			cmd = exec.CommandContext(m.ctx, m.config.Executable)
		}

	case TypeRuntime:
		// 需要运行时：运行时命令 + 入口文件 + 应用参数
		// entry 可以包含多个参数，如 "-jar app.jar" 或 "-m module"
		if m.config.Executable == "" || m.config.Entry == "" {
			return fmt.Errorf("executable and entry are required for runtime type")
		}

		// 解析 entry（可能包含多个参数）
		entryArgs := strings.Fields(m.config.Entry)

		// 构建完整参数列表：entry参数 + 应用参数
		var cmdArgs []string
		cmdArgs = append(cmdArgs, entryArgs...)

		if m.config.Args != "" {
			argList := strings.Fields(m.config.Args)
			cmdArgs = append(cmdArgs, argList...)
		}

		cmd = exec.CommandContext(m.ctx, m.config.Executable, cmdArgs...)
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
	// 在锁外获取命令引用
	m.mu.RLock()
	cmd := m.cmd
	m.mu.RUnlock()

	if cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		// 等待进程退出（阻塞操作，不持有锁）
		cmd.Wait()

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
	defer m.mu.Unlock()

	if !m.running {
		return fmt.Errorf("no running application")
	}

	// 标记为停止状态
	m.running = false

	// 取消上下文，停止监控
	if m.cancel != nil {
		m.cancel()
	}

	// 杀死进程
	if m.cmd != nil && m.cmd.Process != nil {
		pid := m.cmd.Process.Pid
		if err := m.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
		slog.Info("process stopped", "pid", pid)
		if m.logger != nil {
			msg := fmt.Sprintf("Process stopped: PID=%d", pid)
			m.logger.WriteLog("system", msg)
		}
	}

	// 停止日志收集
	if m.logger != nil {
		m.logger.Stop()
	}

	m.cmd = nil
	return nil
}

// Restart 重启应用
func (m *Manager) Restart() error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return fmt.Errorf("no running application")
	}

	config := m.config
	m.mu.Unlock()

	// 停止当前进程
	if err := m.Stop(); err != nil {
		return err
	}

	// 等待一小段时间
	time.Sleep(500 * time.Millisecond)

	// 重新启动
	return m.Start(
		config.Type,
		config.WorkDir,
		config.Executable,
		config.Entry,
		config.Args,
		config.AutoRestart,
		config.MaxRestarts,
	)
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
		uptime = int64(time.Since(m.startTime).Seconds())
		return m.config.Type, m.config.Executable, m.config.Entry, m.config.AutoRestart, m.restartCount, uptime
	}
	return "", "", "", false, 0, 0
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
