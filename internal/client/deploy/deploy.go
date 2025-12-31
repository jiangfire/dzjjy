package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jiangfire/dzjjy/pkg/api"
)

// Client 部署客户端
type Client struct {
	serverURL string
	token     string
	client    *http.Client
}

// NewClient 创建客户端
func NewClient(serverURL, token string) *Client {
	return &Client{
		serverURL: serverURL,
		token:     token,
		client:    &http.Client{},
	}
}

// doRequest 执行 HTTP 请求并解析响应（公共方法，消除重复）
func (c *Client) doRequest(method, url string, body io.Reader) (*api.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result api.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("request failed: %s", result.Message)
	}

	return &result, nil
}

// Deploy 部署应用
func (c *Client) Deploy(filePath, appType, executable, entry, args string, autoRestart bool, maxRestarts int) error {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 创建multipart表单
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// 添加其他字段
	writer.WriteField("type", appType)
	writer.WriteField("executable", executable)
	if entry != "" {
		writer.WriteField("entry", entry)
	}
	if args != "" {
		writer.WriteField("args", args)
	}
	if autoRestart {
		writer.WriteField("auto_restart", "true")
		writer.WriteField("max_restarts", fmt.Sprintf("%d", maxRestarts))
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	// 发送请求
	url := c.serverURL + "/api/v1/deploy"
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	var result api.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("deployment failed: %s", result.Message)
	}

	return nil
}

// Stop 停止应用
func (c *Client) Stop() error {
	url := c.serverURL + "/api/v1/stop"
	_, err := c.doRequest("POST", url, nil)
	return err
}

// Restart 重启应用
func (c *Client) Restart() error {
	url := c.serverURL + "/api/v1/restart"
	_, err := c.doRequest("POST", url, nil)
	return err
}

// Logs 查询日志
func (c *Client) Logs(lines int) ([]map[string]any, error) {
	url := fmt.Sprintf("%s/api/v1/logs?lines=%d", c.serverURL, lines)
	result, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 解析日志数据
	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var logsData map[string]any
	if err := json.Unmarshal(dataBytes, &logsData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal logs data: %w", err)
	}

	// 提取日志列表
	logs, ok := logsData["logs"].([]any)
	if !ok {
		return []map[string]any{}, nil
	}

	result_logs := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		if logMap, ok := log.(map[string]any); ok {
			result_logs = append(result_logs, logMap)
		}
	}

	return result_logs, nil
}

// Status 查询状态
func (c *Client) Status() (*api.StatusResponse, error) {
	url := c.serverURL + "/api/v1/status"
	result, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 将data转换为StatusResponse
	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var status api.StatusResponse
	if err := json.Unmarshal(dataBytes, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status: %w", err)
	}

	return &status, nil
}
