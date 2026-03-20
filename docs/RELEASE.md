# 发布说明

## 本地发布检查

```bash
make fmt
make lint
make test
make build
```

## 构建产物

- `build/dzjjy-server`
- `build/dzjjy-client`
- `dist/` 下的跨平台产物由 `make release` 生成

## 建议发布流程

1. 运行 `make ci`
2. 运行 `make release`
3. 运行 `make checksum`
4. 检查 `README.md` 与 `README.en.md` 的命令示例是否仍然有效
5. 记录版本号、提交号和变更摘要
