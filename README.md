# dzjjy - 简易部署服务

一个用于开发环境的快速部署工具，支持将应用快速部署到远程服务器，自动停止旧服务并启动新服务。

## 特性

- **进程守护**：类似 PM2，支持进程崩溃自动重启，保持应用 24/7 运行
- **简单分类**：只区分两种程序类型（可执行程序和需要运行时的程序）
- **自动重启**：可配置最大重启次数，防止无限重启循环
- **进程监控**：实时监控进程状态、运行时间、重启次数
- **压缩文件支持**：支持 ZIP、TAR、TAR.GZ 格式，自动解压缩
- **简单认证**：基于 Token 的简单认证机制
- **RESTful API**：清晰的 HTTP API 接口
- **命令行工具**：易用的客户端命令行工具
- **结构化日志**：使用 slog 记录结构化日志，便于问题排查
- **Go 优势**：充分利用 Go 的 goroutine 和 context 实现高效的进程管理
- **Unix 哲学**：简单、模块化、可扩展

## 快速开始

### 1. 编译

```bash
# 编译服务端
go build -o dzjjy-server ./cmd/server

# 编译客户端
go build -o dzjjy-client ./cmd/client
```

### 2. 启动服务端

```bash
./dzjjy-server -token your-secret-token -port 8080
```

参数说明：
- `-token`: 认证令牌（必需）
- `-port`: 服务端口（默认：8080）
- `-upload`: 上传目录（默认：./uploads）
- `-work`: 工作目录（默认：./workspace）

### 3. 使用客户端部署

#### 部署应用

**类型 1：可执行程序（exec）**

直接运行的可执行文件，如编译好的二进制程序、shell 脚本等。

```bash
# 部署可执行程序（如编译好的 Go 程序）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp \
  -type exec \
  -executable ./myapp

# 部署 shell 脚本
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file start.sh \
  -type exec \
  -executable ./start.sh
```

**类型 2：需要运行时的程序（runtime）**

需要通过解释器或运行时执行的程序，如 Python、NodeJS、Java 等。

**重要说明：** `entry` 字段可以包含多个空格分隔的参数，用于指定运行时参数和入口文件。

**参数顺序：** `executable` → `entry`（运行时参数 + 入口文件） → `args`（应用参数）

```bash
# 部署 Python 应用
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.py \
  -type runtime \
  -executable python3 \
  -entry app.py

# 部署 Python 模块（使用 -m 参数）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.zip \
  -type runtime \
  -executable python3 \
  -entry "-m uvicorn main:app" \
  -args "--host 0.0.0.0 --port 8000"
# 最终命令：python3 -m uvicorn main:app --host 0.0.0.0 --port 8000

# 部署 NodeJS 应用
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.js \
  -type runtime \
  -executable node \
  -entry app.js

# 部署 NodeJS 应用（带运行时参数）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.js \
  -type runtime \
  -executable node \
  -entry "--experimental-modules index.js" \
  -args "--port 3000"
# 最终命令：node --experimental-modules index.js --port 3000

# 部署 Java 应用（简单情况）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.jar \
  -type runtime \
  -executable java \
  -entry "-jar app.jar"
# 最终命令：java -jar app.jar

# 部署 Java Spring Boot 应用（带配置参数）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.jar \
  -type runtime \
  -executable java \
  -entry "-jar app.jar" \
  -args "--spring.profiles.active=prod --server.port=9090"
# 最终命令：java -jar app.jar --spring.profiles.active=prod --server.port=9090

# 部署 Go 源码（使用 go run）
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file main.go \
  -type runtime \
  -executable go \
  -entry "run main.go"
# 最终命令：go run main.go
```

**启用进程守护（自动重启）**

```bash
# 启用自动重启，无限次重启
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.py \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -auto-restart \
  -max-restarts 0

# 启用自动重启，最多重启 10 次
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file app.js \
  -type runtime \
  -executable node \
  -entry app.js \
  -auto-restart \
  -max-restarts 10
```

**部署压缩文件（自动解压）**

