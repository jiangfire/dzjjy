# 项目计划

> 文档更新：2026-01-01
> 来源：从根目录 PLAN.md 移动

## 📋 项目概述
本计划为 dzjjy 部署服务的三个复杂扩展系统提供详细的实施方案：
- **系统A**：多应用管理
- **系统B**：状态持久化与自动恢复
- **系统E**：插件扩展系统

## 🎯 实施优先级

```
优先级1：系统B（状态持久化）→ 优先级2：系统A（多应用管理）→ 优先级3：系统E（插件系统）
```

---

## 🔧 系统B：状态持久化与自动恢复

### 目标
解决服务重启后状态丢失问题，实现自动恢复能力。

### 核心需求
1. 持久化应用配置、运行状态、重启计数
2. 启动时自动恢复之前的状态
3. PID验证和进程有效性检查
4. 数据一致性保证（原子写入、校验和）

### 数据结构设计

```go
// internal/server/state/types.go
type StateFile struct {
    Version   string    `json:"version"`    // 版本号
    Timestamp int64     `json:"timestamp"`  // 时间戳
    Checksum  string    `json:"checksum"`   // SHA256校验和
    Data      StateData `json:"data"`       // 实际数据
}

type StateData struct {
    Apps map[string]*AppState `json:"apps"`
}

type AppState struct {
    Config       *ProcessConfig `json:"config"`        // 应用配置
    PID          int            `json:"pid"`           // 进程ID
    StartTime    int64          `json:"start_time"`    // 启动时间戳
    RestartCount int            `json:"restart_count"` // 重启次数
    Status       string         `json:"status"`        // 状态：running/stopped/failed
    WorkPath     string         `json:"work_path"`     // 工作目录
}
```

### 实施步骤

#### 阶段1：数据模型与持久化引擎（1.5天）
**文件**：`internal/server/state/types.go`, `internal/server/state/persist.go`

```go
// 原子写入实现
func AtomicWriteFile(path string, data []byte) error {
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmpPath, path)  // Unix原子操作
}

// 文件锁机制
func (s *StateStore) acquireLock() error {
    lockPath := s.stateFile + ".lock"
    // 使用O_EXCL创建，确保原子性
    lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
    if err != nil {
        return fmt.Errorf("another process is persisting")
    }
    s.lockFile = lockFile
    return nil
}
```

#### 阶段2：状态同步逻辑（1.5天）
**文件**：`internal/server/state/sync.go`

```go
// 事件驱动的实时保存
func (s *StateStore) OnAppEvent(event AppEvent) {
    switch event.Type {
    case "start", "stop", "restart", "config_change":
        s.schedulePersist()  // 延迟100ms批量写入
    case "status_update":
        // 状态更新定时批量处理
    }
}

// 定时快照（每30秒）
func (s *StateStore) snapshotLoop(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            s.Persist()
        case <-ctx.Done():
            s.Persist()  // 优雅退出时强制保存
            return
        }
    }
}
```

#### 阶段3：恢复机制（1.5天）
**文件**：`internal/server/state/restore.go`

```go
func (s *StateStore) Restore() error {
    // 1. 读取状态文件
    state, err := s.loadStateFile()
    if err != nil {
        if os.IsNotExist(err) {
            return nil  // 首次启动
        }
        return err
    }

    // 2. 验证校验和
    if !state.Verify() {
        s.log.Warn("state file corrupted, using backup")
        return s.restoreFromBackup()
    }

    // 3. 恢复每个应用
    for name, appState := range state.Data.Apps {
        if err := s.restoreApp(name, appState); err != nil {
            s.log.Error("failed to restore app", "app", name, "error", err)
        }
    }
    return nil
}

func (s *StateStore) restoreApp(name string, state *AppState) error {
    // PID验证
    if state.PID > 0 {
        if process, err := os.FindProcess(state.PID); err == nil {
            // 发送信号0检查进程是否存在
            if err := process.Signal(syscall.Signal(0)); err == nil {
                // 进程仍在运行，恢复管理
                return s.recoverRunningApp(name, state)
            }
        }
    }

    // 进程已死，根据策略处理
    if state.Config.AutoRestart && state.Status != "stopped" {
        go s.manager.StartApp(name, state.Config)
    }
    return nil
}
```

