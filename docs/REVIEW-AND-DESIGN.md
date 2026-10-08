# 项目审查与 YAML / Docker 沙箱设计

> 审查范围：当前代码、测试、CI 和项目文档。
> 更新日期：2026-03-22
> 状态：设计稿，尚未实现新功能。

## 1. 结论

当前 dzjjy 是一个 Go 写的轻量部署与宿主机进程管理工具。结合本轮确认和 Hindsight 中此前的沙盒产品讨论，建议将目标收敛为：

- **单机声明式运行管理**：一份 YAML 描述一个应用的期望状态，风格类似 K8s，但不依赖 Kubernetes、不支持多机调度。
- **轻量调和**：服务端根据配置和实际状态启动、停止或更新应用；不做 etcd、控制器框架和复杂资源对象。
- **双执行方式**：默认 `process`；需要隔离时可选 `docker`。
- **平台维护 Docker runtime template**：用户上传应用制品并选择受支持的模板，不要求用户编写 Dockerfile 或直接传任意 Docker 参数。
- **绝对 TTL**：实例启动成功后开始倒计时，到期后停止、清理并标记过期，不被自动重启或调和循环复活。
- **平台范围**：Linux 和 Windows。Docker 模式依赖目标机可用的 Docker Engine/Desktop；具体容器 OS 由 Docker daemon 决定。

不做 Kubernetes API/集群兼容。也不把当前项目改造成通用 PaaS：暂不加 Docker 网络规则、容器端口映射、Secret、volume 配置、资源配额；静态站点只使用 dzjjy 分配的独立宿主机端口。

### 设计来源

Hindsight 中相关记录属于先前的 Celaeno 产品讨论，不是 dzjjy 已有代码或已确认需求；本设计复用其中与本项目目标一致的产品原则：

- 单机“小 K8s”的声明式期望状态调和，不上 Kubernetes/etcd。
- 用标准 Docker 容器，保留未来切换 gVisor/runsc 的后路；不做云桌面/浏览器桌面形态。
- runtime template 由平台维护，用户代码套模板运行，不要求用户写 Dockerfile。
- 曾讨论容器闲置时释放 CPU/内存、保留存储，以及每次启动分配临时 URL。

静态站点需要独立宿主机监听端口；当前不做 Docker 网络规则、容器端口映射或临时 URL。闲置休眠依赖流量/唤醒入口，也不等同于 TTL，作为后续独立功能保留。

## 2. 当前实现和边界

### 已有能力

- 多应用管理，服务端和客户端均为 Go。
- 上传单文件、ZIP、TAR、TAR.GZ、GZ。
- `exec` 与 `runtime` 两种进程启动方式。
- 自动重启、最大重启次数、日志捕获与轮转。
- Bearer Token 认证。
- 可选状态持久化、校验和、备份和启动恢复。
- 多应用 API 和 `default` 单应用兼容 API。

当前启动链路是上传文件、重建工作目录，然后由 `os/exec` 在宿主机直接启动。它不提供文件系统、网络、CPU/内存或 syscall 隔离；process 模式只能用于可信代码。Docker 模式才作为可选隔离执行后端。

当前 `ProcessConfig.Args` 是一个字符串，运行时用 `strings.Fields` 拆分。这会破坏带空格、引号和转义的参数。新 YAML 必须使用参数数组；旧 API 保持兼容。

## 3. 审查发现

### P1：运行时与状态

#### 3.1 `Manager.Start` 非法类型分支不释放锁

位置：`internal/server/runtime/runtime.go:197-208`

获取互斥锁后，非法 `appType` 分支直接返回，没有 `Unlock`。HTTP 层会先校验，所以现有测试没覆盖；直接调用运行时 API 后可能永久阻塞。

建议：修复锁释放路径，并增加非法类型调用后仍可查询状态的回归测试。

#### 3.2 恢复进程只按 PID 识别

位置：`internal/server/state/restore.go`、`internal/server/runtime/runtime.go`

PID 被系统复用时，服务可能把别的进程识别成自己的应用；恢复的进程也没有重新建立完整监控。

建议：保存并按平台校验进程启动身份、命令行和工作目录；不能确认时标为 stale/unknown，不允许自动 kill。

#### 3.3 重启失败或达到上限后持久化状态可能仍是 running

位置：`internal/server/runtime/runtime.go:415-457`

运行时本地状态设为停止，但没有可靠地发终态事件，状态文件可能保留旧 PID 和 running 状态。

