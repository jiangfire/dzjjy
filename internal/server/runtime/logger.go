package runtime

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxLogLines        = 1000             // 内存中保留的最大日志行数
	defaultMaxFileSize = 10 * 1024 * 1024 // 默认10MB
	defaultMaxFiles    = 10               // 默认保留10个文件
	rotationCheckEvery = 100              // 每100次写入检查一次大小
)

// sanitizeFilename 清理文件名，移除危险字符
func sanitizeFilename(name string) string {
	// 替换路径分隔符
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")

	// 替换其他危险字符
	dangerous := []string{":", "*", "?", "\"", "<", ">", "|", " ", "&", ";", "(", ")", "$", "`", "{", "}", "[", "]"}
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

// RotationConfig 日志轮转配置
type RotationConfig struct {
	MaxSize  int64 // 单个文件最大大小（字节），0表示禁用轮转
	MaxFiles int   // 保留的文件数量，0表示不限制
	Enabled  bool  // 是否启用轮转
}

// Logger 日志管理器
type Logger struct {
	mu       sync.RWMutex
	logs     []LogEntry
	logFile  *os.File
	logPath  string // 存储日志文件路径，即使文件已关闭
	logDir   string
	appName  string
	maxLines int
	done     chan struct{} // 用于控制捕获 goroutine 退出

	// 轮转相关字段
	rotationConfig  RotationConfig
	writeCount      int   // 写入计数器，用于优化性能
	currentFileSize int64 // 当前文件大小
}

// NewLogger 创建日志管理器
func NewLogger(logDir, appName string) *Logger {
	return &Logger{
		logs:     make([]LogEntry, 0, maxLogLines),
		logDir:   logDir,
		appName:  appName,
		maxLines: maxLogLines,
		done:     make(chan struct{}),
		rotationConfig: RotationConfig{
			MaxSize:  defaultMaxFileSize,
			MaxFiles: defaultMaxFiles,
			Enabled:  true,
		},
	}
}

// SetRotationConfig 设置轮转配置
func (l *Logger) SetRotationConfig(config RotationConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotationConfig = config
}

// Start 启动日志收集
func (l *Logger) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 创建日志目录
	if err := os.MkdirAll(l.logDir, 0750); err != nil {
		return fmt.Errorf("failed to create log dir: %w", err)
	}

	// 清理应用名，移除危险字符
	safeAppName := sanitizeFilename(l.appName)

	// 创建日志文件
	timestamp := time.Now().Format("20060102-150405")
	logPath := filepath.Join(l.logDir, fmt.Sprintf("%s-%s.log", safeAppName, timestamp))

	file, err := os.Create(logPath) // #nosec G304 - path sanitized via sanitizeFilename
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	l.logFile = file
	l.logPath = logPath
	l.logs = make([]LogEntry, 0, maxLogLines)
	l.writeCount = 0
	l.currentFileSize = 0

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

		// 检查是否需要轮转（优化：每100次写入检查一次，或文件过大时立即检查）
		l.writeCount++
		if l.rotationConfig.Enabled &&
			(l.writeCount%rotationCheckEvery == 0 || l.currentFileSize > l.rotationConfig.MaxSize) {
			l.checkAndRotate()
		}

		// 写入数据
		n, _ := l.logFile.WriteString(line)
		l.currentFileSize += int64(n)
		if err := l.logFile.Sync(); err != nil {
			slog.Warn("failed to sync log file", "error", err)
		}
	}
}

