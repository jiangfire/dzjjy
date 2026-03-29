# 测试指南

> 文档更新：2026-03-21
> 说明：移除易过时的静态统计，统一以当前仓库 workflow 和测试命令为准

## 快速运行

```bash
# 所有测试
go test ./...

# 带覆盖率
go test -cover ./internal/server/...

# 详细输出
go test -v ./internal/server/auth/...

# 特定测试
go test -v ./internal/server/auth/... -run TestMiddleware_Authenticate_Success

# 生成 HTML 覆盖率报告
go test -coverprofile=coverage.out ./internal/server/...
go tool cover -html=coverage.out

# 竞态检测
go test -race ./internal/server/...
```

## 当前测试基线

- 本地快速校验：`go test ./...`
- CI 主校验：`go test -race -coverprofile=coverage.out ./...`
- 发布前校验：`go test -race ./...`
- 交叉平台验证：GitHub Actions 会在 Linux、Windows、macOS 上做构建冒烟检查
- 质量巡检：`Quality` workflow 会额外输出覆盖率摘要并校验 `go.mod` / `go.sum`

## 测试工具

### Testify
```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```
- `assert` - 失败继续
- `require` - 失败停止

## 最佳实践

### 1. Windows 兼容性
```go
// 使用 Go 二进制而非 shell 脚本
binaryPath := filepath.Join(workDir, name+".exe")
cmd := exec.Command("go", "build", "-o", binaryPath)
```

### 2. 目录隔离
```go
func TestMain(m *testing.M) {
    testDir, _ := os.MkdirTemp("", "dzjjy-test-*")
    defer os.RemoveAll(testDir)
    os.Exit(m.Run())
}
```

### 3. 表驱动测试
```go
tests := []struct {
    name  string
    input string
    want  bool
}{
    {"empty", "", false},
    {"valid", "test", true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        result := validate(tt.input)
        assert.Equal(t, tt.want, result)
    })
}
```

### 4. 并发测试
```go
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        logger.WriteLog("stdout", "msg")
    }(i)
}
wg.Wait()
```

### 5. 安全测试
```go
// 路径遍历防护测试
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

## 调试技巧

```bash
# 查看未覆盖函数
go tool cover -func=coverage.out | grep "0.0%"

# 只显示失败
go test -v ./... 2>&1 | grep FAIL

# 运行多次检查稳定性
go test -count=3 ./internal/server/...

# 查看具体测试输出
go test -v -run TestManager_Start ./internal/runtime/...
```

## 应该做的

- ✅ 测试所有公共函数
- ✅ 包括边界情况
- ✅ 测试并发访问
- ✅ 验证错误处理
- ✅ 清理测试数据
- ✅ 使用临时目录隔离
- ✅ 测试安全防护（路径遍历、命令注入等）

## 避免做的

- ❌ 依赖测试顺序
- ❌ 使用全局状态
- ❌ 硬编码路径
- ❌ 测试私有函数
- ❌ 过度 Mock
- ❌ 依赖外部命令（如 `echo`）

## 关键测试场景

### 🔴 必须测试（高风险）
- 进程自动重启循环（达到最大重启次数）
- 路径遍历攻击防护
- 并发部署冲突处理
- 上下文取消优雅关闭
- 日志内存泄漏检测

### 🟡 应该测试（中风险）
- 大文件日志处理
- 网络超时和重试
- 权限错误处理
- 磁盘空间不足
- 无效配置处理

### 🟢 可选测试（低风险）
- 性能基准测试
- 内存使用优化
- 启动时间测试

## CI/CD 集成

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

jobs:
  verify:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25.8'
          cache: true

      - name: Run tests with coverage
        run: |
          go test -race -coverprofile=coverage.out ./...
          go tool cover -html=coverage.out -o coverage.html

      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage-out
          path: coverage.out
          retention-days: 7
```

## 测试依赖

```go
// go.mod
require (
    github.com/stretchr/testify v1.11.1
)
```

## 注意事项

**测试环境清理**：
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

**并发测试不稳定**：
- 使用 `time.Sleep()` 控制时序
- 增加重试机制
- 使用 `chan` 同步 goroutine

**资源泄漏**：
- 每个测试独立清理
- 使用 `defer` 确保清理
- 监控系统进程列表
