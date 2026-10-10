package handler

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/proxy"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/internal/server/staticserve"
)

func (h *Handler) recordStaticSite(document *appconfig.Document, siteRoot string, port int) (uint64, int64) {
	generation := uint64(1)
	var expiresAt int64
	var ttlDuration time.Duration
	if document.Spec.Lifecycle.TTL != "" {
		ttlDuration, _ = time.ParseDuration(document.Spec.Lifecycle.TTL)
		expiresAt = time.Now().Add(ttlDuration).Unix()
	}
	idleSeconds := 0
	if timeout := idleTimeout(document); timeout > 0 {
		idleSeconds = int(timeout.Seconds())
	}
	exposureToken := ""
	if document.Spec.Exposure != nil {
		exposureToken = document.Spec.Exposure.Token
	}
	if h.syncManager != nil {
		if old := h.syncManager.GetState().Apps[document.Metadata.Name]; old != nil {
			generation = old.Generation + 1
			if exposureToken == "" {
				exposureToken = old.ExposureToken
			}
		}
		config, _ := json.Marshal(document)
		h.syncManager.ReplaceApp(document.Metadata.Name, &state.AppState{
			DesiredConfig: string(config), Generation: generation, Backend: "static",
			Port: port, Status: state.StatusRunning, WorkPath: siteRoot,
			StartTime: time.Now().Unix(), ExpiresAt: expiresAt,
			IdleTimeoutSeconds: idleSeconds, ExposureToken: exposureToken,
			LastActivityUnix: time.Now().Unix(),
		})
		h.persistState()
	}
	if ttlDuration > 0 {
		h.ttlScheduler.Cancel(document.Metadata.Name)
		h.ttlScheduler.Schedule(document.Metadata.Name, generation, time.Now().Add(ttlDuration), h.expireApp)
	}
	return generation, expiresAt
}

func (h *Handler) expireStaticSite(appName string, generation uint64) {
	if h.syncManager == nil {
		return
	}
	app := h.syncManager.GetState().Apps[appName]
	if app == nil || app.Generation != generation || app.Status != state.StatusRunning {
		return
	}
	_, _ = h.closeStaticSite(appName, true)
	app = h.syncManager.GetState().Apps[appName]
	if app != nil && app.Generation == generation {
		app.Status = state.StatusExpired
		app.Port = 0
		app.TerminationReason = "ttl"
		h.syncManager.ReplaceApp(appName, app)
		h.persistState()
	}
}

func (h *Handler) restoreStaticSites(data *state.StateData) {
	if data == nil || h.syncManager == nil {
		return
	}
	for appName, app := range data.Apps {
		if app.Backend != "static" || app.Status != state.StatusRunning {
			continue
		}
		if app.ExpiresAt > 0 && app.ExpiresAt <= time.Now().Unix() {
			_ = os.RemoveAll(app.WorkPath)
			app.Status = state.StatusExpired
			app.Port = 0
			app.TerminationReason = "ttl"
			h.syncManager.ReplaceApp(appName, app)
			continue
		}
		site, err := staticserve.Start("0.0.0.0", app.WorkPath, app.Port)
		if err != nil {
			app.Status = state.StatusFailed
			app.Port = 0
			app.TerminationReason = "restore_failed"
			h.syncManager.ReplaceApp(appName, app)
			continue
		}
		h.sitesMu.Lock()
		h.sites[appName] = site
		h.sitesMu.Unlock()
		h.rememberIdleApp(appName, idleAppRecord{
			generation: app.Generation,
			backend:    "static",
			workPath:   app.WorkPath,
			timeout:    time.Duration(app.IdleTimeoutSeconds) * time.Second,
			token:      bareToken(app.ExposureToken),
		})
		if token := bareToken(app.ExposureToken); token != "" {
			if server, err := h.ensureProxy(); err == nil {
				_ = server.Register(token, proxy.Target{
					AppName:    appName,
					Generation: app.Generation,
					Upstream:   &siteUpstream{site: site},
				})
			}
		}
		if app.IdleTimeoutSeconds > 0 {
			h.idleTracker.Schedule(appName, app.Generation, time.Duration(app.IdleTimeoutSeconds)*time.Second)
		}
		if app.ExpiresAt > 0 {
			h.ttlScheduler.Schedule(appName, app.Generation, time.Unix(app.ExpiresAt, 0), h.expireApp)
		}
	}
}

func (h *Handler) restoreDockerApps(data *state.StateData) {
	if data == nil || h.syncManager == nil {
		return
	}
	for appName, app := range data.Apps {
		if app.Backend != "docker" || app.Status != state.StatusRunning || app.ContainerID == "" {
			continue
		}
		if app.ExpiresAt > 0 && app.ExpiresAt <= time.Now().Unix() {
			_ = h.dockerEngine.StopAndRemove(context.Background(), app.ContainerID, 10)
			_ = os.RemoveAll(app.WorkPath)
			app.ContainerID = ""
			app.Status = state.StatusExpired
			app.TerminationReason = "ttl"
			h.syncManager.ReplaceApp(appName, app)
			continue
		}
		if err := h.dockerEngine.StartExisting(context.Background(), app.ContainerID); err != nil {
			app.ContainerID = ""
			app.Status = state.StatusFailed
			app.TerminationReason = "restore_failed"
			h.syncManager.ReplaceApp(appName, app)
			continue
		}
		h.dockerMu.Lock()
		h.dockerApps[appName] = dockerAppRecord{containerID: app.ContainerID, root: app.WorkPath, generation: app.Generation}
		h.dockerMu.Unlock()
		h.rememberIdleApp(appName, idleAppRecord{
			generation: app.Generation,
			backend:    "docker",
			workPath:   app.WorkPath,
			timeout:    time.Duration(app.IdleTimeoutSeconds) * time.Second,
		})
		if app.IdleTimeoutSeconds > 0 {
			h.idleTracker.Schedule(appName, app.Generation, time.Duration(app.IdleTimeoutSeconds)*time.Second)
		}
		if app.ExpiresAt > 0 {
			h.ttlScheduler.Schedule(appName, app.Generation, time.Unix(app.ExpiresAt, 0), h.expireApp)
		}
	}
}
