# dzjjy - 简易部署服务

> 用于开发环境的快速部署工具，类似 PM2 的进程守护管理器

**核心功能**：
- ✅ **多应用管理**：同时管理多个应用，独立工作目录和日志
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

#### 多应用管理

```bash
# 部署到指定应用（默认: default）
./dzjjy-client deploy -server http://localhost:8080 -token your-token \
  -app myapp -file app.py -type runtime -executable python3 -entry app.py

# 查询指定应用状态
./dzjjy-client status -server http://localhost:8080 -token your-token -app myapp

# 列出所有应用
./dzjjy-client list -server http://localhost:8080 -token your-token

# 停止指定应用
./dzjjy-client stop -server http://localhost:8080 -token your-token -app myapp

# 重启指定应用
./dzjjy-client restart -server http://localhost:8080 -token your-token -app myapp

# 查看指定应用日志
./dzjjy-client logs -server http://localhost:8080 -token your-token -app myapp -lines 50

# 启动已停止的应用
./dzjjy-client start -server http://localhost:8080 -token your-token \
  -app myapp -type runtime -executable python3 -entry app.py

# 删除应用（停止并清理）
./dzjjy-client remove -server http://localhost:8080 -token your-token -app myapp
```

**向后兼容**：不指定 `-app` 参数时，默认操作名为 `default` 的应用。

## API 接口

所有接口需要 `Authorization: Bearer <token>` 头（除 `/health`）。

### 多应用路由（推荐）

| 方法 | 路径 | 说明 | 请求体 |
|------|------|------|--------|
| `GET` | `/api/v1/apps` | 列出所有应用 | - |
| `POST` | `/api/v1/apps/{name}/deploy` | 部署应用 | multipart/form-data |
| `POST` | `/api/v1/apps/{name}/start` | 启动应用 | JSON |
| `POST` | `/api/v1/apps/{name}/stop` | 停止应用 | - |
| `POST` | `/api/v1/apps/{name}/restart` | 重启应用 | - |
| `GET` | `/api/v1/apps/{name}/status` | 查询状态 | - |
| `GET` | `/api/v1/apps/{name}/logs?lines=N` | 查询日志 | - |
| `DELETE` | `/api/v1/apps/{name}` | 删除应用 | - |

> **TODO**: 服务器端路由配置需要更新以支持 `DELETE /api/v1/apps/{name}`。当前实现使用 `POST /api/v1/apps/{name}/remove`。

### 单应用路由（向后兼容）

| 方法 | 路径 | 说明 | 请求体 |
|------|------|------|--------|
| `POST` | `/api/v1/deploy` | 部署到 default 应用 | multipart/form-data |
| `POST` | `/api/v1/start` | 启动 default 应用 | JSON |
| `POST` | `/api/v1/stop` | 停止 default 应用 | - |
| `POST` | `/api/v1/restart` | 重启 default 应用 | - |
| `GET` | `/api/v1/status` | 查询 default 应用状态 | - |
| `GET` | `/api/v1/logs?lines=N` | 查询 default 应用日志 | - |
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
    "app_name": "myapp",
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

### 列出应用示例

```json
{
  "success": true,
  "message": "ok",
  "data": {
    "count": 2,
    "apps": {
      "web-server": {
        "app_name": "web-server",
        "running": true,
        "pid": 12345,
        "type": "runtime",
        "executable": "python3",
        "entry": "app.py",
        "uptime": 3600
      },
      "api-server": {
        "app_name": "api-server",
        "running": false
      }
    }
  }
}
```

## 📚 文档

| 文档 | 说明 |
|------|------|
| **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** | 架构设计、模块结构、核心概念 |
| **[docs/TESTING.md](docs/TESTING.md)** | 测试指南、覆盖率、最佳实践 |
| **[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)** | 开发指南、构建命令、设计原则 |
| **[docs/PLAN.md](docs/PLAN.md)** | 项目计划、功能规划、优先级 |

## 🚀 多应用配置示例

### 场景 1: 同时运行 Web 服务和 API 服务

**服务端启动:**
```bash
./dzjjy-server -token my-secret-token -port 8080 -state ./state.json
```

**部署 Web 服务 (Python Flask):**
```bash
# 1. 打包应用
zip -r web-app.zip app.py requirements.txt static/ templates/

# 2. 部署到 web-server
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token my-secret-token \
  -app web-server \
  -file web-app.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -args "--host=0.0.0.0 --port=5000" \
  -auto-restart \
  -max-restarts 5
```

**部署 API 服务 (Node.js):**
```bash
# 1. 打包应用
zip -r api-app.zip server.js package.json

# 2. 部署到 api-server
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token my-secret-token \
  -app api-server \
  -file api-app.zip \
  -type runtime \
  -executable node \
  -entry server.js \
  -args "--port=3000" \
  -auto-restart \
  -max-restarts 0  # 无限重启
```

**管理多个应用:**
```bash
# 查看所有应用状态
./dzjjy-client list -server http://localhost:8080 -token my-secret-token

# 查看特定应用状态
./dzjjy-client status -server http://localhost:8080 -token my-secret-token -app web-server

# 查看特定应用日志
./dzjjy-client logs -server http://localhost:8080 -token my-secret-token -app api-server -lines 100

# 重启特定应用
./dzjjy-client restart -server http://localhost:8080 -token my-secret-token -app web-server

# 停止特定应用
./dzjjy-client stop -server http://localhost:8080 -token my-secret-token -app api-server

# 删除应用（停止并清理工作目录）
./dzjjy-client remove -server http://localhost:8080 -token my-secret-token -app web-server
```

### 场景 2: 多环境管理

**开发环境:**
```bash
./dzjjy-client deploy -app myapp-dev -file app.py -type runtime -executable python3 -entry app.py -args "--env=dev"
```

**测试环境:**
```bash
./dzjjy-client deploy -app myapp-test -file app.py -type runtime -executable python3 -entry app.py -args "--env=test"
```

**生产环境 (模拟):**
```bash
./dzjjy-client deploy -app myapp-prod -file app.py -type runtime -executable python3 -entry app.py -args "--env=prod" -auto-restart -max-restarts 10
```

### 工作目录结构

启用多应用后，工作目录结构如下：
```
workspace/
├── web-server/      # web-server 的工作目录
│   ├── app.py
│   └── static/
├── api-server/      # api-server 的工作目录
│   ├── server.js
│   └── package.json
└── myapp-dev/       # 开发环境的工作目录
    └── app.py

logs/
├── web-server/      # web-server 的日志文件
├── api-server/      # api-server 的日志文件
└── myapp-dev/       # 开发环境的日志文件

uploads/             # 上传的压缩包临时存放
```

### 状态持久化

使用 `-state` 参数启动服务器后，应用状态会自动保存：
```bash
./dzjjy-server -token my-token -state ./state.json
```

重启服务器后，会自动恢复之前运行的应用状态（需要应用文件仍在工作目录中）。

## 🔧 未来规划

| 功能 | 优先级 | 说明 |
|------|--------|------|
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

本项目采用 **GNU Affero General Public License v3 (AGPL-3.0)** 协议。

**重要说明**：
- 如果您修改本项目并在网络服务中使用，必须开源您的修改版本
- 任何基于本项目的衍生作品也必须使用 AGPL-3.0 协议
- 详细条款请查看 [LICENSE](LICENSE) 文件

---

**文档更新时间**：2026-01-01
**最后更新**：文档归档优化、添加 .gitignore 规则、更新文档导航、切换至 AGPL-3.0 协议
