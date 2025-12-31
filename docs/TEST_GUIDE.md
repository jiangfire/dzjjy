# 测试编写指南

> 📌 本文档：`docs/TEST_GUIDE.md`
> 🔙 返回导航：[docs/README.md](README.md)
> 📖 主文档：[README.md](../README.md)

本文档指导如何为 dzjjy 项目编写高质量的测试。

## 快速开始

### 运行现有测试

```bash
# 运行所有测试
go test ./...

# 运行并显示详细输出
go test -v ./...

# 运行特定模块
go test -v ./internal/server/auth/...

# 运行特定测试
go test -v ./internal/server/auth/... -run TestMiddleware_Authenticate_Success

# 查看覆盖率
go test -cover ./internal/server/...
```

### 生成覆盖率报告

```bash
# 生成覆盖率数据
go test -coverprofile=coverage.out ./internal/server/...

# 查看文本报告
go tool cover -func=coverage.out

# 查看 HTML 报告（浏览器）
go tool cover -html=coverage.out
```

## 测试文件结构

### 基本模板

```go
package yourpackage_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestMain 用于测试环境的全局设置和清理（可选）
func TestMain(m *testing.M) {
    // 测试前设置
    setup()

    // 运行测试
    code := m.Run()

    // 测试后清理
    teardown()

    os.Exit(code)
}

// 示例测试函数
func TestFunctionName(t *testing.T) {
    // Arrange - 准备测试数据
    input := "test"
    expected := "TEST"

    // Act - 执行被测函数
    result := strings.ToUpper(input)

    // Assert - 验证结果
    assert.Equal(t, expected, result)
}

// 表驱动测试
func TestFunctionName_TableDriven(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"normal", "hello", "HELLO"},
        {"empty", "", ""},
        {"numbers", "123", "123"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := strings.ToUpper(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

## 测试最佳实践

### 1. 使用 TestMain 进行全局设置

```go
var testDir string

func TestMain(m *testing.M) {
    var err error
    testDir, err = os.MkdirTemp("", "dzjjy-test-*")
    if err != nil {
        panic(err)
    }
    defer os.RemoveAll(testDir)

    code := m.Run()
    os.Exit(code)
}
```

### 2. 表驱动测试

```go
func TestAuthenticate(t *testing.T) {
    tests := []struct {
        name        string
        token       string
        expectError bool
    }{
        {"valid", "secret", false},
        {"empty", "", true},
        {"wrong", "bad", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            middleware := auth.NewMiddleware(tt.token)
            // ... 测试逻辑
        })
    }
}
```

### 3. 并发测试

```go
func TestConcurrentAccess(t *testing.T) {
    logger := runtime.NewLogger(testDir, "test")
    logger.Start()
    defer logger.Stop()

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            logger.WriteLog("stdout", "message")
        }(i)
    }
    wg.Wait()

    logs := logger.GetAllLogs()
    assert.Equal(t, 10, len(logs))
}
```

### 4. 边界测试

```go
func TestEdgeCases(t *testing.T) {
    tests := []struct {
        name  string
        input string
    }{
        {"empty", ""},
        {"null", "\x00"},
        {"unicode", "测试🔐"},
        {"very long", strings.Repeat("a", 10000)},
        {"special chars", "|;&`$()<>[]{}"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 测试边界情况
        })
    }
}
```

### 5. 安全测试

```go
func TestPathTraversal(t *testing.T) {
    maliciousPaths := []string{
        "../etc/passwd",
        "../../windows/system32",
        "/absolute/path",
        "file\\..\\..\\secret",
    }

    for _, path := range maliciousPaths {
        t.Run(path, func(t *testing.T) {
            // 验证路径被拒绝或清理
        })
    }
}
```

## 测试特定模块

### 测试 Archive 模块

```go
func TestArchive_Extract(t *testing.T) {
    // 创建测试 ZIP
    zipData := createTestZip(t, map[string]string{
        "file.txt": "content",
    })

    // 写入临时文件
    zipPath := filepath.Join(testDir, "test.zip")
    os.WriteFile(zipPath, zipData, 0644)

    // 解压
    destDir := filepath.Join(testDir, "dest")
    os.MkdirAll(destDir, 0755)
    err := archive.Extract(zipPath, destDir)

    require.NoError(t, err)
    assert.FileExists(t, filepath.Join(destDir, "file.txt"))
}
```

### 测试 Auth 模块

```go
func TestAuth_Authenticate(t *testing.T) {
    middleware := auth.NewMiddleware("secret")

    handler := func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }

    // 测试成功
    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer secret")
    w := httptest.NewRecorder()

    middleware.Authenticate(handler)(w, req)
    assert.Equal(t, http.StatusOK, w.Code)
}
```

### 测试 Logger 模块

```go
func TestLogger_WriteLog(t *testing.T) {
    logger := runtime.NewLogger(testDir, "test")
    require.NoError(t, logger.Start())
    defer logger.Stop()

    logger.WriteLog("stdout", "test message")

    logs := logger.GetLogs(1)
    require.Equal(t, 1, len(logs))
    assert.Equal(t, "stdout", logs[0].Type)
    assert.Equal(t, "test message", logs[0].Message)
}
```

### 测试 Runtime Manager

```go
func TestManager_Start_Stop(t *testing.T) {
    manager := runtime.NewManager(testDir)

    // 创建测试应用
    appPath := createTestBinary(t, testDir, "test")

    err := manager.Start("exec", testDir, appPath, "", "", false, 0)
    require.NoError(t, err)

    assert.True(t, manager.IsRunning())

    err = manager.Stop()
    require.NoError(t, err)

    assert.False(t, manager.IsRunning())
}
```

## 使用测试辅助函数

### test/helpers.go

```go
import "github.com/jiangfire/dzjjy/test"

