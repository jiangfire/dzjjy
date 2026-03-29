package deploy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jiangfire/dzjjy/internal/client/deploy"
	"github.com/jiangfire/dzjjy/pkg/api"
)

func BenchmarkClientStatusParallel(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: api.StatusResponse{
				AppName:      "demo",
				Running:      true,
				PID:          12345,
				Type:         "exec",
				Executable:   "demo.exe",
				AutoRestart:  true,
				RestartCount: 2,
				Uptime:       3600,
			},
		})
	}))
	defer server.Close()

	client := deploy.NewClient(server.URL, "bench-token")
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			status, err := client.Status("demo")
			if err != nil {
				b.Fatalf("status failed: %v", err)
			}
			if status.PID != 12345 {
				b.Fatalf("unexpected pid: %d", status.PID)
			}
		}
	})
}

func BenchmarkClientListApps100Apps(b *testing.B) {
	apps := make(map[string]api.StatusResponse, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("app-%03d", i)
		apps[name] = api.StatusResponse{
			AppName:      name,
			Running:      i%2 == 0,
			PID:          1000 + i,
			Type:         "exec",
			Executable:   "demo.exe",
			AutoRestart:  true,
			RestartCount: i % 5,
			Uptime:       int64(i),
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.Response{
			Success: true,
			Message: "ok",
			Data: map[string]any{
				"count": len(apps),
				"apps":  apps,
			},
		})
	}))
	defer server.Close()

	client := deploy.NewClient(server.URL, "bench-token")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		list, err := client.ListApps()
		if err != nil {
			b.Fatalf("list apps failed: %v", err)
		}
		if len(list) != len(apps) {
			b.Fatalf("unexpected app count: %d", len(list))
		}
	}
}