#### 阶段4：集成与测试（0.5天）
**文件**：`cmd/server/main.go`, `internal/server/handler/handler.go`

- 在服务启动时调用 `Restore()`
- 修改 Handler 以支持状态持久化
- 编写测试用例

### 关键文件清单
- `internal/server/state/types.go` - 数据结构定义
- `internal/server/state/persist.go` - 持久化引擎
- `internal/server/state/sync.go` - 状态同步
- `internal/server/state/restore.go` - 恢复逻辑
- `internal/server/state/backup.go` - 备份管理

---

## 🔧 系统A：多应用管理

### 目标
支持同时管理多个应用进程，每个应用独立配置和状态。

### 核心需求
1. 应用注册表，支持多应用注册和管理
2. 每个应用独立工作目录、日志文件
3. 并发安全的多应用操作
4. 向后兼容现有单应用API

### 数据结构设计

```go
// internal/server/registry/types.go
type AppRegistry struct {
    mu      sync.RWMutex
    apps    map[string]*AppEntry
    logDir  string
    workDir string
    state   *state.StateStore  // 复用系统B的状态持久化
}

type AppEntry struct {
    manager  *runtime.Manager
    config   *ProcessConfig
    status   AppStatus
    workPath string  // 独立工作目录: /workspace/app1/
}

type AppStatus struct {
    Running      bool      `json:"running"`
    PID          int       `json:"pid"`
    StartTime    time.Time `json:"start_time"`
    RestartCount int       `json:"restart_count"`
}
```

### 实施步骤

#### 阶段1：注册表核心实现（1天）
**文件**：`internal/server/registry/registry.go`

```go
func (r *AppRegistry) Register(name string, config *ProcessConfig) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.apps[name]; exists {
        return fmt.Errorf("app %s already exists", name)
    }

    // 创建独立工作目录
    workPath := filepath.Join(r.workDir, name)
    if err := os.MkdirAll(workPath, 0755); err != nil {
        return err
    }

    entry := &AppEntry{
        config:   config,
        workPath: workPath,
        status:   AppStatus{Running: false},
    }

    r.apps[name] = entry

    // 持久化注册信息
    if r.state != nil {
        r.state.OnAppEvent(AppEvent{
            Type: "register",
            Name: name,
            Data: config,
        })
    }

    return nil
}

func (r *AppRegistry) Get(name string) (*AppEntry, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    entry, exists := r.apps[name]
    if !exists {
        return nil, fmt.Errorf("app %s not found", name)
    }
    return entry, nil
}
```

#### 阶段2：Runtime层改造（1天）
**文件**：`internal/server/runtime/runtime.go`

```go
// 为Manager添加应用标识
type Manager struct {
    appName    string          // 新增：应用名称
    workPath   string          // 新增：独立工作目录
    // ... 其他字段保持不变
}

func (m *Manager) Start() error {
    // 日志文件包含应用名：app1-stdout-20251228.log
    logger, err := NewLogger(m.appName, m.config.Executable, m.logDir)
    if err != nil {
        return err
    }
    m.logger = logger

    // 设置工作目录
    if m.workPath != "" {
        m.cmd.Dir = m.workPath
    }

    // ... 其余启动逻辑
}
```

#### 阶段3：Handler层重构（1.5天）
**文件**：`internal/server/handler/handler.go`

