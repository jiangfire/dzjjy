# 发布说明

> 文档更新：2026-10-10
> 最新正式版本：`v1.1.0`

## 当前发布基线

- 发布分支：`main`
- 发布触发：推送语义化版本标签，例如 `v1.1.0`
- 发布 workflow：`.github/workflows/release.yml`
- 当前 workflow Go 版本：`1.26.9`
- 当前最新正式标签：`v1.1.0`
- `v1.1.0` 对应提交：`5bd16235d41b5720df7cba87576024e931808e2c`

## CI/CD 概览

仓库共有 4 个 workflow：

| Workflow | 触发 | 内容 |
|----------|------|------|
| `CI` | `push` 到 `main/develop`，或针对它们的 PR | golangci-lint、`go test -race`、govulncheck、gosec、Linux/Windows/macOS 构建验证 |
| `Release` | 推送语义化版本标签 | 发布前再次 lint/test/security 检查，生成 `dist/` 产物、`checksums.txt` 与压缩包，创建 GitHub Release |
| `Tag Validation` | 任意 tag push | 校验标签是否符合语义化版本规则 |
| `Quality` | 手动执行或每周一 02:00 UTC 定时 | 覆盖率报告、`go.mod`/`go.sum` 一致性检查、关键文档存在性检查 |

Action 版本基线：

- Checkout：`actions/checkout@v5`
- Go：`actions/setup-go@v6`
- Lint：`golangci/golangci-lint-action@v9`

`Quality` 的 Docs Check 任务会断言以下文件存在，删除它们会导致 CI 失败：

```text
README.md
README.en.md
docs/ARCHITECTURE.md
docs/DEVELOPMENT.md
docs/TESTING.md
docs/RELEASE.md
```

## 本地发布检查

```bash
make fmt
make lint
make test
make build
```

如果要尽量贴近 GitHub Release workflow，建议补跑：

```bash
go test -race ./...
make release
make checksum
make package
```

或直接运行完整本地 CI 流程：

```bash
make ci
```

## 构建产物

- `build/dzjjy-server`
- `build/dzjjy-client`
- `dist/` 下的跨平台产物由 `make release` 生成
- `dist/checksums.txt` 由 `make checksum` 生成
- `dist/*.tar.gz` 由 `make package` 生成

## 建议发布流程

1. 确认本地位于 `main`，并且已经同步远端最新提交。
2. 运行 `make ci`。
3. 运行 `make release`、`make checksum`、`make package` 做发布前验证。
4. 检查 `README.md`、`README.en.md` 与本文件中的命令示例是否仍然有效。
5. 创建带注释的语义化标签，例如 `git tag -a v1.1.1 -m "Release v1.1.1"`。
6. 推送标签，例如 `git push origin v1.1.1`。
7. 在 GitHub Actions 中确认 `Tag Validation` 和 `Release` 两个 workflow 都成功。
8. 核对 GitHub Release 页面里的二进制、压缩包和 `checksums.txt`。
9. 记录版本号、提交号和变更摘要。

### 预发布版本

带 `-beta` / `-rc` 后缀的标签会在 GitHub Release 中标记为预发布：

```bash
git tag v1.2.0-beta.1
git push origin v1.2.0-beta.1
```

## 版本号建议

- 补丁版本：仅修复 bug、CI/CD、文档或安全问题，例如 `v1.1.1`
- 次版本：新增向后兼容功能，例如 `v1.2.0`
- 预发布：使用 `-beta.N` 或 `-rc.N` 后缀，例如 `v1.2.0-rc.1`

## 故障排查

### CI 失败

优先在本地复现：

```bash
go test -race ./...
golangci-lint run
govulncheck ./...
gosec ./...
```

### Release 失败

优先检查：

1. 标签格式是否是 `vX.Y.Z` 或 `vX.Y.Z-suffix`
2. 本地是否能完成下面这些命令

```bash
make release
make checksum
make package
```

### go.mod / go.sum 检查失败

```bash
go mod tidy
git diff go.mod go.sum
```

## 最佳实践

1. 先等 CI 通过，再打发布标签。
2. 预发布版本使用 `-beta.N` 或 `-rc.N`。
3. `Quality` 更适合周期性治理，不必在每次提交都重复跑。
4. 发布前优先核对 `.github/workflows/*.yml` 里的实际 Go 版本和 action 版本，避免文档与 workflow 漂移。

## 最近一次发布

### v1.1.0

- 发布时间：2026-03-29
- 标签：`v1.1.0`
- 提交：`5bd16235d41b5720df7cba87576024e931808e2c`
- 包含提交：
  - `1f19ccb` feat: enhance test coverage and robustness
  - `5bd1623` fix: make handler tests platform-agnostic
- 主要内容：
  - 补充 70+ 测试用例，覆盖率从约 58% 提升到约 78%
  - 修复 start/stop 事件竞态、进程退出资源清理、关闭时状态持久化
  - `Close()` 增加并发安全保护
  - 增强压缩包与日志模块的边界处理

> 注：v1.1.0 之后 `main` 分支继续推进了 Phase 0-2 声明式运行时、TTL、Docker 后端等改动，尚未打新标签。

## 相关文件

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.github/workflows/tag-check.yml`
- `.github/workflows/quality.yml`
- `README.md`
- `README.en.md`
- `docs/TESTING.md`
