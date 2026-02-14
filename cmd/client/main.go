package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jiangfire/dzjjy/internal/client/deploy"
)

func main() {
	// 配置 slog
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 子命令
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "deploy":
		deployCmd()
	case "stop":
		stopCmd()
	case "restart":
		restartCmd()
	case "status":
		statusCmd()
	case "logs":
		logsCmd()
	case "list":
		listCmd()
	case "start":
		startCmd()
	case "remove":
		removeCmd()
	default:
		slog.Error("unknown command") // #nosec G706 - avoid logging raw user input
		printUsage()
		os.Exit(1)
	}
}

func deployCmd() {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")
	file := fs.String("file", "", "file to deploy (required)")
	appType := fs.String("type", "", "application type: exec or runtime (required)")
	executable := fs.String("executable", "", "executable path or runtime command (required)")
	entry := fs.String("entry", "", "entry file (required for runtime type)")
	args := fs.String("args", "", "application arguments")
	autoRestart := fs.Bool("auto-restart", false, "enable auto restart on crash")
	maxRestarts := fs.Int("max-restarts", 0, "max restart attempts (0 = unlimited)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" || *file == "" || *appType == "" || *executable == "" {
		slog.Error("missing required parameters",
			"token", *token != "",
			"file", *file != "",
			"type", *appType != "",
			"executable", *executable != "",
		)
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	if err := client.Deploy(*app, *file, *appType, *executable, *entry, *args, *autoRestart, *maxRestarts); err != nil {
		slog.Error("deployment failed", "error", err)
		os.Exit(1)
	}

	slog.Info("deployment successful",
		"app", *app,
		"server", *server,
		"type", *appType,
		"executable", *executable,
		"auto_restart", *autoRestart,
		"max_restarts", *maxRestarts,
	)
}

func stopCmd() {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	if err := client.Stop(*app); err != nil {
		slog.Error("stop failed", "error", err)
		os.Exit(1)
	}

	slog.Info("application stopped successfully", "app", *app, "server", *server)
}

func restartCmd() {
	fs := flag.NewFlagSet("restart", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	if err := client.Restart(*app); err != nil {
		slog.Error("restart failed", "error", err)
		os.Exit(1)
	}

	slog.Info("application restarted successfully", "app", *app, "server", *server)
}

func logsCmd() {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")
	lines := fs.Int("lines", 100, "number of log lines to retrieve")
	follow := fs.Bool("follow", false, "follow log output (not implemented yet)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	if *follow {
		slog.Warn("follow mode is not implemented yet, showing last logs only")
	}

	client := deploy.NewClient(*server, *token)
	logs, err := client.Logs(*app, *lines)
	if err != nil {
		slog.Error("logs query failed", "error", err)
		os.Exit(1)
	}

	if len(logs) == 0 {
		slog.Info("no logs available", "app", *app)
		return
	}

	// 打印日志
	for _, log := range logs {
		timestamp := log["timestamp"]
		logType := log["type"]
		message := log["message"]
		fmt.Printf("[%v] [%v] %v\n", timestamp, logType, message)
	}

	slog.Info("logs retrieved", "app", *app, "count", len(logs), "server", *server)
}

func statusCmd() {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	status, err := client.Status(*app)
	if err != nil {
		slog.Error("status query failed", "error", err)
		os.Exit(1)
	}

	// 打印状态信息（保留用户友好的格式）
	if status.AppName != "" {
		fmt.Printf("App Name: %s\n", status.AppName)
	}
	fmt.Printf("Running: %v\n", status.Running)
	if status.Running {
		fmt.Printf("PID: %d\n", status.PID)
		fmt.Printf("Type: %s\n", status.Type)
		fmt.Printf("Executable: %s\n", status.Executable)
		if status.Entry != "" {
			fmt.Printf("Entry: %s\n", status.Entry)
		}
		fmt.Printf("Auto Restart: %v\n", status.AutoRestart)
		fmt.Printf("Restart Count: %d\n", status.RestartCount)
		fmt.Printf("Uptime: %d seconds\n", status.Uptime)
	}

	slog.Info("status retrieved", "app", *app, "running", status.Running, "server", *server)
}

// listCmd 列出所有应用
func listCmd() {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	apps, err := client.ListApps()
	if err != nil {
		slog.Error("list apps failed", "error", err)
		os.Exit(1)
	}

	if len(apps) == 0 {
		fmt.Println("No applications running")
		return
	}

	fmt.Printf("Found %d application(s):\n\n", len(apps))
	for name, info := range apps {
		fmt.Printf("App: %s\n", name)
		fmt.Printf("  Running: %v\n", info.Running)
		if info.Running {
			fmt.Printf("  PID: %d\n", info.PID)
			fmt.Printf("  Type: %s\n", info.Type)
			fmt.Printf("  Executable: %s\n", info.Executable)
			if info.Entry != "" {
				fmt.Printf("  Entry: %s\n", info.Entry)
			}
			fmt.Printf("  Uptime: %d seconds\n", info.Uptime)
		}
		fmt.Println()
	}

	slog.Info("listed applications", "count", len(apps), "server", *server)
}

// startCmd 启动已停止的应用
func startCmd() {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")
	appType := fs.String("type", "", "application type: exec or runtime (required)")
	executable := fs.String("executable", "", "executable path or runtime command (required)")
	entry := fs.String("entry", "", "entry file (required for runtime type)")
	args := fs.String("args", "", "application arguments")
	autoRestart := fs.Bool("auto-restart", false, "enable auto restart on crash")
	maxRestarts := fs.Int("max-restarts", 0, "max restart attempts (0 = unlimited)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" || *appType == "" || *executable == "" {
		slog.Error("missing required parameters")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	if err := client.Start(*app, *appType, *executable, *entry, *args, *autoRestart, *maxRestarts); err != nil {
		slog.Error("start failed", "error", err)
		os.Exit(1)
	}

	slog.Info("application started successfully", "app", *app, "server", *server)
}

// removeCmd 删除应用（停止并清理）
func removeCmd() {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "server URL")
	token := fs.String("token", "", "auth token (required)")
	app := fs.String("app", "default", "application name (for multi-app mode)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	if *token == "" {
		slog.Error("token is required")
		fs.Usage()
		os.Exit(1)
	}

	client := deploy.NewClient(*server, *token)
	if err := client.Remove(*app); err != nil {
		slog.Error("remove failed", "error", err)
		os.Exit(1)
	}

	slog.Info("application removed successfully", "app", *app, "server", *server)
}

func printUsage() {
	fmt.Println("Usage: dzjjy-client <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  deploy   Deploy application to server")
	fmt.Println("  start    Start a stopped application")
	fmt.Println("  stop     Stop running application")
	fmt.Println("  restart  Restart running application")
	fmt.Println("  status   Query application status")
	fmt.Println("  logs     View application logs")
	fmt.Println("  list     List all applications")
	fmt.Println("  remove   Remove application (stop and cleanup)")
	fmt.Println("\nUse 'dzjjy-client <command> -h' for more information about a command.")
	fmt.Println("\nMulti-app mode: Use -app <name> to specify application (default: 'default')")
}
