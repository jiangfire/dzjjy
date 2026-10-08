package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"

	"github.com/jiangfire/dzjjy/pkg/api"
)

// ApplyConfig submits one validated YAML document and its optional artifact.
func (c *Client) ApplyConfig(appName string, config []byte, artifactName string, artifact io.Reader) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("config", string(config)); err != nil {
		return fmt.Errorf("failed to write config field: %w", err)
	}
	if artifact != nil {
		part, err := writer.CreateFormFile("artifact", artifactName)
		if err != nil {
			return fmt.Errorf("failed to create artifact field: %w", err)
		}
		if _, err := io.Copy(part, artifact); err != nil {
			return fmt.Errorf("failed to copy artifact: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}
	endpoint := c.serverURL + "/api/v1/apps/" + url.PathEscape(appName) + "/apply"
	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(req) // #nosec G704 - serverURL is explicit client configuration
	if err != nil {
		return fmt.Errorf("failed to send apply request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	var result api.Response
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode apply response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("apply failed: %s", result.Message)
	}
	return nil
}
