# 测试指南

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
```

## 测试统计

| 模块 | 用例 | 覆盖率 | 状态 |
|------|------|--------|------|
| Archive | 18 | 81.1% | ✅ |
| Auth | 14 | 100% | ✅ |
| Logger | 49 | 92.9%+ | ✅ |
| Manager | 14 | 全面 | ✅ |
| State | 8 | 全面 | ✅ |
| Handler | 60+ | 76.4% | ✅ |
| Client | 45+ | 89.8% | ✅ |
| **总计** | **200+** | **~75%** | **优秀** |

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

### 辅助函数 (test/helpers.go)
```go
dir := test.SetupTestDir(t)
defer test.CleanupTestDir(t, dir)
zipData := test.CreateTestZip(t, map[string]string{"main.go": "code"})
```

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

## 调试技巧

```bash
# 查看未覆盖函数
go tool cover -func=coverage.out | grep "0.0%"

# 只显示失败
go test -v ./... 2>&1 | grep FAIL

# 运行多次检查稳定性
go test -count=3 ./internal/server/...
```

## 注意事项

**应该做的**：
- ✅ 测试所有公共函数
- ✅ 包括边界情况
- ✅ 测试并发访问
- ✅ 验证错误处理
- ✅ 清理测试数据

**避免做的**：
- ❌ 依赖测试顺序
- ❌ 使用全局状态
- ❌ 硬编码路径
- ❌ 测试私有函数
- ❌ 过度 Mock
