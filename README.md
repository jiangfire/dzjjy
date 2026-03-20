# dzjjy

[中文](README.md) | [English](README.en.md)

[![CI](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml)
[![Quality](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)

一个面向开发/测试环境的轻量级部署与进程管理工具。

`dzjjy` 可以把“上传应用 -> 启动进程 -> 查看状态/日志 -> 重启/停止”整合成一套简单的服务端 + 客户端工作流，支持多应用管理。

## 目录

- [项目定位](#项目定位)
- [核心特性](#核心特性)
- [快速开始](#快速开始)
- [使用示例](#使用示例)
- [API 概览](#api-概览)
- [开发与测试](#开发与测试)
- [注意事项](#注意事项)
- [许可证](#许可证)

## 项目定位

适用场景：

- 本地开发环境托管多个进程
- 微服务联调时统一管理应用生命周期
- CI/CD 或测试环境中的快速部署步骤

项目目标：

- 保持使用成本低（命令简单、无复杂依赖）
- 多应用可管理（独立工作目录、独立状态、独立日志）
- 对崩溃场景具备基础恢复能力（自动重启 + 状态持久化）

## 核心特性

- 多应用管理：按应用名部署与操作，不同应用互不干扰。
- 部署方式灵活：支持单文件或压缩包（`.zip`、`.tar`、`.tar.gz`、`.gz`）。
- 进程守护：支持 `auto-restart` 和最大重启次数限制。
- 日志查看：按应用查询日志，支持 `lines` 参数，客户端支持 `--follow` 轮询跟随。
- 状态持久化：可通过 `-state` 启用状态文件，服务重启后恢复应用元数据，并按配置自动重启。
- 基础安全控制：Bearer Token 认证、路径/输入校验。

## 快速开始

### 1. 构建

```bash
git clone https://github.com/jiangfire/dzjjy.git
cd dzjjy
make deps
make build
```

构建产物：

- `build/dzjjy-server`
- `build/dzjjy-client`

Windows 下对应为 `.exe` 可执行文件。

### 2. 启动服务端

```bash
./build/dzjjy-server \
  -token your-secret-token \
  -port 8080 \
  -upload ./uploads \
  -work ./workspace \
  -log ./logs \
  -state ./state.json
```

参数说明：

- `-token`：认证令牌（必填）
- `-port`：服务端口（默认 `8080`）
- `-upload`：上传暂存目录（默认 `./uploads`，保留每个应用最近一次上传的原始文件）
- `-work`：工作目录（默认 `./workspace`）
- `-log`：日志目录（默认 `./logs`）
- `-state`：状态文件（可选，建议开启）

健康检查：

```text
GET /health
```

### 3. 客户端命令

```bash
./build/dzjjy-client <command> [options]
```

支持命令：

- `deploy`
- `start`
- `stop`
- `restart`
- `status`
- `logs`
- `list`
- `remove`

## 使用示例

### 部署 runtime 类型应用（例如 Python）

```bash
./build/dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -app demo-python \
  -file app.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -args "--port 9000" \
  -auto-restart \
  -max-restarts 5
```

### 部署 exec 类型应用（例如 Go 二进制）

```bash
./build/dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -app demo-go \
  -file demo-go.exe \
  -type exec \
  -executable demo-go.exe \
  -auto-restart
```

### 常用管理命令

```bash
# 列出所有应用
./build/dzjjy-client list -server http://localhost:8080 -token your-secret-token

# 查看状态
./build/dzjjy-client status -server http://localhost:8080 -token your-secret-token -app demo-python

# 查看最近 200 行日志
./build/dzjjy-client logs -server http://localhost:8080 -token your-secret-token -app demo-python -lines 200

# 重启应用
./build/dzjjy-client restart -server http://localhost:8080 -token your-secret-token -app demo-python

# 停止应用
./build/dzjjy-client stop -server http://localhost:8080 -token your-secret-token -app demo-python
```

说明：不传 `-app` 时，默认应用名为 `default`。

## API 概览

认证头：

```text
Authorization: Bearer <token>
```

多应用接口：

| Method | Path | 说明 |
| --- | --- | --- |
| GET | `/api/v1/apps` | 列出应用 |
| POST | `/api/v1/apps/{name}/deploy` | 部署应用 |
| POST | `/api/v1/apps/{name}/start` | 启动应用 |
| POST | `/api/v1/apps/{name}/stop` | 停止应用 |
| POST | `/api/v1/apps/{name}/restart` | 重启应用 |
| GET | `/api/v1/apps/{name}/status` | 查看状态 |
| GET | `/api/v1/apps/{name}/logs?lines=N` | 查看日志 |
| DELETE | `/api/v1/apps/{name}/remove` | 删除应用 |

兼容删除路径：

- `DELETE /api/v1/apps/{name}`

兼容单应用接口（`default`）：

- `POST /api/v1/deploy`
- `POST /api/v1/start`
- `POST /api/v1/stop`
- `POST /api/v1/restart`
- `GET /api/v1/status`
- `GET /api/v1/logs`

## 开发与测试

```bash
make deps        # 依赖管理
make build       # 构建
make test        # 单元测试
make lint        # 静态检查
make fmt         # 代码格式化
make ci          # 本地 CI 流程
```

相关文档：

- `docs/ARCHITECTURE.md`
- `docs/DEVELOPMENT.md`
- `docs/TESTING.md`
- `docs/PLAN.md`
- `docs/RELEASE.md`

## 注意事项

- 本项目优先面向开发/测试环境，不是完整生产级 PaaS。
- 请使用强 Token，避免将服务直接暴露到公网。
- 部署会覆盖对应应用目录，建议提前备份关键文件。
- 开启 `-state` 时请保证状态文件路径可写。

## 许可证

本项目使用 [AGPL-3.0](LICENSE) 许可证。