```go
// 新增端点
func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
    apps := h.registry.List()
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "apps": apps,
    })
}

func (h *Handler) DeployApp(w http.ResponseWriter, r *http.Request) {
    appName := r.URL.Query().Get("app")
    if appName == "" {
        appName = "_default"  // 默认应用名
    }

    // 停止同名应用（如果存在）
    if err := h.registry.Stop(appName); err != nil && !errors.Is(err, ErrAppNotFound) {
        respondError(w, http.StatusInternalServerError, err)
        return
    }

    // 清理该应用的工作目录
    if err := h.registry.CleanWorkDir(appName); err != nil {
        respondError(w, http.StatusInternalServerError, err)
        return
    }

    // 保存上传文件到应用独立目录
    filePath, err := h.saveUpload(r, appName)
    if err != nil {
        respondError(w, http.StatusInternalServerError, err)
        return
    }

    // 解压（如果需要）
    if isArchive(filePath) {
        if err := h.extractArchive(filePath, h.registry.workDir, appName); err != nil {
            respondError(w, http.StatusInternalServerError, err)
            return
        }
    }

    // 启动应用
    config := parseConfig(r)
    if err := h.registry.Start(appName, config); err != nil {
        respondError(w, http.StatusInternalServerError, err)
        return
    }

    respondJSON(w, http.StatusOK, map[string]string{
        "status": "success",
        "app":    appName,
    })
}

// 保持现有端点兼容性
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
    // 调用 DeployApp，默认应用名 "_default"
    h.DeployApp(w, r)
}
```

#### 阶段4：客户端适配（0.5天）
**文件**：`cmd/client/main.go`, `internal/client/deploy/deploy.go`

```go
// 添加 -app 参数
deployCmd := flag.String("app", "_default", "application name")
```

### 关键文件清单
- `internal/server/registry/types.go` - 注册表数据结构
- `internal/server/registry/registry.go` - 注册表核心逻辑
- `internal/server/runtime/runtime.go` - 添加应用标识
- `internal/server/handler/handler.go` - 新增多应用端点
- `cmd/server/main.go` - 路由调整

---

## 🔧 系统E：插件扩展系统

### 目标
提供可扩展的插件机制，支持生命周期钩子、日志处理、健康检查等。

### 核心需求
1. 插件接口定义和生命周期管理
2. 安全沙箱隔离（防止插件破坏系统）
3. 错误隔离（插件崩溃不影响主系统）
4. 支持内置插件和Webhook

### 接口设计

```go
// internal/server/plugin/types.go
type Plugin interface {
    Name() string
    Version() string
    Init(config map[string]any) error

    // 生命周期钩子
    OnDeploy(appName string, config *DeployConfig) error
    OnStart(appName string, pid int) error
    OnStop(appName string, reason string) error
    OnRestart(appName string, count int) error

    // 日志钩子
    OnLog(entry LogEntry) error

    // 健康检查
    HealthCheck(appName string) HealthStatus

    // 事件通知
    OnEvent(event Event)
}

type Event struct {
    Type      string
    Timestamp time.Time
    Data      map[string]any
}

type HealthStatus struct {
    Healthy bool
    Message string
    Details map[string]any
}
```

### 实施步骤

#### 阶段1：插件管理器与接口（1.5天）
**文件**：`internal/server/plugin/manager.go`

```go
type PluginManager struct {
    plugins []Plugin
    mu      sync.RWMutex
    timeout time.Duration
}

func (pm *PluginManager) Load(configs []PluginConfig) error {
    for _, cfg := range configs {
        plugin, err := pm.loadPlugin(cfg)
        if err != nil {
            return fmt.Errorf("failed to load plugin %s: %w", cfg.Name, err)
        }

        if err := plugin.Init(cfg.Config); err != nil {
            return fmt.Errorf("failed to init plugin %s: %w", cfg.Name, err)
        }

        pm.plugins = append(pm.plugins, plugin)
        slog.Info("plugin loaded", "name", plugin.Name(), "version", plugin.Version())
    }
    return nil
}

func (pm *PluginManager) ExecuteHook(hookName string, data any) {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    for _, plugin := range pm.plugins {
        go pm.executeSafe(plugin, hookName, data)
    }
}

func (pm *PluginManager) executeSafe(plugin Plugin, hookName string, data any) {
    defer func() {
        if r := recover(); r != nil {
            slog.Error("plugin panic recovered",
                "plugin", plugin.Name(),
                "panic", r,
            )
        }
    }()

    ctx, cancel := context.WithTimeout(context.Background(), pm.timeout)
    defer cancel()

    done := make(chan error, 1)
    go func() {
        done <- pm.callHook(plugin, hookName, data)
    }()

    select {
    case err := <-done:
        if err != nil {
            slog.Warn("plugin hook failed",
                "plugin", plugin.Name(),
                "hook", hookName,
                "error", err,
            )
        }
    case <-ctx.Done():
        slog.Warn("plugin hook timeout",
            "plugin", plugin.Name(),
            "hook", hookName,
            "timeout", pm.timeout,
        )
    }
}
```

