# dzjjy 文档导航

欢迎阅读 dzjjy（简易部署服务）的文档。

## 📚 文档分类

### 📖 项目文档（核心）

| 文档 | 说明 | 适用人群 |
|------|------|----------|
| [PROJECT_PROMPT.md](PROJECT_PROMPT.md) | 项目需求文档和功能说明 | 所有人 |
| [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) | 实现计划和架构设计 | 开发者 |
| [PLAN.md](PLAN.md) | 复杂系统实现计划 | 高级开发者 |

### 🧪 测试文档

| 文档 | 说明 | 适用人群 |
|------|------|----------|
| [TESTING.md](TESTING.md) | 测试覆盖和运行指南 | 开发者、测试者 |
| [COVERAGE.md](COVERAGE.md) | 详细的覆盖率报告 | 开发者、质量保证 |
| [TEST_GUIDE.md](TEST_GUIDE.md) | 如何编写测试 | 新贡献者 |

### 📂 其他文档（根目录）

- **README.md** - 项目概述、快速开始、API 文档
- **CLAUDE.md** - 开发指南和代码规范

## 🚀 快速导航

### 我想了解...
- **项目做什么？** → [PROJECT_PROMPT.md](PROJECT_PROMPT.md)
- **如何使用？** → [README.md](../README.md)（根目录）
- **如何运行测试？** → [TESTING.md](TESTING.md)
- **测试覆盖率？** → [COVERAGE.md](COVERAGE.md)
- **如何写测试？** → [TEST_GUIDE.md](TEST_GUIDE.md)
- **未来规划？** → [PLAN.md](PLAN.md)

## 📊 测试概览

| 模块 | 测试用例 | 覆盖率 | 状态 |
|------|---------|--------|------|
| Archive | 18 | 81.1% | ✅ |
| Auth | 14 | 100% | ✅ |
| Logger | 16 | 100% | ✅ |
| Manager | 14 | 全面 | ✅ |
| **总计** | **62+** | **~70%** | **优秀** |

## 🎯 核心功能

### 进程管理
- 自动重启（类似 PM2）
- 进程监控和状态查询
- 优雅停止

### 日志管理
- 实时捕获 stdout/stderr
- 内存缓存（1000 行）
- 文件持久化

### 安全特性
- 路径遍历防护
- 时序攻击防护
- 输入验证

## 🔧 开发指南

### 新增功能时的测试要求

1. **编写单元测试**
   ```bash
   # 在同目录创建 *_test.go
   internal/server/yourmodule/yourmodule_test.go
   ```

2. **覆盖核心场景**
   - 正常流程
   - 错误处理
   - 边界情况
   - 并发安全

3. **运行测试**
   ```bash
   go test -v ./internal/server/yourmodule/...
   go test -cover ./internal/server/yourmodule/...
   ```

### 测试工具

使用 [testify](https://github.com/stretchr/testify)：
- `require` - 失败即停止
- `assert` - 失败继续

## 📝 文档贡献

发现文档问题或有改进建议？
1. 在 GitHub 提交 Issue
2. 创建 Pull Request 更新文档
3. 参考 [TEST_GUIDE.md](TEST_GUIDE.md) 编写新测试

## 📞 联系

- 项目地址：https://github.com/jiangfire/dzjjy
- 问题反馈：GitHub Issues

---

**最后更新**: 2025-12-31
**版本**: 1.0.0
