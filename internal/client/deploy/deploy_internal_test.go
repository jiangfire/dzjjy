package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingReadCloser struct {
	err error
}

func (f failingReadCloser) Read([]byte) (int, error) {
	return 0, f.err
}

func (f failingReadCloser) Close() error {
	return nil
}

func newTempDeployFile(t *testing.T, name, content string) string {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestDoRequest_ResponseBodyReadError(t *testing.T) {
	client := NewClient("http://example.com", "token")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       failingReadCloser{err: errors.New("read failed")},
		}, nil
	})

	resp, err := client.doRequest(http.MethodGet, client.serverURL+"/api/v1/apps/default/status", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to read response")
}

func TestDoRequest_EmptyNonJSONResponseFallsBackToStatus(t *testing.T) {
	client := NewClient("http://example.com", "token")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})

	resp, err := client.doRequest(http.MethodGet, client.serverURL+"/api/v1/apps/default/status", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, "request failed: 502 Bad Gateway", err.Error())
}

func TestDoRequest_TransportTimeout(t *testing.T) {
	client := NewClient("http://example.com", "token")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})

	resp, err := client.doRequest(http.MethodGet, client.serverURL+"/api/v1/apps/default/status", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to send request")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestStart_SendsJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/apps/app/start", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		var req api.DeployRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "runtime", req.Type)
		assert.Equal(t, "python", req.Executable)
		assert.Equal(t, "app.py", req.Entry)
		assert.Equal(t, "--port 8080", req.Args)
		assert.True(t, req.AutoRestart)
		assert.Equal(t, 5, req.MaxRestarts)

		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "started",
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.Start("app", "runtime", "python", "app.py", "--port 8080", true, 5)

	require.NoError(t, err)
}

func TestListApps_SkipsInvalidEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"apps": map[string]any{
					"valid": map[string]any{
						"running":       true,
						"pid":           42,
						"type":          "exec",
						"executable":    "valid.exe",
						"auto_restart":  true,
						"restart_count": 2,
						"uptime":        3,
					},
					"invalid-data":      "oops",
					"invalid-structure": []any{"unexpected"},
				},
			},
		}))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	apps, err := client.ListApps()

	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Contains(t, apps, "valid")
	assert.Equal(t, 42, apps["valid"].PID)
}

func TestDeploy_OpeningDirectoryReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	err := client.Deploy("default", t.TempDir(), "exec", "app.exe", "", "", false, 0)

	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "failed to open file") ||
			strings.Contains(err.Error(), "failed to copy file"),
		"unexpected error: %v", err,
	)
}

func TestDeploy_PropagatesRequestErrorFromCustomTransport(t *testing.T) {
	filePath := newTempDeployFile(t, "app.exe", "binary")
	client := NewClient("http://example.com", "token")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})

	err := client.Deploy("default", filePath, "exec", "app.exe", "", "", false, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request")
	assert.Contains(t, err.Error(), "dial failed")
}
