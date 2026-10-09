package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/runtime"
	"github.com/jiangfire/dzjjy/internal/server/state"
)

func (h *Handler) applyProcess(w http.ResponseWriter, r *http.Request, document *appconfig.Document, configText string) {
	if err := validateAppName(document.Metadata.Name); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(document.Spec.Command) == 0 {
		h.sendError(w, "spec.command is required for process apply", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(h.workDir, 0750); err != nil {
		h.sendError(w, "failed to prepare work directory", http.StatusInternalServerError)
		return
	}
	staging, err := os.MkdirTemp(h.workDir, ".apply-")
	if err != nil {
		h.sendError(w, "failed to create staging directory", http.StatusInternalServerError)
		return
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if document.Spec.Artifact != "" {
		if err := extractApplyArtifact(r.MultipartForm, document.Spec.Artifact, staging); err != nil {
			h.sendError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	commandPath := document.Spec.Command[0]
	if filepath.IsAbs(commandPath) {
		h.sendError(w, "process command must be relative to the managed work directory or found on PATH", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(commandPath, `/\\`) {
		resolvedCommand := filepath.Clean(filepath.Join(staging, commandPath))
		rel, err := filepath.Rel(staging, resolvedCommand)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			h.sendError(w, "process command escapes artifact directory", http.StatusBadRequest)
			return
		}
		info, err := os.Lstat(resolvedCommand)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			h.sendError(w, "process command executable is missing or unsafe", http.StatusBadRequest)
			return
		}
		if err := os.Chmod(resolvedCommand, info.Mode()|0o100); err != nil {
			h.sendError(w, "failed to mark command executable", http.StatusInternalServerError)
			return
		}
	}
	normalizedConfig, err := json.Marshal(document)
	if err != nil {
		h.sendError(w, "failed to normalize configuration", http.StatusInternalServerError)
		return
	}
	oldState := (*state.AppState)(nil)
	if h.syncManager != nil {
		oldState = h.syncManager.GetState().Apps[document.Metadata.Name]
	}
	if oldState != nil && oldState.Status == state.StatusRunning {
		if oldState.DesiredConfig == string(normalizedConfig) {
			h.sendApplyProcessResult(w, document, oldState.Generation, oldState.ExpiresAt, oldState.PID, "running")
			return
		}
		h.sendError(w, "application is running; stop it before applying a changed configuration", http.StatusConflict)
		return
	}

	appWorkDir := filepath.Join(h.workDir, document.Metadata.Name)
	backupDir := appWorkDir + ".previous"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(appWorkDir); err == nil {
		if err := os.Rename(appWorkDir, backupDir); err != nil {
			h.sendError(w, "failed to preserve previous work directory", http.StatusInternalServerError)
			return
		}
	}
	if err := os.Rename(staging, appWorkDir); err != nil {
		_ = os.Rename(backupDir, appWorkDir)
		h.sendError(w, "failed to activate staged artifact", http.StatusInternalServerError)
		return
	}
	keepStaging = true
	command := append([]string(nil), document.Spec.Command...)
	processConfig := &runtime.ProcessConfig{
		Type: runtime.TypeExec, WorkDir: appWorkDir, Executable: command[0], Command: command,
		AutoRestart: document.Spec.Restart == "on-failure",
	}
	ctx := r.Context()
	if err := h.multiManager.StartApp(ctx, document.Metadata.Name, processConfig); err != nil {
		_ = os.RemoveAll(appWorkDir)
		_ = os.Rename(backupDir, appWorkDir)
		h.sendError(w, fmt.Sprintf("failed to start app: %v", err), http.StatusInternalServerError)
		return
	}
	_ = os.RemoveAll(backupDir)
	info, _ := h.multiManager.GetAppInfo(document.Metadata.Name)
	generation := uint64(1)
	if oldState != nil {
		generation = oldState.Generation + 1
	}
	expiresAt := int64(0)
	if document.Spec.Lifecycle.TTL != "" {
		ttlDuration, _ := time.ParseDuration(document.Spec.Lifecycle.TTL)
		expiresAt = time.Now().Add(ttlDuration).Unix()
	}
	if h.syncManager != nil {
		encoded, _ := json.Marshal(document)
		h.syncManager.ReplaceApp(document.Metadata.Name, &state.AppState{
			Config: processConfig, DesiredConfig: string(encoded), Generation: generation,
			Backend: "process", PID: info.PID, StartTime: time.Now().Unix(),
			Status: state.StatusRunning, WorkPath: appWorkDir, ExpiresAt: expiresAt,
			ProcessStartID: info.ProcessStartID,
		})
		// 进程可能在 apply 返回前就已退出，其终态事件可能先于上面的 ReplaceApp 到达而被
		// running 快照覆盖。若运行时已不在线，等待终态事件落地；仍卡在 running 时以
		// 运行时事实兜底，避免状态永久停留在 running。
		if !info.Running {
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil && app.Status != state.StatusRunning {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil && app.Status == state.StatusRunning {
				app.Status = state.StatusStopped
				app.PID = 0
				h.syncManager.ReplaceApp(document.Metadata.Name, app)
			}
		}
		h.persistState()
	}
	if document.Spec.Lifecycle.TTL != "" {
		ttlDuration, _ := time.ParseDuration(document.Spec.Lifecycle.TTL)
		h.ttlScheduler.Cancel(document.Metadata.Name)
		h.ttlScheduler.Schedule(document.Metadata.Name, generation, time.Now().Add(ttlDuration), h.expireApp)
	}
	responseStatus, responsePID := "running", info.PID
	if h.syncManager != nil {
		if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil {
			responseStatus, responsePID = app.Status, app.PID
		}
	}
	h.sendApplyProcessResult(w, document, generation, expiresAt, responsePID, responseStatus)
}

func (h *Handler) sendApplyProcessResult(w http.ResponseWriter, document *appconfig.Document, generation uint64, expiresAt int64, pid int, status string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true, "message": "configuration applied",
		"data": map[string]any{"app_name": document.Metadata.Name, "kind": document.Kind, "status": status, "pid": pid, "generation": generation, "expires_at": expiresAt},
	})
}

func (h *Handler) expireApp(appName string, generation uint64) {
	if h.syncManager == nil {
		return
	}
	app := h.syncManager.GetState().Apps[appName]
	if app == nil || app.Generation != generation || app.Status != state.StatusRunning {
		return
	}
	if app.Backend == "static" {
		h.expireStaticSite(appName, generation)
		return
	}
	if app.Backend == "docker" {
		_, cleanupErr := h.stopDockerApp(appName, "ttl", true)
		app.Status = state.StatusExpired
		app.ContainerID = ""
		app.TerminationReason = "ttl"
		if cleanupErr != nil {
			app.Status = state.StatusFailed
			app.TerminationReason = "cleanup_failure"
		}
		h.syncManager.ReplaceApp(appName, app)
		h.persistState()
		return
	}
	// 进程可能已自行退出（状态事件尚未落地）；这不算清理失败。
	if err := h.multiManager.StopApp(appName); err != nil && !strings.Contains(err.Error(), "no running application") {
		app.Status = state.StatusFailed
		app.PID = 0
		app.TerminationReason = "cleanup_failure"
	} else {
		app.Status = state.StatusExpired
		app.PID = 0
		app.TerminationReason = "ttl"
	}
	h.syncManager.ReplaceApp(appName, app)
	h.persistState()
}
