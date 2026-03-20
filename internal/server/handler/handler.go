package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiangfire/dzjjy/internal/server/archive"
	"github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/pkg/api"
)

// Handler HTTP处理器（支持多应用）
type Handler struct {
	multiManager *runtime.MultiManager // 多应用管理器
	uploadDir    string
	workDir      string
	logDir       string
	stateStore   *state.StateStore     // 状态存储
	syncManager  *state.SyncManager    // 状态同步器
	restoreMgr   *state.RestoreManager // 恢复管理器
}

// NewHandler 创建处理器（不带状态持久化）
func NewHandler(uploadDir, workDir, logDir string) *Handler {
	return &Handler{
		multiManager: runtime.NewMultiManager(logDir),
		uploadDir:    uploadDir,
		workDir:      workDir,
		logDir:       logDir,
	}
}

// NewHandlerWithState 创建支持状态持久化的处理器
func NewHandlerWithState(uploadDir, workDir, logDir, stateFile string) *Handler {
	h := &Handler{
		uploadDir: uploadDir,
		workDir:   workDir,
		logDir:    logDir,
	}

	// 初始化状态持久化组件
	h.stateStore = state.NewStateStore(stateFile)
	h.syncManager = state.NewSyncManager(h.stateStore)
	h.restoreMgr = state.NewRestoreManager(h.stateStore, h.syncManager)

	// 创建多应用管理器（带状态通知器）
	h.multiManager = runtime.NewMultiManagerWithState(logDir, h)

	return h
}

// OnAppEvent 实现 StateNotifier 接口，接收 Manager 的事件并转发给 SyncManager
func (h *Handler) OnAppEvent(eventType string, appName string, config *runtime.ProcessConfig, pid int) {
	if h.syncManager == nil {
		return
	}

	// 发送事件到状态同步器（类型相同，无需转换）
	h.syncManager.OnAppEvent(state.AppEvent{
		Type:    eventType,
		AppName: appName,
		Config:  (*state.ProcessConfig)(config), // 类型别名转换
		PID:     pid,
	})
}

// RestoreState 恢复之前的状态并自动重启需要自动重启的应用
func (h *Handler) RestoreState() error {
	if h.restoreMgr == nil {
		return nil // 无状态持久化，直接返回
	}

	// 1. 加载状态文件并恢复到 syncManager
	err := h.restoreMgr.Restore()
	if err != nil {
		slog.Error("failed to restore state", "error", err)
		return err
	}

	// 2. 清理无效状态（已停止的进程）
	if err := h.restoreMgr.Cleanup(); err != nil {
		slog.Warn("cleanup failed", "error", err)
	}

	// 3. 从 syncManager 获取状态，先恢复到内存管理器，再决定是否自动重启
	stateData := h.syncManager.GetState()
	ctx := context.Background()

	for appName, appState := range stateData.Apps {
		if appState.Config != nil {
			if err := h.multiManager.RestoreApp(
				appName,
				(*runtime.ProcessConfig)(appState.Config),
				appState.PID,
				appState.StartTime,
				appState.RestartCount,
				appState.Status == state.StatusRunning,
				appState.LogPath,
			); err != nil {
				slog.Error("failed to restore app into runtime manager", "app", appName, "error", err)
			}
		}

		// 只处理已停止但需要自动重启的应用
		if appState.Status == state.StatusStopped &&
			appState.Config != nil &&
			appState.Config.AutoRestart {

			slog.Info("auto-restarting app on restore", "app", appName)

			// 类型相同，直接转换（无需手动复制字段）
			config := (*runtime.ProcessConfig)(appState.Config)

			// 启动应用
			if err := h.multiManager.StartApp(ctx, appName, config); err != nil {
				slog.Error("failed to auto-restart app", "app", appName, "error", err)
				// 继续处理其他应用
			}
		}
	}

	// 4. 手动触发持久化以保存重启后的状态
	if h.syncManager != nil {
		if err := h.syncManager.ManualPersist(); err != nil {
			slog.Warn("failed to persist state on restore", "error", err)
		}
	}

	return nil
}

