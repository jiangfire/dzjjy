# 状态持久化模块

> 📌 本文档：`docs/STATE_PERSISTENCE.md`
> 🔙 返回导航：[docs/README.md](README.md)

## 概述

状态持久化模块为 dzjjy 服务器提供应用状态的持久化存储和自动恢复功能。当服务器重启或进程崩溃时，可以自动恢复之前的应用状态。

---

## 🏗️ 架构设计

### 核心组件

```
internal/server/state/
├── types.go          # 数据结构定义
├── persist.go        # 持久化引擎（原子写入、校验和）
├── sync.go           # 状态同步管理器（事件驱动）
├── restore.go        # 恢复管理器（PID验证、进程检查）
├── adapter.go        # 事件适配器
└── state_test.go     # 单元测试
```

### 数据流

```
runtime.Manager → EventNotifier → SyncManager → StateStore → state.json
                                      ↑
                                  RestoreManager
```

---

## 🚀 使用方法

### 启动服务器

```bash
# 启用状态持久化
./dzjjy-server.exe -token your-token -state ./state.json

# 不启用（默认行为）
./dzjjy-server.exe -token your-token
```

### 状态文件格式

```json
{
  "version": "1.0.0",
  "timestamp": 1704067200,
  "checksum": "a1b2c3d4e5f6...",
  "data": {
    "apps": {
      "runtime-python3": {
        "config": {
          "type": "runtime",
          "work_dir": "./workspace",
          "executable": "python3",
          "entry": "app.py",
          "args": "--port 8000",
          "auto_restart": true,
          "max_restarts": 10
        },
        "pid": 12345,
        "start_time": 1704067200,
        "restart_count": 2,
        "status": "running",
        "work_path": "./workspace",
        "log_path": "./logs/app.log"
      }
    }
  }
}
```

---

## ✨ 功能特性

### 1. 原子写入
- 使用临时文件 + 重命名确保写入原子性
- 防止写入过程中断导致文件损坏

### 2. 校验和验证
- SHA256 校验和保证数据完整性
- 自动检测文件损坏并尝试从备份恢复

### 3. 自动备份
- 每次持久化前自动创建备份
- 备份文件名格式：`state.json.backup.20231231-150405`

### 4. 事件驱动同步
- 通过事件更新状态，避免直接修改
- 100ms 延迟批量写入，减少IO开销

### 5. 进程恢复
- 自动检测 PID 是否有效
- Windows: 使用 `tasklist` 命令，检查行数 > 1
- Unix: 使用 `syscall.Signal(0)`

### 6. 状态清理
- 自动清理无效 PID
- 将运行状态重置为停止状态

---

## 🔄 恢复流程

服务器启动时的恢复流程：

1. **加载状态文件** - 检查文件、验证校验和、尝试备份恢复
2. **恢复应用状态** - 检查 PID，标记为运行中/待重启/已停止
3. **清理无效状态** - 移除不存在的 PID
4. **同步到内存** - 通过事件更新 SyncManager

---

## 🔒 线程安全

- `StateStore`: `sync.RWMutex` 保护文件操作
- `SyncManager`: `sync.RWMutex` 保护内存状态
- 事件队列：带缓冲的 channel (100个事件)

---

## ⚡ 性能考虑

- **延迟持久化**: 100ms 批量写入
- **手动触发**: 关键操作后立即持久化
- **备份保留**: 需要手动清理

---

## ⚠️ 限制

- **单应用**: 仅支持管理单个应用的状态
- **开发环境**: 不适合生产环境的高并发场景
- **备份保留**: 不自动清理旧备份文件

---

## 📊 测试覆盖

```
✓ TestStateStore_PersistAndLoad        # 持久化和加载
✓ TestStateStore_ChecksumValidation    # 校验和验证
✓ TestStateStore_Backup                # 备份功能
✓ TestStateStore_Clear                 # 清除功能
✓ TestSyncManager_OnAppEvent           # 事件同步
✓ TestRestoreManager_Restore           # 恢复流程
✓ TestRestoreManager_Cleanup           # 状态清理
✓ TestStateStore_AtomictWrite          # 原子写入
```

---

## 🔧 关键 Bug 修复记录

### Bug 1: 校验和验证逻辑错误
**问题**: `jsonData[:len(jsonData)-2]` 错误地假设校验和占2个字符
**修复**: 正确的校验和验证流程

```go
// 修复前
if !s.verifyChecksum(jsonData[:len(jsonData)-2], stateFile.Checksum) { ... }

// 修复后
storedChecksum := stateFile.Checksum
stateFile.Checksum = ""
dataWithoutChecksum, json.MarshalIndent(stateFile, "", "  ")
if !s.verifyChecksum(dataWithoutChecksum, storedChecksum) { ... }
```

### Bug 2: Windows 进程检查错误
**问题**: `tasklist` 输出解析错误，总是返回 true
**修复**: 检查输出行数而非长度

```go
// 修复前
return len(output) > 0

// 修复后
lines := 0
for i := 0; i < len(outputStr); i++ {
    if outputStr[i] == '\n' {
        lines++
    }
}
return lines > 1
```

### Bug 3: Restart() 死锁
**问题**: 持有写锁时调用需要读锁的 `GetPID()`
**修复**: 直接访问字段

```go
// 修复前
oldPID := m.GetPID()  // 死锁！

// 修复后
oldPID := 0
if m.cmd != nil && m.cmd.Process != nil {
    oldPID = m.cmd.Process.Pid
}
```

### Bug 4: Stop() 与监控 goroutine 死锁
**问题**: Stop() 不等待监控 goroutine 完成
**修复**: 添加 `monitorDone` channel 同步

```go
// Manager 新增字段
monitorDone chan struct{}

// waitProcess() 和 monitor() 中
defer close(m.monitorDone)

// Stop() 中
<-m.monitorDone  // 等待完成
```

---

## 📁 相关文件

### 新增文件
- `internal/server/state/types.go`
- `internal/server/state/persist.go`
- `internal/server/state/sync.go`
- `internal/server/state/restore.go`
- `internal/server/state/adapter.go`
- `internal/server/state/state_test.go`

### 修改文件
- `cmd/server/main.go`: 添加 `-state` 参数
- `internal/server/handler/handler.go`: 集成状态持久化
- `internal/server/runtime/runtime.go`: 添加事件通知接口

---

## 💡 开发注意事项

### 关键设计决策

1. **事件驱动架构**: 解耦 Manager 和 State 模块
2. **延迟持久化**: 平衡性能和数据安全
3. **原子操作**: 确保数据一致性
4. **错误恢复**: 备份机制保证可用性
5. **死锁预防**: 严格的锁管理策略

### 未来扩展

1. 考虑添加状态文件大小限制
2. 实现自动备份清理策略
3. 添加状态恢复的详细日志
4. 考虑支持多应用状态管理

---

**最后更新**: 2025-12-31 | **版本**: 1.0.0
