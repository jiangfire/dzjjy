package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jiangfire/dzjjy/internal/server/archive"
	"github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/jiangfire/dzjjy/pkg/api"
)

// Handler HTTP处理器
type Handler struct {
	manager   *runtime.Manager
	uploadDir string
	workDir   string
	logDir    string
}

// NewHandler 创建处理器
func NewHandler(uploadDir, workDir, logDir string) *Handler {
	return &Handler{
		manager:   runtime.NewManager(logDir),
		uploadDir: uploadDir,
		workDir:   workDir,
		logDir:    logDir,
	}
}

// Deploy 处理部署请求
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析multipart表单
	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB
		h.sendError(w, fmt.Sprintf("failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	// 获取部署配置
	appType := r.FormValue("type")
	executable := r.FormValue("executable")
	entry := r.FormValue("entry")
	args := r.FormValue("args")
	autoRestart := r.FormValue("auto_restart") == "true"
	maxRestarts := 0
	if mr := r.FormValue("max_restarts"); mr != "" {
		fmt.Sscanf(mr, "%d", &maxRestarts)
	}

	if appType == "" || executable == "" {
		h.sendError(w, "type and executable are required", http.StatusBadRequest)
		return
	}

	// 停止当前运行的应用
	if h.manager.IsRunning() {
		if err := h.manager.Stop(); err != nil {
			h.sendError(w, fmt.Sprintf("failed to stop current app: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// 清理工作目录
	if err := os.RemoveAll(h.workDir); err != nil {
		h.sendError(w, fmt.Sprintf("failed to clean work dir: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(h.workDir, 0755); err != nil {
		h.sendError(w, fmt.Sprintf("failed to create work dir: %v", err), http.StatusInternalServerError)
		return
	}

	// 保存上传的文件
	file, header, err := r.FormFile("file")
	if err != nil {
		h.sendError(w, fmt.Sprintf("failed to get file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 保存到工作目录
	destPath := filepath.Join(h.workDir, header.Filename)
	dest, err := os.Create(destPath)
	if err != nil {
		h.sendError(w, fmt.Sprintf("failed to create file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		h.sendError(w, fmt.Sprintf("failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	dest.Close() // 关闭文件以便解压

	// 检查是否是压缩文件，如果是则解压
	if archive.IsArchive(header.Filename) {
		slog.Info("detected archive file, extracting",
			"filename", header.Filename,
			"dest", h.workDir,
		)

		if err := archive.Extract(destPath, h.workDir); err != nil {
			slog.Error("failed to extract archive",
				"error", err,
				"file", header.Filename,
			)
			h.sendError(w, fmt.Sprintf("failed to extract archive: %v", err), http.StatusInternalServerError)
			return
		}

		// 删除压缩文件
		if err := os.Remove(destPath); err != nil {
			slog.Warn("failed to remove archive file",
				"error", err,
				"file", destPath,
			)
		}

		slog.Info("archive extracted successfully", "file", header.Filename)
	}

	// 启动应用
	if err := h.manager.Start(appType, h.workDir, executable, entry, args, autoRestart, maxRestarts); err != nil {
		h.sendError(w, fmt.Sprintf("failed to start app: %v", err), http.StatusInternalServerError)
		return
	}

	h.sendSuccess(w, "deployment successful", map[string]any{
		"pid":          h.manager.GetPID(),
		"type":         appType,
		"executable":   executable,
		"entry":        entry,
		"auto_restart": autoRestart,
		"max_restarts": maxRestarts,
	})
}

// Stop 停止应用
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.manager.IsRunning() {
		h.sendError(w, "no running application", http.StatusBadRequest)
		return
	}

	if err := h.manager.Stop(); err != nil {
		h.sendError(w, fmt.Sprintf("failed to stop: %v", err), http.StatusInternalServerError)
		return
	}

	h.sendSuccess(w, "application stopped", nil)
}

// Start 启动应用
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req api.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if h.manager.IsRunning() {
		h.sendError(w, "application is already running", http.StatusBadRequest)
		return
	}

	if err := h.manager.Start(req.Type, h.workDir, req.Executable, req.Entry, req.Args, req.AutoRestart, req.MaxRestarts); err != nil {
		h.sendError(w, fmt.Sprintf("failed to start: %v", err), http.StatusInternalServerError)
		return
	}

	h.sendSuccess(w, "application started", map[string]any{
		"pid": h.manager.GetPID(),
	})
}

// Restart 重启应用
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.manager.IsRunning() {
		h.sendError(w, "no running application", http.StatusBadRequest)
		return
	}

	if err := h.manager.Restart(); err != nil {
		h.sendError(w, fmt.Sprintf("failed to restart: %v", err), http.StatusInternalServerError)
		return
	}

	h.sendSuccess(w, "application restarted", map[string]any{
		"pid": h.manager.GetPID(),
	})
}

// Status 查询状态
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appType, executable, entry, autoRestart, restartCount, uptime := h.manager.GetInfo()
	status := api.StatusResponse{
		Running:      h.manager.IsRunning(),
		PID:          h.manager.GetPID(),
		Type:         appType,
		Executable:   executable,
		Entry:        entry,
		AutoRestart:  autoRestart,
		RestartCount: restartCount,
		Uptime:       uptime,
	}

	h.sendSuccess(w, "ok", status)
}

// Logs 查询日志
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取查询参数
	linesStr := r.URL.Query().Get("lines")
	lines := 100 // 默认返回最近 100 行

	if linesStr != "" {
		fmt.Sscanf(linesStr, "%d", &lines)
	}

	// 获取日志
	logs := h.manager.GetLogs(lines)
	logFile := h.manager.GetLogFile()

	h.sendSuccess(w, "ok", map[string]any{
		"logs":     logs,
		"log_file": logFile,
		"count":    len(logs),
	})
}

// sendSuccess 发送成功响应
func (h *Handler) sendSuccess(w http.ResponseWriter, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(api.Response{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// sendError 发送错误响应
func (h *Handler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(api.Response{
		Success: false,
		Message: message,
	})
}
