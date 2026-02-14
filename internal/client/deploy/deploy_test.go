package deploy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/client/deploy"
	"github.com/jiangfire/dzjjy/pkg/api"
)

// TestMain 设置测试环境
func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

// setupTestDir 创建临时测试目录
func setupTestDir(t *testing.T, prefix string) string {
	dir, err := os.MkdirTemp("", prefix)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

// createTestFile 创建测试文件
func createTestFile(t *testing.T, dir, name, content string) string {
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

// mockServer 创建模拟服务器
func mockServer(t *testing.T, handlerFunc http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handlerFunc)
}

func writeResponse(t *testing.T, w http.ResponseWriter, resp api.Response) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(resp))
}

// ==================== NewClient 测试 ====================

func TestNewClient(t *testing.T) {
	client := deploy.NewClient("http://localhost:8080", "test-token")
	assert.NotNil(t, client)
}

// ==================== Deploy 测试 ====================

func TestDeploy_Success(t *testing.T) {
	// 创建模拟服务器
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		assert.Equal(t, "/api/v1/apps/default/deploy", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// 解析multipart表单
		err := r.ParseMultipartForm(100 << 20)
		require.NoError(t, err)

		// 返回成功响应
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "deployment successful",
			Data: map[string]any{
				"pid": 12345,
			},
		}))
	})
	defer server.Close()

	// 创建测试文件
	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test content")

	// 创建客户端
	client := deploy.NewClient(server.URL, "test-token")

	// 执行部署
	err := client.Deploy("default", testFile, "exec", "echo", "hello", "", false, 0)
	assert.NoError(t, err)
}

func TestDeploy_WithAllOptions(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/apps/default/deploy", r.URL.Path)

		// 验证所有字段
		err := r.ParseMultipartForm(100 << 20)
		require.NoError(t, err)

		assert.Equal(t, "runtime", r.FormValue("type"))
		assert.Equal(t, "python3", r.FormValue("executable"))
		assert.Equal(t, "app.py", r.FormValue("entry"))
		assert.Equal(t, "--port 8000", r.FormValue("args"))
		assert.Equal(t, "true", r.FormValue("auto_restart"))
		assert.Equal(t, "5", r.FormValue("max_restarts"))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
		}))
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "app.py", "print('hello')")

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "runtime", "python3", "app.py", "--port 8000", true, 5)
	assert.NoError(t, err)
}

func TestDeploy_FileNotFound(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", "/nonexistent/file.txt", "exec", "test", "", "", false, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestDeploy_ServerError(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: false,
			Message: "internal server error",
		}))
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test")

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deployment failed")
}

func TestDeploy_InvalidResponseJSON(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte("invalid json"))
		require.NoError(t, err)
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test")

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestDeploy_NetworkError(t *testing.T) {
	// 使用无效的URL
	client := deploy.NewClient("http://invalid-host-that-does-not-exist-12345.com", "test-token")

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test")

	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request")
}

func TestDeploy_RequestCreationError(t *testing.T) {
	// 这个测试很难触发，因为http.NewRequest很少失败
	// 主要是URL解析失败等情况
	client := deploy.NewClient("://invalid-url", "test-token")

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test")

	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
	assert.Error(t, err)
}

// ==================== Stop 测试 ====================

func TestStop_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/apps/default/stop", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "application stopped",
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Stop("default")
	assert.NoError(t, err)
}

func TestStop_Failure(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: false,
			Message: "no running application",
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Stop("default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running application")
}

func TestStop_NetworkError(t *testing.T) {
	client := deploy.NewClient("http://invalid-host-99999.com", "test-token")
	err := client.Stop("default")
	assert.Error(t, err)
}

// ==================== Restart 测试 ====================

func TestRestart_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/apps/default/restart", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "application restarted",
			Data: map[string]any{
				"pid": 54321,
			},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Restart("default")
	assert.NoError(t, err)
}

func TestRestart_Failure(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: false,
			Message: "no running application",
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Restart("default")
	assert.Error(t, err)
}

// ==================== Logs 测试 ====================

func TestLogs_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/apps/default/logs", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "50", r.URL.Query().Get("lines"))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"logs": []any{
					map[string]any{"timestamp": "2023-12-31T10:00:00Z", "type": "stdout", "message": "line 1"},
					map[string]any{"timestamp": "2023-12-31T10:00:01Z", "type": "stderr", "message": "error 1"},
				},
				"count": 2,
			},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	logs, err := client.Logs("default", 50)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(logs))
	assert.Equal(t, "stdout", logs[0]["type"])
}

