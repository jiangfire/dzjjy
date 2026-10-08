package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/state"
)

type dockerAppRecord struct {
	containerID string
	root        string
	generation  uint64
}

func (h *Handler) applyDocker(w http.ResponseWriter, r *http.Request, document *appconfig.Document) {
	if err := validateAppName(document.Metadata.Name); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if document.Spec.Runtime == nil || len(document.Spec.Command) == 0 || document.Spec.Artifact == "" {
		h.sendError(w, "Docker App requires a supported runtime template, command, and artifact", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(h.workDir, 0750); err != nil {
		h.sendError(w, "failed to prepare work directory", http.StatusInternalServerError)
		return
	}
	staging, err := os.MkdirTemp(h.workDir, ".docker-apply-")
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
	if err := extractApplyArtifact(r.MultipartForm, document.Spec.Artifact, staging); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		h.sendError(w, "failed to normalize configuration", http.StatusInternalServerError)
		return
	}
	var oldState *state.AppState
	if h.syncManager != nil {
		oldState = h.syncManager.GetState().Apps[document.Metadata.Name]
	}
	if oldState != nil && oldState.Status == state.StatusRunning {
		if oldState.DesiredConfig == string(canonical) {
			h.sendApplyDockerResult(w, document, oldState.Generation, oldState.ExpiresAt, oldState.ContainerID)
			return
		}
		h.sendError(w, "application is running; stop it before applying a changed configuration", http.StatusConflict)
		return
	}
	appDir := filepath.Join(h.workDir, document.Metadata.Name)
	backupDir := appDir + ".previous"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(appDir); err == nil {
		if err := os.Rename(appDir, backupDir); err != nil {
			h.sendError(w, "failed to preserve previous artifact", http.StatusInternalServerError)
			return
		}
	}
	if err := os.Rename(staging, appDir); err != nil {
		_ = os.Rename(backupDir, appDir)
		h.sendError(w, "failed to activate artifact", http.StatusInternalServerError)
		return
	}
	keepStaging = true
	generation := uint64(1)
	if oldState != nil {
		generation = oldState.Generation + 1
	}
	command := append([]string(nil), document.Spec.Command...)
	containerID, err := h.dockerEngine.Start(r.Context(), document.Metadata.Name, generation, document.Spec.Runtime.Type, document.Spec.Runtime.Version, appDir, command)
	if err != nil {
		_ = os.RemoveAll(appDir)
		_ = os.Rename(backupDir, appDir)
		h.sendError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	_ = os.RemoveAll(backupDir)
	h.dockerMu.Lock()
	h.dockerApps[document.Metadata.Name] = dockerAppRecord{containerID: containerID, root: appDir, generation: generation}
	h.dockerMu.Unlock()
	expiresAt := int64(0)
	if document.Spec.Lifecycle.TTL != "" {
		ttlDuration, _ := time.ParseDuration(document.Spec.Lifecycle.TTL)
		expiresAt = time.Now().Add(ttlDuration).Unix()
		h.ttlScheduler.Schedule(document.Metadata.Name, generation, time.Now().Add(ttlDuration), h.expireApp)
	}
	if h.syncManager != nil {
		h.syncManager.ReplaceApp(document.Metadata.Name, &state.AppState{
			DesiredConfig: string(canonical), Generation: generation, Backend: "docker",
			ContainerID: containerID, WorkPath: appDir, Status: state.StatusRunning,
			StartTime: time.Now().Unix(), ExpiresAt: expiresAt,
		})
		h.persistState()
	}
	h.sendApplyDockerResult(w, document, generation, expiresAt, containerID)
}

func (h *Handler) sendApplyDockerResult(w http.ResponseWriter, document *appconfig.Document, generation uint64, expiresAt int64, containerID string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "configuration applied", "data": map[string]any{
		"app_name": document.Metadata.Name, "kind": document.Kind, "status": "running",
		"container_id": containerID, "generation": generation, "expires_at": expiresAt,
	}})
}

func (h *Handler) stopDockerApp(appName, reason string, removeFiles bool) (bool, error) {
	h.ttlScheduler.Cancel(appName)
	h.dockerMu.Lock()
	record, ok := h.dockerApps[appName]
	if ok {
		delete(h.dockerApps, appName)
	}
	h.dockerMu.Unlock()
	if !ok && h.syncManager != nil {
		if app := h.syncManager.GetState().Apps[appName]; app != nil && app.Backend == "docker" {
			record = dockerAppRecord{containerID: app.ContainerID, root: app.WorkPath, generation: app.Generation}
			ok = record.containerID != ""
		}
	}
	if !ok {
		return false, nil
	}
	err := h.dockerEngine.StopAndRemove(context.Background(), record.containerID, 10)
	if removeFiles {
		if removeErr := os.RemoveAll(record.root); err == nil {
			err = removeErr
		}
	}
	if h.syncManager != nil {
		if app := h.syncManager.GetState().Apps[appName]; app != nil && app.Generation == record.generation {
			app.Status = state.StatusStopped
			app.ContainerID = ""
			app.TerminationReason = reason
			if removeFiles {
				app.Port = 0
			}
			h.syncManager.ReplaceApp(appName, app)
			h.persistState()
		}
	}
	return true, err
}
