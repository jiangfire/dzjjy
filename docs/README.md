# dzjjy 文档导航

> 📌 本文档：`docs/README.md`
> 📖 根目录 README：[../README.md](../README.md) - 完整用户手册和 API 文档

---

## 📚 文档导航

| 文档 | 说明 | 适用人群 |
|------|------|----------|
| **[README.md](#)** | 项目概述、核心功能、快速开始 | 所有人 |
| **[TESTING.md](TESTING.md)** | 测试指南、覆盖率、编写规范 | 开发者、测试者 |
| **[STATE_PERSISTENCE.md](STATE_PERSISTENCE.md)** | 状态持久化模块详解 | 开发者 |
| **[PLAN.md](PLAN.md)** | 未来规划和扩展方向 | 高级开发者 |

---

## 🎯 项目概述

dzjjy (简易部署服务) 是一个用于开发环境的快速部署工具，帮助开发者快速将应用部署到服务器，无需手动停止-复制文件-启动服务。

**核心目标**：实现类似 PM2 的进程守护管理器，帮助管理应用程序并保持 24/7 全天候在线。

---

## ✅ 已完成功能

### 1. 进程管理
- ✅ **进程分类**：`exec`（可执行程序）和 `runtime`（需要运行时的程序）
- ✅ **自动重启**：进程崩溃后自动重启
- ✅ **重启限制**：可配置最大重启次数（0 = 无限制）
- ✅ **延迟重启**：崩溃后等待 1 秒再重启
- ✅ **优雅停止**：通过 context 取消信号优雅停止
- ✅ **手动重启**：支持手动重启命令
- ✅ **实时监控**：监控进程状态、PID、运行时间、重启次数

### 2. 文件上传和部署
- ✅ **单文件上传**：支持上传单个文件（源码、可执行文件等）
- ✅ **压缩文件支持**：ZIP、TAR、TAR.GZ、GZ 格式
- ✅ **自动解压**：检测格式并自动解压到工作目录
- ✅ **安全防护**：防止路径穿越攻击
- ✅ **自动清理**：解压后删除压缩文件

### 3. 日志管理
- ✅ **实时捕获**：自动捕获 stdout 和 stderr
- ✅ **系统日志**：记录启动、停止、重启等事件
- ✅ **双重存储**：内存缓存（1000行）+ 文件持久化
- ✅ **日志分类**：stdout、stderr、system
- ✅ **快速查询**：从内存快速获取最近日志
- ✅ **结构化日志**：使用 slog，JSON/文本格式

### 4. 安全认证
- ✅ **Token 认证**：简单的 Bearer Token 认证
- ✅ **中间件**：统一的认证中间件
- ✅ **时序攻击防护**：使用 `crypto/subtle.ConstantTimeCompare`

### 5. 状态持久化
- ✅ **原子写入**：临时文件 + 重命名
- ✅ **校验和**：SHA256 数据完整性验证
- ✅ **自动备份**：带时间戳的备份文件
- ✅ **事件驱动**：100ms 延迟批量持久化
- ✅ **进程恢复**：PID 验证和自动清理
- ✅ **跨平台**：Windows/Unix 兼容

### 6. API 接口
- ✅ `POST /api/v1/deploy` - 部署应用
- ✅ `POST /api/v1/stop` - 停止应用
- ✅ `POST /api/v1/restart` - 重启应用
- ✅ `POST /api/v1/start` - 启动应用
- ✅ `GET /api/v1/status` - 查询状态
- ✅ `GET /api/v1/logs` - 查询日志
- ✅ `GET /health` - 健康检查

### 7. 命令行工具
- ✅ **服务端**：`dzjjy-server -token <token> -port 8080 -state ./state.json`
- ✅ **客户端**：`deploy`, `status`, `logs`, `restart`, `stop`

---

## 🚀 快速开始

### 构建

```bash
# 构建服务端
go build -o dzjjy-server ./cmd/server

# 构建客户端
go build -o dzjjy-client ./cmd/client

# Windows
go build -o dzjjy-server.exe ./cmd/server && go build -o dzjjy-client.exe ./cmd/client
```

### 运行服务端

```bash
# 最小化启动（需要 token）
./dzjjy-server -token <your-token>

# 完整配置
./dzjjy-server -token <token> -port 8080 -upload ./uploads -work ./workspace -log ./logs -state ./state.json
```

### 客户端命令

```bash
# 部署应用
./dzjjy-client deploy -server <url> -token <token> -file <file> -type <exec|runtime> -executable <cmd> [-entry <file>] [-auto-restart] [-max-restarts <n>]

# 查询状态
./dzjjy-client status -server <url> -token <token>

# 查看日志
./dzjjy-client logs -server <url> -token <token> [-lines <n>]

# 重启/停止
./dzjjy-client restart -server <url> -token <token>
./dzjjy-client stop -server <url> -token <token>
```

---

## 🏗️ 架构概览

### 核心模块

```
internal/server/
├── runtime/       # 进程和日志管理
├── archive/       # 压缩文件处理
├── auth/          # Token 认证
├── handler/       # HTTP 处理器
└── state/         # 状态持久化

internal/client/
└── deploy/        # 客户端部署逻辑
```

### 应用类型

**简单分类，不细分语言**：

- **`exec`**: 直接运行的可执行文件（二进制、shell 脚本等）
- **`runtime`**: 需要运行时的程序（Python、Java、NodeJS 等）

**参数说明**：
- `executable` → `entry`（运行时参数 + 入口文件） → `args`（应用参数）

**示例**：
- Python: `python3 app.py` → executable=`python3`, entry=`app.py`
- Java JAR: `java -jar app.jar --port=8000` → executable=`java`, entry=`-jar app.jar`, args=`--port=8000`
- Python 模块: `python3 -m uvicorn main:app --host 0.0.0.0` → executable=`python3`, entry=`-m uvicorn main:app`, args=`--host 0.0.0.0`

---

## 📊 测试覆盖

| 模块 | 测试用例 | 覆盖率 | 状态 |
|------|---------|--------|------|
| Archive | 18 | 81.1% | ✅ |
| Auth | 14 | 100% | ✅ |
| Logger | 16 | 100% | ✅ |
| Manager | 14 | 全面 | ✅ |
| State | 8 | 全面 | ✅ |
| **总计** | **70+** | **~70%** | **优秀** |

运行测试：
```bash
go test ./...                    # 所有测试
go test -cover ./internal/...    # 带覆盖率
go test -v ./internal/server/auth/...  # 详细输出
```

---

## 🎯 设计原则

### Unix 设计哲学
- **模块化**：写简单的程序，用好的接口连接
- **清晰性**：清楚透明的算法比"高明"的代码更好
- **简单性**：专注核心功能，避免过度设计
- **可扩展性**：预留扩展空间

### Go 语言优势
- **Goroutine**：每个进程监控在独立 goroutine 中
- **Context**：优雅控制进程生命周期
- **Channel**：通过通信共享内存
- **RWMutex**：读写锁保护共享状态
- **编译型**：单一二进制，跨平台部署

---

## ⚠️ 注意事项

1. **仅限开发环境**：不适合生产环境
2. **Token 安全**：认证简单，请妥善保管
3. **数据覆盖**：上传会覆盖工作目录同名文件
4. **备份管理**：不自动清理旧备份，需手动管理
5. **日志增长**：建议定期清理旧日志文件
6. **单应用**：当前仅支持管理单个应用

---

## 🔗 相关链接

- **开发指南**：[../CLAUDE.md](../CLAUDE.md) - 代码规范和开发指南
- **GitHub**：https://github.com/jiangfire/dzjjy

---

**最后更新**: 2025-12-31 | **版本**: 1.0.0
