package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiangfire/dzjjy/internal/server/auth"
	"github.com/jiangfire/dzjjy/internal/server/handler"
)

func main() {
	port := flag.String("port", "8080", "server port")
	token := flag.String("token", "", "auth token (required)")
	uploadDir := flag.String("upload", "./uploads", "upload directory")
	workDir := flag.String("work", "./workspace", "work directory")
	logDir := flag.String("log", "./logs", "log directory")
	stateFile := flag.String("state", "", "state file for persistence (optional)")
	flag.Parse()

	// 配置 slog
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if *token == "" {
		slog.Error("token is required")
		flag.Usage()
		os.Exit(1)
	}

	// 创建目录
	if err := os.MkdirAll(*uploadDir, 0750); err != nil {
		slog.Error("failed to create upload dir", "error", err, "dir", *uploadDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(*workDir, 0750); err != nil {
		slog.Error("failed to create work dir", "error", err, "dir", *workDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(*logDir, 0750); err != nil {
		slog.Error("failed to create log dir", "error", err, "dir", *logDir)
		os.Exit(1)
	}

	// 创建处理器（支持状态持久化）
	var h *handler.Handler
	if *stateFile != "" {
		slog.Info("state persistence enabled", "file", *stateFile)
		h = handler.NewHandlerWithState(*uploadDir, *workDir, *logDir, *stateFile)

		// 恢复之前的状态
		if err := h.RestoreState(); err != nil {
			slog.Warn("failed to restore state", "error", err)
		}
	} else {
		h = handler.NewHandler(*uploadDir, *workDir, *logDir)
	}

	authMw := auth.NewMiddleware(*token)

	// 注册路由（支持多应用）
	// 旧版单应用路由（向后兼容，默认使用 "default" 应用）
	http.HandleFunc("/api/v1/deploy", authMw.Authenticate(h.Deploy))
	http.HandleFunc("/api/v1/stop", authMw.Authenticate(h.Stop))
	http.HandleFunc("/api/v1/start", authMw.Authenticate(h.Start))
	http.HandleFunc("/api/v1/restart", authMw.Authenticate(h.Restart))
	http.HandleFunc("/api/v1/status", authMw.Authenticate(h.Status))
	http.HandleFunc("/api/v1/logs", authMw.Authenticate(h.Logs))

	// 新版多应用路由
	http.HandleFunc("/api/v1/apps/", authMw.Authenticate(createMultiAppHandler(h)))

	// 健康检查
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			slog.Warn("failed to write health response", "error", err)
		}
	})

	addr := ":" + *port
	slog.Info("server starting",
		"addr", addr,
		"upload_dir", *uploadDir,
		"work_dir", *workDir,
		"log_dir", *logDir,
		"state_file", *stateFile,
	)

	// 配置带超时的 HTTP 服务器
	server := &http.Server{
		Addr:         addr,
		Handler:      nil,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// createMultiAppHandler 创建多应用路由处理器
func createMultiAppHandler(h *handler.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 提取路径后缀: /api/v1/apps/{appName}/{action}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")

		// 空路径或仅斜杠，列出所有应用
		if path == "" || path == "/" {
			h.ListApps(w, r)
			return
		}

		// 解析应用名和操作
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 1 {
			sendJSONError(w, "invalid path", http.StatusBadRequest)
			return
		}

		appName := parts[0]

		// 如果只有应用名，根据方法决定行为
		if len(parts) == 1 {
			if r.Method == http.MethodDelete {
				r.URL.Path = "/api/v1/apps/" + appName + "/remove"
				h.RemoveApp(w, r)
				return
			}

			// 临时修改路径供 handler 使用
			r.URL.Path = "/api/v1/apps/" + appName + "/status"
			h.Status(w, r)
			return
		}

		// 有应用名和操作
		action := parts[1]

		// 临时修改路径供 handler 使用
		r.URL.Path = "/api/v1/apps/" + appName + "/" + action

		switch action {
		case "deploy":
			h.Deploy(w, r)
		case "stop":
			h.Stop(w, r)
		case "start":
			h.Start(w, r)
		case "restart":
			h.Restart(w, r)
		case "status":
			h.Status(w, r)
		case "logs":
			h.Logs(w, r)
		case "remove":
			h.RemoveApp(w, r)
		default:
			sendJSONError(w, "unknown action: "+action, http.StatusNotFound)
		}
	}
}

// sendJSONError 发送JSON错误响应
func sendJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": message,
	}); err != nil {
		slog.Warn("failed to encode error response", "error", err)
	}
}
