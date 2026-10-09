package state

import "github.com/jiangfire/dzjjy/pkg/api"

// StateFile 持久化状态文件结构
type StateFile struct {
	Version   string    `json:"version"`
	Timestamp int64     `json:"timestamp"`
	Checksum  string    `json:"checksum"`
	Data      StateData `json:"data"`
}

// StateData 状态数据
type StateData struct {
	Apps map[string]*AppState `json:"apps"`
}

// AppState 单个应用的期望配置与观测状态
type AppState struct {
	Config            *ProcessConfig `json:"config,omitempty"`
	PID               int            `json:"pid,omitempty"`
	StartTime         int64          `json:"start_time,omitempty"`
	RestartCount      int            `json:"restart_count,omitempty"`
	Status            string         `json:"status,omitempty"`
	WorkPath          string         `json:"work_path,omitempty"`
	LogPath           string         `json:"log_path,omitempty"`
	DesiredConfig     string         `json:"desired_config,omitempty"`
	Generation        uint64         `json:"generation,omitempty"`
	Backend           string         `json:"backend,omitempty"`
	ContainerID       string         `json:"container_id,omitempty"`
	Port              int            `json:"port,omitempty"`
	ExpiresAt         int64          `json:"expires_at,omitempty"`
	TerminationReason string         `json:"termination_reason,omitempty"`
	ProcessStartID    string         `json:"process_start_id,omitempty"`
	IdentityVerified  bool           `json:"-"` // 恢复阶段判定，仅内存传递，不落盘
}

// ProcessConfig 进程配置（使用pkg/api中的公共类型）
type ProcessConfig = api.ProcessConfig

const (
	StateFileVersion = "1.0.0"
	DefaultStateFile = "./state.json"
	StatusRunning    = "running"
	StatusStopped    = "stopped"
	StatusFailed     = "failed"
	StatusExpired    = "expired"
)