建议：统一 `running/stopped/failed/expired` 转换，终态清除 PID 并持久化。

#### 3.4 stop 不保证子进程树退出

位置：`internal/server/runtime/runtime.go:501-564`

当前只杀主进程。process 模式需要 Linux 进程组和 Windows Job Object 等平台实现；Docker 模式以容器为生命周期回收单位。

#### 3.5 缺少优雅关闭

位置：`cmd/server/main.go:96-108`

服务没有 SIGINT/SIGTERM shutdown 流程，不能确保停止实例、清理容器和 flush 状态。

### P1/P2：输入、并发和接口

#### 3.6 归档解压可能静默截断

位置：`internal/server/archive/archive.go`

当前单条目 `io.LimitReader` 没有检查超限，也没有归档总展开大小和文件数量限制。超限文件可能截断后仍被视为成功。

建议：超限立即报错，设置单文件、总展开大小、文件数和上传大小上限。

#### 3.7 同一 app 生命周期操作没有串行化

deploy、start、stop、restart、remove 并发时可能互相删除工作目录或覆盖状态。新增 reconcile 和 TTL 后冲突面会扩大。

建议：按 app name 串行化部署和生命周期变更；调和操作应可重入、幂等。

#### 3.8 命令参数解析依赖 `strings.Fields`

位置：`internal/server/runtime/runtime.go:305-340`

YAML 使用 `args: []string`，内部运行配置也应保留参数数组；旧字符串接口只作为兼容入口。

#### 3.9 其他接口/维护问题

- `/api/v1/apps` 与 `/api/v1/apps/` 路由需统一并增加真实 HTTP 测试。
- 不存在应用、状态冲突等错误有大量 500，应区分 400/404/409/500。
- `start` JSON body 未统一限制大小。
- 客户端 URL 直接拼接 app name。
- `go.mod` 声明 Go 1.24，而 CI 使用 Go 1.25.8，最低支持策略需要明确。
- `docs/ARCHITECTURE.md` 应明确当前 process 不是安全沙箱。

### 验证基线

- `go test ./...`：通过。
- `go vet ./...`：通过。
- `git diff --check`：通过。
- 本机 `go test -race ./...` 无法运行：环境 `CGO_ENABLED=0`，race detector 需要 cgo。

## 4. YAML：单应用声明与轻量调和

### 4.1 配置示例

一份文件描述一个应用，不引入 ApplicationSet、Service、Ingress 等 K8s 资源：

```yaml
apiVersion: dzjjy/v1
kind: App
metadata:
  name: demo
spec:
  artifact: ./app.zip
  runtime:
    type: python
    version: "3.12"
  command: [python, app.py, --port, "9000"]
  restart: on-failure
  sandbox:
    type: docker
  lifecycle:
    ttl: 30m
```

该配置表示一个需要运行时的应用；Python、Node.js、Go 是第一批一等 runtime template。静态网页使用单独的 `StaticSite` kind，不声明 runtime，也不启动应用进程：

```yaml
apiVersion: dzjjy/v1
kind: StaticSite
metadata:
  name: docs
spec:
  artifact: ./site.zip
  port: 0
  lifecycle:
    ttl: 30m
```

`artifact` 可以是静态文件目录的归档，默认站点入口为 `index.html`。静态站点由 dzjjy 内置 HTTP 文件服务托管，每个站点独占一个监听端口；`port: 0` 建议表示由系统分配空闲端口，部署响应和 status 返回实际端口。静态站点不创建进程/容器，但 TTL 到期或 remove 时关闭 listener 并清理站点目录。

静态文件服务必须限制在该站点根目录内，拒绝路径穿越，不列目录；首版可将 SPA 未命中文件回退到 `index.html`。静态站点端口是宿主机 listener 端口，不是 Docker 容器端口映射。listener 复用 dzjjy 服务端 bind 地址；当前服务默认监听 `:8080`，即所有网卡。静态站点内容通过普通 HTTP 提供，不受 dzjjy API 的 Bearer Token 保护；只能部署允许公开访问的文件，并应使用主机防火墙限制不需要的外部访问。

不需要隔离时可选 process runtime；Docker 模式中的 `runtime` 是平台维护的模板标识，不是用户任意 Docker image，也不要求用户提交 Dockerfile。模板负责选择受支持的基础镜像、入口和运行环境；制品通过 dzjjy 受控方式放入运行环境。