// checkAndRotate 检查并执行日志轮转
func (l *Logger) checkAndRotate() {
	// 获取当前文件大小
	if l.logFile == nil {
		return
	}

	// MaxSize <= 0 表示禁用轮转
	if l.rotationConfig.MaxSize <= 0 {
		return
	}

	// 如果文件大小未超过限制，不需要轮转
	if l.currentFileSize <= l.rotationConfig.MaxSize {
		return
	}

	// 关闭当前文件
	if err := l.logFile.Close(); err != nil {
		slog.Warn("failed to close log file", "error", err)
	}
	l.logFile = nil

	// 重命名当前文件为轮转文件
	currentPath := l.logPath
	if _, err := os.Stat(currentPath); err == nil {
		// 生成轮转文件名：app-20251231-150405.rotated.001.log
		timestamp := time.Now().Format("20060102-150405")
		rotatedPath := fmt.Sprintf("%s.rotated.%s.log",
			strings.TrimSuffix(currentPath, ".log"), timestamp)

		// 如果目标已存在，添加序号
		if _, err := os.Stat(rotatedPath); err == nil {
			for i := 1; i < 1000; i++ {
				rotatedPath = fmt.Sprintf("%s.rotated.%s.%03d.log",
					strings.TrimSuffix(currentPath, ".log"), timestamp, i)
				if _, err := os.Stat(rotatedPath); err != nil {
					break
				}
			}
		}

		if err := os.Rename(currentPath, rotatedPath); err != nil {
			slog.Warn("failed to rename log file", "from", currentPath, "to", rotatedPath, "error", err)
		}
	}

	// 创建新文件
	safeAppName := sanitizeFilename(l.appName)
	newTimestamp := time.Now().Format("20060102-150405")
	newPath := filepath.Join(l.logDir, fmt.Sprintf("%s-%s.log", safeAppName, newTimestamp))

	file, err := os.Create(newPath) // #nosec G304 - path sanitized via sanitizeFilename
	if err != nil {
		// 如果创建失败，尝试创建一个临时文件
		file, err = os.CreateTemp(l.logDir, fmt.Sprintf("%s-*.log", safeAppName))
		if err != nil {
			return
		}
	}

	l.logFile = file
	l.logPath = newPath
	l.currentFileSize = 0

	// 清理旧文件
	l.cleanupOldFiles()
}

// cleanupOldFiles 清理旧的日志文件
func (l *Logger) cleanupOldFiles() {
	if l.rotationConfig.MaxFiles <= 0 {
		return // 不限制文件数量
	}

	// 获取所有日志文件
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}

	// 过滤出当前应用的日志文件
	safeAppName := sanitizeFilename(l.appName)
	var logFiles []os.FileInfo
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		// 匹配应用名前缀
		if strings.HasPrefix(name, safeAppName+"-") &&
			(strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.rotated.*.log")) {
			info, _ := f.Info()
			logFiles = append(logFiles, info)
		}
	}

	// 如果文件数量未超过限制，不需要清理
	if len(logFiles) <= l.rotationConfig.MaxFiles {
		return
	}

	// 按修改时间排序（最旧的在前）
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].ModTime().Before(logFiles[j].ModTime())
	})

	// 删除旧文件，保留最新的 MaxFiles 个
	for i := 0; i < len(logFiles)-l.rotationConfig.MaxFiles; i++ {
		filePath := filepath.Join(l.logDir, logFiles[i].Name())
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove old log file", "file", filePath, "error", err)
		}
	}
}

// GetLogs 获取日志（最近 n 行）
func (l *Logger) GetLogs(lines int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.logs) == 0 && l.logPath != "" {
		return readLogsFromFile(l.logDir, l.logPath, lines)
	}

	if lines <= 0 || lines > len(l.logs) {
		lines = len(l.logs)
	}

	start := len(l.logs) - lines
	result := make([]LogEntry, lines)
	copy(result, l.logs[start:])

	return result
}

func readLogsFromFile(logDir, logPath string, lines int) []LogEntry {
	root, err := os.OpenRoot(logDir)
	if err != nil {
		return []LogEntry{}
	}
	defer func() { _ = root.Close() }()

	relPath, err := filepath.Rel(logDir, logPath)
	if err != nil {
		return []LogEntry{}
	}

	file, err := root.Open(relPath)
	if err != nil {
		return []LogEntry{}
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil || len(content) == 0 {
		return []LogEntry{}
	}

	rawLines := strings.Split(strings.TrimRight(string(content), "\r\n"), "\n")
	if lines <= 0 || lines > len(rawLines) {
		lines = len(rawLines)
	}

	start := len(rawLines) - lines
	result := make([]LogEntry, 0, lines)
	for _, line := range rawLines[start:] {
		result = append(result, LogEntry{
			Type:    "system",
			Message: line,
		})
	}
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

	return l.logPath
}

// SetLogPath 为恢复态注入已有日志路径，不打开文件。
func (l *Logger) SetLogPath(logPath string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logPath = logPath
}
