# 架构设计

> 文档更新：2026-10-10
> 说明：同步 Phase 0-3（声明式运行时 + 闲置暂停、临时 URL、网络映射、Secret、volume、资源配额、gVisor、Kubernetes 导入）后的模块与安全边界

## 核心模块

```
internal/
├── server/
│   ├── runtime/          # 进程管理 + 日志系统
│   │   ├── runtime.go        # Manager：单应用进程生命周期管理（含进程树回收与身份校验）
│   │   ├── multimanager.go   # 多应用管理器（包装 Manager，per-app 生命周期锁）
│   │   ├── logger.go         # 双重存储（内存+文件 + 轮转）
│   │   ├── identity_*.go     # 进程启动身份（Linux /proc 起始时间；Windows 父 PID）
│   │   └── proctree_*.go     # 跨平台进程树终止（Linux 进程组；Windows taskkill /T）
│   ├── appconfig/        # dzjjy/v1 YAML 严格解析（App/StaticSite、未知字段拒绝）
│   ├── archive/          # 压缩包处理
│   │   └── archive.go        # ZIP/TAR/GZ 解压（单文件/总量/文件数上限，超限报错）
│   ├── staticserve/      # StaticSite 独立端口的静态文件服务（路径穿越防护、SPA fallback）
│   ├── dockerbackend/    # Docker runtime template 注册表与容器生命周期（受控标签与挂载）
│   │   ├── engine.go        # 模板解析、容器创建/停止/删除/恢复
│   │   └── options.go       # Phase 3：端口映射、Secret 注入、volume、配额、runsc 探测、暂停/恢复
│   ├── secretstore/     # 平台托管 Secret（值只来自进程环境，AES-GCM 加密落盘，永不进状态/API）
│   ├── idle/            # 闲置调度器（按 HTTP 空闲时长挂起，挂起可逆）
│   ├── proxy/           # 临时 URL 代理（不可猜测 token 路径，唤醒后转发）
│   ├── k8simport/       # Kubernetes Deployment/Service → dzjjy/v1 转换器
│   ├── ttl/              # 绝对 TTL 调度器（按 expires_at 排序，generation 校验）
│   ├── auth/             # 认证
│   │   └── auth.go           # Bearer Token + 时序攻击防护
│   ├── handler/          # HTTP API
│   │   ├── handler.go        # 部署/状态/日志等端点与状态恢复
│   │   ├── apply*.go         # 声明式 apply/reconcile 与各 backend 生命周期
│   │   └── phase3.go         # Phase 3 接线：选项翻译、挂起/恢复、token 暴露
│   └── state/            # 状态持久化
│       ├── persist.go        # 原子写入 + SHA256
│       ├── sync.go           # 事件驱动同步（含终止原因与进程身份）
│       └── restore.go        # 启动恢复（终态保留、PID 身份校验）
├── k8simport/         # Kubernetes 清单导入（Deployment/Service → dzjjy/v1）
└── client/
    └── deploy/           # 客户端部署
        ├── deploy.go         # API 调用封装（legacy multipart）
        └── apply.go          # dzjjy-client apply/validate 的 YAML 提交与结果透出
```

## 核心特性

| 模块 | 关键特性 |
|------|----------|
| **Runtime** | 进程生命周期管理、自动重启（1秒延迟）、并发安全（sync.RWMutex）、上下文优雅关闭 |
| **Logger** | 内存环形缓冲区（1000行）、文件持久化、自动轮转、三种日志类型（stdout/stderr/system） |
| **Archive** | ZIP/TAR/GZ 支持、路径遍历防护、大小限制（100MB）、格式自动检测 |
| **Auth** | Bearer Token 认证、时序攻击防护、所有端点（除 /health）需认证 |
| **State** | 原子写入、SHA256 校验、自动备份、事件驱动同步（100ms 延迟） |
| **Handler** | 部署流程自动化、文件上传验证、多应用支持（通过路径参数） |

## 状态持久化

### 数据流
```
runtime.Manager → EventNotifier → SyncManager → StateStore → state.json
                                      ↑
                              RestoreManager (启动时)
```

### 核心机制

| 机制 | 说明 |
|------|------|
| **原子写入** | 临时文件 + 重命名，防止写入中断损坏 |
| **校验和** | SHA256 验证数据完整性，损坏时自动从备份恢复 |
| **自动备份** | 覆盖主状态文件前创建 `state.json.backup.YYYYMMDD-HHMMSS` |
| **事件驱动** | 100ms 延迟批量写入，减少 IO 开销 |
| **进程恢复** | Windows: `tasklist` 检查行数 > 1；Unix: `syscall.Signal(0)` |
| **状态清理** | 自动移除无效 PID，重置状态 |

### 恢复流程（服务器启动时）

1. **加载状态文件** → 验证校验和 → 尝试备份恢复
2. **恢复应用状态** → 检查 PID → 标记运行中/待重启/已停止
3. **清理无效状态** → 移除不存在的 PID
4. **同步到内存** → 通过事件更新 SyncManager
5. **恢复运行态** → 已恢复的应用重新出现在 `status/list/remove` 能力中
6. **自动重启** → 对标记为 `AutoRestart` 的已停止应用执行重启

