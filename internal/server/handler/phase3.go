package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/dockerbackend"
	"github.com/jiangfire/dzjjy/internal/server/proxy"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/internal/server/staticserve"
)

// buildDockerOptions translates a validated document into Docker engine
// options. Every secret is resolved here and nowhere else, so a missing secret
// fails the apply instead of silently starting a container without it.
func (h *Handler) buildDockerOptions(document *appconfig.Document) (dockerbackend.Options, error) {
	opts := dockerbackend.Options{
		Env:       map[string]string{},
		SecretEnv: map[string]string{},
		Isolator:  document.Spec.Sandbox.Isolator,
	}
	for _, env := range document.Spec.Env {
		opts.Env[env.Name] = env.Value
	}
	for _, secret := range document.Spec.Secrets {
		value, ok := h.secrets.Resolve(secret.Key)
		if !ok {
			return dockerbackend.Options{}, fmt.Errorf("secret %q is not present in the platform secret store", secret.Key)
		}
		name := secret.Env
		if name == "" {
			name = sanitizeEnvName(secret.Key)
		}
		opts.SecretEnv[name] = value
	}
	for _, volume := range document.Spec.Volumes {
		opts.Volumes = append(opts.Volumes, dockerbackend.VolumeMount{Name: volume.Name, Path: volume.Path})
	}
	if document.Spec.Network != nil {
		for _, mapping := range document.Spec.Network.Ports {
			opts.Ports = append(opts.Ports, dockerbackend.PortBinding{Container: mapping.Container, Host: mapping.Host})
		}
	}
	if !document.Spec.Resources.IsZero() {
		limits := dockerbackend.ResourceLimits{Pids: 0}
		if document.Spec.Resources.Memory != "" {
			size, err := appconfig.ParseByteSize(document.Spec.Resources.Memory)
			if err != nil {
				return dockerbackend.Options{}, err
			}
			limits.Memory = size
		}
		if document.Spec.Resources.MemorySwap != "" {
			size, err := appconfig.ParseByteSize(document.Spec.Resources.MemorySwap)
			if err != nil {
				return dockerbackend.Options{}, err
			}
			limits.MemorySwap = size
		}
		if document.Spec.Resources.CPUs != "" {
			cpus, err := appconfig.ParseCPUs(document.Spec.Resources.CPUs)
			if err != nil {
				return dockerbackend.Options{}, err
			}
			limits.CPUs = cpus
		}
		if document.Spec.Resources.Pids != nil {
			limits.Pids = *document.Spec.Resources.Pids
		}
		opts.Resources = limits
	}
	return opts, nil
}

// sanitizeEnvName derives a valid environment variable name from a secret key.
func sanitizeEnvName(key string) string {
	var builder strings.Builder
	for _, ch := range key {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		default:
			builder.WriteRune('_')
		}
	}
	name := builder.String()
	if name == "" {
		return "DZJJY_SECRET"
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "_" + name
	}
	return name
}

// idleTimeout resolves spec.lifecycle.idleTimeout into a duration.
func idleTimeout(document *appconfig.Document) time.Duration {
	if document.Spec.Lifecycle.IdleTimeout == "" {
		return 0
	}
	duration, err := time.ParseDuration(document.Spec.Lifecycle.IdleTimeout)
	if err != nil || duration <= 0 {
		return 0
	}
	return duration
}

// ensureProxy starts the token proxy the first time a site is exposed.
func (h *Handler) ensureProxy() (*proxy.Proxy, error) {
	h.proxyMu.Lock()
	defer h.proxyMu.Unlock()
	if h.proxyServer != nil {
		return h.proxyServer, nil
	}
	server, err := proxy.Start("0.0.0.0", 0, h.resumeSuspendedApp, h.touchIdleApp)
	if err != nil {
		return nil, err
	}
	h.proxyServer = server
	return server, nil
}

// exposureToken returns the token a StaticSite is published under, generating
// one on first use when the manifest does not pin it.
func (h *Handler) exposureToken(appName string, document *appconfig.Document, existing string) (string, error) {
	if document.Spec.Exposure != nil && document.Spec.Exposure.Token != "" {
		return document.Spec.Exposure.Token, nil
	}
	if existing != "" {
		return existing, nil
	}
	token, err := proxy.NewToken()
	if err != nil {
		return "", err
	}
	return token, nil
}

// publishSite registers a StaticSite with the token proxy and returns the URL.
func (h *Handler) publishSite(appName string, document *appconfig.Document, site *staticserve.Site, generation uint64, existing string) (string, error) {
	if document.Spec.Exposure == nil {
		return "", nil
	}
	token, err := h.exposureToken(appName, document, existing)
	if err != nil {
		return "", err
	}
	server, err := h.ensureProxy()
	if err != nil {
		return "", err
	}
	upstream := &siteUpstream{site: site}
	if err := server.Register(token, proxy.Target{AppName: appName, Generation: generation, Upstream: upstream}); err != nil {
		return "", err
	}
	timeout := idleTimeout(document)
	if timeout > 0 {
		h.idleTracker.Schedule(appName, generation, timeout)
	}
	return fmt.Sprintf("%s%s/", proxy.TokenPrefix, token), nil
}

