package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/handler"
	"github.com/jiangfire/dzjjy/pkg/api"
)

// TestMultiAppIntegration 端到端多应用集成测试
func TestMultiAppIntegration(t *testing.T) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "handler-integration-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	uploadDir := filepath.Join(tmpDir, "uploads")
	workDir := filepath.Join(tmpDir, "workspace")
	logDir := filepath.Join(tmpDir, "logs")
	stateFile := filepath.Join(tmpDir, "state.json")

	require.NoError(t, os.MkdirAll(uploadDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))
	require.NoError(t, os.MkdirAll(logDir, 0755))

	// 创建支持状态持久化的 Handler
	h := handler.NewHandlerWithState(uploadDir, workDir, logDir, stateFile)

	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/app1/deploy":
			h.Deploy(w, r)
		case "/api/v1/apps/app2/deploy":
			h.Deploy(w, r)
		case "/api/v1/apps/app1/status":
			h.Status(w, r)
		case "/api/v1/apps/app2/status":
			h.Status(w, r)
		case "/api/v1/apps":
			h.ListApps(w, r)
		case "/api/v1/apps/app1/stop":
			h.Stop(w, r)
		case "/api/v1/apps/app2/stop":
			h.Stop(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// 确定可执行文件后缀
	execSuffix := ""
	if os.PathSeparator == '\\' {
		execSuffix = ".exe"
	}

	// 步骤 1: 部署两个应用 (app1 和 app2 - 都长时间运行)
	app1Path := createTestApp(t, tmpDir, "app1", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("app1 started")
	time.Sleep(30 * time.Second)
}
`)

	app2Path := createTestApp(t, tmpDir, "app2", `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("app2 started")
	time.Sleep(30 * time.Second)
}
`)

	deployApp(t, server.URL, "app1", app1Path, "exec", "app1"+execSuffix, "", "", true, 5)
	deployApp(t, server.URL, "app2", app2Path, "exec", "app2"+execSuffix, "", "", true, 5)

	// 等待应用完全启动
	time.Sleep(200 * time.Millisecond)

	// 验证两个应用状态
	status1 := getStatus(t, server.URL, "app1")
	assert.True(t, status1.Running, "app1 should be running")
	assert.Greater(t, status1.PID, 0, "app1 should have PID")
	assert.True(t, status1.AutoRestart, "app1 should have auto-restart enabled")

	status2 := getStatus(t, server.URL, "app2")
	assert.True(t, status2.Running, "app2 should be running")
	assert.Greater(t, status2.PID, 0, "app2 should have PID")
	assert.True(t, status2.AutoRestart, "app2 should have auto-restart enabled")

	// 步骤 2: 列出所有应用
	apps := listApps(t, server.URL)
	assert.Equal(t, 2, len(apps), "should have 2 apps")
	assert.Contains(t, apps, "app1")
	assert.Contains(t, apps, "app2")

	// 步骤 3: 停止 app1
	stopApp(t, server.URL, "app1")
	time.Sleep(200 * time.Millisecond)

	// 验证 app1 已停止
	status1 = getStatus(t, server.URL, "app1")
	assert.False(t, status1.Running, "app1 should be stopped")

	// 步骤 4: 验证状态持久化文件存在
	_, err = os.Stat(stateFile)
	assert.NoError(t, err, "state file should exist")

	// 读取状态文件内容
	stateContent, err := os.ReadFile(stateFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, stateContent, "state file should not be empty")

	// 验证状态文件包含两个应用
	var stateData map[string]interface{}
	err = json.Unmarshal(stateContent, &stateData)
	require.NoError(t, err)

	// 检查 data.apps 结构
	data, ok := stateData["data"].(map[string]interface{})
	require.True(t, ok, "state should have data field")
	appsData, ok := data["apps"].(map[string]interface{})
	require.True(t, ok, "data should have apps field")
	assert.Equal(t, 2, len(appsData), "state should have 2 apps")

	// 步骤 5: 停止 app2
	stopApp(t, server.URL, "app2")
	time.Sleep(200 * time.Millisecond)

	// 验证 app2 已停止
	status2 = getStatus(t, server.URL, "app2")
	assert.False(t, status2.Running, "app2 should be stopped")

	// 步骤 6: 模拟服务器重启 - 创建新的 Handler 并恢复状态
	h2 := handler.NewHandlerWithState(uploadDir, workDir, logDir, stateFile)
	err = h2.RestoreState()
	require.NoError(t, err, "restore should succeed")

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/app1/status":
			h2.Status(w, r)
		case "/api/v1/apps/app2/status":
			h2.Status(w, r)
		case "/api/v1/apps":
			h2.ListApps(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server2.Close()

	// 等待一下
	time.Sleep(300 * time.Millisecond)

	restoredStatus1 := getStatus(t, server2.URL, "app1")
	assert.True(t, restoredStatus1.Running, "app1 should be auto-restarted after restore")

	restoredStatus2 := getStatus(t, server2.URL, "app2")
	assert.True(t, restoredStatus2.Running, "app2 should be auto-restarted after restore")

	restoredApps := listApps(t, server2.URL)
	assert.Len(t, restoredApps, 2)

	// 验证状态文件仍然存在且包含两个应用
	stateData2 := h2.GetStateSnapshot()
	assert.Equal(t, 2, len(stateData2), "should have 2 apps after restore")
}

// TestStatePersistence 测试状态持久化的基本功能
func TestStatePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-persist-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	workDir := filepath.Join(tmpDir, "work")
	logDir := filepath.Join(tmpDir, "logs")
	stateFile := filepath.Join(tmpDir, "state.json")

	require.NoError(t, os.MkdirAll(workDir, 0755))
	require.NoError(t, os.MkdirAll(logDir, 0755))

	// 确定可执行文件后缀
	execSuffix := ""
	if os.PathSeparator == '\\' {
		execSuffix = ".exe"
	}

	// 创建 Handler
	h := handler.NewHandlerWithState(workDir, workDir, logDir, stateFile)

	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/test/deploy":
			h.Deploy(w, r)
		case "/api/v1/apps/test/status":
			h.Status(w, r)
		case "/api/v1/apps/test/stop":
			h.Stop(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// 创建并部署测试应用
	appPath := createTestApp(t, tmpDir, "test", `package main