#### 阶段2：内置插件实现（1.5天）
**文件**：`internal/server/plugin/builtin/`

```go
// internal/server/plugin/builtin/webhook.go
type WebhookPlugin struct {
    endpoint string
    events   []string
    timeout  time.Duration
}

func (p *WebhookPlugin) OnEvent(event Event) error {
    if !p.shouldHandle(event.Type) {
        return nil
    }

    data, _ := json.Marshal(event)
    req, _ := http.NewRequest("POST", p.endpoint, bytes.NewBuffer(data))
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: p.timeout}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return fmt.Errorf("webhook returned %d", resp.StatusCode)
    }
    return nil
}

// internal/server/plugin/builtin/metrics.go
type MetricsPlugin struct {
    port int
    mux  *http.ServeMux
}

func (p *MetricsPlugin) Init(config map[string]any) error {
    p.mux = http.NewServeMux()
    p.mux.HandleFunc("/metrics", p.handleMetrics)
    go http.ListenAndServe(fmt.Sprintf(":%d", p.port), p.mux)
    return nil
}

func (p *MetricsPlugin) OnEvent(event Event) error {
    // 记录指标
    metricsCounter.WithLabelValues(event.Type).Inc()
    return nil
}
```

#### 阶段3：安全加固（1.5天）
**文件**：`internal/server/plugin/security.go`

```go
// 熔断器
type CircuitBreaker struct {
    failures      int
    threshold     int
    lastFailure   time.Time
    cooldown      time.Duration
    isOpen        bool
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    if cb.isOpen {
        if time.Since(cb.lastFailure) < cb.cooldown {
            return fmt.Errorf("circuit breaker is open")
        }
        cb.isOpen = false
        cb.failures = 0
    }

    err := fn()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.threshold {
            cb.isOpen = true
            slog.Warn("circuit breaker opened")
        }
        return err
    }

    cb.failures = 0
    return nil
}

// 资源限制
func (pm *PluginManager) executeWithLimits(plugin Plugin, hook string, data any) error {
    // 限制内存使用（可选）
    // 限制CPU时间（通过context timeout）
    // 限制网络访问（通过自定义http.Client）
    return pm.executeSafe(plugin, hook, data)
}
```

#### 阶段4：配置与集成（1.5天）
**文件**：`cmd/server/main.go`, `config/plugins.yaml`

```yaml
# config/plugins.yaml
plugins:
  - name: slack-notify
    type: webhook
    config:
      endpoint: "https://hooks.slack.com/..."
      events: ["deploy", "restart", "crash"]
      timeout: 5s

  - name: metrics
    type: builtin
    config:
      port: 9090

  - name: backup
    type: builtin
    config:
      backup_dir: "/backup"
      keep_versions: 5
```

```go
// 在 Handler 中调用插件
func (h *Handler) DeployApp(w http.ResponseWriter, r *http.Request) {
    // ... 部署逻辑

    // 触发插件钩子
    if h.pluginManager != nil {
        go h.pluginManager.ExecuteHook("OnDeploy", DeployEvent{
            AppName: appName,
            Config:  config,
            User:    getUser(r),
        })
    }

    // ... 启动应用

    if h.pluginManager != nil {
        go h.pluginManager.ExecuteHook("OnStart", StartEvent{
            AppName: appName,
            PID:     pid,
        })
    }
}
```

### 关键文件清单
- `internal/server/plugin/types.go` - 接口定义
- `internal/server/plugin/manager.go` - 插件管理器
- `internal/server/plugin/security.go` - 安全机制
- `internal/server/plugin/builtin/` - 内置插件
- `config/plugins.yaml` - 配置文件

---

## 📊 实施时间线总结