如果用户部署的是已打包好的本机二进制，可省略 runtime template，直接声明可执行文件和参数。建议字段维持最少：

| 字段 | 含义 |
| --- | --- |
| `kind` | 固定为 `App` 或 `StaticSite` |
| `metadata.name` | 现有 app name |
| `spec.artifact` | 可选制品路径/上传内容 |
| `spec.runtime` | 可选平台模板；首批为 Python、Node.js、Go |
| `spec.command` | `App` 的字符串数组，保持参数边界 |
| `spec.restart` | `never` 或 `on-failure`，映射现有重启能力 |
| `spec.sandbox.type` | 默认 `process`，或 `docker` |
| `spec.port` | `StaticSite` 可选固定宿主机端口；省略或 `0` 自动分配，冲突时报错 |
| `spec.lifecycle.ttl` | 可选绝对 TTL，如 `30m` |

首版不支持用户自定义 Dockerfile、任意 Docker create 参数、环境变量/Secret、volume、容器网络配置和资源限制。未知字段应报错，避免无效字段被静默忽略。
### 4.2 声明式行为

YAML 表达期望状态；服务端保存规范化配置与运行状态。应用 `apply` 或服务启动恢复时，轻量调和器比较期望与实际状态并执行必要动作：

| 期望配置 | 实际状态 | 调和行为 |
| --- | --- | --- |
| desired running | 不存在 | 创建并启动实例 |
| desired running | 配置未变且实例运行 | 不操作 |
| desired running | 配置变化 | 停止旧 generation，部署新 generation |
| `StaticSite` desired running | 站点目录有效 | 监听分配端口并提供静态文件 |
| `StaticSite` desired running | 端口绑定失败 | 标记 failed，保留明确错误，不报告运行中 |
| desired stopped | 实例运行 | 停止并保留应用配置 |
| TTL 已过期 | 实例仍运行 | 停止、清理，记为 expired；不得自动重启 |

调和器只做确定性状态转换，不需要后台复杂控制器体系。单进程 timer/队列加启动时恢复足以支撑单机多 app。针对同一 app 的 reconcile、手动 stop/restart、remove 和 TTL expire 必须串行化。

一个重要区别：TTL 到期不是“配置仍要求 running，所以立刻再拉起”。每次显式 apply/restart 产生新的 generation；TTL 终结的是该 generation。过期 generation 不自动复活，新的显式操作才创建新的生命周期。

### 4.3 CLI/API

客户端新增简单命令：

```text
dzjjy-client apply -file dzjjy.yaml
```

可另加本地 `validate`，只解析和校验、不启动应用。服务端接口沿用 app 生命周期 API，增加配置 apply 请求即可；第一版不需要新增一套 `/configs` CRUD 资源 API。

`apply` 的最小语义：校验完整配置 -> 上传/传输 artifact -> 服务端按 app name 更新 desired state -> reconcile -> 返回当前 generation 和状态。校验或传输失败不得先删除当前可运行实例。旧 multipart deploy 和 CLI 保持兼容。

首版不做文件 watch、不自动 prune，也不因为 YAML 文件删除而删除运行应用。用户显式 apply 某个配置才改变期望状态。

### 4.4 状态持久化

在现有 app state 上增量保存：

- 规范化 desired config（包括 template id/version）；
- 当前 generation；
- backend（process/docker/static）；
- PID、Docker container ID 或静态站点监听端口；
- `started_at`、`expires_at`；
- observed status 和 termination reason。

不引入数据库或外部协调存储。服务重启时，从状态文件恢复 desired config 和实际运行标识，再执行一次 reconcile。对不确定身份的宿主机 PID 不得自动 kill；Docker 容器用 dzjjy 标签识别归属后再管理。

## 5. 可选 Docker 沙箱

### 5.1 Runtime template

Docker 模式采用平台维护的模板注册表，例如：

```text
python:3.12
node:22
```

每个模板由平台定义基础镜像、容器内工作目录、默认执行约定和允许的 runtime 版本。YAML 只引用模板 ID 和版本，不接收任意 Dockerfile，也不允许直接配置 privileged、host path 等 Docker 参数。

这是对上一版“用户直接指定 image”的修订：结合 Hindsight 中“平台维护模板、用户代码套模板”的明确偏好，应用配置引用受控 runtime template 更匹配产品方向，也能让 Linux/Windows 行为一致。

### 5.2 生命周期

Docker backend 的单实例生命周期：

