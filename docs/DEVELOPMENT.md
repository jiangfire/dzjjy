# 开发指南

> 文档更新：2026-03-21
> 说明：已同步当前多应用 API、构建目录和发布后的文档状态

This file provides guidance to developers when working with code in this repository.

## Project Overview

dzjjy (简易部署服务) is a lightweight deployment tool for development environments, similar to PM2. It provides process management, automatic restart on crash, log collection, and remote deployment capabilities. The project is written in Go and follows Unix design philosophy.

## Build and Run Commands

### Building
```bash
# Recommended
make build

# Or build individually
go build -o build/dzjjy-server ./cmd/server
go build -o build/dzjjy-client ./cmd/client
```

### Running Server
```bash
# Minimal start (token required)
./build/dzjjy-server -token <your-token>

# Full configuration
./build/dzjjy-server -token <token> -port 8080 -upload ./uploads -work ./workspace -log ./logs -state ./state.json
```

### Client Commands
```bash
# Deploy application
./build/dzjjy-client deploy -server <url> -token <token> -app <name> -file <file> -type <exec|runtime> -executable <cmd> [-entry <file>] [-auto-restart] [-max-restarts <n>]

# Query status
./build/dzjjy-client status -server <url> -token <token> -app <name>

# View logs
./build/dzjjy-client logs -server <url> -token <token> -app <name> [-lines <n>] [--follow]

# Restart/Stop
./build/dzjjy-client restart -server <url> -token <token> -app <name>
./build/dzjjy-client stop -server <url> -token <token> -app <name>
```

## Application Types

The system distinguishes two application types:

- **`exec`**: Direct executables (binaries, shell scripts)
  - Command: `executable [args...]`
  - Example: `./myapp --port 8080`

- **`runtime`**: Interpreted programs requiring runtime
  - Command: `executable entry-args... [app-args...]`
  - The `entry` field can contain multiple space-separated arguments (runtime flags + entry file)
  - Examples:
    - Python: `python3 app.py` → executable=`python3`, entry=`app.py`
    - Node.js: `node index.js` → executable=`node`, entry=`index.js`
    - Java JAR: `java -jar app.jar --spring.profiles.active=prod` → executable=`java`, entry=`-jar app.jar`, args=`--spring.profiles.active=prod`
    - Python module: `python3 -m uvicorn main:app --host 0.0.0.0` → executable=`python3`, entry=`-m uvicorn main:app`, args=`--host 0.0.0.0`
    - Node with flags: `node --experimental-modules index.js --port 3000` → executable=`node`, entry=`--experimental-modules index.js`, args=`--port 3000`

## Logging Standards

**Use structured logging with `log/slog`** - never use `fmt.Println` or `log.Printf`:

```go
// Server: JSON format to stdout
slog.Info("process started", "pid", pid, "type", appType, "executable", executable)
slog.Warn("process exited, restarting", "error", err, "attempt", count)
slog.Error("failed to start", "error", err)

// Client: Text format to stderr
slog.Info("deployment successful", "server", url, "type", appType)
slog.Error("deployment failed", "error", err)
```

Always include relevant context fields (pid, type, executable, error, etc.) for debugging.

## Key Design Principles

1. **Two-type classification**: Only `exec` vs `runtime` - no language-specific handling
2. **Simple over complex**: Avoid over-engineering, focus on core functionality
3. **Goroutine per process**: Each monitored process runs in its own goroutine
4. **Context for lifecycle**: Use `context.Context` for cancellation propagation
5. **Path traversal protection**: Always validate extracted archive paths against work directory
6. **Security first**: Comprehensive error handling, input validation, and security checks

## API Endpoints

All require `Authorization: Bearer <token>` except `/health`.

Current multi-app endpoints:

- `GET /api/v1/apps` - list applications
- `POST /api/v1/apps/{name}/deploy` - multipart upload with metadata
- `POST /api/v1/apps/{name}/start` - start application
- `POST /api/v1/apps/{name}/stop` - stop application
- `POST /api/v1/apps/{name}/restart` - restart application
- `GET /api/v1/apps/{name}/status` - query status
- `GET /api/v1/apps/{name}/logs?lines=N` - retrieve recent logs
- `DELETE /api/v1/apps/{name}/remove` - remove application
- `GET /health` - health check

Legacy single-app compatibility endpoints remain for `default`:

- `POST /api/v1/deploy`
- `POST /api/v1/start`
- `POST /api/v1/stop`
- `POST /api/v1/restart`
- `GET /api/v1/status`
- `GET /api/v1/logs`

## Important Constraints

- **Development only**: Not designed for production use
- **Multi-application**: MultiManager supports multiple apps simultaneously
- **Work directory**: Cleared on each deployment (per app)
- **Log retention**: Only last 1000 lines in memory, rest in files with rotation
- **Restart delay**: Fixed 1-second delay between restart attempts
- **Archive extraction**: Files extracted to work directory root

## Common Patterns

**Adding new runtime support**: No code changes needed - users specify executable command and entry with runtime flags (e.g., `-executable ruby -entry script.rb` or `-executable java -entry " -jar app.jar"`)

**Extending archive formats**: Add handler in `internal/server/archive/archive.go` and update `IsArchive()` and `Extract()` switch statements

**Adding API endpoints**:
1. Add handler method to `internal/server/handler/handler.go`
2. Register route in `cmd/server/main.go` with auth middleware
3. Add client method to `internal/client/deploy/deploy.go`
4. Add CLI command to `cmd/client/main.go`

## Testing

For comprehensive testing information, see [TESTING.md](./TESTING.md).

Quick commands:
```bash
# All tests
go test ./...

# With coverage
go test -cover ./internal/server/...

# Generate HTML report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Match CI release-grade checks more closely
go test -race ./...
```

## Security

This project has undergone comprehensive security hardening:

- ✅ **G301/G302/G306**: Directory and file permission fixes
- ✅ **G304**: Path traversal protection (archive extraction, file operations)
- ✅ **G204**: Command injection prevention (executable validation)
- ✅ **G115**: Integer overflow protection (file mode validation)
- ✅ **G110**: Decompression bomb prevention (100MB size limit)
- ✅ **G104**: Comprehensive error handling

See ARCHITECTURE.md for security design details.