| 阶段 | 系统 | 时间 | 依赖 | 产出 |
|------|------|------|------|------|
| 1 | B - 持久化引擎 | 1.5天 | 无 | 状态存储基础 |
| 2 | B - 状态同步 | 1.5天 | 阶段1 | 实时保存 |
| 3 | B - 恢复机制 | 1.5天 | 阶段2 | 自动恢复 |
| 4 | B - 测试集成 | 0.5天 | 阶段3 | 完整系统B |
| 5 | A - 注册表 | 1天 | B | 多应用基础 |
| 6 | A - Runtime改造 | 1天 | 阶段5 | 应用隔离 |
| 7 | A - Handler重构 | 1.5天 | 阶段6 | 新API端点 |
| 8 | A - 客户端适配 | 0.5天 | 阶段7 | 完整系统A |
| 9 | E - 管理器接口 | 1.5天 | A | 插件框架 |
| 10 | E - 内置插件 | 1.5天 | 阶段9 | 基础插件 |
| 11 | E - 安全加固 | 1.5天 | 阶段10 | 安全机制 |
| 12 | E - 配置集成 | 1.5天 | 阶段11 | 完整系统E |

**总计**：约 15-16 天

---

## ⚠️ 风险与应对

### 系统B风险
- **数据损坏**：原子写入 + 校验和 + 备份
- **PID误判**：多重验证（PID + 启动时间 + 进程名）
- **性能影响**：混合持久化策略（实时+定时）

### 系统A风险
- **死锁**：统一锁层级，避免嵌套
- **资源泄漏**：严格目录清理，监控资源使用
- **兼容性**：保持现有API，使用默认应用名

### 系统E风险
- **安全漏洞**：严格沙箱 + 超时控制 + 权限限制
- **性能开销**：异步执行 + 熔断机制
- **错误传播**：Panic捕获 + 错误隔离

---

## 🎯 建议实施顺序

1. **立即开始**：系统B（解决核心痛点）
2. **视需求**：系统A（提升多服务管理能力）
3. **谨慎评估**：系统E（高级定制需求）

每个系统独立完成并充分测试后再进行下一个，确保系统稳定性。

---

## 🧪 系统C：测试覆盖率提升计划

### 📊 当前测试现状

**严重问题**：项目目前**零测试覆盖**（1,853行代码，0个测试文件）

| 模块 | 覆盖率 | 风险等级 | 代码行数 |
|------|--------|----------|----------|
| Runtime Manager | 0% | 🔴 **极高** | 409 |
| Logger | 0% | 🔴 **极高** | 210 |
| Archive | 0% | 🔴 **极高** | 286 |
| Auth | 0% | 🔴 **高** | 43 |
| Handler | 0% | 🔴 **极高** | 371 |
| Client Deploy | 0% | 🔴 **高** | 198 |
| Main Programs | 0% | 🟡 **中** | 305 |

### 🎯 测试目标

| 阶段 | 时间 | 覆盖率目标 | 产出 |
|------|------|------------|------|
| **短期** | 1-2周 | 60% | 核心模块单元测试 |
| **中期** | 3-4周 | 80% | 集成测试 + 并发测试 |
| **长期** | 1-2月 | 90%+ | 端到端测试 + 性能基准 |

### 📋 测试框架设计

#### 测试工具链
```go
// 核心依赖
import (
    "testing"                      // 标准测试框架
    "net/http/httptest"           // HTTP 测试
    "github.com/stretchr/testify/assert"   // 断言库
    "github.com/stretchr/testify/require"  // 阻断断言
    "github.com/stretchr/testify/mock"     // Mock 框架
)
```

#### 测试目录结构
```
dzjjy/
├── internal/
│   ├── server/
│   │   ├── runtime/
│   │   │   ├── runtime.go
│   │   │   └── runtime_test.go      # 新增
│   │   ├── logger/
│   │   │   ├── logger.go
│   │   │   └── logger_test.go       # 新增
│   │   ├── archive/
│   │   │   ├── archive.go
│   │   │   └── archive_test.go      # 新增
│   │   ├── auth/
│   │   │   ├── auth.go
│   │   │   └── auth_test.go         # 新增
│   │   └── handler/
│   │       ├── handler.go
│   │       └── handler_test.go      # 新增
│   └── client/
│       └── deploy/
│           ├── deploy.go
│           └── deploy_test.go       # 新增
├── test/
│   ├── fixtures/                    # 测试数据
│   │   ├── test-app.zip            # 正常应用
│   │   ├── malicious-traversal.zip # 路径遍历攻击
│   │   └── corrupted.zip           # 损坏文件
│   ├── integration/                 # 集成测试
│   └── performance/                 # 性能测试
└── go.mod                          # 添加测试依赖
```

