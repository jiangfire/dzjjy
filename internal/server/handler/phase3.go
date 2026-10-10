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

// exposurePath renders the persisted/public form of an access token. The
// state file and API always carry the full path without a trailing slash, so
// there is exactly one on-the-wire representation.
func exposurePath(token string) string {
	if token == "" {
		return ""
	}
	return proxy.TokenPrefix + token
}

// bareToken converts a stored exposure path back into the proxy key. It
// tolerates a value that is already a bare token so restored or hand-edited
// state cannot break routing.
func bareToken(stored string) string {
	if stored == "" {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(stored, proxy.TokenPrefix), "/")
}

// publishSite registers a StaticSite with the token proxy and returns the URL.
func (h *Handler) publishSite(appName string, document *appconfig.Document, site *staticserve.Site, generation uint64, existing string) (string, error) {
	timeout := idleTimeout(document)
	token := bareToken(existing)
	if document.Spec.Exposure != nil {
		resolved, err := h.exposureToken(appName, document, token)
		if err != nil {
			return "", err
		}
		token = resolved
		server, err := h.ensureProxy()
		if err != nil {
			return "", err
		}
		upstream := &siteUpstream{site: site}
		if err := server.Register(token, proxy.Target{AppName: appName, Generation: generation, Upstream: upstream}); err != nil {
			return "", err
		}
	}
	if timeout > 0 {
		h.idleTracker.Schedule(appName, generation, timeout)
	}
	h.rememberIdleApp(appName, idleAppRecord{
		generation: generation,
		backend:    "static",
		workPath:   site.Root(),
		timeout:    timeout,
		token:      token,
	})
	if token == "" {
		return "", nil
	}
	return exposurePath(token) + "/", nil
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

// rememberIdleApp records the facts an idle transition needs, so suspend and
// resume keep working when the server runs without a state store.
func (h *Handler) rememberIdleApp(appName string, record idleAppRecord) {
	h.idleAppsMu.Lock()
	h.idleApps[appName] = record
	h.idleAppsMu.Unlock()
}

// forgetIdleApp drops an app from the idle registry.
func (h *Handler) forgetIdleApp(appName string) {
	h.idleAppsMu.Lock()
	delete(h.idleApps, appName)
	h.idleAppsMu.Unlock()
}

// lookupIdleApp reads the in-memory idle record.
func (h *Handler) lookupIdleApp(appName string) (idleAppRecord, bool) {
	h.idleAppsMu.Lock()
	defer h.idleAppsMu.Unlock()
	record, ok := h.idleApps[appName]
	return record, ok
}

// suspendIdleApp stops an app that has been idle for too long. Suspension is
// reversible: the artifact stays on disk and the next request restarts it.
func (h *Handler) suspendIdleApp(appName string, generation uint64) error {
	record, known := h.lookupIdleApp(appName)
	if !known {
		return nil
	}
	// A stale callback must never touch a restarted app.
	if record.generation != generation {
		return nil
	}
	backend := record.backend
	if h.syncManager != nil {
		app := h.syncManager.GetState().Apps[appName]
		if app == nil || app.Generation != generation || app.Status != state.StatusRunning {
			return nil
		}
		backend = app.Backend
	}
	switch backend {
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
		containerID := ""
		if h.syncManager != nil {
			if app := h.syncManager.GetState().Apps[appName]; app != nil {
				containerID = app.ContainerID
			}
		}
		if containerID != "" && h.dockerEngine != nil {
			// Pause rather than remove so the container can be resumed.
			if err := h.dockerEngine.Pause(context.Background(), containerID); err != nil {
				slog.Warn("failed to pause idle container", "app", appName, "error", err)
			}
		}
	}
	if h.syncManager != nil {
		app := h.syncManager.GetState().Apps[appName]
		if app != nil {
			app.Status = state.StatusStopped
			app.Suspended = true
			app.Port = 0
			app.TerminationReason = "idle"
			h.syncManager.ReplaceApp(appName, app)
			h.persistState()
		}
	}
	h.idleAppsMu.Lock()
	if current, ok := h.idleApps[appName]; ok && current.generation == generation {
		current.suspended = true
		h.idleApps[appName] = current
	}
	h.idleAppsMu.Unlock()
	return nil
}

// resumeSuspendedApp restarts an app that was suspended for idleness. It is
// called by the proxy before traffic is forwarded.
func (h *Handler) resumeSuspendedApp(appName string, generation uint64) error {
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

	record, known := h.lookupIdleApp(appName)
	if !known || !record.suspended || record.generation != generation {
		return nil
	}
	workPath := record.workPath
	token := record.token
	if h.syncManager != nil {
		app := h.syncManager.GetState().Apps[appName]
		if app == nil || !app.Suspended || app.Generation != generation {
			return nil
		}
		workPath = app.WorkPath
		token = app.ExposureToken
	}
	if workPath == "" {
		return fmt.Errorf("app %q has no artifact to resume", appName)
	}
	if _, err := os.Stat(workPath); err != nil {
		return fmt.Errorf("artifact for %q is unavailable: %w", appName, err)
	}
	switch record.backend {
	case "static":
		site, err := staticserve.Start("0.0.0.0", workPath, 0)
		if err != nil {
			return err
		}
		h.sitesMu.Lock()
		h.sites[appName] = site
		h.sitesMu.Unlock()
		token = bareToken(token)
		if token != "" {
			if server, err := h.ensureProxy(); err == nil {
				_ = server.Register(token, proxy.Target{
					AppName:    appName,
					Generation: generation,
					Upstream:   &siteUpstream{site: site},
				})
			}
		}
		if h.syncManager != nil {
			if app := h.syncManager.GetState().Apps[appName]; app != nil {
				app.Status = state.StatusRunning
				app.Suspended = false
				app.Port = site.Port()
				app.TerminationReason = ""
				app.LastActivityUnix = time.Now().Unix()
				h.syncManager.ReplaceApp(appName, app)
				h.persistState()
			}
		}
	case "docker":
		if h.dockerEngine == nil {
			return fmt.Errorf("app %q has no docker engine", appName)
		}
		containerID := ""
		if h.syncManager != nil {
			if app := h.syncManager.GetState().Apps[appName]; app != nil {
				containerID = app.ContainerID
			}
		}
		if containerID == "" {
			return fmt.Errorf("app %q has no container to resume", appName)
		}
		// A suspended container is paused, not stopped.
		if err := h.dockerEngine.Unpause(context.Background(), containerID); err != nil {
			return err
		}
		if h.syncManager != nil {
			if app := h.syncManager.GetState().Apps[appName]; app != nil {
				app.Status = state.StatusRunning
				app.Suspended = false
				app.TerminationReason = ""
				app.LastActivityUnix = time.Now().Unix()
				// The container keeps the ports it was published with.
				if len(app.PublishedPorts) > 0 && app.Port == 0 {
					app.Port = app.PublishedPorts[0].Host
				}
				h.syncManager.ReplaceApp(appName, app)
				h.persistState()
			}
		}
	default:
		return nil
	}

	h.idleAppsMu.Lock()
	if current, ok := h.idleApps[appName]; ok && current.generation == generation {
		current.suspended = false
		h.idleApps[appName] = current
	}
	h.idleAppsMu.Unlock()
	if record.timeout > 0 {
		h.idleTracker.Schedule(appName, generation, record.timeout)
	}
	return nil
}
