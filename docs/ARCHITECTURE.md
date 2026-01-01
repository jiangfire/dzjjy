# 架构设计

> 文档更新：2026-01-01
> 优化：移除重复内容，专注于技术架构

## 核心模块

```
internal/
├── server/
│   ├── runtime/          # 进程管理 + 日志系统
│   │   ├── manager.go        # 单应用进程生命周期管理
│   │   ├── multimanager.go   # 多应用管理器（包装 Manager）
│   │   └── logger.go         # 双重存储（内存+文件 + 轮转）
│   ├── archive/          # 压缩包处理
│   │   └── archive.go        # ZIP/TAR/GZ 解压
│   ├── auth/             # 认证
│   │   └── auth.go           # Bearer Token + 时序攻击防护
│   ├── handler/          # HTTP API
│   │   └── handler.go        # 部署/状态/日志等端点
│   └── state/            # 状态持久化
│       ├── persist.go        # 原子写入 + SHA256
│       ├── sync.go           # 事件驱动同步
│       └── restore.go        # 自动恢复
└── client/
    └── deploy/           # 客户端部署
        └── deploy.go         # API 调用封装
```

## 状态持久化

### 数据流
```
runtime.Manager → EventNotifier → SyncManager → StateStore → state.json
                                      ↑
                              RestoreManager (启动时)
```

### 核心特性

| 特性 | 说明 |
|------|------|
| **原子写入** | 临时文件 + 重命名，防止写入中断损坏 |
| **校验和** | SHA256 验证数据完整性，损坏时自动从备份恢复 |
| **自动备份** | 每次写入前创建 `state.json.backup.YYYYMMDD-HHMMSS` |
| **事件驱动** | 100ms 延迟批量写入，减少 IO 开销 |
| **进程恢复** | Windows: `tasklist` 检查行数 > 1；Unix: `syscall.Signal(0)` |
| **状态清理** | 自动移除无效 PID，重置状态 |

### 恢复流程（服务器启动时）

1. **加载状态文件** → 验证校验和 → 尝试备份恢复
2. **恢复应用状态** → 检查 PID → 标记运行中/待重启/已停止
3. **清理无效状态** → 移除不存在的 PID
4. **同步到内存** → 通过事件更新 SyncManager
5. **自动重启** → 对标记为 `AutoRestart` 的已停止应用执行重启

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

### 双重存储
- **内存**：环形缓冲区，保留最近 1000 行
- **文件**：持久化到 `logs/{appName}/{type}-{executable}-{timestamp}.log`

### 日志轮转
- **自动轮转**：文件超过指定大小时自动分割
- **保留策略**：可配置保留文件数量（默认 10 个）
- **文件命名**：`app-20251231-150405.rotated.001.log`

### 日志类型
- `stdout` - 标准输出
- `stderr` - 标准错误
- `system` - 系统事件（启动/停止/重启）

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
}
```

### 认证防护
- Bearer Token 认证
- 时序攻击防护（恒定时间比较）
- 所有端点除 `/health` 外均需认证

## 关键约束

| 约束 | 说明 |
|------|------|
| **多应用** | MultiManager 支持同时管理多个应用 |
| **开发环境** | 不适合生产使用 |
| **工作目录** | 每次部署清空（按应用隔离） |
| **日志保留** | 内存 1000 行，文件支持轮转 |
| **重启延迟** | 固定 1 秒，防止快速失败循环 |
| **提取方式** | 文件直接解压到应用工作目录根部 |
