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
	// PublishedPorts records host ports that were actually bound for this
	// app, so a restart can reuse or re-resolve them deterministically.
	PublishedPorts []PublishedPort `json:"published_ports,omitempty"`
	// Isolator records the isolation layer applied to a container, such as
	// "runsc"; it is empty for the default Docker runtime.
	Isolator string `json:"isolator,omitempty"`
	// ExposureToken is the unguessable path token serving a StaticSite
	// publicly. The token grants access to the site content only.
	ExposureToken string `json:"exposure_token,omitempty"`
	// IdleTimeoutSeconds mirrors spec.lifecycle.idleTimeout so a suspended
	// app can be resumed without re-reading its desired config.
	IdleTimeoutSeconds int `json:"idle_timeout_seconds,omitempty"`
	// Suspended marks an idle-suspended app. Suspended apps keep their
	// artifact on disk and are restarted on the next request.
	Suspended bool `json:"suspended,omitempty"`
	// LastActivityUnix is the last observed request time, used to rebuild the
	// idle schedule after a service restart.
	LastActivityUnix int64 `json:"last_activity_unix,omitempty"`
}

// PublishedPort is a resolved host-to-container port binding.
type PublishedPort struct {
	Container int `json:"container"`
	Host      int `json:"host"`
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