支持的压缩格式：`.zip`、`.tar`、`.tar.gz`、`.gz`

```bash
# 部署 ZIP 压缩包
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -auto-restart

# 部署 TAR.GZ 压缩包
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp.tar.gz \
  -type runtime \
  -executable node \
  -entry index.js

# 部署 TAR 压缩包
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp.tar \
  -type exec \
  -executable ./start.sh
```

**工作原理：**
1. 上传压缩文件到服务器
2. 服务器自动检测文件格式（根据扩展名）
3. 自动解压到工作目录
4. 删除压缩文件
5. 使用指定的命令启动应用

**注意事项：**
- 压缩包内的文件会被解压到工作目录的根目录
- `entry` 参数应该是解压后的相对路径
- 确保压缩包内包含所有必要的依赖文件

#### 查询状态

```bash
./dzjjy-client status \
  -server http://localhost:8080 \
  -token your-secret-token
```

输出示例：
```
Running: true
PID: 12345
Type: runtime
Executable: python3
Entry: app.py
Auto Restart: true
Restart Count: 2
Uptime: 3600 seconds
```

#### 重启应用

```bash
./dzjjy-client restart \
  -server http://localhost:8080 \
  -token your-secret-token
```

#### 停止应用

```bash
./dzjjy-client stop \
  -server http://localhost:8080 \
  -token your-secret-token
```

#### 查看日志

```bash
# 查看最近 100 行日志（默认）
./dzjjy-client logs \
  -server http://localhost:8080 \
  -token your-secret-token

# 查看最近 50 行日志
./dzjjy-client logs \
  -server http://localhost:8080 \
  -token your-secret-token \
  -lines 50
```

输出示例：
```
[2025-11-29T18:45:23Z] [system] Starting application: type=runtime, executable=python3, entry=app.py
[2025-11-29T18:45:23Z] [system] Process started: PID=12345
[2025-11-29T18:45:24Z] [stdout] Server starting on port 8000
[2025-11-29T18:45:24Z] [stdout] Ready to accept connections
[2025-11-29T18:46:30Z] [stderr] Warning: deprecated API usage

Total: 5 lines
```

## API 接口

### 部署应用

```
POST /api/v1/deploy
Authorization: Bearer <token>
Content-Type: multipart/form-data

参数：
- file: 应用文件（必需）
- type: 程序类型，exec 或 runtime（必需）
- executable: 可执行程序路径或运行时命令（必需）
- entry: 入口文件（runtime 类型需要）
- args: 启动参数（可选）
- auto_restart: 是否启用自动重启，true 或 false（可选，默认 false）
- max_restarts: 最大重启次数，0 表示无限制（可选，默认 0）
```

### 停止应用

```
POST /api/v1/stop
Authorization: Bearer <token>
```

### 重启应用

```
POST /api/v1/restart
Authorization: Bearer <token>
```

### 启动应用

```
POST /api/v1/start
Authorization: Bearer <token>
Content-Type: application/json

{
  "type": "runtime",
  "executable": "python3",
  "entry": "app.py",
  "args": "",
  "auto_restart": true,
  "max_restarts": 10
}
```

### 查询状态

```
GET /api/v1/status
Authorization: Bearer <token>

响应示例：
{
  "success": true,
  "message": "ok",
  "data": {
    "running": true,
    "pid": 12345,
    "type": "runtime",
    "executable": "python3",
    "entry": "app.py",
    "auto_restart": true,
    "restart_count": 2,
    "uptime": 3600
  }
}
```

### 查询日志

```
GET /api/v1/logs?lines=100
Authorization: Bearer <token>

参数：
- lines: 返回的日志行数（可选，默认 100）

响应示例：
{
  "success": true,
  "message": "ok",
  "data": {
    "logs": [
      {
        "timestamp": "2025-11-29T18:45:23Z",
        "type": "system",
        "message": "Process started: PID=12345"
      },
      {
        "timestamp": "2025-11-29T18:45:24Z",
        "type": "stdout",
        "message": "Server starting on port 8000"
      }
    ],
    "log_file": "/path/to/logs/app-20251129-184523.log",
    "count": 2
  }
}
```