// Deploy 处理部署请求（支持多应用）
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 提取和验证参数
	appName, config, err := h.extractDeployParams(r)
	if err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 2. 准备工作目录
	if err := h.prepareWorkDir(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. 处理文件上传和解压
	if err := h.handleFileUpload(r, appName); err != nil {
		h.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. 注册应用到状态管理器
	h.registerApp(appName, config)

	// 5. 启动应用
	ctx := r.Context()
	if err := h.multiManager.StartApp(ctx, appName, config); err != nil {
		h.sendError(w, fmt.Sprintf("failed to start app: %v", err), http.StatusInternalServerError)
		return
	}

	// 6. 持久化状态
	h.persistState()

	// 7. 返回成功响应
	manager, _ := h.multiManager.GetApp(appName)
	h.sendSuccess(w, "deployment successful", map[string]any{
		"app_name":     appName,
		"pid":          manager.GetPID(),
		"type":         config.Type,
		"executable":   config.Executable,
		"entry":        config.Entry,
		"auto_restart": config.AutoRestart,
		"max_restarts": config.MaxRestarts,
	})
}

// extractDeployParams 提取和验证部署参数
func (h *Handler) extractDeployParams(r *http.Request) (string, *runtime.ProcessConfig, error) {
	// 从 URL 路径获取应用名称
	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		return "", nil, err
	}

	// 解析multipart表单
	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB
		return "", nil, fmt.Errorf("failed to parse form: %v", err)
	}

	// 获取部署配置
	appType := r.FormValue("type")
	executable := r.FormValue("executable")
	entry := r.FormValue("entry")
	args := r.FormValue("args")
	autoRestart := r.FormValue("auto_restart") == "true"
	maxRestarts := 0
	if mr := r.FormValue("max_restarts"); mr != "" {
		if _, err := fmt.Sscanf(mr, "%d", &maxRestarts); err != nil {
			return "", nil, fmt.Errorf("invalid max_restarts: %v", err)
		}
	}

	// 验证配置
	if err := h.validateAppConfig(appType, executable, entry, maxRestarts); err != nil {
		return "", nil, err
	}

	// 检查应用是否已存在，如果存在且正在运行则停止
	if manager, exists := h.multiManager.GetApp(appName); exists && manager.IsRunning() {
		// 使用MultiManager的StopApp方法，而不是直接调用manager.Stop()
		if err := h.multiManager.StopApp(appName); err != nil {
			return "", nil, fmt.Errorf("failed to stop current app: %v", err)
		}
	}

	// 构建配置
	config := &runtime.ProcessConfig{
		Type:        appType,
		WorkDir:     filepath.Join(h.workDir, appName),
		Executable:  executable,
		Entry:       entry,
		Args:        args,
		AutoRestart: autoRestart,
		MaxRestarts: maxRestarts,
	}

	return appName, config, nil
}

// validateAppConfig 验证应用配置
func (h *Handler) validateAppConfig(appType, executable, entry string, maxRestarts int) error {
	if appType == "" || executable == "" {
		return fmt.Errorf("type and executable are required")
	}

	if appType != runtime.TypeExec && appType != runtime.TypeRuntime {
		return fmt.Errorf("invalid type: %s (must be 'exec' or 'runtime')", appType)
	}

	executable = strings.TrimSpace(executable)
	if executable == "" {
		return fmt.Errorf("executable cannot be empty")
	}

	if strings.ContainsAny(executable, "|;&`$()<>[]{}") {
		return fmt.Errorf("executable contains invalid characters")
	}

	if maxRestarts < 0 {
		return fmt.Errorf("max_restarts cannot be negative")
	}

	if appType == runtime.TypeRuntime && entry == "" {
		return fmt.Errorf("entry is required for runtime type")
	}

	return nil
}

// prepareWorkDir 准备应用工作目录
func (h *Handler) prepareWorkDir(appName string) error {
	appWorkDir := filepath.Join(h.workDir, appName)
	if err := os.RemoveAll(appWorkDir); err != nil { // #nosec G703 - appName is validated by validateAppName
		return fmt.Errorf("failed to clean work dir: %v", err)
	}
	if err := os.MkdirAll(appWorkDir, 0750); err != nil { // #nosec G703 - appName is validated by validateAppName
		return fmt.Errorf("failed to create work dir: %v", err)
	}
	return nil
}

