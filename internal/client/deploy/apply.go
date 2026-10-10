package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

// ApplyResult carries the observed state the server reports after apply.
type ApplyResult struct {
	AppName    string `json:"app_name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	Generation uint64 `json:"generation"`
	ExpiresAt  int64  `json:"expires_at"`
	// ContainerID is set for Docker-backed apps.
	ContainerID string `json:"container_id"`
	// Isolator reports the applied isolation layer, e.g. "runsc".
	Isolator string `json:"isolator"`
	// PublicPath is the unguessable token path a StaticSite is served under
	// when spec.exposure is configured.
	PublicPath string `json:"public_path"`
	// PublishedPorts lists the resolved host-to-container bindings.
	PublishedPorts []PublishedPortBinding `json:"published_ports"`
}

// PublishedPortBinding is one resolved host-to-container port mapping.
type PublishedPortBinding struct {
	Container int `json:"container"`
	Host      int `json:"host"`
}

// ApplyConfig submits one validated YAML document and its optional artifact.
func (c *Client) ApplyConfig(appName string, config []byte, artifactName string, artifact io.Reader) (*ApplyResult, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("config", string(config)); err != nil {
		return nil, fmt.Errorf("failed to write config field: %w", err)
	}
	if artifact != nil {
		part, err := writer.CreateFormFile("artifact", artifactName)
		if err != nil {
			return nil, fmt.Errorf("failed to create artifact field: %w", err)
		}
		if _, err := io.Copy(part, artifact); err != nil {
			return nil, fmt.Errorf("failed to copy artifact: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}
	endpoint := c.serverURL + "/api/v1/apps/" + url.PathEscape(appName) + "/apply"
	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(req) // #nosec G704 - serverURL is explicit client configuration
	if err != nil {
		return nil, fmt.Errorf("failed to send apply request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	var result struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode apply response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("apply failed: %s", result.Message)
	}
	var applyResult ApplyResult
	if len(result.Data) > 0 {
		if err := json.Unmarshal(result.Data, &applyResult); err != nil {
			return nil, fmt.Errorf("failed to decode apply result: %w", err)
		}
	}
	return &applyResult, nil
}