```text
确认 Docker 可用
  -> 解析 runtime template
  -> 准备 artifact 和隔离工作目录
  -> create/start container
  -> 保存 container ID 和 generation
  -> 收集日志/检查状态
  -> stop(timeout)
  -> 超时则 kill
  -> remove container 和临时工作目录
```

Docker 不可用或模板不支持目标平台时必须明确失败，不能静默退回 process。容器须带 dzjjy 管理标签，服务只清理自己创建的容器。应用容器不挂载 Docker socket。

初始安全默认值：不启用 privileged、host PID、host network、任意 host path；停止使用显式 timeout，超过后强制 kill 并 remove。只读根文件系统、非 root 用户需要与各 runtime template 的写目录契约一起验证后启用，不能对任意镜像假定可运行。

### 5.3 跨平台和隔离等级

dzjjy 通过 Docker Engine 管理容器，避免自行维护两套容器生命周期代码。Linux 和 Windows Docker daemon 的容器能力可能不同；template 应声明支持的平台，部署时检查并报出不支持，不做隐式转换。

标准 Docker 是首版边界。Hindsight 讨论中的 gVisor/runsc 作为后续可插拔 runtime 方向，不属于 MVP，也不能仅通过配置字段宣称已有更强隔离。若未来运行不可信租户代码，应单独做威胁模型和安全验收；Docker 容器本身不应被描述成对所有攻击都绝对安全。

### 5.4 artifact 策略

现有项目已经以上传制品为主，因此推荐 Docker MVP 支持“平台 runtime template + 上传 artifact”，不要求制品预先烘焙在用户镜像中。容器通过受控工作目录或只读绑定方式访问制品，不开放任意主机路径。

跨平台路径和权限需要在实现阶段针对 Docker Desktop/Linux daemon 实测。若 MVP 时间必须收缩，可先只支持一种 artifact 形式，但不要把平台模板设计成只能跑预置镜像中的 demo 应用。

## 6. TTL 与函数云生命周期

### 6.1 首版 TTL 语义

TTL 是实例的硬生存时间，不是空闲超时：

- 从进程/容器成功启动时开始计算。
- 不设置 TTL 表示长期运行；显式 `ttl: 0` 拒绝，避免歧义。
- `expires_at` 持久化，服务重启不刷新寿命。
- 自动崩溃重启仍是同一 generation，不刷新 TTL。
- 显式手动 restart 或配置更新创建新 generation，从新实例成功启动时重算 TTL。
- 到期停止实例；Docker stop 超时后 kill，再 remove。
- 到期记为 `expired`，不允许 auto-restart/reconcile 自动拉起。
- 手动 stop、崩溃、重启耗尽、TTL 到期和清理失败要有不同终止原因。

函数云通常将 timeout 作为单次 invocation 的硬上限；dzjjy 当前模型管理的是长期 app/container，因此借用硬截止时间、明确超时终止的原则，不照搬 invocation/并发扩缩容/计费语义。

### 6.2 调度和恢复

一个进程内 TTL scheduler 即可：按 `expires_at` 排序，触发 app 生命周期操作。避免每个实例创建长期 ticker。服务启动时从状态文件重建未到期任务；已到期的实例直接清理，不重新启动。

状态示例：

```json
{
  "app_name": "demo",
  "backend": "docker",
  "container_id": "...",
  "generation": 2,
  "status": "running",
  "started_at": 1774173600,
  "expires_at": 1774175400,
  "termination_reason": ""
}
```

### 6.3 与闲置休眠的边界

此前产品讨论的“闲置时释放 CPU/内存、保留存储”应作为单独能力，不等于 TTL：

- TTL：到达绝对期限后终止并清理运行实例。
- idle suspend：无访问一段时间后停止容器，保留工作数据；有入口时再唤醒。

静态站点由独立宿主机 listener 提供访问；没有 Docker 容器的请求入口，因此 idle suspend 和临时 URL 暂缓。后续引入 HTTP 入口时，再定义空闲判定、唤醒延迟、保留卷和 URL 生命周期。

## 7. 推荐实施顺序

### Phase 0：现有可靠性修复

- 修复 `Manager.Start` 锁泄漏。
- 统一 failed/max-restart 状态事件。
- 加服务优雅关闭。
- 对 app 生命周期操作串行化。
- 修复归档超限静默截断并添加总量护栏。
- 补 PID 身份校验和 process 子进程树回收策略。
- 增加真实路由、并发、状态转换和恢复测试。