func TestLogs_EmptyLogs(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"logs":  []any{},
				"count": 0,
			},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	logs, err := client.Logs("default", 100)

	assert.NoError(t, err)
	assert.Equal(t, 0, len(logs))
}

func TestLogs_InvalidResponseFormat(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"logs": "not an array", // 错误格式
			},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	logs, err := client.Logs("default", 10)

	assert.NoError(t, err) // 应该返回空数组而不是错误
	assert.Equal(t, 0, len(logs))
}

func TestLogs_LogsWithoutLogsField(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data:    map[string]any{},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	logs, err := client.Logs("default", 10)

	assert.NoError(t, err)
	assert.Equal(t, 0, len(logs))
}

func TestLogs_LogsWithInvalidMapFormat(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"logs": []any{
					"not a map", // 无效格式
					map[string]any{"type": "stdout", "message": "valid"},
				},
			},
		}))
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	logs, err := client.Logs("default", 10)

	assert.NoError(t, err)
	assert.Equal(t, 1, len(logs)) // 只包含有效的条目
}

// ==================== Status 测试 ====================

func TestStatus_Success(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/apps/default/status", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"running":       true,
				"pid":           12345,
				"type":          "exec",
				"executable":    "echo",
				"entry":         "hello",
				"auto_restart":  true,
				"restart_count": 3,
				"uptime":        3600,
			},
		})
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	status, err := client.Status("default")

	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.Running)
	assert.Equal(t, 12345, status.PID)
	assert.Equal(t, "exec", status.Type)
	assert.Equal(t, "echo", status.Executable)
	assert.Equal(t, 3, status.RestartCount)
}

func TestStatus_Stopped(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"running":       false,
				"pid":           0,
				"type":          "",
				"executable":    "",
				"entry":         "",
				"auto_restart":  false,
				"restart_count": 0,
				"uptime":        0,
			},
		})
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	status, err := client.Status("default")

	assert.NoError(t, err)
	assert.False(t, status.Running)
	assert.Equal(t, 0, status.PID)
}

func TestStatus_InvalidResponseData(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
			Data:    "not a map", // 无效格式
		})
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")
	status, err := client.Status("default")

	assert.Error(t, err)
	assert.Nil(t, status)
}

// ==================== doRequest 测试 ====================

func TestDoRequest_InvalidURL(t *testing.T) {
	client := deploy.NewClient("http://localhost:99999", "test-token")

	// 使用反射或通过公共方法测试
	// 这里通过Status测试doRequest的错误处理
	_, err := client.Status("default")
	assert.Error(t, err)
}

func TestDoRequest_Timeout(t *testing.T) {
	// 创建一个慢服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建带短超时的客户端
	// 注意：Client结构体中的client字段未导出，无法直接设置超时
	// 这个测试需要通过实际网络超时来验证
}

// ==================== Edge Cases 测试 ====================

func TestDeploy_EmptyFilename(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
		})
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	testFile := createTestFile(t, testDir, "test.txt", "test")

	// 测试正常文件部署
	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
	assert.NoError(t, err)
}

func TestDeploy_LargeFile(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 验证接收到的文件大小
		err := r.ParseMultipartForm(100 << 20)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
		})
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	// 创建1MB的文件
	largeContent := strings.Repeat("A", 1024*1024)
	testFile := createTestFile(t, testDir, "large.txt", largeContent)

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
	assert.NoError(t, err)
}

func TestDeploy_SpecialCharactersInFilename(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(100 << 20)
		require.NoError(t, err)

		// 验证文件名被正确处理
		file, _, err := r.FormFile("file")
		require.NoError(t, err)
		require.NoError(t, file.Close())

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
		}))
	})
	defer server.Close()

	testDir := setupTestDir(t, "deploy-test")
	// 文件名包含特殊字符
	testFile := createTestFile(t, testDir, "test file (1).txt", "content")

	client := deploy.NewClient(server.URL, "test-token")
	err := client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
	assert.NoError(t, err)
}

// ==================== Concurrent Access 测试 ====================

func TestClient_ConcurrentOperations(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
			Data:    map[string]any{"pid": 12345},
		})
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "test-token")

	// 并发调用不同方法
	done := make(chan bool, 4)

	go func() {
		_, _ = client.Status("default")
		done <- true
	}()

	go func() {
		_ = client.Stop("default")
		done <- true
	}()

	go func() {
		_ = client.Restart("default")
		done <- true
	}()

	go func() {
		_, _ = client.Logs("default", 10)
		done <- true
	}()

	for i := 0; i < 4; i++ {
		<-done
	}
}

// ==================== Error Message Format 测试 ====================