### 🔧 核心测试场景

#### 1. Runtime 模块测试（最高优先级）

**进程生命周期测试**：
```go
func TestManager_Start_Stop(t *testing.T) {
    // 测试正常启动和停止
    config := &ProcessConfig{
        Executable: "echo",
        Args:       []string{"hello"},
        Type:       "exec",
    }

    manager := runtime.NewManager(config, "./workspace", "./logs")
    err := manager.Start()
    require.NoError(t, err)

    // 验证进程运行
    assert.True(t, manager.IsRunning())
    assert.Greater(t, manager.GetPID(), 0)

    // 停止进程
    err = manager.Stop()
    require.NoError(t, err)

    // 验证进程已停止
    assert.False(t, manager.IsRunning())
}
```

**自动重启测试**：
```go
func TestManager_AutoRestart(t *testing.T) {
    config := &ProcessConfig{
        Executable:   "sh",
        Args:         []string{"-c", "exit 1"},  // 立即退出
        Type:         "exec",
        AutoRestart:  true,
        MaxRestarts:  3,
    }

    manager := runtime.NewManager(config, "./workspace", "./logs")
    err := manager.Start()
    require.NoError(t, err)

    // 等待重启
    time.Sleep(2 * time.Second)

    // 验证重启次数
    assert.Equal(t, 3, manager.GetRestartCount())

    // 停止
    manager.Stop()
}
```

**并发安全测试**：
```go
func TestManager_ConcurrentOperations(t *testing.T) {
    config := &ProcessConfig{
        Executable: "sleep",
        Args:       []string{"1"},
        Type:       "exec",
    }

    manager := runtime.NewManager(config, "./workspace", "./logs")

    // 并发启动/停止
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            manager.Start()
            time.Sleep(100 * time.Millisecond)
            manager.Stop()
        }()
    }
    wg.Wait()

    // 使用 race 检测竞态条件
}
```

#### 2. Archive 模块测试（安全重点）

