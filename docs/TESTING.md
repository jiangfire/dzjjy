# 测试文档

> 📌 本文档：`docs/TESTING.md`
> 🔙 返回导航：[docs/README.md](README.md)
> 📖 主文档：[README.md](../README.md)

本项目包含全面的单元测试和集成测试，确保代码质量和可靠性。

## 测试覆盖情况

| 模块 | 测试文件 | 测试用例数 | 覆盖率 |
|------|---------|-----------|--------|
| Archive (归档处理) | `internal/server/archive/archive_test.go` | 18 | 81.1% |
| Auth (认证) | `internal/server/auth/auth_test.go` | 14 | 100% |
| Runtime Logger (日志管理) | `internal/server/runtime/logger_test.go` | 16 | 100% |
| Runtime Manager (进程管理) | `internal/runtime/runtime_test.go` | 14 | 全面测试 |

**总测试用例：62+**

## 运行测试

### 基本命令

```bash
# 运行所有测试
go test ./...

# 运行特定模块测试
go test ./internal/server/archive/...
go test ./internal/server/auth/...
go test ./internal/server/runtime/...
go test ./internal/runtime/...

# 查看测试覆盖率
go test -cover ./internal/server/...

# 生成详细覆盖率报告
go test -coverprofile=coverage.out ./internal/server/...
go tool cover -html=coverage.out
```

### 高级用法

```bash
# 运行特定测试
go test -v ./internal/server/auth/... -run TestMiddleware_Authenticate_Success

# 运行多次以检查稳定性
go test -count=3 ./internal/server/...

# 查看测试输出
go test -v ./internal/server/runtime/... 2>&1 | grep -E "(PASS|FAIL|RUN)"
```

## 测试特性详解

### Archive 模块测试 (`archive_test.go`)

**18个测试用例，覆盖81.1%代码**

- ✅ **格式支持测试**
  - ZIP 格式解压
  - TAR 格式解压
  - TAR.GZ 格式解压
  - GZ 格式解压

- ✅ **安全测试**
  - 路径遍历攻击防护 (`../etc/passwd`)
  - 恶意文件检测
  - 绝对路径处理
  - 符号链接处理

- ✅ **边界情况**
  - 空压缩包
  - 损坏的压缩文件
  - 大文件处理 (1MB)
  - 文件覆盖行为
  - 混合内容（多层目录）

- ✅ **文件系统测试**
  - 目录自动创建
  - 文件权限保留
  - 嵌套目录结构

### Auth 模块测试 (`auth_test.go`)

**14个测试用例，100%覆盖率**

- ✅ **认证成功场景**
  - 正确 token 验证
  - Token 前后空格修剪
  - 特殊字符 token
  - 长 token
  - Unicode token

- ✅ **认证失败场景**
  - 缺少 Authorization 头
  - 错误的头格式（无 Bearer、无空格等）
  - 错误的 token
  - 空 token
  - 大小写不匹配

- ✅ **安全测试**
  - 时序攻击防护（使用 `crypto/subtle.ConstantTimeCompare`）
  - 多请求并发处理
  - 响应头保留

### Logger 模块测试 (`logger_test.go`)

**16个测试用例，100%覆盖率**

- ✅ **生命周期测试**
  - 启动/停止
  - 多次停止（安全）
  - 停止后写入

- ✅ **日志功能测试**
  - 写入不同类型的日志（stdout/stderr/system）
  - 获取指定行数
  - 获取所有日志
  - 空消息处理
  - 时间戳验证

- ✅ **性能和安全测试**
  - 内存限制（1000行）
  - 并发写入安全（10个 goroutine，每个写10行）
  - 文件同步验证
  - 文件名清理（防路径遍历）

- ✅ **输出捕获测试**
  - stdout/stderr 捕获
  - Done channel 关闭验证

### Runtime Manager 测试 (`runtime_test.go`)

**14个测试用例，全面测试进程管理**

- ✅ **基本生命周期**
  - 启动/停止
  - 重启功能
  - 状态查询

- ✅ **自动重启机制**
  - 崩溃后自动重启
  - 重启次数限制
  - 重启延迟（1秒）