// siteUpstream adapts a StaticSite to the proxy upstream interface.
type siteUpstream struct {
	site *staticserve.Site
}

func (u *siteUpstream) HostPort() string { return "static" }

func (u *siteUpstream) Serve(w http.ResponseWriter, r *http.Request) {
	u.site.ServeHTTP(w, r)
}

// touchIdleApp resets an app's idle window after proxy traffic.
func (h *Handler) touchIdleApp(appName string, generation uint64) {
	if h.idleTracker == nil {
		return
	}
	timeout := h.idleTimeoutFor(appName)
	if timeout <= 0 {
		return
	}
	h.idleTracker.Touch(appName, generation, timeout)
}

// idleTimeoutFor reads the persisted idle window for an app.
func (h *Handler) idleTimeoutFor(appName string) time.Duration {
	if h.syncManager == nil {
		return 0
	}
	app := h.syncManager.GetState().Apps[appName]
	if app == nil || app.IdleTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(app.IdleTimeoutSeconds) * time.Second
}

// suspendIdleApp stops an app that has been idle for too long. Suspension is
// reversible: the artifact stays on disk and the next request restarts it.
func (h *Handler) suspendIdleApp(appName string, generation uint64) error {
	if h.syncManager == nil {
		return nil
	}
	app := h.syncManager.GetState().Apps[appName]
	if app == nil || app.Generation != generation || app.Status != state.StatusRunning {
		return nil
	}
	switch app.Backend {
	case "static":
		h.sitesMu.Lock()
		site, ok := h.sites[appName]
		delete(h.sites, appName)
		h.sitesMu.Unlock()
		if ok {
			// Keep the root: an idle suspend is not a removal.
			_ = site.Close()
		}
	case "docker":
		if app.ContainerID != "" {
			// Pause rather than remove so the container can be resumed.
			if err := h.dockerEngine.Pause(context.Background(), app.ContainerID); err != nil {
				slog.Warn("failed to pause idle container", "app", appName, "error", err)
			}
		}
	}
	// The suspended flag is recorded even when no live listener or container
	// was found, so the state stays consistent with what the caller observed.
	app.Status = state.StatusStopped
	app.Suspended = true
	app.Port = 0
	app.TerminationReason = "idle"
	h.syncManager.ReplaceApp(appName, app)
	h.persistState()
	return nil
}

// resumeSuspendedApp restarts an app that was suspended for idleness. It is
// called by the proxy before traffic is forwarded.
func (h *Handler) resumeSuspendedApp(appName string, generation uint64) error {
	if h.syncManager == nil {
		return nil
	}
	h.resumeMu.Lock()
	if h.resuming[appName] {
		h.resumeMu.Unlock()
		return nil
	}
	h.resuming[appName] = true
	h.resumeMu.Unlock()
	defer func() {
		h.resumeMu.Lock()
		delete(h.resuming, appName)
		h.resumeMu.Unlock()
	}()

	app := h.syncManager.GetState().Apps[appName]
	if app == nil || !app.Suspended || app.Generation != generation {
		return nil
	}
	if app.WorkPath == "" {
		return fmt.Errorf("app %q has no artifact to resume", appName)
	}
	if _, err := os.Stat(app.WorkPath); err != nil {
		return fmt.Errorf("artifact for %q is unavailable: %w", appName, err)
	}
	switch app.Backend {
	case "static":
		site, err := staticserve.Start("0.0.0.0", app.WorkPath, 0)
		if err != nil {
			return err
		}
		h.sitesMu.Lock()
		h.sites[appName] = site
		h.sitesMu.Unlock()
		if app.ExposureToken != "" {
			if server, err := h.ensureProxy(); err == nil {
				_ = server.Register(app.ExposureToken, proxy.Target{
					AppName:    appName,
					Generation: app.Generation,
					Upstream:   &siteUpstream{site: site},
				})
			}
		}
		app.Port = site.Port()
	case "docker":
		if app.ContainerID == "" {
			return fmt.Errorf("app %q has no container to resume", appName)
		}
		// A suspended container is paused, not stopped.
		if err := h.dockerEngine.Unpause(context.Background(), app.ContainerID); err != nil {
			return err
		}
		// The container keeps the ports it was published with, so restore the
		// primary host port for status reporting.
		if len(app.PublishedPorts) > 0 && app.Port == 0 {
			app.Port = app.PublishedPorts[0].Host
		}
	}
	app.Status = state.StatusRunning
	app.Suspended = false
	app.TerminationReason = ""
	app.LastActivityUnix = time.Now().Unix()
	h.syncManager.ReplaceApp(appName, app)
	h.persistState()

	if timeout := h.idleTimeoutFor(appName); timeout > 0 {
		h.idleTracker.Schedule(appName, app.Generation, timeout)
	}
	return nil
}