**路径遍历防护测试**：
```go
func TestArchive_SafeExtractPath(t *testing.T) {
    tests := []struct {
        name      string
        entryPath string
        destDir   string
        expectErr bool
    }{
        {"normal", "app/main.go", "/workspace", false},
        {"parent_dir", "../etc/passwd", "/workspace", true},
        {"absolute", "/etc/passwd", "/workspace", true},
        {"symlink", "link->/etc", "/workspace", true},
        {"empty", "", "/workspace", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := archive.SafeExtractPath(tt.destDir, tt.entryPath)
            if tt.expectErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

**恶意压缩包测试**：
```go
func TestArchive_MaliciousZip(t *testing.T) {
    // 创建包含路径遍历的 ZIP
    maliciousZip := createMaliciousZip(t)

    err := archive.Extract(maliciousZip, "/tmp/test")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "path traversal")
}
```

#### 3. Logger 模块测试

**日志捕获测试**：
```go
func TestLogger_CaptureOutput(t *testing.T) {
    logger, err := runtime.NewLogger("test-app", "test", "./logs")
    require.NoError(t, err)
    defer logger.Close()

    // 模拟 stdout/stderr
    go logger.Capture("stdout", "test message")
    go logger.Capture("stderr", "error message")

    time.Sleep(100 * time.Millisecond)

    logs := logger.GetLogs("stdout", 10)
    assert.Contains(t, logs, "test message")
}
```

**内存限制测试**：
```go
func TestLogger_MemoryLimit(t *testing.T) {
    logger, _ := runtime.NewLogger("test", "test", "./logs")
    defer logger.Close()

    // 写入超过 1000 行
    for i := 0; i < 1500; i++ {
        logger.WriteLog("stdout", fmt.Sprintf("line %d", i))
    }

    logs := logger.GetLogs("stdout", 2000)
    assert.LessOrEqual(t, len(logs), 1000)  // 最多保留 1000 行
}
```

#### 4. Handler 模块测试（集成测试）

**完整部署流程测试**：
```go
func TestHandler_DeployFlow(t *testing.T) {
    // 创建测试服务器
    handler := handler.NewHandler("./workspace", "./logs", "test-token")
    server := httptest.NewServer(handler)
    defer server.Close()

    // 1. 上传应用
    body := new(bytes.Buffer)
    writer := multipart.NewWriter(body)
    file, _ := writer.CreateFormFile("file", "app.zip")
    file.Write(createTestZip(t))
    writer.Close()

    req, _ := http.NewRequest("POST", server.URL+"/api/v1/deploy", body)
    req.Header.Set("Authorization", "Bearer test-token")
    req.Header.Set("Content-Type", writer.FormDataContentType())

    client := &http.Client{}
    resp, err := client.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusOK, resp.StatusCode)

    // 2. 查询状态
    req, _ = http.NewRequest("GET", server.URL+"/api/v1/status", nil)
    req.Header.Set("Authorization", "Bearer test-token")
    resp, _ = client.Do(req)

    var status map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&status)
    assert.True(t, status["running"].(bool))
}
```

**认证测试**：
```go
func TestHandler_Auth(t *testing.T) {
    handler := handler.NewHandler("./workspace", "./logs", "secret")

    tests := []struct {
        name     string
        token    string
        expectOK bool
    }{
        {"valid", "Bearer secret", true},
        {"invalid", "Bearer wrong", false},
        {"missing", "", false},
        {"malformed", "secret", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", "/api/v1/status", nil)
            if tt.token != "" {
                req.Header.Set("Authorization", tt.token)
            }

            w := httptest.NewRecorder()
            handler.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusOK)
            })).ServeHTTP(w, req)

            if tt.expectOK {
                assert.Equal(t, http.StatusOK, w.Code)
            } else {
                assert.Equal(t, http.StatusUnauthorized, w.Code)
            }
        })
    }
}
```

### 🚀 分阶段实施计划

#### 阶段1：测试基础设施（1天）
```bash
# 1. 添加测试依赖
go get github.com/stretchr/testify

# 2. 创建测试目录
mkdir -p test/fixtures test/integration test/performance

# 3. 创建测试辅助文件
# test/helpers.go - 测试辅助函数
# test/fixtures/ - 测试数据生成
```

**产出**：测试框架就绪，可开始编写单元测试

#### 阶段2：核心模块单元测试（3天）

**Day 1: Runtime 模块**
- [ ] 进程生命周期测试（Start/Stop/Restart）
- [ ] 自动重启逻辑测试
- [ ] 并发安全测试
- [ ] 错误场景测试

**Day 2: Logger & Archive 模块**
- [ ] 日志捕获和持久化测试
- [ ] 内存管理测试
- [ ] 多格式解压测试
- [ ] 路径遍历安全测试

**Day 3: Auth & Client 模块**
- [ ] Token 认证测试
- [ ] HTTP 客户端测试
- [ ] 错误处理测试

**产出**：核心模块 70% 覆盖率

#### 阶段3：集成测试（2天）

**Day 4: Handler 集成测试**
- [ ] 完整部署流程测试
- [ ] 并发部署测试
- [ ] 错误响应测试

**Day 5: 端到端测试**
- [ ] 真实场景测试
- [ ] 资源清理验证
- [ ] 性能基准测试

**产出**：集成测试覆盖主要流程，总覆盖率 80%

#### 阶段4：高级测试（2天）

**Day 6: 并发与竞态测试**
```bash
# 使用 race 标志检测竞态
go test -race ./internal/server/runtime/...
go test -race ./internal/server/logger/...
```

**Day 7: 安全测试**
- [ ] 路径遍历攻击模拟
- [ ] 注入攻击测试
- [ ] 权限边界测试

**产出**：并发安全验证，安全防护验证

#### 阶段5：质量保障（1天）

**Day 8: 覆盖率优化与文档**
- [ ] 生成覆盖率报告
- [ ] 优化低覆盖率区域
- [ ] 编写测试文档
- [ ] 添加 CI/CD 集成

```bash
# 生成 HTML 覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# 设置覆盖率阈值检查
go test -covermode=atomic -coverprofile=coverage.out ./... | grep -E 'coverage:.*\d+\.\d+%' | awk '{if($2+0 < 70) exit 1}'
```

### 📊 覆盖率监控策略

#### 本地开发
```bash
# 实时查看覆盖率
go test -cover ./internal/server/runtime/...

