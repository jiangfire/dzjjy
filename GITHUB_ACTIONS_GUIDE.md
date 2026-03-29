# GitHub Actions 快速使用指南

> 文档更新：2026-03-21
> 当前正式版本：`v1.0.1`

## 概述

当前仓库的 GitHub CI/CD 分为 4 个 workflow：

1. `CI`：主校验，面向日常提交和 PR
2. `Release`：版本标签触发的正式发布
3. `Tag Validation`：标签格式校验
4. `Quality`：手动或定时运行的质量巡检

## 工作流说明

当前 workflow 基线：

- Go 版本：CI / Release / Quality 使用 `1.25.8`
- Checkout Action：`actions/checkout@v5`
- Go Action：`actions/setup-go@v6`
- Lint Action：`golangci/golangci-lint-action@v9`

### CI

- 触发：`push` 到 `main/develop`，或针对 `main/develop` 的 PR
- 内容：
  - `golangci-lint`
  - `go test -race`
  - `govulncheck`
  - `gosec`
  - Linux / Windows / macOS 构建验证

### Release

- 触发：推送语义化版本标签，例如 `v1.2.3`、`v1.2.3-beta.1`
- 内容：
  - 发布前再次执行 lint / test / security check
  - 生成 `dist/` 下的多平台构建产物
  - 生成 `checksums.txt`
  - 生成压缩包
  - 自动创建 GitHub Release

### Tag Validation

- 触发：任意 tag push
- 内容：
  - 检查标签是否符合语义化版本规则

### Quality

- 触发：手动执行，或每周定时巡检
- 内容：
  - 覆盖率报告
  - `go.mod` / `go.sum` 一致性检查
  - 关键文档存在性检查

## 快速开始

### 1. 提交代码触发 CI

```bash
git add .
git commit -m "feat: your change"
git push origin main
```

GitHub Actions 会自动运行 CI。

### 2. 创建发布版本

```bash
git checkout main
git pull origin main
git tag v1.0.1
git push origin v1.0.1
```

推送后会先经过 `Tag Validation`，再进入 `Release`。

### 3. 创建预发布版本

```bash
git tag v1.1.0-beta.1
git push origin v1.1.0-beta.1
```

带 `-beta` / `-rc` 的标签会在 GitHub Release 中标记为预发布。

## 本地对应命令

发布相关 workflow 仍然复用仓库里的构建命令：

```bash
go test -race ./...
golangci-lint run
govulncheck ./...
gosec ./...
make release
make checksum
make package
```

## 常见场景

### 日常开发

```bash
git checkout -b feature/your-feature
# ... 开发 ...
git add .
git commit -m "feat: add your feature"
git push origin feature/your-feature
```

创建 PR 后，CI 会自动检查。

### 正式发布

```bash
git checkout main
git pull origin main
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

如果要发布后续版本：

```bash
git tag v1.2.3
git push origin v1.2.3
```

### 质量巡检

在 GitHub 仓库的 `Actions` 页面手动运行 `Quality`，适合定期检查覆盖率、依赖和文档状态。

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

运行：

```bash
go mod tidy
git diff go.mod go.sum
```

## 最佳实践

1. 先等 CI 通过，再打发布标签。
2. 预发布版本使用 `-beta.N` 或 `-rc.N`。
3. `Quality` 更适合周期性治理，不必在每次提交都重复跑。
4. 发布前优先核对 `.github/workflows/*.yml` 里的实际 Go 版本和 action 版本，避免文档与 workflow 漂移。
5. 版本发布前确认 README 和 `docs/RELEASE.md` 里的命令仍然有效。

## 相关文件

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.github/workflows/tag-check.yml`
- `.github/workflows/quality.yml`
- `docs/RELEASE.md`
- `README.md`
