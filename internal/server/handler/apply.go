package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiangfire/dzjjy/internal/server/appconfig"
	"github.com/jiangfire/dzjjy/internal/server/archive"
	"github.com/jiangfire/dzjjy/internal/server/state"
	"github.com/jiangfire/dzjjy/internal/server/staticserve"
)

// Apply validates and applies a dzjjy/v1 YAML document with an optional artifact.
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (100<<20)+(2<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil { // #nosec G120 -- request body is already bounded by MaxBytesReader above.
		h.sendError(w, "invalid multipart request", http.StatusBadRequest)
		return
	}
	configText := r.FormValue("config")
	if configText == "" {
		h.sendError(w, "config field is required", http.StatusBadRequest)
		return
	}
	document, err := appconfig.Parse([]byte(configText))
	if err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if document.Spec.Sandbox.Type == "docker" {
		if document.Kind != "App" {
			h.sendError(w, "docker sandbox is only valid for App", http.StatusBadRequest)
			return
		}
		h.applyDocker(w, r, document)
		return
	}
	if document.Kind == "App" {
		if document.Spec.Runtime != nil {
			h.sendError(w, "runtime templates require sandbox.type \"docker\"; the process sandbox runs spec.command directly without a runtime template", http.StatusNotImplemented)
			return
		}
		h.applyProcess(w, r, document, configText)
		return
	}

	if err := validateAppName(document.Metadata.Name); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalizedConfig, err := json.Marshal(document)
	if err != nil {
		h.sendError(w, "failed to normalize configuration", http.StatusInternalServerError)
		return
	}
	if h.syncManager != nil {
		if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil && app.Status == state.StatusRunning && app.DesiredConfig == string(normalizedConfig) {
			h.sitesMu.Lock()
			site := h.sites[document.Metadata.Name]
			h.sitesMu.Unlock()
			if site != nil {
				generation, expiresAt := app.Generation, app.ExpiresAt
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "configuration applied", "data": map[string]any{"app_name": document.Metadata.Name, "kind": document.Kind, "status": "running", "port": site.Port(), "generation": generation, "expires_at": expiresAt}})
				return
			}
		}
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
	if err := extractApplyArtifact(r.MultipartForm, document.Spec.Artifact, staging); err != nil {
		h.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	indexPath := filepath.Join(staging, "index.html")
	if info, err := os.Stat(indexPath); err != nil || !info.Mode().IsRegular() {
		h.sendError(w, "StaticSite artifact must contain index.html", http.StatusBadRequest)
		return
	}

	port := 0
	if document.Spec.Port != nil {
		port = *document.Spec.Port
	}
	site, err := staticserve.Start("0.0.0.0", staging, port)
	if err != nil {
		h.sendError(w, fmt.Sprintf("failed to start static site: %v", err), http.StatusConflict)
		return
	}
	h.sitesMu.Lock()
	old := h.sites[document.Metadata.Name]
	h.sites[document.Metadata.Name] = site
	h.sitesMu.Unlock()
	keepStaging = true
	if old != nil {
		_ = old.Close()
		_ = os.RemoveAll(old.Root())
	}
	generation, expiresAt := h.recordStaticSite(document, staging, site.Port())
	response := map[string]any{"app_name": document.Metadata.Name, "kind": document.Kind, "status": "running", "port": site.Port(), "generation": generation, "expires_at": expiresAt}
	// A site with spec.exposure is published behind the token proxy so the raw
	// port is not the way people reach it.
	if document.Spec.Exposure != nil {
		h.proxyMu.Lock()
		previous := ""
		if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil {
			previous = app.ExposureToken
		}
		h.proxyMu.Unlock()
		publicPath, err := h.publishSite(document.Metadata.Name, document, site, generation, previous)
		if err != nil {
			h.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if h.syncManager != nil {
			if app := h.syncManager.GetState().Apps[document.Metadata.Name]; app != nil {
				app.ExposureToken = strings.TrimSuffix(publicPath, "/")
				h.syncManager.ReplaceApp(document.Metadata.Name, app)
				h.persistState()
			}
		}
		response["public_path"] = publicPath
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "configuration applied", "data": response}); err != nil {
		slog.Warn("failed to encode apply response", "error", err)
	}
}
func extractApplyArtifact(form *multipart.Form, artifact, staging string) error {
	if artifact == "" {
		return fmt.Errorf("artifact file is required")
	}
	files := form.File["artifact"]
	if len(files) != 1 {
		return fmt.Errorf("exactly one artifact file is required")
	}
	file, err := files[0].Open()
	if err != nil {
		return fmt.Errorf("failed to open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	name := filepath.Base(files[0].Filename)
	if err := (&Handler{}).validateFilename(name); err != nil {
		return err
	}
	artifactPath := filepath.Join(staging, name)
	dest, err := os.Create(artifactPath) // #nosec G304 - name is validated and joined under the staging root
	if err != nil {
		return fmt.Errorf("failed to stage artifact: %w", err)
	}
	_, copyErr := io.Copy(dest, io.LimitReader(file, 100<<20+1))
	closeErr := dest.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return err
	}
	if info.Size() > 100<<20 {
		return fmt.Errorf("artifact upload exceeds 100 MiB")
	}
	if archive.IsArchive(name) {
		return archive.Extract(artifactPath, staging)
	}
	if name != "index.html" {
		return fmt.Errorf("StaticSite artifact must be an archive or index.html")
	}
	return nil
}