- ✅ **并发安全**
  - 手动停止与自动重启并发
  - 多个并发操作
  - 线程安全状态访问

- ✅ **错误处理**
  - 无效类型处理
  - 已运行时启动检测
  - 未运行时停止/重启

- ✅ **运行时支持**
  - Runtime 类型测试
  - 日志捕获
  - 信息查询

## 测试工具

### 使用 Testify

本项目使用 [testify](https://github.com/stretchr/testify) 作为测试框架：

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

**assert vs require:**
- `assert` - 失败时继续执行后续断言
- `require` - 失败时立即停止测试

### 测试辅助函数

`test/helpers.go` 提供了常用的测试工具：

```go
// 创建临时目录
dir := test.SetupTestDir(t)
defer test.CleanupTestDir(t, dir)

// 创建测试 ZIP 文件
zipData := test.CreateTestZip(t, map[string]string{
    "file.txt": "content",
})

// 创建测试应用（Go 二进制）
appPath := test.CreateTestApp(t, workDir)
```

## 测试最佳实践

### 1. Windows 兼容性

```go
// 使用 Go 二进制而非 shell 脚本
func createTestBinary(t *testing.T, workDir, name string) string {
    goCode := `package main
import "fmt"
func main() { fmt.Println("test") }`

    // 编译或创建可执行文件
    // ...
}
```

### 2. 目录隔离

```go
func TestMain(m *testing.M) {
    // 创建独立的测试目录
    testDir, _ := os.MkdirTemp("", "test-*")
    defer os.RemoveAll(testDir)

    code := m.Run()
    os.Exit(code)
}
```

### 3. 并发安全

```go
// 测试并发写入
func TestLogger_ConcurrentWrite(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            logger.WriteLog("stdout", "message")
        }(i)
    }
    wg.Wait()
}
```

### 4. 边界测试

```go
tests := []struct {
    name  string
    input string
    expect string
}{
    {"empty", "", "unknown"},
    {"special chars", "app<>\"|?*chars", "app_chars"},
    {"long name", strings.Repeat("a", 150), strings.Repeat("a", 100)},
}
```

### 5. 时间相关测试

```go
// 添加延迟确保时间差异
before := time.Now()
time.Sleep(1 * time.Millisecond)
logger.WriteLog("test")

logs := logger.GetLogs(1)
assert.True(t, logs[0].Timestamp.After(before))
```

## 测试覆盖率分析

### 查看覆盖率报告

```bash
# 生成覆盖率数据
go test -coverprofile=coverage.out ./internal/server/...

# 查看文本报告
go tool cover -func=coverage.out

# 查看 HTML 报告（浏览器打开）
go tool cover -html=coverage.out
```

### 覆盖率解读

- **80%+**: 优秀，核心逻辑已覆盖
- **100%**: 完美，所有分支和边界情况已测试

### 提升覆盖率

1. **Handler 模块** - 需要集成测试（复杂度高）
2. **Client 模块** - 需要 HTTP mock 测试
3. **Main 函数** - 需要端到端测试

## 持续集成

### CI 配置建议

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - run: go test ./... -cover
```

## 常见问题

### Q: 测试偶尔失败？

A: Windows 下进程终止可能有竞争条件，重试即可。核心功能是稳定的。

### Q: 如何测试 Handler？

A: Handler 涉及文件上传和进程管理，建议：
1. 使用 httptest.ResponseRecorder 模拟 HTTP
2. 使用临时目录隔离文件操作
3. 使用短生命周期的测试应用

### Q: 覆盖率为什么是 0%？

A: 某些包（如 handler）没有测试文件，或者测试在不同包中（如 runtime 测试在 internal/runtime）。

## 总结

本项目建立了完善的测试体系：
- ✅ 62+ 测试用例
- ✅ 核心模块 80%+ 覆盖率
- ✅ Auth 和 Logger 达到 100%
- ✅ Windows/Linux 兼容
- ✅ 并发安全验证
- ✅ 安全漏洞防护测试

测试是代码质量的第一道防线！