import "time"
func main() {
	time.Sleep(5 * time.Second)
}
`)

	deployApp(t, server.URL, "test", appPath, "exec", "test"+execSuffix, "", "", false, 3)

	// 验证应用运行
	status := getStatus(t, server.URL, "test")
	assert.True(t, status.Running)

	// 停止应用
	stopApp(t, server.URL, "test")
	time.Sleep(200 * time.Millisecond)

	// 验证状态文件存在
	_, err = os.Stat(stateFile)
	require.NoError(t, err)

	// 读取并验证状态文件内容
	content, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	var state map[string]interface{}
	err = json.Unmarshal(content, &state)
	require.NoError(t, err)

	// 验证结构
	assert.Contains(t, state, "version")
	assert.Contains(t, state, "checksum")
	assert.Contains(t, state, "data")

	data := state["data"].(map[string]interface{})
	apps := data["apps"].(map[string]interface{})
	assert.Contains(t, apps, "test")

	testApp := apps["test"].(map[string]interface{})
	assert.Equal(t, "stopped", testApp["status"])
	assert.Equal(t, float64(0), testApp["pid"])

	h2 := handler.NewHandlerWithState(workDir, workDir, logDir, stateFile)
	require.NoError(t, h2.RestoreState())

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/test/status":
			h2.Status(w, r)
		case "/api/v1/apps":
			h2.ListApps(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server2.Close()

	restoredStatus := getStatus(t, server2.URL, "test")
	assert.False(t, restoredStatus.Running)

	restoredApps := listApps(t, server2.URL)
	assert.Contains(t, restoredApps, "test")
}

func TestRemoveApp_CleansRuntimeAndFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "remove-app-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	uploadDir := filepath.Join(tmpDir, "uploads")
	workDir := filepath.Join(tmpDir, "workspace")
	logDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(uploadDir, 0755))
	require.NoError(t, os.MkdirAll(workDir, 0755))
	require.NoError(t, os.MkdirAll(logDir, 0755))

	h := handler.NewHandler(uploadDir, workDir, logDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/apps/test/deploy":
			h.Deploy(w, r)
		case "/api/v1/apps/test/status":
			h.Status(w, r)
		case "/api/v1/apps/test/remove":
			h.RemoveApp(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	execSuffix := ""
	if os.PathSeparator == '\\' {
		execSuffix = ".exe"
	}

	appPath := createTestApp(t, tmpDir, "test", `package main
