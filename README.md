# dzjjy - 简易部署服务

<div align="center">

[![CI](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml)
[![Quality](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://golang.org)

**轻量级进程管理和部署工具，专为开发环境设计**

[快速开始](#快速开始) • [功能特性](#功能特性) • [使用指南](#使用指南) • [API 文档](#api-接口) • [文档](#文档)

</div>

---

## 简介

dzjjy 是一个类似 PM2 的进程守护管理器，专为开发环境设计。它提供简单易用的应用部署、进程管理和日志查看功能，让你可以快速部署和管理多个应用程序。

### 为什么选择 dzjjy？

- 🚀 **快速部署** - 一条命令完成应用上传、解压、启动
- 🔄 **进程守护** - 自动重启崩溃的应用，保持服务稳定
- 📦 **多应用管理** - 同时管理多个应用，互不干扰
- 📝 **日志管理** - 实时查看应用日志，支持日志轮转
- 💾 **状态持久化** - 服务重启后自动恢复应用状态
- 🔐 **简单安全** - Token 认证保护你的服务
- 🌍 **跨平台** - 支持 Linux、macOS、Windows

### 适用场景

- 开发环境快速部署测试
- 多个微服务本地联调
- 简单的应用托管和进程管理
- CI/CD 流水线中的部署步骤

> ⚠️ **注意**：dzjjy 专为开发环境设计，不建议用于生产环境。

---

## 功能特性

### 核心功能

| 功能 | 说明 |
|------|------|
| ✅ **多应用管理** | 同时管理多个应用，独立工作目录和日志 |
| ✅ **进程守护** | 自动重启、崩溃恢复、可配置重启次数 |
| ✅ **文件部署** | 支持 ZIP/TAR/GZ 自动解压 |
| ✅ **日志管理** | 内存缓存 + 文件持久化 + 自动轮转 |
| ✅ **状态持久化** | 自动恢复、原子写入 |
| ✅ **Token 认证** | 简单安全的 Bearer Token 认证 |

### 支持的应用类型

- **exec** - 直接可执行文件（Go、Rust 等编译型语言）
- **runtime** - 需要运行时的应用（Python、Node.js、Java 等）

---

## 快速开始

### 安装

#### 方式 1: 下载预编译二进制文件（推荐）

从 [Releases](https://github.com/jiangfire/dzjjy/releases) 页面下载适合你平台的版本：

```bash
# Linux/macOS
tar -xzf dzjjy-server-linux-amd64.tar.gz
chmod +x dzjjy-server dzjjy-client

# Windows
# 解压 dzjjy-server-windows-amd64.tar.gz
```

#### 方式 2: 从源码编译

```bash
# 克隆仓库
git clone https://github.com/jiangfire/dzjjy.git
cd dzjjy

# 编译
make build

# 二进制文件位于 build/ 目录
```

### 启动服务端

```bash
# 基础启动
./dzjjy-server -token your-secret-token -port 8080

# 启用状态持久化（推荐）
./dzjjy-server -token your-secret-token -port 8080 -state ./state.json
```

**参数说明：**
- `-token` - 认证令牌（必需，请使用强密码）
- `-port` - 服务端口（默认 8080）
- `-state` - 状态文件路径（可选，启用后服务重启会自动恢复应用）

### 部署第一个应用

#### 示例 1: 部署 Python 应用

```bash
# 打包应用
zip -r myapp.zip app.py requirements.txt

# 部署
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -auto-restart \
  -max-restarts 5
```

#### 示例 2: 部署 Node.js 应用

```bash
# 打包应用
zip -r nodeapp.zip server.js package.json

# 部署
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file nodeapp.zip \
  -type runtime \
  -executable node \
  -entry server.js \
  -args "--port=3000" \
  -auto-restart
```

#### 示例 3: 部署 Go 可执行文件

```bash
# 编译应用
go build -o myapp main.go

# 部署
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -file myapp \
  -type exec \
  -executable ./myapp \
  -auto-restart
```

### 查看应用状态

```bash
# 查看所有应用
./dzjjy-client list -server http://localhost:8080 -token your-secret-token

# 查看特定应用状态
./dzjjy-client status -server http://localhost:8080 -token your-secret-token -app myapp

# 查看应用日志
./dzjjy-client logs -server http://localhost:8080 -token your-secret-token -app myapp -lines 100
```

---

## 使用指南

### 多应用管理

dzjjy 支持同时管理多个应用，每个应用有独立的工作目录和日志。

```bash
# 部署多个应用
./dzjjy-client deploy -app web-server -file web.zip ...
./dzjjy-client deploy -app api-server -file api.zip ...
./dzjjy-client deploy -app worker -file worker.zip ...

# 管理特定应用
./dzjjy-client status -app web-server
./dzjjy-client restart -app api-server
./dzjjy-client stop -app worker
./dzjjy-client logs -app web-server -lines 50

# 删除应用（停止并清理工作目录）
./dzjjy-client remove -app web-server
```

**向后兼容：** 不指定 `-app` 参数时，默认操作名为 `default` 的应用。

### 进程守护配置

```bash
# 无限重启（适合长期运行的服务）
./dzjjy-client deploy ... -auto-restart -max-restarts 0

# 限制重启次数（防止频繁失败）
./dzjjy-client deploy ... -auto-restart -max-restarts 10

# 不自动重启
./dzjjy-client deploy ... # 默认不启用自动重启
```

**重启策略：**
- 进程崩溃后等待 1 秒再重启（防止快速失败循环）
- 达到最大重启次数后停止自动重启
- 手动重启不计入重启次数

### 压缩文件部署

支持的格式：`.zip`、`.tar`、`.tar.gz`、`.gz`

```bash
# 自动检测并解压
./dzjjy-client deploy -file app.zip ...
./dzjjy-client deploy -file app.tar.gz ...
```

**工作流程：**
1. 上传压缩文件
2. 服务端自动检测格式
3. 解压到应用工作目录
4. 删除压缩包
5. 启动应用

### 工作目录结构

```
workspace/
├── web-server/      # web-server 应用的工作目录
│   ├── app.py
│   └── static/
├── api-server/      # api-server 应用的工作目录
│   ├── server.js
│   └── package.json
└── default/         # 默认应用的工作目录

logs/
├── web-server/      # web-server 的日志文件
├── api-server/      # api-server 的日志文件
└── default/         # 默认应用的日志文件

uploads/             # 上传文件的临时目录
```

### 状态持久化

启用状态持久化后，服务重启会自动恢复应用：

```bash
# 启动时指定状态文件
./dzjjy-server -token my-token -state ./state.json
```

**持久化内容：**
- 应用配置（类型、可执行文件、入口文件等）
- 自动重启设置
- 重启次数统计

**注意：** 应用文件必须仍在工作目录中才能恢复。

---

## API 接口

所有接口需要 `Authorization: Bearer <token>` 头（除 `/health`）。

### 多应用路由

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/apps` | 列出所有应用 |
| `POST` | `/api/v1/apps/{name}/deploy` | 部署应用 |
| `POST` | `/api/v1/apps/{name}/start` | 启动应用 |
| `POST` | `/api/v1/apps/{name}/stop` | 停止应用 |
| `POST` | `/api/v1/apps/{name}/restart` | 重启应用 |
| `GET` | `/api/v1/apps/{name}/status` | 查询状态 |
| `GET` | `/api/v1/apps/{name}/logs?lines=N` | 查询日志 |
| `POST` | `/api/v1/apps/{name}/remove` | 删除应用 |

### 单应用路由（向后兼容）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/deploy` | 部署到 default 应用 |
| `POST` | `/api/v1/start` | 启动 default 应用 |
| `POST` | `/api/v1/stop` | 停止 default 应用 |
| `POST` | `/api/v1/restart` | 重启 default 应用 |
| `GET` | `/api/v1/status` | 查询 default 应用状态 |
| `GET` | `/api/v1/logs?lines=N` | 查询 default 应用日志 |
| `GET` | `/health` | 健康检查（无需认证） |

### 请求示例

**部署应用（multipart/form-data）：**
```bash
curl -X POST http://localhost:8080/api/v1/apps/myapp/deploy \
  -H "Authorization: Bearer your-token" \
  -F "file=@app.zip" \
  -F "type=runtime" \
  -F "executable=python3" \
  -F "entry=app.py" \
  -F "auto_restart=true" \
  -F "max_restarts=10"
```

**启动应用（JSON）：**
```bash
curl -X POST http://localhost:8080/api/v1/apps/myapp/start \
  -H "Authorization: Bearer your-token" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "runtime",
    "executable": "python3",
    "entry": "app.py",
    "auto_restart": true,
    "max_restarts": 10
  }'
```

**查询状态：**
```bash
curl http://localhost:8080/api/v1/apps/myapp/status \
  -H "Authorization: Bearer your-token"
```

### 响应示例

**状态响应：**
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

**列出应用：**
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

---

## 故障排查

### 常见问题

**Q: 应用部署后无法启动？**

A: 检查以下几点：
1. 确认可执行文件路径正确（相对于工作目录）
2. 检查文件权限（Linux/macOS 需要执行权限）
3. 查看应用日志：`./dzjjy-client logs -app myapp -lines 100`
4. 确认运行时已安装（Python、Node.js 等）

**Q: 服务重启后应用没有自动恢复？**

A: 确保：
1. 启动服务端时使用了 `-state` 参数
2. 应用文件仍在工作目录中
3. 检查状态文件是否存在且可读

**Q: 应用频繁重启？**

A: 可能原因：
1. 应用启动失败（检查日志）
2. 端口冲突
3. 依赖缺失
4. 配置错误

建议：设置合理的 `max_restarts` 值，避免无限重启。

**Q: 无法连接到服务端？**

A: 检查：
1. 服务端是否正在运行
2. 端口是否正确
3. 防火墙设置
4. Token 是否正确

**Q: 日志文件过大？**

A: dzjjy 支持日志自动轮转：
- 内存仅保留最近 1000 行
- 文件日志会自动轮转（可配置）

---

## 文档

| 文档 | 说明 |
|------|------|
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 架构设计、模块结构、核心概念 |
| [TESTING.md](docs/TESTING.md) | 测试指南、覆盖率、最佳实践 |
| [DEVELOPMENT.md](docs/DEVELOPMENT.md) | 开发指南、构建命令、设计原则 |
| [PLAN.md](docs/PLAN.md) | 项目计划、功能规划、优先级 |
| [RELEASE.md](docs/RELEASE.md) | 发布流程、版本管理 |

---

## 开发

### 构建

```bash
# 安装依赖
make deps

# 编译
make build

# 运行测试
make test

# 代码检查
make lint

# 格式化代码
make fmt
```

### 发布

```bash
# 构建所有平台的发布包
make release

# 生成校验和
make checksum

# 打包
make package
```

详细发布流程请参考 [docs/RELEASE.md](docs/RELEASE.md)。

---

## 注意事项

- ⚠️ **仅用于开发环境**，不适合生产环境
- 🔐 Token 认证简单，请妥善保管，不要使用弱密码
- 💾 部署新版本会停止旧服务，请注意保存数据
- 📁 上传文件会覆盖工作目录中的同名文件
- 🔄 建议设置合理的 `max_restarts` 值，避免无限重启
- ⏱️ 进程崩溃后等待 1 秒再重启
- 🗑️ 日志支持自动轮转，内存仅保留最近 1000 行

---

## 许可证

本项目采用 **GNU Affero General Public License v3 (AGPL-3.0)** 协议。

**重要说明：**
- 如果您修改本项目并在网络服务中使用，必须开源您的修改版本
- 任何基于本项目的衍生作品也必须使用 AGPL-3.0 协议
- 详细条款请查看 [LICENSE](LICENSE) 文件

---

## 贡献

欢迎提交 Issue 和 Pull Request！

在提交 PR 前，请确保：
- 代码通过所有测试：`make test`
- 代码格式正确：`make fmt`
- 代码通过检查：`make lint`

---

## 致谢

感谢所有贡献者和使用者！

---

<div align="center">

**[⬆ 回到顶部](#dzjjy---简易部署服务)**

Made with ❤️ by [jiangfire](https://github.com/jiangfire)

</div>