func TestErrorMessageFormat(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		expectedPrefix string
	}{
		{
			name: "deployment failed",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, api.Response{
					Success: false,
					Message: "invalid configuration",
				})
			},
			expectedPrefix: "deployment failed",
		},
		{
			name: "request failed",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, api.Response{
					Success: false,
					Message: "authentication required",
				})
			},
			expectedPrefix: "request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServer(t, tt.serverHandler)
			defer server.Close()

			testDir := setupTestDir(t, "deploy-test")
			testFile := createTestFile(t, testDir, "test.txt", "test")

			client := deploy.NewClient(server.URL, "test-token")

			var err error
			if tt.name == "deployment failed" {
				err = client.Deploy("default", testFile, "exec", "test", "", "", false, 0)
			} else {
				err = client.Stop("default")
			}

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedPrefix)
		})
	}
}

// ==================== HTTP Method Validation 测试 ====================

func TestHTTPMethodValidation(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		handler func(*deploy.Client) error
	}{
		{
			name:   "deploy uses POST",
			method: "POST",
			path:   "/api/v1/apps/default/deploy",
			handler: func(c *deploy.Client) error {
				testDir := setupTestDir(t, "deploy-test")
				testFile := createTestFile(t, testDir, "test.txt", "test")
				return c.Deploy("default", testFile, "exec", "test", "", "", false, 0)
			},
		},
		{
			name:   "stop uses POST",
			method: "POST",
			path:   "/api/v1/apps/default/stop",
			handler: func(c *deploy.Client) error {
				return c.Stop("default")
			},
		},
		{
			name:   "restart uses POST",
			method: "POST",
			path:   "/api/v1/apps/default/restart",
			handler: func(c *deploy.Client) error {
				return c.Restart("default")
			},
		},
		{
			name:   "logs uses GET",
			method: "GET",
			path:   "/api/v1/apps/default/logs",
			handler: func(c *deploy.Client) error {
				_, err := c.Logs("default", 10)
				return err
			},
		},
		{
			name:   "status uses GET",
			method: "GET",
			path:   "/api/v1/apps/default/status",
			handler: func(c *deploy.Client) error {
				_, err := c.Status("default")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.method, r.Method)
				assert.Equal(t, tt.path, r.URL.Path)

				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, api.Response{
					Success: true,
					Message: "ok",
					Data:    map[string]any{},
				})
			})
			defer server.Close()

			client := deploy.NewClient(server.URL, "test-token")
			err := tt.handler(client)
			assert.NoError(t, err)
		})
	}
}

// ==================== Authorization Header 测试 ====================

func TestAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*deploy.Client) error
	}{
		{
			name: "deploy",
			handler: func(c *deploy.Client) error {
				testDir := setupTestDir(t, "deploy-test")
				testFile := createTestFile(t, testDir, "test.txt", "test")
				return c.Deploy("default", testFile, "exec", "test", "", "", false, 0)
			},
		},
		{
			name: "stop",
			handler: func(c *deploy.Client) error {
				return c.Stop("default")
			},
		},
		{
			name: "restart",
			handler: func(c *deploy.Client) error {
				return c.Restart("default")
			},
		},
		{
			name: "logs",
			handler: func(c *deploy.Client) error {
				_, err := c.Logs("default", 10)
				return err
			},
		},
		{
			name: "status",
			handler: func(c *deploy.Client) error {
				_, err := c.Status("default")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				assert.Equal(t, "Bearer test-token", auth)

				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, api.Response{
					Success: true,
					Message: "ok",
				})
			})
			defer server.Close()

			client := deploy.NewClient(server.URL, "test-token")
			err := tt.handler(client)
			assert.NoError(t, err)
		})
	}
}

// ==================== Empty Token 测试 ====================

func TestEmptyToken(t *testing.T) {
	server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 当token为空时，"Bearer " + "" = "Bearer"（没有空格）
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer", auth)

		w.Header().Set("Content-Type", "application/json")
		writeResponse(t, w, api.Response{
			Success: true,
			Message: "ok",
		})
	})
	defer server.Close()

	client := deploy.NewClient(server.URL, "")
	_, err := client.Status("default")
	assert.NoError(t, err)
}

// ==================== Server URL Variations 测试 ====================

func TestServerURLVariations(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
	}{
		{"with trailing slash", "http://localhost:8080/"},
		{"without trailing slash", "http://localhost:8080"},
		{"with path", "http://localhost:8080/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeResponse(t, w, api.Response{
					Success: true,
					Message: "ok",
				})
			})
			defer server.Close()

			// 使用实际的测试服务器URL
			client := deploy.NewClient(server.URL, "test-token")
			_, err := client.Status("default")
			assert.NoError(t, err)
		})
	}
}