import "time"
func main() {
	time.Sleep(5 * time.Second)
}
`)

	deployApp(t, server.URL, "test", appPath, "exec", "test"+execSuffix, "", "", true, 3)

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/apps/test/remove", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var result api.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result.Success)

	statusReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/apps/test/status", nil)
	require.NoError(t, err)
	statusResp, err := http.DefaultClient.Do(statusReq)
	require.NoError(t, err)
	defer func() { _ = statusResp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, statusResp.StatusCode)

	_, err = os.Stat(filepath.Join(workDir, "test"))
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(filepath.Join(uploadDir, "test"))
	assert.True(t, os.IsNotExist(err))
}

// Helper functions

func createTestApp(t *testing.T, tmpDir, name, mainCode string) string {
	appDir := filepath.Join(tmpDir, name)
	require.NoError(t, os.MkdirAll(appDir, 0755))

	mainFile := filepath.Join(appDir, "main.go")
	err := os.WriteFile(mainFile, []byte(mainCode), 0644)
	require.NoError(t, err)

	// 编译成二进制
	binaryPath := filepath.Join(appDir, name)
	if os.PathSeparator == '\\' {
		binaryPath = filepath.Join(appDir, name+".exe")
	}

	cmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to compile test app: %v\nOutput: %s", err, output)
	}

	// Ensure binary has executable permissions (important for Linux/CI environments)
	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to set executable permissions: %v", err)
	}

	return binaryPath
}

func deployApp(t *testing.T, serverURL, appName, filePath, appType, executable, entry, args string, autoRestart bool, maxRestarts int) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件
	file, err := os.Open(filePath)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	require.NoError(t, err)
	_, err = io.Copy(part, file)
	require.NoError(t, err)

	// 添加字段
	require.NoError(t, writer.WriteField("type", appType))
	require.NoError(t, writer.WriteField("executable", executable))
	if entry != "" {
		require.NoError(t, writer.WriteField("entry", entry))
	}
	if args != "" {
		require.NoError(t, writer.WriteField("args", args))
	}
	if autoRestart {
		require.NoError(t, writer.WriteField("auto_restart", "true"))
		require.NoError(t, writer.WriteField("max_restarts", "5"))
	}

	require.NoError(t, writer.Close())

	// 发送请求
	url := serverURL + "/api/v1/apps/" + appName + "/deploy"
	req, err := http.NewRequest("POST", url, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// 验证响应
	var result api.Response
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.Success, "deploy should succeed: %v", result.Message)
}

func getStatus(t *testing.T, serverURL, appName string) api.StatusResponse {
	url := serverURL + "/api/v1/apps/" + appName + "/status"
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var result api.Response
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.Success, "status should succeed")

	// 解析 status 数据
	dataBytes, _ := json.Marshal(result.Data)
	var status api.StatusResponse
	require.NoError(t, json.Unmarshal(dataBytes, &status))

	return status
}

func listApps(t *testing.T, serverURL string) map[string]api.StatusResponse {
	url := serverURL + "/api/v1/apps"
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var result api.Response
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.Success, "list should succeed")

	// 解析 apps 数据
	dataBytes, _ := json.Marshal(result.Data)
	var listData map[string]interface{}
	require.NoError(t, json.Unmarshal(dataBytes, &listData))

	appsRaw, ok := listData["apps"].(map[string]interface{})
	if !ok {
		return map[string]api.StatusResponse{}
	}

	apps := make(map[string]api.StatusResponse)
	for name, infoRaw := range appsRaw {
		infoBytes, _ := json.Marshal(infoRaw)
		var info api.StatusResponse
		require.NoError(t, json.Unmarshal(infoBytes, &info))
		apps[name] = info
	}

	return apps
}

func stopApp(t *testing.T, serverURL, appName string) {
	url := serverURL + "/api/v1/apps/" + appName + "/stop"
	req, err := http.NewRequest("POST", url, nil)
	require.NoError(t, err)

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var result api.Response
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.Success, "stop should succeed: %s", result.Message)
}