# 生成详细报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

#### CI/CD 集成
```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run tests with coverage
        run: |
          go test -race -coverprofile=coverage.out ./...
          go tool cover -html=coverage.out -o coverage.html

      - name: Check coverage threshold
        run: |
          COVERAGE=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}' | sed 's/%//')
          echo "Total coverage: $COVERAGE%"
          if (( $(echo "$COVERAGE < 70" | bc -l) )); then
            echo "Coverage below 70% threshold"
            exit 1
          fi

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out
```

### 🎯 关键测试场景清单

#### 🔴 必须测试（高风险）
- [ ] 进程自动重启循环（达到最大重启次数）
- [ ] 路径遍历攻击防护
- [ ] 并发部署冲突处理
- [ ] 上下文取消优雅关闭
- [ ] 日志内存泄漏检测

#### 🟡 应该测试（中风险）
- [ ] 大文件日志处理
- [ ] 网络超时和重试
- [ ] 权限错误处理
- [ ] 磁盘空间不足
- [ ] 无效配置处理

#### 🟢 可选测试（低风险）
- [ ] 性能基准测试
- [ ] 内存使用优化
- [ ] 启动时间测试

### ⚠️ 测试风险与应对

#### 1. 测试环境污染
**风险**：测试文件残留，影响后续测试
**应对**：
```go
func TestMain(m *testing.M) {
    // 测试前清理
    os.RemoveAll("./test-workspace")
    os.MkdirAll("./test-workspace", 0755)

    // 运行测试
    code := m.Run()

    // 测试后清理
    os.RemoveAll("./test-workspace")
    os.Exit(code)
}
```

#### 2. 并发测试不稳定
**风险**：时序问题导致测试失败
**应对**：
- 使用 `time.Sleep()` 控制时序
- 增加重试机制
- 使用 `chan` 同步 goroutine

#### 3. 资源泄漏
**风险**：测试进程未正确清理
**应对**：
- 每个测试独立清理
- 使用 `defer` 确保清理
- 监控系统进程列表

### 📈 时间估算与资源

| 阶段 | 时间 | 人力 | 产出 |
|------|------|------|------|
| 基础设施 | 1天 | 1人 | 测试框架 |
| 核心单元测试 | 3天 | 1人 | 70% 覆盖率 |
| 集成测试 | 2天 | 1人 | 80% 覆盖率 |
| 高级测试 | 2天 | 1人 | 并发/安全验证 |
| 质量保障 | 1天 | 1人 | CI/CD + 文档 |
| **总计** | **9天** | **1人** | **90%+ 覆盖率** |

### 🎯 实施建议

1. **立即开始**：测试基础设施 + Runtime 模块
2. **并行进行**：Logger 和 Archive 模块
3. **逐步集成**：Handler 集成测试
4. **持续优化**：根据覆盖率报告调整

### 📋 测试依赖清单

```go
// go.mod 添加
require (
    github.com/stretchr/testify v1.8.4
    golang.org/x/sync v0.5.0  // 并发测试辅助
)

// 可选：性能测试
require (
    github.com/pkg/profile v1.7.0
)
```

---

## 🎯 总结与优先级

### 三大系统实施顺序
1. **系统C（测试）** - 立即开始（9天）
2. **系统B（持久化）** - 测试完成后（5天）
3. **系统A（多应用）** - B完成后（4天）
4. **系统E（插件）** - 谨慎评估（6天）

### 总时间估算
- **测试系统**：9天
- **三大系统**：15天
- **总计**：约 24 天

### 关键成功因素
- ✅ 充分的测试保障代码质量
- ✅ 测试驱动开发（TDD）思维
- ✅ 持续集成和覆盖率监控
- ✅ 详细的测试文档

**建议**：先完成测试系统，确保代码质量，再实施其他功能系统。