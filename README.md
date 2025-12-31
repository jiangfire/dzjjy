# dzjjy - 简易部署服务

> 用于开发环境的快速部署工具，类似 PM2 的进程守护管理器

**核心功能**：
- ✅ 进程守护（自动重启、崩溃恢复）
- ✅ 文件部署（支持 ZIP/TAR/GZ 自动解压）
- ✅ 日志管理（内存缓存 + 文件持久化 + 自动轮转）
- ✅ 状态持久化（自动恢复、原子写入）
- ✅ Token 认证（简单安全）

## 快速开始

### 1. 编译和启动

```bash
# 编译
go build -o dzjjy-server ./cmd/server
go build -o dzjjy-client ./cmd/client

# 启动服务端（推荐启用状态持久化）
./dzjjy-server -token your-secret-token -port 8080 -state ./state.json
```

**参数**：
- `-token`：认证令牌（必需）
- `-port`：服务端口（默认 8080）
- `-state`：状态持久化文件（可选，启用后可自动恢复）

### 2. 部署应用

#### 应用类型

**exec** - 直接可执行文件
```bash
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -file myapp -type exec -executable ./myapp
```

**runtime** - 需要运行时
```bash
# Python
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -file app.py -type runtime -executable python3 -entry app.py

# Node.js（带参数）
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -file app.js -type runtime -executable node -entry "index.js --port 3000"

# Java JAR
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -file app.jar -type runtime -executable java -entry "-jar app.jar" -args "--server.port=8080"
```

**参数说明**：
- `executable`：运行时命令或可执行文件
- `entry`：入口文件（可包含运行时参数）
- `args`：应用参数（可选）
- `auto_restart`：是否自动重启
- `max_restarts`：最大重启次数（0=无限）

#### 进程守护

```bash
# 自动重启（无限次）
./dzjjy-client deploy ... -auto-restart -max-restarts 0

# 自动重启（最多10次）
./dzjjy-client deploy ... -auto-restart -max-restarts 10
```

#### 压缩文件部署

支持：`.zip`, `.tar`, `.tar.gz`, `.gz`

```bash
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -file app.zip -type runtime -executable python3 -entry app.py
```

**工作流程**：上传 → 自动检测格式 → 解压到工作目录 → 删除压缩包 → 启动应用

#### 其他操作

```bash
# 查询状态
./dzjjy-client status -server http://localhost:8080 -token your-token

# 重启应用
./dzjjy-client restart -server http://localhost:8080 -token your-token

# 停止应用
./dzjjy-client stop -server http://localhost:8080 -token your-token

# 查看日志（默认100行）
./dzjjy-client logs -server http://localhost:8080 -token your-token
# 指定行数
./dzjjy-client logs -server http://localhost:8080 -token your-token -lines 50
```

## API 接口

所有接口需要 `Authorization: Bearer <token>` 头（除 `/health`）。

| 方法 | 路径 | 说明 | 请求体 |
|------|------|------|--------|
| `POST` | `/api/v1/deploy` | 部署应用 | multipart/form-data |
| `POST` | `/api/v1/start` | 启动应用 | JSON |
| `POST` | `/api/v1/stop` | 停止应用 | - |
| `POST` | `/api/v1/restart` | 重启应用 | - |
| `GET` | `/api/v1/status` | 查询状态 | - |
| `GET` | `/api/v1/logs?lines=N` | 查询日志 | - |
| `GET` | `/health` | 健康检查 | - |

### 部署请求示例

**multipart/form-data**:
```
file: 应用文件
type: exec | runtime
executable: 命令或路径
entry: 入口文件（runtime需要）
args: 启动参数（可选）
auto_restart: true | false
max_restarts: 0（无限）或正整数
```

**JSON (POST /api/v1/start)**:
```json
{
  "type": "runtime",
  "executable": "python3",
  "entry": "app.py",
  "args": "--port 8000",
  "auto_restart": true,
  "max_restarts": 10
}
```

### 状态响应示例

```json
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

## 📚 文档

| 文档 | 说明 |
|------|------|
| **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** | 架构设计、模块结构、核心概念 |
| **[docs/TESTING.md](docs/TESTING.md)** | 测试指南、覆盖率、最佳实践 |

## 🔧 未来规划

| 功能 | 优先级 | 说明 |
|------|--------|------|
| **多应用管理** | 🔴 高 | 支持同时管理多个应用 |
| **插件系统** | 🟡 中 | 可扩展的插件机制（Webhook、指标收集等） |
| **配置文件** | 🟡 中 | YAML/JSON 配置支持 |
| **环境变量** | 🟡 中 | 应用环境设置 |
| **健康检查** | 🟢 低 | 定期健康探测 |
| **Web UI** | 🟢 低 | Web 管理界面 |

**设计原则**：
1. **保持简单**：避免过度设计
2. **向后兼容**：不破坏现有 API
3. **充分测试**：功能独立测试
4. **文档先行**：实现前先设计接口

## 注意事项

- ⚠️ 仅用于开发环境，不适合生产
- 🔐 Token 认证简单，请妥善保管
- 💾 服务端会自动停止旧服务，请保存数据
- 📁 上传文件会覆盖工作目录同名文件
- 🔄 自动重启建议设置合理的 `max_restarts` 值
- ⏱️ 进程崩溃后等待 1 秒再重启（防快速失败）
- 🗑️ 日志支持自动轮转，可配置保留策略
- 💾 内存仅保留最近 1000 行日志

## 许可证

MIT License