### 线程安全
- `StateStore`: `sync.RWMutex` 保护文件操作
- `SyncManager`: `sync.RWMutex` 保护内存状态
- 事件队列：带缓冲 channel (100 事件)

## 并发模式

### 进程监控
```go
func (m *Manager) monitor() {
    for {
        select {
        case <-m.ctx.Done():  // 优雅关闭信号
            return
        default:
            m.cmd.Wait()  // 阻塞直到进程退出
            // 检查重启限制，延迟，然后重启
        }
    }
}
```

### 线程安全
- 所有 Manager 状态由 `sync.RWMutex` 保护
- 读操作（GetPID, IsRunning, GetInfo）使用 `RLock()`
- 写操作（Start, Stop, Restart）使用 `Lock()`
- MultiManager 通过 `map[string]*Manager` 隔离应用

## 日志系统

### 架构
- **双重存储**：内存环形缓冲区（1000行）+ 文件持久化
- **自动轮转**：文件超过指定大小时自动分割
- **保留策略**：可配置保留文件数量（默认 10 个）

### 日志类型
| 类型 | 说明 | 文件命名 |
|------|------|----------|
| `stdout` | 标准输出 | `stdout-{executable}-{timestamp}.log` |
| `stderr` | 标准错误 | `stderr-{executable}-{timestamp}.log` |
| `system` | 系统事件 | `system-{executable}-{timestamp}.log` |

### 轮转示例
```
app-20251231-150405.rotated.001.log
app-20251231-150405.rotated.002.log
...
```

### 捕获方式
```go
cmd.StdoutPipe()  // 捕获 stdout
cmd.StderrPipe()  // 捕获 stderr
```

## 安全设计

### 路径遍历防护
```go
func safeExtractPath(path, dest string) error {
    // 1. 转换为绝对路径
    // 2. 必须在目标目录内
    // 3. 不能包含 ".."
    // 4. 阻止危险前缀（/etc/, C:\Windows\ 等）
}
```

### 命令注入防护
```go
func isValidExecutable(path string) bool {
    // 1. 阻止路径遍历（..）
    // 2. 相对路径安全（转换为绝对路径后验证）
    // 3. 阻止系统敏感目录
    // 4. 阻止路径遍历变种
}
```

### 其他安全机制
- **认证**：Bearer Token + 时序攻击防护（恒定时间比较）
- **解压限制**：100MB 大小限制，防止解压炸弹
- **文件权限**：可执行文件自动设置 0755 权限
- **错误处理**：所有关键路径都有错误处理和日志记录

## 关键约束

| 约束 | 说明 |
|------|------|
| **多应用** | MultiManager 支持同时管理多个应用 |
| **开发环境** | 不适合生产使用 |
| **工作目录** | 每次部署清空（按应用隔离） |
| **上传目录** | 保留每个应用最近一次上传的原始文件，与工作目录分离 |
| **日志保留** | 内存 1000 行，文件支持轮转 |
| **重启延迟** | 固定 1 秒，防止快速失败循环 |
| **提取方式** | 文件直接解压到应用工作目录根部 |

## 声明式应用与运行后端（Phase 0–2）

`dzjjy/v1` 配置由 `appconfig` 严格解析，拒绝未知字段及多个 YAML 文档。`App` 使用 `spec.command` 参数数组；`StaticSite` 使用独立 HTTP listener，可指定端口或设置 `0` 自动分配，并提供 SPA fallback。`dzjjy-client validate -file` 仅校验；`apply -file` 上传 YAML 和相对 YAML 路径解析的 artifact。

Apply 先在独立 staging 目录解包和校验，再激活 artifact。相同 desired config 重复 apply 保持 generation 与运行实例；运行时配置变化返回冲突，需先停止再 apply。应用状态保存 backend、desired config、generation、端口/容器 ID 和绝对过期时间；TTL callback 校验 generation，服务重启时按剩余 TTL 重建调度。

`process` backend 直接以服务端用户权限启动进程，**不是安全沙箱**，只适用于可信代码。`docker` backend 仅允许平台维护的 Python 3.12、Node 22、Go 1.24 templates，要求 Linux Docker daemon；workspace 只读挂载且禁止 Docker socket、任意镜像、Dockerfile、自定义 Docker 参数与宿主路径。Docker 不可用时 apply 明确失败，不降级为 process。

Phase 3（闲置暂停、临时 URL、网络/端口映射、Secret、volume、资源配额、gVisor/runsc、Kubernetes 导入）已在下文「Phase 3 能力」中实现。Phase 4 未定义。

