package handler

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/internal/server/staticserve"
)

func (h *Handler) closeStaticSite(appName string, removeFiles bool) (bool, error) {
	h.ttlScheduler.Cancel(appName)
	h.idleTracker.Cancel(appName)
	h.forgetIdleApp(appName)
	h.proxyMu.Lock()
	proxyServer := h.proxyServer
	h.proxyMu.Unlock()
	if proxyServer != nil && h.syncManager != nil {
		if app := h.syncManager.GetState().Apps[appName]; app != nil && app.ExposureToken != "" {
			proxyServer.Unregister(bareToken(app.ExposureToken))
		}
	}
	h.sitesMu.Lock()
	site, ok := h.sites[appName]
	if ok {
		delete(h.sites, appName)
	}
	h.sitesMu.Unlock()
	if !ok {
		return false, nil
	}
	var errs []error
	if err := site.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		errs = append(errs, err)
	}
	if removeFiles {
		if err := os.RemoveAll(site.Root()); err != nil {
			errs = append(errs, err)
		}
	}
	return true, errors.Join(errs...)
}

func (h *Handler) markStaticStopped(appName, reason string) {
	if h.syncManager == nil {
		return
	}
	app := h.syncManager.GetState().Apps[appName]
	if app == nil || app.Backend != "static" {
		return
	}
	app.Status = state.StatusStopped
	app.Port = 0
	app.TerminationReason = reason
	h.syncManager.ReplaceApp(appName, app)
	h.persistState()
}

// Shutdown stops all process and container backends, closes listeners, and flushes state.
func (h *Handler) Shutdown() error {
	h.ttlScheduler.Close()
	h.idleTracker.Close()
	h.proxyMu.Lock()
	proxyServer := h.proxyServer
	h.proxyServer = nil
	h.proxyMu.Unlock()
	if proxyServer != nil {
		if err := proxyServer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("close exposure proxy: %w", err)
		}
	}
	h.sitesMu.Lock()
	sites := make(map[string]*staticserve.Site, len(h.sites))
	for name, site := range h.sites {
		sites[name] = site
	}
	h.sites = make(map[string]*staticserve.Site)
	h.sitesMu.Unlock()

	h.dockerMu.Lock()
	dockerApps := make(map[string]dockerAppRecord, len(h.dockerApps))
	for name, record := range h.dockerApps {
		dockerApps[name] = record
	}
	h.dockerApps = make(map[string]dockerAppRecord)
	h.dockerMu.Unlock()

	var errs []error
	for name, site := range sites {
		if err := site.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			errs = append(errs, fmt.Errorf("close site %q: %w", name, err))
		}
	}
	for name, record := range dockerApps {
		if err := h.dockerEngine.StopAndRemove(context.Background(), record.containerID, 10); err != nil {
			errs = append(errs, fmt.Errorf("remove Docker app %q: %w", name, err))
		}
		if err := os.RemoveAll(record.root); err != nil {
			errs = append(errs, fmt.Errorf("remove Docker artifact %q: %w", name, err))
		}
	}
	if err := h.multiManager.StopAll(); err != nil {
		errs = append(errs, err)
	}
	if h.syncManager != nil {
		if err := h.syncManager.ManualPersist(); err != nil {
			errs = append(errs, err)
		}
		h.syncManager.Close()
	}
	return errors.Join(errs...)
}
