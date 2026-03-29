# 发布说明

> 文档更新：2026-03-21
> 最新正式版本：`v1.0.1`

## 当前发布基线

- 发布分支：`main`
- 发布触发：推送语义化版本标签，例如 `v1.0.1`
- 发布 workflow：`.github/workflows/release.yml`
- 当前 Release 环境：Go `1.25.8`
- 当前最新正式标签：`v1.0.1`
- `v1.0.1` 对应提交：`b5eb13309eb66b963c0dcb7e8c154c3bf734c290`

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
4. 检查 `README.md`、`README.en.md`、`GITHUB_ACTIONS_GUIDE.md` 与本文件中的命令示例是否仍然有效。
5. 创建带注释的语义化标签，例如 `git tag -a v1.0.2 -m "Release v1.0.2"`。
6. 推送标签，例如 `git push origin v1.0.2`。
7. 在 GitHub Actions 中确认 `Tag Validation` 和 `Release` 两个 workflow 都成功。
8. 核对 GitHub Release 页面里的二进制、压缩包和 `checksums.txt`。
9. 记录版本号、提交号和变更摘要。

## 版本号建议

- 补丁版本：仅修复 bug、CI/CD、文档或安全问题，例如 `v1.0.1`
- 次版本：新增向后兼容功能，例如 `v1.1.0`
- 预发布：使用 `-beta.N` 或 `-rc.N` 后缀，例如 `v1.1.0-rc.1`

## 最近一次发布

### v1.0.1

- 发布时间：2026-03-21
- 标签：`v1.0.1`
- 提交：`b5eb13309eb66b963c0dcb7e8c154c3bf734c290`
- 主要内容：
  - 修复 GitHub Actions 发布链路相关问题
  - 修复 Go 1.24+/根路径写入兼容性问题
  - 修复 multipart 与 gosec 相关误报/校验问题