进程状态恢复带 PID 身份校验：启动时持久化进程身份（Linux 为 `/proc` 起始时间，Windows 为父 PID），恢复时比对；不一致说明 PID 已被系统复用，绝不接管。身份缺失（legacy 状态）或平台无法确认时以"未验证"身份接管——状态可见但 stop 不执行 kill。进程树回收跨平台提供：Linux 下子进程放入独立进程组，stop 时整组 SIGKILL；Windows 下使用 `taskkill /T /F` 终止进程树。崩溃与重启耗尽会发出终态事件：状态转为 `failed`、清除 PID 并记录终止原因（`crash` / `max_restart` / `restart_failure`），与 `manual_stop` / `ttl` / `cleanup_failure` 区分。手动 restart 创建新 generation 并从新实例启动时重算 TTL；服务重启后按持久化的绝对 `expires_at` 重建调度，停机期间到期的实例直接清理并标记 `expired`，`expired` 是终态，恢复与调和都不会复活它。

## Phase 3 能力

### 闲置暂停（idle suspend）

`spec.lifecycle.idleTimeout` 声明一个空闲窗口。`internal/server/idle` 维护按 deadline 排序的应用队列，超过窗口未收到流量即调用 `suspendIdleApp`：

- `StaticSite`：关闭 listener，**保留站点目录**（挂起不是删除）
- Docker：`docker pause` 冻结容器，不删除不重建

挂起后状态转为 `stopped` + `suspended=true`，终止原因记为 `idle`。挂起**可逆**：`suspended` 不是终态，下一次请求经代理触发 `resumeSuspendedApp` 时重新监听端口或 `docker unpause`，并按原 generation 继续服务。

调度器按固定 `pollInterval`（50ms）重新读取时钟而不是睡到 deadline——这样注入的测试时钟和宿主机时钟跳变都能被正确感知。

### 临时 URL（token 暴露）

`spec.exposure` 让 StaticSite 通过平台代理对外提供：站点内容挂在 `/_dzjjy/<token>/` 路径下，token 默认由 `proxy.NewToken()` 用 192 位加密随机数生成，也可由 `spec.exposure.token` 固定。代理只做路径改写后转发给站点自己的 traversal-safe handler，站点文件结构不变。

token 存放在状态文件中（`exposure_token`），服务重启后自动重新注册；stop/TTL/remove 会注销 token。**token 会出现在访问日志中**，这是该方案的已知代价。

### 网络与端口映射

`spec.network.ports[]` 声明 `container` / `host` 端口对。`host` 省略或为 `0` 时由 Docker 分配临时端口，dzjjy 通过 `docker port` 反查真实绑定并写入 `published_ports`，使状态和 API 返回的是**实际**端口而非请求值。仍只允许平台维护的 bridge 网络，不开放自定义网络、host 路径挂载或 Docker socket。

### Secret

`spec.secrets[]` 只声明**键名**，值由 `internal/server/secretstore` 从服务端进程环境解析（AES-GCM 加密后可落盘，密钥由 operator 另行提供）。约束：

- Secret 值**永不**写入 desired config、状态文件或任何 API 响应
- 缺失的键会让 apply 直接失败，不会静默启动一个没有该变量的容器
- `spec.secrets[].env` 未指定时从键名派生合法环境变量名

### Volume

`spec.volumes[]` 只接受**命名卷**，由 dzjjy 打上自有标签后创建，容器以只读 workspace + 指定卷挂载启动。宿主路径挂载依然不开放。Kubernetes 导入只会把 `emptyDir` 翻译成命名卷，`secret`/`configMap` 卷会被记录为导入 note 并跳过。

### 资源配额

`spec.resources` 支持 `memory`、`memorySwap`、`cpus`、`pids`，映射到 Docker 的 `--memory` / `--memory-swap` / `--cpus` / `--pids-limit`。内存下限 6m，CPU 上限 64。StaticSite 不接受任何资源字段。

### gVisor / runsc 隔离

`spec.sandbox.isolator: runsc` 在 Docker 后端之上叠加 gVisor 隔离层：

- apply 前用 `docker info --format '{{json .Runtimes}}'` 探测 runsc 是否已注册
- **不可用时 apply 明确失败（HTTP 501），绝不降级为默认 runtime**
- 容器以 `--runtime runsc` 创建，并保留 dzjjy 自有标签，`stop/pause/unpause` 全部走同样的所有权校验

CI 的 `gVisor E2E (runsc)` job 会下载 runsc、写入 `/etc/docker/daemon.json`、重启 daemon，再用真实容器验证运行时的确实是 runsc 并跑通完整生命周期。

### Kubernetes 导入

`dzjjy-client import-k8s -file manifest.yaml` 把 Deployment/Service 转换成 `dzjjy/v1`，产出的清单会先经过 dzjjy 自己的严格解析器校验才输出。设计原则是**不静默降级**：

- 任意镜像、多容器 Pod、无法表示的 volume 源直接报错
- replicas>1、`envFrom`、Secret/ConfigMap 卷、NodePort 之外的 Service 类型等记为 note，明确告知被降级
- 资源配额按 Kubernetes 语义归一（`256Mi` → `256m`、`500m` CPU → `0.5`）
- `secretKeyRef` 翻译成 `spec.secrets[]` 引用而非明文

导入是**单向转换**，不连接集群；集群侧连通性由可选的验证 job 覆盖。
