package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jiangfire/dzjjy/internal/client/deploy"
	"github.com/jiangfire/dzjjy/internal/server/appconfig"
)

func applyCmd() {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	file := fs.String("file", "", "YAML config file (required)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}
	if *token == "" || *file == "" {
		slog.Error("token and file are required")
		fs.Usage()
		os.Exit(1)
	}
	config, document, err := readConfig(*file)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	var artifact io.Reader
	artifactName := ""
	if document.Spec.Artifact != "" {
		artifactPath, err := resolveArtifactPath(*file, document.Spec.Artifact)
		if err != nil {
			slog.Error("artifact not found", "error", err)
			os.Exit(1)
		}
		f, err := os.Open(artifactPath)
		if err != nil {
			slog.Error("failed to open artifact", "error", err)
			os.Exit(1)
		}
		defer func() { _ = f.Close() }()
		artifact, artifactName = f, filepath.Base(artifactPath)
	}
	client := deploy.NewClient(*server, *token)
	result, err := client.ApplyConfig(document.Metadata.Name, config, artifactName, artifact)
	if err != nil {
		slog.Error("apply failed", "error", err)
		os.Exit(1)
	}
	attrs := []any{"app", result.AppName, "kind", result.Kind, "status", result.Status, "generation", result.Generation}
	if result.Port != 0 {
		attrs = append(attrs, "port", result.Port)
	}
	if result.PID != 0 {
		attrs = append(attrs, "pid", result.PID)
	}
	if result.ExpiresAt != 0 {
		attrs = append(attrs, "expires_at", result.ExpiresAt)
	}
	slog.Info("configuration applied", attrs...)
}

func validateCmd() {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	file := fs.String("file", "", "YAML config file (required)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}
	if *file == "" {
		slog.Error("file is required")
		fs.Usage()
		os.Exit(1)
	}
	_, document, err := readConfig(*file)
	if err != nil {
		slog.Error("configuration is invalid", "error", err)
		os.Exit(1)
	}
	if document.Spec.Artifact != "" {
		if _, err := resolveArtifactPath(*file, document.Spec.Artifact); err != nil {
			slog.Error("artifact not found", "error", err)
			os.Exit(1)
		}
	}
	fmt.Printf("configuration valid: %s (%s)\n", document.Metadata.Name, document.Kind)
}

func readConfig(path string) ([]byte, *appconfig.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	document, err := appconfig.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	return data, document, nil
}

func resolveArtifactPath(configPath, artifact string) (string, error) {
	if filepath.IsAbs(artifact) {
		return "", fmt.Errorf("artifact path must be relative to the config file")
	}
	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	root := filepath.Dir(configAbs)
	candidate := filepath.Clean(filepath.Join(root, artifact))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("artifact path escapes config directory")
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("artifact must be a regular file")
	}
	return candidate, nil
}
