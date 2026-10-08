package api

// ProcessConfig configures a managed process while retaining legacy string args.
type ProcessConfig struct {
	Type        string   `json:"type"`
	WorkDir     string   `json:"work_dir"`
	Executable  string   `json:"executable"`
	Entry       string   `json:"entry"`
	Args        string   `json:"args"`
	Command     []string `json:"command,omitempty"`
	AutoRestart bool     `json:"auto_restart"`
	MaxRestarts int      `json:"max_restarts"`
}

// DeployRequest is the legacy process deploy request.
type DeployRequest struct {
	Type        string `json:"type"`
	Executable  string `json:"executable"`
	Entry       string `json:"entry"`
	Args        string `json:"args"`
	AutoRestart bool   `json:"auto_restart"`
	MaxRestarts int    `json:"max_restarts"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type StatusResponse struct {
	AppName      string `json:"app_name,omitempty"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Type         string `json:"type,omitempty"`
	Executable   string `json:"executable,omitempty"`
	Entry        string `json:"entry,omitempty"`
	AutoRestart  bool   `json:"auto_restart"`
	RestartCount int    `json:"restart_count"`
	Uptime       int64  `json:"uptime"`
}
