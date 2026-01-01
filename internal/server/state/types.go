package state

import "github.com/jiangfire/dzjjy/pkg/api"

// StateFile 持久化状态文件结构
type StateFile struct {
	Version   string    `json:"version"`   // 版本号
	Timestamp int64     `json:"timestamp"` // 时间戳
	Checksum  string    `json:"checksum"`  // SHA256校验和
	Data      StateData `json:"data"`      // 实际数据
}

// StateData 状态数据
type StateData struct {
	Apps map[string]*AppState `json:"apps"` // 应用映射
}

// AppState 单个应用的状态
type AppState struct {
	Config       *ProcessConfig `json:"config"`        // 应用配置
	PID          int            `json:"pid"`           // 进程ID
	StartTime    int64          `json:"start_time"`    // 启动时间戳
	RestartCount int            `json:"restart_count"` // 重启次数
	Status       string         `json:"status"`        // 状态：running/stopped/failed
	WorkPath     string         `json:"work_path"`     // 工作目录
	LogPath      string         `json:"log_path"`      // 日志文件路径
}

// ProcessConfig 进程配置（使用pkg/api中的公共类型）
type ProcessConfig = api.ProcessConfig

const (
	// StateFileVersion 当前状态文件版本
	StateFileVersion = "1.0.0"
	// DefaultStateFile 默认状态文件路径
	DefaultStateFile = "./state.json"
	// StatusRunning 运行中
	StatusRunning = "running"
	// StatusStopped 已停止
	StatusStopped = "stopped"
	// StatusFailed 失败
	StatusFailed = "failed"
)
