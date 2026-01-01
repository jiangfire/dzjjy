# GitHub Actions 说明

本目录包含 dzjjy 项目的 CI/CD 自动化工作流。

## 工作流概览

### 1. CI (ci.yml)
**触发条件**: 推送到 `main` 或 `develop` 分支，或创建 Pull Request

**功能**:
- ✅ 代码测试 (带覆盖率)
- ✅ 跨平台构建验证 (Linux, Windows, macOS)
- ✅ 代码质量检查
- ✅ 安全扫描 (govulncheck, gosec)
- ✅ 生成构建产物

**使用场景**: 日常开发、代码审查、合并前验证

### 2. Release (release.yml)
**触发条件**: 创建版本标签 (如 `v1.0.0`, `v1.2.3-beta.1`)

**功能**:
- ✅ 自动构建所有平台二进制文件
- ✅ 生成 SHA256 校验和
- ✅ 创建 GitHub Release
- ✅ 上传所有平台的发布包
- ✅ 生成发布说明

**支持平台**:
- Linux: amd64, arm64
- macOS: amd64, arm64
- Windows: amd64

### 3. Tag Check (tag-check.yml)
**触发条件**: 创建标签或提交到 main 分支

**功能**:
- ✅ 验证标签格式 (必须符合语义化版本)
- ✅ 预发布检查 (测试、构建)
- ✅ 文档验证
- ✅ 生成预发布报告

**标签格式要求**:
- 正式版: `v1.0.0`, `v1.2.3`
- 预发布: `v1.0.0-beta.1`, `v1.0.0-rc.2`
- 构建元数据: `v1.0.0+build.123`

### 4. Code Quality (quality.yml)
**触发条件**: 推送到 `main` 或 `develop` 分支，或创建 Pull Request

**功能**:
- ✅ 代码格式化检查
- ✅ Linter 静态分析
- ✅ 安全漏洞扫描
- ✅ 测试覆盖率检查 (阈值: 80%)
- ✅ 依赖项检查
- ✅ 跨平台构建验证
- ✅ 文档完整性检查

## 使用指南

### 日常开发流程

1. **编写代码**
   ```bash
   git checkout -b feature/your-feature
   # 编写代码...
   ```

2. **提交代码**
   ```bash
   git add .
   git commit -m "feat: add your feature"
   git push origin feature/your-feature
   ```

3. **创建 Pull Request**
   - GitHub Actions 自动运行 CI
   - 等待所有检查通过
   - 请求代码审查

4. **合并到 main**
   - CI 再次运行
   - 代码质量检查
   - 合并后自动部署到测试环境（如果配置）

### 发布新版本流程

1. **准备发布**
   ```bash
   git checkout main
   git pull origin main
   ```

2. **更新版本信息**
   - 更新 CHANGELOG.md（可选但推荐）
   - 更新 README 中的版本号（如果需要）

3. **创建标签**
   ```bash
   # 正式版
   git tag v1.2.0

   # 或预发布版
   git tag v1.2.0-beta.1
   ```

4. **推送标签**
   ```bash
   git push origin v1.2.0
   ```

5. **自动发布**
   - Tag Check workflow 运行（验证）
   - Release workflow 自动触发
   - 构建所有平台二进制
   - 创建 GitHub Release
   - 上传所有发布包

6. **验证发布**
   - 访问 GitHub Releases 页面
   - 检查所有文件已上传
   - 验证校验和文件

### 手动触发工作流

你也可以在 GitHub Actions 页面手动触发工作流：

1. 访问 GitHub 仓库 → Actions 标签页
2. 选择要运行的工作流
3. 点击 "Run workflow"
4. 选择分支或输入参数

## 环境变量和 Secrets

### 需要的 Secrets（可选）

以下 secrets 只在需要特定功能时才需要配置：

- `GITHUB_TOKEN`: 自动提供，无需配置
- `CODECOV_TOKEN`: 用于上传覆盖率到 Codecov（可选）
- `DOCKER_USERNAME` 和 `DOCKER_PASSWORD`: 用于 Docker 发布（当前禁用）

### Makefile 集成

所有工作流都使用 Makefile 中定义的命令：

- `make ci` - 完整 CI 流程
- `make ci-release` - 完整发布流程
- `make release` - 构建所有平台
- `make test` - 运行测试
- `make lint` - 代码检查

## 故障排除

### CI 失败

1. **测试失败**: 本地运行 `make test` 检查失败原因
2. **构建失败**: 检查 Go 版本和依赖项
3. **Linter 警告**: 运行 `make fmt` 和 `make lint` 修复

### Release 失败

1. **标签格式错误**: 确保使用语义化版本格式
2. **构建失败**: 本地运行 `make release` 测试
3. **上传失败**: 检查 GitHub Token 权限

### 验证工作流

本地验证工作流配置：
```bash
# 检查 Makefile
make help

# 测试构建
make build

# 运行测试
make test

# 测试完整发布流程（不上传）
make release
make checksum
```

## 最佳实践

1. **标签管理**
   - 使用语义化版本
   - 避免删除已发布的标签
   - 预发布版本用于测试

2. **提交信息**
   - 使用 Conventional Commits 格式
   - 例如: `feat: add deployment support`

3. **发布频率**
   - 小版本: 功能完成后
   - 大版本: 重要里程碑
   - 预发布: 测试阶段

4. **安全考虑**
   - 不要在代码中硬编码敏感信息
   - 使用 GitHub Secrets
   - 定期更新依赖项

## 相关文档

- [Makefile 说明](../Makefile) - 构建命令
- [README](../README.md) - 项目说明
- [开发指南](../docs/DEVELOPMENT.md) - 开发流程
- [架构文档](../docs/ARCHITECTURE.md) - 技术架构

## 联系与支持

如有问题，请：
1. 查看 GitHub Actions 日志
2. 阅读相关文档
3. 提交 Issue 或 Pull Request