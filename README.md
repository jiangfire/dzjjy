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
- 闲置暂停：`lifecycle.idleTimeout` 到期自动挂起，下次请求自动唤醒。
- 临时 URL：`exposure` 用不可猜测的 token 路径发布静态站点。
- 容器能力：端口映射、Secret 注入、命名卷、资源配额（内存/CPU/PIDs）。
- 沙箱隔离：可选 gVisor `runsc`，不可用时明确失败而不静默降级。
- Kubernetes 导入：`import-k8s` 把 Deployment/Service 转成 `dzjjy/v1`。

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

- `deploy`（legacy multipart 部署）
- `apply`（声明式 YAML 部署）
- `validate`（本地校验 YAML，不连接服务端）
- `import-k8s`（Kubernetes Deployment/Service 转 `dzjjy/v1`）
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

### 声明式配置（apply / validate）

`apply` 和 `validate` 使用严格的 `dzjjy/v1` YAML。配置中的 `command` 必须是参数数组，避免 shell 字符串拆分导致参数边界变化：

```yaml
apiVersion: dzjjy/v1
kind: App
metadata:
  name: demo-python
spec:
  artifact: app.zip
  runtime: {type: python, version: "3.12"}
  command: [python, app.py, "--port", "9000"]
  sandbox: {type: docker}
  lifecycle: {ttl: 2h}
```

```bash
./build/dzjjy-client validate -file app.yaml
./build/dzjjy-client apply -server http://localhost:8080 -token your-secret-token -file app.yaml
```

artifact 路径相对于 YAML 文件。`StaticSite` 配置可使用 `spec.port`（`0` 表示自动分配），静态站点支持 SPA fallback。TTL 到期后应用进入 expired 状态。

Docker sandbox 只接受平台维护的 Python 3.12、Node 22、Go 1.24 runtime template；需要 Linux Docker daemon。任意镜像、Dockerfile、Docker 参数、挂载和宿主路径均不开放，Docker 不可用时会明确失败，不会回退到宿主机进程。

**安全边界：** `sandbox: {type: process}` 直接以服务端用户权限运行，并非安全隔离或恶意代码沙箱。请只用于可信应用；需要隔离时选择受控 Docker template 或 `sandbox.isolator: runsc`。

### Phase 3 能力

```yaml
apiVersion: dzjjy/v1
kind: App
metadata:
  name: demo-api
spec:
  artifact: app.tar.gz
  runtime: {type: python, version: "3.12"}
  command: [python, "app.py", "--port", "9000"]
  sandbox: {type: docker, isolator: runsc}   # 可选 gVisor 隔离层
  network:
    ports:
      - {container: 9000}                    # host 省略时由 Docker 分配
  secrets:
    - {key: "web/api-key", env: API_KEY}     # 值来自服务端环境，不写进清单
  volumes:
    - {name: demo-cache, path: /cache}       # 仅命名卷，不接受宿主路径
  resources:
    memory: 512m
    cpus: "1.5"
    pids: 256
  lifecycle:
    ttl: 24h
    idleTimeout: 30m                         # 空闲 30 分钟后挂起，下次请求唤醒
```

`StaticSite` 可用 `spec.exposure` 发布到不可猜测的 token 路径：

```yaml
apiVersion: dzjjy/v1
kind: StaticSite
metadata:
  name: demo-site
spec:
  artifact: dist.zip
  port: 0                 # 自动分配
  exposure: {}            # 生成随机 token；也可用 exposure.token 固定
  lifecycle:
    idleTimeout: 10m
```

apply 成功后 `data.public_path` 返回形如 `/_dzjjy/<token>/` 的公开路径。token 会保存在状态文件中，服务重启后继续有效。

Secret 只声明键名，值由服务端进程环境提供；缺失的键会让 apply 明确失败，不会静默启动缺少变量的容器。

Kubernetes 导入：

```bash
./build/dzjjy-client import-k8s -file k8s/deployment.yaml -out app.yaml
```

转换器只接受平台维护的 runtime 镜像与单容器 Pod；无法表示的配置直接报错，可降级的（多副本、`envFrom`、Secret 卷等）会输出 note 说明。

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

当前 GitHub Actions 基线使用 Go `1.26.9` 执行 CI / Release / Quality workflow。

相关文档：

- `docs/ARCHITECTURE.md`
- `docs/DEVELOPMENT.md`
- `docs/TESTING.md`
- `docs/RELEASE.md`

## 注意事项

- 本项目优先面向开发/测试环境，不是完整生产级 PaaS。
- 请使用强 Token，避免将服务直接暴露到公网。
- 部署会覆盖对应应用目录，建议提前备份关键文件。
- 开启 `-state` 时请保证状态文件路径可写。

## 许可证

本项目使用 [AGPL-3.0](LICENSE) 许可证。
