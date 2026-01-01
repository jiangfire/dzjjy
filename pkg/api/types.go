package api

// ProcessConfig 进程配置（公共类型，供runtime和state包使用）
// 遵循DRY原则，消除重复定义
type ProcessConfig struct {
	Type        string `json:"type"`         // exec 或 runtime
	WorkDir     string `json:"work_dir"`     // 工作目录
	Executable  string `json:"executable"`   // 可执行程序
	Entry       string `json:"entry"`        // 入口文件
	Args        string `json:"args"`         // 启动参数
	AutoRestart bool   `json:"auto_restart"` // 自动重启
	MaxRestarts int    `json:"max_restarts"` // 最大重启次数
}

// DeployRequest 部署请求
type DeployRequest struct {
	Type        string `json:"type"`         // exec: 可执行程序, runtime: 需要运行时
	Executable  string `json:"executable"`   // 可执行程序路径或运行时命令
	Entry       string `json:"entry"`        // 入口文件（runtime类型需要）
	Args        string `json:"args"`         // 启动参数
	AutoRestart bool   `json:"auto_restart"` // 是否自动重启
	MaxRestarts int    `json:"max_restarts"` // 最大重启次数，0表示无限制
}

// Response 通用响应
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// StatusResponse 状态响应
type StatusResponse struct {
	AppName      string `json:"app_name,omitempty"` // 应用名称（多应用模式）
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Type         string `json:"type,omitempty"`
	Executable   string `json:"executable,omitempty"`
	Entry        string `json:"entry,omitempty"`
	AutoRestart  bool   `json:"auto_restart"`
	RestartCount int    `json:"restart_count"`
	Uptime       int64  `json:"uptime"` // 运行时间（秒）
}
