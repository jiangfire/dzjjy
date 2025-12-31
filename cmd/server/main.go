package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
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
	if err := os.MkdirAll(*uploadDir, 0755); err != nil {
		slog.Error("failed to create upload dir", "error", err, "dir", *uploadDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(*workDir, 0755); err != nil {
		slog.Error("failed to create work dir", "error", err, "dir", *workDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(*logDir, 0755); err != nil {
		slog.Error("failed to create log dir", "error", err, "dir", *logDir)
		os.Exit(1)
	}

	// 创建处理器和中间件
	h := handler.NewHandler(*uploadDir, *workDir, *logDir)
	authMw := auth.NewMiddleware(*token)

	// 注册路由
	http.HandleFunc("/api/v1/deploy", authMw.Authenticate(h.Deploy))
	http.HandleFunc("/api/v1/stop", authMw.Authenticate(h.Stop))
	http.HandleFunc("/api/v1/start", authMw.Authenticate(h.Start))
	http.HandleFunc("/api/v1/restart", authMw.Authenticate(h.Restart))
	http.HandleFunc("/api/v1/status", authMw.Authenticate(h.Status))
	http.HandleFunc("/api/v1/logs", authMw.Authenticate(h.Logs))

	// 健康检查
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := ":" + *port
	slog.Info("server starting",
		"addr", addr,
		"upload_dir", *uploadDir,
		"work_dir", *workDir,
		"log_dir", *logDir,
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