### 健康检查

```
GET /health
```

## 项目结构

```
dzjjy/
├── cmd/
│   ├── server/          # 服务端入口
│   └── client/          # 客户端入口
├── internal/
│   ├── server/
│   │   ├── handler/     # HTTP 处理器
│   │   ├── runtime/     # 运行时管理
│   │   └── auth/        # 认证模块
│   └── client/
│       └── deploy/      # 部署逻辑
├── pkg/
│   └── api/             # API 定义
└── config/              # 配置示例
```

## 设计原则

### Unix 设计哲学

- **模块化**：清晰的模块划分，通过接口连接
- **简单性**：专注核心功能，避免过度设计
- **通用性**：支持多种运行时，易于扩展
- **可扩展性**：预留扩展空间，便于添加新功能

### Go 语言优势

本项目充分利用了 Go 语言的特性来实现高效的进程守护：

1. **Goroutine 并发**
   - 每个进程监控运行在独立的 goroutine 中
   - 轻量级协程，资源占用极小
   - 可以同时监控多个进程（未来扩展）

2. **Context 控制**
   - 使用 `context.Context` 优雅地控制进程生命周期
   - 支持取消信号传播，确保资源正确释放
   - 避免 goroutine 泄漏

3. **Channel 通信**
   - 通过 channel 实现进程状态的安全通信
   - 符合 "不要通过共享内存来通信，而要通过通信来共享内存" 的理念

4. **RWMutex 同步**
   - 使用读写锁保护共享状态
   - 允许多个读操作并发执行
   - 保证数据一致性

5. **编译型语言**
   - 单一二进制文件，无需依赖
   - 跨平台编译，部署简单
   - 性能优异，资源占用低

## 进程守护实现

类似 PM2 的核心功能：

- **自动重启**：进程崩溃后自动重启，保持服务运行
- **重启限制**：可配置最大重启次数，防止无限重启循环
- **优雅停止**：通过 context 取消信号优雅停止进程
- **状态监控**：实时监控进程状态、PID、运行时间、重启次数
- **延迟重启**：崩溃后等待 1 秒再重启，避免快速失败循环

## 日志管理功能

完善的日志收集和查询系统：

### 日志收集

- **实时捕获**：自动捕获应用的 stdout 和 stderr 输出
- **系统日志**：记录进程启动、停止、重启等系统事件
- **双重存储**：
  - 内存缓存：保留最近 1000 行日志，快速查询
  - 文件持久化：所有日志写入文件，便于长期分析
- **日志分类**：
  - `stdout`: 标准输出
  - `stderr`: 标准错误
  - `system`: 系统事件（启动、停止、重启等）

### 日志文件

- **自动命名**：`{type}-{executable}-{timestamp}.log`
- **时间戳**：每次启动创建新的日志文件
- **存储位置**：可配置的日志目录（默认 `./logs`）

### 日志查询

- **快速查询**：从内存中快速获取最近的日志
- **行数控制**：可指定返回的日志行数
- **时间戳**：每条日志都带有精确的时间戳
- **类型标识**：清晰标识日志来源（stdout/stderr/system）

### 使用场景

1. **快速定位问题**：查看最近的错误日志
2. **监控应用输出**：实时了解应用运行状态
3. **调试部署**：查看启动过程中的输出
4. **分析崩溃**：查看崩溃前的日志信息

## 注意事项

1. 本工具仅用于开发环境，不适合生产环境
2. Token 认证较为简单，请妥善保管
3. 服务端会自动停止旧服务，请确保数据已保存
4. 上传的文件会覆盖工作目录中的同名文件
5. 启用自动重启时，建议设置合理的 `max_restarts` 值，避免无限重启
6. 进程崩溃后会等待 1 秒再重启，这是为了避免快速失败循环
7. 日志文件会持续增长，建议定期清理旧日志
8. 内存中只保留最近 1000 行日志，更早的日志需要查看日志文件

## 许可证

MIT License