func TestExample(t *testing.T) {
    // 创建临时目录
    dir := test.SetupTestDir(t)
    defer test.CleanupTestDir(t, dir)

    // 创建测试 ZIP
    zipData := test.CreateTestZip(t, map[string]string{
        "main.go": "package main",
    })

    // 创建测试应用
    appPath := test.CreateTestApp(t, dir)

    // 等待条件
    test.WaitFor(func() bool {
        return manager.IsRunning()
    }, 1000)
}
```

## 常见测试模式

### 1. 测试 HTTP 处理器

```go
func TestHandler_Endpoint(t *testing.T) {
    h := handler.NewHandler(uploadDir, workDir, logDir)

    // 准备请求
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    // ... 添加字段和文件
    writer.Close()

    req := httptest.NewRequest("POST", "/deploy", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    w := httptest.NewRecorder()

    // 执行
    h.Deploy(w, req)

    // 验证
    assert.Equal(t, http.StatusOK, w.Code)

    var resp api.Response
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.True(t, resp.Success)
}
```

### 2. 测试错误处理

```go
func TestErrorHandling(t *testing.T) {
    tests := []struct {
        name        string
        setup       func() error
        expectError bool
    }{
        {
            name: "invalid input",
            setup: func() error {
                return someFunction("")
            },
            expectError: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.setup()
            if tt.expectError {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### 3. 测试并发安全

```go
func TestConcurrency(t *testing.T) {
    obj := NewObject()

    // 并发读写
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            if id%2 == 0 {
                obj.Write(id)
            } else {
                obj.Read()
            }
        }(i)
    }
    wg.Wait()
}
```

### 4. 测试时间相关

```go
func TestTimestamp(t *testing.T) {
    before := time.Now()
    time.Sleep(1 * time.Millisecond) // 确保时间差异

    // 执行操作

    after := time.Now()

    logs := logger.GetLogs(1)
    assert.True(t, logs[0].Timestamp.After(before))
    assert.True(t, !logs[0].Timestamp.After(after))
}
```

## 调试测试

### 查看测试输出

```bash
# 详细输出
go test -v ./internal/server/auth/...

# 只显示失败
go test -v ./... 2>&1 | grep FAIL

# 显示测试时间
go test -v -timeout 30s ./...
```

### 调试失败的测试

```go
func TestDebug(t *testing.T) {
    // 使用 t.Logf 打印调试信息
    t.Logf("Input: %v", input)
    t.Logf("Expected: %v", expected)

    result := function(input)
    t.Logf("Result: %v", result)

    assert.Equal(t, expected, result)
}
```

## 性能测试

### 基准测试

```go
func BenchmarkLogger(b *testing.B) {
    logger := runtime.NewLogger(testDir, "bench")
    logger.Start()
    defer logger.Stop()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        logger.WriteLog("stdout", "benchmark message")
    }
}
```

运行基准测试：
```bash
go test -bench=. ./internal/server/runtime/...
```

## 测试覆盖率目标

| 模块 | 当前 | 目标 | 状态 |
|------|------|------|------|
| Archive | 81.1% | 85% | ✅ |
| Auth | 100% | 100% | ✅ |
| Logger | 100% | 100% | ✅ |
| Manager | 全面 | 全面 | ✅ |
| Handler | 0% | 70% | 📋 |
| Client | 0% | 60% | 📋 |

## 测试注意事项

### ✅ 应该做的

1. **测试所有公共函数**
2. **包括边界情况**
3. **测试并发访问**
4. **验证错误处理**
5. **清理测试数据**
6. **使用描述性名称**
7. **保持测试独立**

### ❌ 避免做的

1. **不要依赖测试顺序**
2. **不要使用全局状态**
3. **不要硬编码路径**
4. **不要忽略错误**
5. **不要测试私有函数**
6. **不要过度 Mock**
7. **不要测试第三方库**

## 持续集成

### GitHub Actions 示例

```yaml
name: Test

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        go: ['1.23', '1.24']

    steps:
      - uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}

      - name: Run tests
        run: go test ./... -cover -timeout 5m

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out
```

## 总结

编写好的测试需要：
1. ✅ 理解被测代码
2. ✅ 覆盖各种场景
3. ✅ 保持测试简洁
4. ✅ 使用合适的工具
5. ✅ 定期运行测试

记住：测试是代码质量的保障！
