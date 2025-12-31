# dzjjy 未来规划

> 📌 本文档：`docs/PLAN.md`
> 🔙 返回导航：[docs/README.md](README.md)

## 📋 概述

本文档记录 dzjjy 项目的未来扩展方向和高级功能规划。

---

## ✅ 已完成功能

**核心功能**：
- ✅ 进程管理（启动/停止/重启/监控）
- ✅ 文件上传和部署（ZIP/TAR/GZ 支持）
- ✅ 日志管理（内存缓存 + 文件持久化）
- ✅ 安全认证（Token 认证 + 时序攻击防护）
- ✅ 状态持久化（原子写入 + 自动恢复）

详见：[README.md](README.md) - 项目概述

---

## 🔧 未来扩展方向

### 1. 多应用管理

**目标**：支持同时管理多个应用进程

**核心需求**：
- 应用注册表，支持多应用注册和管理
- 每个应用独立工作目录、日志文件
- 并发安全的多应用操作
- 向后兼容现有单应用API

**关键设计**：
```go
type AppRegistry struct {
    apps map[string]*AppEntry
    state *state.StateStore
}

type AppEntry struct {
    manager  *runtime.Manager
    workPath string
}
```

---

### 2. 插件扩展系统

**目标**：提供可扩展的插件机制

**核心需求**：
- 插件接口定义和生命周期管理
- 安全沙箱隔离
- 错误隔离（插件崩溃不影响主系统）
- 支持内置插件和Webhook

**插件接口**：
```go
type Plugin interface {
    Name() string
    Version() string
    Init(config map[string]any) error
    OnDeploy(appName string, config *DeployConfig) error
    OnStart(appName string, pid int) error
    OnStop(appName string, reason string) error
    OnRestart(appName string, count int) error
    OnEvent(event Event)
}
```

---

### 3. 高级功能

| 功能 | 描述 | 优先级 |
|------|------|--------|
| **日志轮转** | 自动清理旧日志文件 | 中 |
| **配置文件** | 支持 YAML/JSON 配置 | 中 |
| **环境变量** | 支持设置环境变量 | 低 |
| **健康检查** | 定期检查应用健康 | 低 |
| **Web UI** | 提供 Web 管理界面 | 低 |
| **指标收集** | CPU、内存使用率 | 低 |
| **集群模式** | 支持多实例部署 | 低 |

---

## ⚠️ 注意事项

### 设计原则
1. **保持简单**：避免过度设计，专注核心功能
2. **向后兼容**：新功能不应破坏现有API
3. **充分测试**：每个功能独立测试后再集成
4. **文档完善**：新功能必须有对应文档

### 风险评估
- **死锁风险**：统一锁层级，避免嵌套
- **资源泄漏**：严格目录清理，监控资源使用
- **安全漏洞**：插件系统需要严格沙箱
- **性能开销**：异步执行 + 熔断机制

---

## 🚀 建议实施路径

1. **评估需求**：明确实际需要的功能
2. **小步迭代**：每次只实现一个功能
3. **充分测试**：确保稳定性后再继续
4. **文档更新**：完成后立即更新文档

---

**最后更新**: 2025-12-31 | **版本**: 1.0.0