### Phase 1：YAML 与 reconcile（process）
- 定义 `dzjjy/v1` YAML 和严格解析；支持 `App` 与 `StaticSite` 两类 kind。
- 静态站点以独立宿主机端口托管文件，不启动 runtime/Docker 容器。
- `command` 改为参数数组，旧 API 兼容。
- 保存 desired config、generation 和 observed status。
- 实现显式 `apply` 与轻量 reconcile；不做 watch/prune。
- YAML 校验和 artifact 传输失败时不破坏当前运行应用。

### Phase 2：Docker templates + TTL

- 引入 Docker Engine 客户端和容器 backend。
- 平台维护 Python、Node.js、Go 三类一等 runtime template，明确首批版本和目标平台支持矩阵。
- 支持 artifact 在容器中的受控路径。
- 保存容器 ID、模板版本、generation 和 `expires_at`。
- 实现 TTL scheduler、stop timeout、kill/remove、服务恢复。
- 在 Linux 和 Windows Docker 环境测试实际 create/start/log/stop/remove；没有 Docker 的 CI 环境只做单元/契约测试，不能把 mock 结果说成 Docker E2E。
- 保留未来 gVisor/runsc 的 backend/runtime 选项，但不实现。

### Phase 3：后续产品能力（不在当前范围）

- 空闲暂停、保留存储和访问时唤醒。
- 临时 URL/入口路由和端口复用。
- 网络、端口、资源限制、Secret、volume 配置。
- gVisor/runsc 等更强隔离运行时。
- Kubernetes manifest 导入器（只有明确需求时才考虑）。

## 8. 验收标准

### YAML / reconcile

- `App` 支持 process/Docker template；静态页面使用 `StaticSite`，不启动应用 runtime。
- 静态站点上传目录归档后可通过实际分配端口访问，部署响应和 status 返回端口。
- 两个站点不共享监听端口；端口绑定冲突会明确失败，不会报告运行中。
- 静态文件请求不能通过路径穿越读到站点根目录以外的文件。
- 非法 YAML、未知字段、缺少必填字段和不支持的 template 在启动前拒绝。
- `command` 参数数组保持空格、引号和 Windows 路径边界。
- 相同配置重复 apply 不造成不必要重启。
- desired running 且 observed running 时 reconcile 无副作用。
- 配置更新只替换该 app 的旧 generation。
- apply 失败不删除现存可用实例；不因 YAML 文件缺失自动 prune。

### Docker

- Docker 不可用、template 不支持目标平台时明确报错，不退回 process。
- 只操作带 dzjjy 管理标识的容器。
- stop 超时后 kill，再确认 remove。
- 手动 remove、TTL 和服务 shutdown 都清理容器。
- artifact 路径限制在 dzjjy 管理目录，容器不可见 Docker socket 或任意宿主路径。

### TTL

- 服务重启和自动崩溃重启均不延长原 generation 的 TTL。
- 手动 restart 创建新 generation 并重置 TTL。
- 到期后状态为 expired，调和循环不复活旧 generation。
- 进程树或容器确实退出；清理失败有明确状态和错误。
- termination reason 能区分 manual stop、crash、max restart、ttl 和 cleanup failure。

## 9. 已确认决策与剩余讨论

### 已确认

```text
配置：类似 K8s 的简单 YAML，支持 App 和 StaticSite
模型：单机声明式期望状态 + 轻量 reconcile，不上 Kubernetes/etcd
runtime：Python、Node.js、Go 为第一批一等模板
静态站点：内置文件服务，每站点一个独立宿主机端口，不启动 runtime
端口：默认自动分配空闲端口；可选固定端口，冲突时报错；apply/status 返回实际端口
监听：复用服务端 bind 地址；当前默认所有网卡
访问控制：静态内容不受 API Bearer Token 保护，只部署可公开内容；按需使用主机防火墙
粒度：一份 YAML 一个 app/site
运行：process 默认；Docker 可选
Docker：平台维护 runtime template，用户不需要 Dockerfile
平台：Linux 和 Windows；检查 backend/template 平台支持
TTL：从成功启动开始，持久化绝对 expires_at
自动重启：不刷新 TTL
手动 restart：新 generation，重新计算 TTL
Docker 网络/端口映射、Secret、资源配额：当前不做配置
闲置休眠、临时 URL：后续
后续方向：Docker runtime 可预留 gVisor/runsc，但不进入 MVP
```
