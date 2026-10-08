package appconfig

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const APIVersion = "dzjjy/v1"

type Document struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Spec struct {
	Artifact  string    `yaml:"artifact,omitempty" json:"artifact,omitempty"`
	Runtime   *Runtime  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Command   []string  `yaml:"command,omitempty" json:"command,omitempty"`
	Restart   string    `yaml:"restart,omitempty" json:"restart,omitempty"`
	Sandbox   *Sandbox  `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	Port      *int      `yaml:"port,omitempty" json:"port,omitempty"`
	Lifecycle Lifecycle `yaml:"lifecycle,omitempty" json:"lifecycle,omitempty"`
}

type Runtime struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
}

type Sandbox struct {
	Type string `yaml:"type" json:"type"`
}

type Lifecycle struct {
	TTL string `yaml:"ttl,omitempty" json:"ttl,omitempty"`
}

func Parse(data []byte) (*Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if err := document.Validate(); err != nil {
		return nil, err
	}
	return &document, nil
}

func (d *Document) Validate() error {
	if d.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if d.Kind != "App" && d.Kind != "StaticSite" {
		return fmt.Errorf("kind must be App or StaticSite")
	}
	if d.Metadata.Name == "" || len(d.Metadata.Name) > 64 || strings.ContainsAny(d.Metadata.Name, `/\\`) || strings.Contains(d.Metadata.Name, "..") {
		return fmt.Errorf("metadata.name is invalid")
	}
	if d.Kind == "StaticSite" && d.Spec.Artifact == "" {
		return fmt.Errorf("spec.artifact is required")
	}
	if d.Spec.Restart == "" {
		d.Spec.Restart = "never"
	}
	if d.Spec.Restart != "never" && d.Spec.Restart != "on-failure" {
		return fmt.Errorf("spec.restart must be never or on-failure")
	}
	if d.Spec.Lifecycle.TTL != "" {
		ttl, err := time.ParseDuration(d.Spec.Lifecycle.TTL)
		if err != nil || ttl <= 0 {
			return fmt.Errorf("spec.lifecycle.ttl must be a positive duration")
		}
	}
	if d.Spec.Sandbox == nil {
		d.Spec.Sandbox = &Sandbox{Type: "process"}
	}
	if d.Spec.Sandbox.Type != "process" && d.Spec.Sandbox.Type != "docker" {
		return fmt.Errorf("spec.sandbox.type must be process or docker")
	}
	if d.Kind == "StaticSite" {
		if d.Spec.Runtime != nil || len(d.Spec.Command) != 0 || d.Spec.Sandbox.Type != "process" {
			return fmt.Errorf("StaticSite does not support runtime, command, or Docker sandbox")
		}
		if d.Spec.Port != nil && (*d.Spec.Port < 0 || *d.Spec.Port > 65535) {
			return fmt.Errorf("spec.port must be between 0 and 65535")
		}
		return nil
	}
	if d.Spec.Port != nil {
		return fmt.Errorf("spec.port is only valid for StaticSite")
	}
	if len(d.Spec.Command) == 0 {
		return fmt.Errorf("spec.command is required for App")
	}
	if d.Spec.Runtime != nil {
		if err := validateTemplate(d.Spec.Runtime.Type, d.Spec.Runtime.Version); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplate(runtimeType, version string) error {
	allowed := map[string]map[string]bool{
		"python": {"3.12": true},
		"node":   {"22": true},
		"go":     {"1.24": true},
	}
	if !allowed[runtimeType][version] {
		return fmt.Errorf("unsupported runtime template %q:%q", runtimeType, version)
	}
	return nil
}
