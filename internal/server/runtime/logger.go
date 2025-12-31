package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxLogLines = 1000 // 内存中保留的最大日志行数
)

// sanitizeFilename 清理文件名，移除危险字符
func sanitizeFilename(name string) string {
	// 替换路径分隔符
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")

	// 替换其他危险字符
	dangerous := []string{":", "*", "?", "\"", "<", ">", "|", " ", "&", ";", "(", ")", "$", "`", "{" ,"}", "[" ,"]"}
	for _, char := range dangerous {
		name = strings.ReplaceAll(name, char, "_")
	}

	// 移除连续的下划线
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}

	// 移除首尾下划线
	name = strings.Trim(name, "_")

	// 限制长度
	if len(name) > 100 {
		name = name[:100]
	}

	if name == "" {
		name = "unknown"
	}

	return name
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // stdout, stderr, system
	Message   string    `json:"message"`
}

// Logger 日志管理器
type Logger struct {
	mu         sync.RWMutex
	logs       []LogEntry
	logFile    *os.File
	logDir     string
	appName    string
	maxLines   int
	done       chan struct{} // 用于控制捕获 goroutine 退出
}

// NewLogger 创建日志管理器
func NewLogger(logDir, appName string) *Logger {
	return &Logger{
		logs:     make([]LogEntry, 0, maxLogLines),
		logDir:   logDir,
		appName:  appName,
		maxLines: maxLogLines,
		done:     make(chan struct{}),
	}
}

// Start 启动日志收集
func (l *Logger) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 创建日志目录
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log dir: %w", err)
	}

	// 创建日志文件
	timestamp := time.Now().Format("20060102-150405")
	logPath := filepath.Join(l.logDir, fmt.Sprintf("%s-%s.log", l.appName, timestamp))

	file, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	l.logFile = file
	l.logs = make([]LogEntry, 0, maxLogLines)

	return nil
}

// Stop 停止日志收集
func (l *Logger) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 关闭 done channel，通知捕获 goroutine 退出
	select {
	case <-l.done:
		// 已经关闭
	default:
		close(l.done)
	}

	if l.logFile != nil {
		err := l.logFile.Close()
		l.logFile = nil
		return err
	}
	return nil
}

// WriteLog 写入日志
func (l *Logger) WriteLog(logType, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Type:      logType,
		Message:   message,
	}

	// 添加到内存
	l.logs = append(l.logs, entry)

	// 保持最大行数限制
	if len(l.logs) > l.maxLines {
		l.logs = l.logs[len(l.logs)-l.maxLines:]
	}

	// 写入文件
	if l.logFile != nil {
		line := fmt.Sprintf("[%s] [%s] %s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Type,
			entry.Message)
		l.logFile.WriteString(line)
		l.logFile.Sync()
	}
}

// GetLogs 获取日志（最近 n 行）
func (l *Logger) GetLogs(lines int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if lines <= 0 || lines > len(l.logs) {
		lines = len(l.logs)
	}

	start := len(l.logs) - lines
	result := make([]LogEntry, lines)
	copy(result, l.logs[start:])

	return result
}

// GetAllLogs 获取所有内存中的日志
func (l *Logger) GetAllLogs() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]LogEntry, len(l.logs))
	copy(result, l.logs)
	return result
}

// CaptureOutput 捕获进程输出
func (l *Logger) CaptureOutput(reader io.Reader, logType string) {
	scanner := bufio.NewScanner(reader)
	for {
		select {
		case <-l.done:
			// 收到停止信号，退出
			return
		default:
			if !scanner.Scan() {
				// 读取结束或出错
				return
			}
			l.WriteLog(logType, scanner.Text())
		}
	}
}

// GetLogFile 获取当前日志文件路径
func (l *Logger) GetLogFile() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.logFile != nil {
		return l.logFile.Name()
	}
	return ""
}
