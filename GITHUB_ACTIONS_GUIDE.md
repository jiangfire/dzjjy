# GitHub Actions 快速使用指南

## 📋 概述

本项目已配置完整的 CI/CD 自动化流程，包含 4 个 GitHub Actions workflow：

1. **CI** - 自动测试和构建
2. **Release** - 自动发布多平台版本
3. **Tag Check** - 标签验证和预发布检查
4. **Code Quality** - 代码质量保证

## 🚀 快速开始

### 1. 推送代码自动测试

```bash
git add .
git commit -m "feat: your feature"
git push origin main
```

GitHub Actions 会自动：
- ✅ 运行单元测试
- ✅ 构建二进制文件
- ✅ 代码质量检查
- ✅ 安全扫描

### 2. 创建发布版本

```bash
# 1. 确保代码已合并到 main
git checkout main
git pull origin main

# 2. 创建版本标签
git tag v1.0.0

# 3. 推送标签
git push origin v1.0.0
```

GitHub Actions 会自动：
- ✅ 构建所有平台二进制 (Linux/Windows/macOS)
- ✅ 生成校验和文件
- ✅ 创建 GitHub Release
- ✅ 上传所有发布包

### 3. 预发布测试

```bash
# 创建预发布版本
git tag v1.0.0-beta.1
git push origin v1.0.0-beta.1
```

## 📦 发布的产物

每次发布会自动生成：

### 二进制文件
- `dzjjy-server-linux-amd64`
- `dzjjy-server-linux-arm64`
- `dzjjy-server-windows-amd64.exe`
- `dzjjy-server-darwin-amd64`
- `dzjjy-server-darwin-arm64`
- 同样的客户端文件 `dzjjy-client-*`

### 打包文件
- `dzjjy-server-linux-amd64.tar.gz`
- `dzjjy-client-linux-amd64.tar.gz`
- 其他平台的 tar.gz 包

### 校验文件
- `checksums.txt` - 所有文件的 SHA256 校验和

## 🔧 使用发布的二进制

### 下载和安装

```bash
# 1. 下载对应平台的包
wget https://github.com/yourusername/dzjjy/releases/download/v1.0.0/dzjjy-server-linux-amd64.tar.gz

# 2. 解压
tar -xzf dzjjy-server-linux-amd64.tar.gz

# 3. 验证校验和（可选）
sha256sum -c checksums.txt 2>/dev/null | grep dzjjy-server

# 4. 运行
./dzjjy-server -token your-token -port 8080 -state ./state.json
```

### 客户端使用

```bash
# 部署应用
./dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-token \
  -file app.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -auto-restart \
  -max-restarts 5
```

## 📊 工作流状态说明

### CI 状态
- ✅ **绿色**: 所有检查通过
- ⏳ **黄色**: 正在运行
- ❌ **红色**: 某个检查失败

### Release 状态
- ✅ **成功**: 所有平台构建完成，Release 已创建
- ❌ **失败**: 检查 Actions 日志查看详情

## 🎯 常见场景

### 场景 1: 日常开发

```bash
# 开发功能
git checkout -b feature/deployment-enhancement
# ... 编码 ...

# 提交并推送
git add .
git commit -m "feat: improve deployment error handling"
git push origin feature/deployment-enhancement

# 创建 PR，等待 CI 通过，然后合并
```

### 场景 2: 紧急修复

```bash
# 从 main 创建修复分支
git checkout -b hotfix/security-fix
# ... 修复 ...

# 提交并创建标签
git add .
git commit -m "fix: security vulnerability in auth"
git tag v1.0.1
git push origin hotfix/security-fix
git push origin v1.0.1
```

### 场景 3: 测试版本

```bash
# 创建测试版本
git tag v1.1.0-beta.1
git push origin v1.1.0-beta.1

# 测试发布包
# 下载测试，确认无误后创建正式版
git tag v1.1.0
git push origin v1.1.0
```

## 🔍 查看工作流运行

### GitHub 网页界面
1. 访问仓库主页
2. 点击 **Actions** 标签
3. 选择具体的工作流
4. 查看运行日志

### 命令行查看
```bash
# 查看最近的运行
gh run list

# 查看具体运行详情
gh run view <run-id>

# 查看日志
gh run view <run-id> --log
```

## ⚙️ 配置说明

### Makefile 集成

所有 workflow 都使用 Makefile 命令：

```bash
make ci          # 完整 CI 流程
make ci-release  # 完整发布流程
make release     # 构建所有平台
make test        # 运行测试
make lint        # 代码检查
make build       # 本地构建
```

### 触发条件

| Workflow | 触发方式 |
|----------|----------|
| CI | 推送到 main/develop，或 PR |
| Release | 创建 v* 标签 |
| Tag Check | 创建任何标签 |
| Quality | 推送到 main/develop，或 PR |

## 🐛 故障排除

### CI 失败

```bash
# 本地复现问题
make clean
make deps
make test
make lint

# 修复后重新提交
git add .
git commit -m "fix: resolve test failures"
git push
```

### Release 失败

1. **检查标签格式**: 必须是 `v1.2.3` 格式
2. **本地测试**: `make release`
3. **查看日志**: GitHub Actions → Release → 具体运行

### 构建产物缺失

```bash
# 检查本地构建
make release
ls -la dist/

# 如果本地成功，检查 Actions 配置
```

## 📝 最佳实践

1. **标签规范**
   - ✅ 使用语义化版本: `v1.2.3`
   - ✅ 预发布: `v1.2.3-beta.1`
   - ❌ 不要: `release-1.2.3`, `v1.2`

2. **提交信息**
   - ✅ `feat: add new feature`
   - ✅ `fix: resolve bug`
   - ✅ `docs: update readme`
   - ❌ `update code`

3. **发布频率**
   - 小功能: 合并后立即发布
   - 大功能: 稳定后发布
   - 修复: 修复后立即发布

4. **测试**
   - 本地测试通过再推送
   - 等待 CI 通过再合并
   - 预发布测试后再正式发布

## 📚 相关文档

- [Makefile 说明](./Makefile) - 构建命令详解
- [工作流文件](./.github/workflows/) - 原始配置
- [工作流说明](./.github/README.md) - 详细文档
- [项目 README](./README.md) - 项目介绍

## 🆘 获取帮助

1. **查看日志**: GitHub Actions 页面
2. **检查配置**: `.github/workflows/` 目录
3. **本地测试**: 使用 Makefile 命令
4. **提交 Issue**: 报告问题或建议

---

**提示**: 所有 workflow 都是可配置的，你可以根据需要调整触发条件、构建平台等参数。