// handleFileUpload 处理文件上传和解压
func (h *Handler) handleFileUpload(r *http.Request, appName string) error {
	file, header, err := r.FormFile("file")
	if err != nil {
		return fmt.Errorf("failed to get file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// 验证文件名
	if err := h.validateFilename(header.Filename); err != nil {
		return err
	}

	appUploadDir := filepath.Join(h.uploadDir, appName)
	if err := os.RemoveAll(appUploadDir); err != nil { // #nosec G703 - appName is validated by validateAppName
		return fmt.Errorf("failed to clean upload dir: %v", err)
	}
	if err := os.MkdirAll(appUploadDir, 0750); err != nil { // #nosec G703 - appName is validated by validateAppName
		return fmt.Errorf("failed to create upload dir: %v", err)
	}

	appWorkDir := filepath.Join(h.workDir, appName)
	stagedPath := filepath.Join(appUploadDir, header.Filename)

	// 验证路径安全（防止目录遍历）
	absUploadDir, _ := filepath.Abs(appUploadDir)
	absStagedPath, _ := filepath.Abs(stagedPath)
	if !strings.HasPrefix(absStagedPath, absUploadDir+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path: %s (path traversal detected)", header.Filename)
	}

	// 保存文件
	dest, err := os.Create(stagedPath) // #nosec G304,G703 - path validated above
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer func() { _ = dest.Close() }()

	if _, err := io.Copy(dest, file); err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}
	if err := dest.Close(); err != nil {
		return fmt.Errorf("failed to close file: %v", err)
	}

	// 处理压缩文件
	if archive.IsArchive(header.Filename) {
		// #nosec G706 - app/file names are operational diagnostics
		slog.Info("detected archive file, extracting",
			"app", appName,
			"filename", header.Filename,
			"dest", appWorkDir,
		)

		if err := archive.Extract(stagedPath, appWorkDir); err != nil {
			// #nosec G706 - app/file names are operational diagnostics
			slog.Error("failed to extract archive", "error", err, "file", header.Filename)
			if err := os.RemoveAll(appWorkDir); err != nil { // #nosec G703 - appName is validated by validateAppName
				slog.Warn("failed to remove work dir", "error", err)
			}
			if err := os.MkdirAll(appWorkDir, 0750); err != nil { // #nosec G703 - appName is validated by validateAppName
				slog.Warn("failed to recreate work dir", "error", err)
			}
			return fmt.Errorf("failed to extract archive: %v", err)
		}

		// #nosec G706 - app/file names are operational diagnostics
		slog.Info("archive extracted successfully", "app", appName, "file", header.Filename)
		return nil
	}

	workPath := filepath.Join(appWorkDir, header.Filename)
	absWorkPath, _ := filepath.Abs(workPath)
	if absWorkPath != absStagedPath {
		if err := copyFileBetweenRoots(appUploadDir, stagedPath, appWorkDir, workPath); err != nil {
			return fmt.Errorf("failed to copy uploaded file to work dir: %v", err)
		}
	}

	// 简单策略：如果文件没有扩展名，或者扩展名为 .exe/.bin，设置可执行权限
	execExt := filepath.Ext(header.Filename)
	noExt := execExt == ""
	isExecExt := execExt == ".exe" || execExt == ".bin"
	if noExt || isExecExt {
		if err := os.Chmod(workPath, 0755); err != nil { // #nosec G302,G703 - executable artifact requires execute bit and path is validated
			slog.Warn("failed to set executable permissions", "error", err)
		}
	}

	return nil
}

func copyFileBetweenRoots(srcRootDir, srcPath, dstRootDir, dstPath string) error {
	srcRelPath, err := filepath.Rel(srcRootDir, srcPath)
	if err != nil {
		return err
	}
	dstRelPath, err := filepath.Rel(dstRootDir, dstPath)
	if err != nil {
		return err
	}

	srcRoot, err := os.OpenRoot(srcRootDir)
	if err != nil {
		return err
	}
	defer func() { _ = srcRoot.Close() }()

	dstRoot, err := os.OpenRoot(dstRootDir)
	if err != nil {
		return err
	}
	defer func() { _ = dstRoot.Close() }()

	src, err := srcRoot.Open(srcRelPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := dstRoot.Create(dstRelPath)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

// validateFilename 验证文件名
func (h *Handler) validateFilename(filename string) error {
	if filename == "" {
		return fmt.Errorf("filename cannot be empty")
	}
	if len(filename) > 255 {
		return fmt.Errorf("filename too long")
	}
	if strings.ContainsAny(filename, ":\\<>|\"*?") {
		return fmt.Errorf("invalid filename")
	}
	return nil
}

func validateAppName(appName string) error {
	if appName == "" {
		return fmt.Errorf("app name cannot be empty")
	}
	if len(appName) > 64 {
		return fmt.Errorf("app name too long")
	}
	if appName == "." || appName == ".." {
		return fmt.Errorf("invalid app name")
	}
	if strings.Contains(appName, "..") || strings.ContainsAny(appName, `/\`) {
		return fmt.Errorf("invalid app name")
	}
	for _, ch := range appName {
		isLower := ch >= 'a' && ch <= 'z'
		isUpper := ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if !isLower && !isUpper && !isDigit && ch != '-' && ch != '_' && ch != '.' {
			return fmt.Errorf("invalid app name")
		}
	}
	return nil
}

// registerApp 注册应用到状态管理器
func (h *Handler) registerApp(appName string, config *runtime.ProcessConfig) {
	if h.syncManager == nil {
		return
	}

	// 类型相同，直接转换（无需手动复制字段）
	h.syncManager.OnAppEvent(state.AppEvent{
		Type:    "register",
		AppName: appName,
		Config:  (*state.ProcessConfig)(config),
	})
}

// persistState 触发状态持久化
func (h *Handler) persistState() {
	if h.syncManager != nil {
		if err := h.syncManager.ManualPersist(); err != nil {
			slog.Warn("failed to persist state", "error", err)
		}
	}
}

// Stop 停止应用（支持多应用）
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.multiManager.StopApp(appName); err != nil {
		h.sendError(w, fmt.Sprintf("failed to stop: %v", err), http.StatusInternalServerError)
		return
	}

	h.persistState()
	h.sendSuccess(w, "application stopped", map[string]any{"app_name": appName})
}

// Start 启动应用（支持多应用）
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req api.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// 验证配置
	if err := h.validateAppConfig(req.Type, req.Executable, req.Entry, req.MaxRestarts); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 检查应用是否已存在且正在运行
	if manager, exists := h.multiManager.GetApp(appName); exists && manager.IsRunning() {
		h.sendError(w, "application is already running", http.StatusBadRequest)
		return
	}

	// 准备应用工作目录
	appWorkDir := filepath.Join(h.workDir, appName)
	if err := os.MkdirAll(appWorkDir, 0750); err != nil { // #nosec G703 - appName is validated by validateAppName
		h.sendError(w, fmt.Sprintf("failed to create work dir: %v", err), http.StatusInternalServerError)
		return
	}

	// 创建进程配置
	config := &runtime.ProcessConfig{
		Type:        req.Type,
		WorkDir:     appWorkDir,
		Executable:  req.Executable,
		Entry:       req.Entry,
		Args:        req.Args,
		AutoRestart: req.AutoRestart,
		MaxRestarts: req.MaxRestarts,
	}

	// 注册应用到状态管理器
	h.registerApp(appName, config)

	// 启动应用
	ctx := r.Context()
	if err := h.multiManager.StartApp(ctx, appName, config); err != nil {
		h.sendError(w, fmt.Sprintf("failed to start: %v", err), http.StatusInternalServerError)
		return
	}

	// 持久化状态
	h.persistState()

	// 获取启动后的信息
	manager, _ := h.multiManager.GetApp(appName)
	h.sendSuccess(w, "application started", map[string]any{
		"app_name": appName,
		"pid":      manager.GetPID(),
	})
}

// Restart 重启应用（支持多应用）
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.multiManager.RestartApp(appName); err != nil {
		h.sendError(w, fmt.Sprintf("failed to restart: %v", err), http.StatusInternalServerError)
		return
	}

	h.persistState()

	// 获取重启后的信息
	manager, _ := h.multiManager.GetApp(appName)
	h.sendSuccess(w, "application restarted", map[string]any{
		"app_name": appName,
		"pid":      manager.GetPID(),
	})
}

// Status 查询状态（支持多应用）
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, exists := h.multiManager.GetAppInfo(appName)
	if !exists {
		h.sendError(w, "app not found", http.StatusNotFound)
		return
	}

	status := api.StatusResponse{
		Running:      info.Running,
		PID:          info.PID,
		Type:         info.Type,
		Executable:   info.Executable,
		Entry:        info.Entry,
		AutoRestart:  info.AutoRestart,
		RestartCount: info.RestartCount,
		Uptime:       info.Uptime,
		AppName:      appName,
	}

	h.sendSuccess(w, "ok", status)
}

// Logs 查询日志（支持多应用）
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 获取查询参数
	linesStr := r.URL.Query().Get("lines")
	lines := 100 // 默认返回最近 100 行

	if linesStr != "" {
		if _, err := fmt.Sscanf(linesStr, "%d", &lines); err != nil {
			slog.Warn("invalid lines parameter", "error", err)
			lines = 100 // 使用默认值
		}
	}

	// 获取日志
	manager, exists := h.multiManager.GetApp(appName)
	if !exists {
		h.sendError(w, "app not found", http.StatusNotFound)
		return
	}

	logs := manager.GetLogs(lines)
	logFile := manager.GetLogFile()

	h.sendSuccess(w, "ok", map[string]any{
		"app_name": appName,
		"logs":     logs,
		"log_file": logFile,
		"count":    len(logs),
	})
}

// ListApps 列出所有应用
func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apps := h.multiManager.ListApps()
	infos := make(map[string]api.StatusResponse)

	for _, appName := range apps {
		if info, exists := h.multiManager.GetAppInfo(appName); exists {
			infos[appName] = api.StatusResponse{
				Running:      info.Running,
				PID:          info.PID,
				Type:         info.Type,
				Executable:   info.Executable,
				Entry:        info.Entry,
				AutoRestart:  info.AutoRestart,
				RestartCount: info.RestartCount,
				Uptime:       info.Uptime,
				AppName:      appName,
			}
		}
	}

	h.sendSuccess(w, "ok", map[string]any{
		"count": len(apps),
		"apps":  infos,
	})
}

// RemoveApp 删除应用（停止并清理）
func (h *Handler) RemoveApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appName := h.getAppNameWithDefault(r)
	if err := validateAppName(appName); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 停止应用
	if err := h.multiManager.StopApp(appName); err != nil {
		// 如果应用不存在，也返回成功（幂等性）
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no running application") {
			h.sendError(w, fmt.Sprintf("failed to stop: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if err := h.multiManager.RemoveApp(appName); err != nil && !strings.Contains(err.Error(), "not found") {
		h.sendError(w, fmt.Sprintf("failed to remove runtime state: %v", err), http.StatusInternalServerError)
		return
	}

	// 清理工作目录
	appWorkDir := filepath.Join(h.workDir, appName)
	if err := os.RemoveAll(appWorkDir); err != nil { // #nosec G703 - appName is validated by validateAppName
		slog.Warn("failed to remove work dir", "error", err)
	}
	appUploadDir := filepath.Join(h.uploadDir, appName)
	if err := os.RemoveAll(appUploadDir); err != nil { // #nosec G703 - appName is validated by validateAppName
		slog.Warn("failed to remove upload dir", "error", err)
	}

	// 从状态管理器移除
	if h.syncManager != nil {
		h.syncManager.OnAppEvent(state.AppEvent{
			Type:    "deregister",
			AppName: appName,
		})
		h.persistState()
	}

	h.sendSuccess(w, "application removed", map[string]any{"app_name": appName})
}

// extractAppName 从 URL 路径提取应用名称
// 路径格式: /api/v1/apps/{name}/deploy 或 /api/v1/deploy (返回空，使用默认)
func (h *Handler) extractAppName(path string) string {
	parts := strings.Split(path, "/")
	// 查找 "apps" 关键字
	for i, part := range parts {
		if part == "apps" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// getAppNameWithDefault 从请求中提取应用名称，提供默认值
func (h *Handler) getAppNameWithDefault(r *http.Request) string {
	appName := h.extractAppName(r.URL.Path)
	if appName == "" {
		return "default"
	}
	return appName
}

// sendSuccess 发送成功响应
func (h *Handler) sendSuccess(w http.ResponseWriter, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(api.Response{
		Success: true,
		Message: message,
		Data:    data,
	}); err != nil {
		slog.Warn("failed to encode success response", "error", err)
	}
}

// sendError 发送错误响应
func (h *Handler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(api.Response{
		Success: false,
		Message: message,
	}); err != nil {
		slog.Warn("failed to encode error response", "error", err)
	}
}

// GetStateSnapshot 获取所有应用的状态快照（用于持久化）
func (h *Handler) GetStateSnapshot() map[string]*state.AppState {
	if h.syncManager == nil {
		return nil
	}
	stateData := h.syncManager.GetState()
	return stateData.Apps
}
