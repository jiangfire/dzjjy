# 开发指南

> 文档更新：2026-01-01
> 来源：从根目录 CLAUDE.md 移动

This file provides guidance to developers when working with code in this repository.

## Project Overview

dzjjy (简易部署服务) is a lightweight deployment tool for development environments, similar to PM2. It provides process management, automatic restart on crash, log collection, and remote deployment capabilities. The project is written in Go and follows Unix design philosophy.

## Build and Run Commands

### Building
```bash
# Build server
go build -o dzjjy-server ./cmd/server

# Build client
go build -o dzjjy-client ./cmd/client

# Build both (Windows)
go build -o dzjjy-server.exe ./cmd/server && go build -o dzjjy-client.exe ./cmd/client
```

### Running Server
```bash
# Minimal start (token required)
./dzjjy-server -token <your-token>

# Full configuration
./dzjjy-server -token <token> -port 8080 -upload ./uploads -work ./workspace -log ./logs
```

### Client Commands
```bash
# Deploy application
./dzjjy-client deploy -server <url> -token <token> -file <file> -type <exec|runtime> -executable <cmd> [-entry <file>] [-auto-restart] [-max-restarts <n>]

# Query status
./dzjjy-client status -server <url> -token <token>

# View logs
./dzjjy-client logs -server <url> -token <token> [-lines <n>]

# Restart/Stop
./dzjjy-client restart -server <url> -token <token>
./dzjjy-client stop -server <url> -token <token>
```

## Architecture Overview

### Core Components

**1. Process Management (`internal/server/runtime/`)**
- `runtime.Manager`: Central process lifecycle manager
  - Uses `context.Context` for graceful shutdown
  - Runs monitoring in separate goroutine
  - Uses `sync.RWMutex` for thread-safe state access
  - Implements automatic restart with configurable limits
  - Waits 1 second between restart attempts to avoid rapid failure loops

**2. Log Management (`internal/server/runtime/logger.go`)**
- Dual storage: in-memory ring buffer (1000 lines) + file persistence
- Captures stdout/stderr via `cmd.StdoutPipe()` and `cmd.StderrPipe()`
- Three log types: `stdout`, `stderr`, `system`
- Log files named: `{type}-{executable}-{timestamp}.log`

**3. Archive Handling (`internal/server/archive/`)**
- Supports ZIP, TAR, TAR.GZ, GZ formats
- Path traversal protection: validates all extracted paths
- Auto-detects format by file extension
- Extracts to work directory and removes archive after

**4. HTTP Handler (`internal/server/handler/`)**
- Deployment flow:
  1. Stop current app if running
  2. Clean work directory
  3. Save uploaded file
  4. Detect and extract if archive
  5. Start app with runtime.Manager
- All endpoints require Bearer token authentication (except `/health`)

### Application Types

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

### Concurrency Patterns

**Process Monitoring**:
```go
// Runs in goroutine, monitors process exit
func (m *Manager) monitor() {
    for {
        select {
        case <-m.ctx.Done():  // Graceful shutdown signal
            return
        default:
            m.cmd.Wait()  // Blocks until process exits
            // Check restart limits, delay, then restart
        }
    }
}
```

**Thread Safety**:
- All Manager state protected by `sync.RWMutex`
- Read operations (GetPID, IsRunning, GetInfo) use `RLock()`
- Write operations (Start, Stop, Restart) use `Lock()`

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

## API Endpoints

All require `Authorization: Bearer <token>` except `/health`:

- `POST /api/v1/deploy` - multipart/form-data upload with metadata
- `POST /api/v1/stop` - stop running application
- `POST /api/v1/restart` - restart running application
- `POST /api/v1/start` - start with JSON config
- `GET /api/v1/status` - query process status
- `GET /api/v1/logs?lines=N` - retrieve recent logs
- `GET /health` - health check (no auth)

## Important Constraints

- **Development only**: Not designed for production use
- **Single application**: Manager handles one application at a time
- **Work directory**: Cleared on each deployment
- **Log retention**: Only last 1000 lines in memory, rest in files
- **Restart delay**: Fixed 1-second delay between restart attempts
- **Archive extraction**: Files extracted to work directory root (no subdirectory isolation)

## Common Patterns

**Adding new runtime support**: No code changes needed - users specify executable command and entry with runtime flags (e.g., `-executable ruby -entry script.rb` or `-executable java -entry "-jar app.jar"`)

**Extending archive formats**: Add handler in `internal/server/archive/archive.go` and update `IsArchive()` and `Extract()` switch statements

**Adding API endpoints**:
1. Add handler method to `internal/server/handler/handler.go`
2. Register route in `cmd/server/main.go` with auth middleware
3. Add client method to `internal/client/deploy/deploy.go`
4. Add CLI command to `cmd/client/main.go`

## Testing

### Test Structure

The project uses comprehensive testing with the following structure:

```
internal/
├── server/
│   ├── archive/
│   │   └── archive_test.go      # 18 tests, 81.1% coverage
│   ├── auth/
│   │   └── auth_test.go         # 14 tests, 100% coverage
│   └── runtime/
│       ├── logger_test.go       # 16 tests, 100% coverage
│       └── runtime.go           # Tested via internal/runtime
├── runtime/
│   └── runtime_test.go          # 14 tests for Manager
```

### Running Tests

```bash
# All tests
go test ./...

# Specific modules
go test ./internal/server/archive/...
go test ./internal/server/auth/...
go test ./internal/server/runtime/...
go test ./internal/runtime/...

# With coverage
go test -cover ./internal/server/...
go test -coverprofile=coverage.out ./internal/server/...
go tool cover -html=coverage.out
```

### Test Design Principles

1. **Windows Compatibility**: Use Go binaries instead of shell scripts
2. **Isolated Testing**: Each test uses temporary directories
3. **Comprehensive Coverage**: Test success, failure, and edge cases
4. **Concurrent Safety**: Tests verify thread-safe operations
5. **Auto Cleanup**: TestMain handles setup/teardown

### Test Dependencies

- `github.com/stretchr/testify/assert` - Flexible assertions
- `github.com/stretchr/testify/require` - Fail-fast assertions

### Adding New Tests

When adding functionality:
1. Create test file in same package as implementation
2. Use `TestMain` for shared setup/teardown if needed
3. Test all public functions
4. Include edge cases (empty, invalid, boundary values)
5. Verify concurrent access safety
6. Use helper functions in `test/helpers.go` when possible
