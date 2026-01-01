# dzjjy - 简易部署服务 Makefile
# 支持 GitHub Actions CI/CD

# 变量定义
PROJECT_NAME := dzjjy
MODULE := github.com/jiangfire/dzjjy
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")

# 构建输出目录
DIST_DIR := ./dist
BUILD_DIR := ./build

# Go 构建参数
GO := go
GO_BUILD := $(GO) build -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"
GO_TEST := $(GO) test -v -race -coverprofile=coverage.out
GO_LINT := golangci-lint run

# 目标二进制文件名
SERVER_BIN := dzjjy-server
CLIENT_BIN := dzjjy-client

# 平台支持 (用于交叉编译)
PLATFORMS := linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64

# 默认目标
.DEFAULT_GOAL := help

# ==============================================================================
# 开发目标
# ==============================================================================

.PHONY: help
help: ## 显示帮助信息
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

.PHONY: build
build: ## 编译服务端和客户端
	@echo "Building $(PROJECT_NAME) v$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO_BUILD) -o $(BUILD_DIR)/$(SERVER_BIN) ./cmd/server
	$(GO_BUILD) -o $(BUILD_DIR)/$(CLIENT_BIN) ./cmd/client
	@echo "✓ Build complete: $(BUILD_DIR)/"

.PHONY: server
server: ## 编译并运行服务端 (开发模式)
	@$(MAKE) build
	@echo "Starting server on port 8080..."
	@$(BUILD_DIR)/$(SERVER_BIN) -token dev-token -port 8080 -state ./state.json

.PHONY: client
client: ## 编译客户端
	@$(MAKE) build
	@echo "Client binary: $(BUILD_DIR)/$(CLIENT_BIN)"

.PHONY: clean
clean: ## 清理构建产物和临时文件
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@rm -f coverage.out coverage.html
	@rm -rf uploads/ workspace/ logs/
	@rm -f state.json
	@echo "✓ Clean complete"

.PHONY: test
test: ## 运行单元测试
	@echo "Running tests..."
	$(GO_TEST) ./...
	@echo "✓ Tests complete"

.PHONY: test-cover
test-cover: ## 运行测试并生成覆盖率报告
	@echo "Running tests with coverage..."
	$(GO_TEST) ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"

.PHONY: lint
lint: ## 运行代码检查
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		$(GO_LINT); \
	else \
		echo "Warning: golangci-lint not found, skipping..."; \
	fi

.PHONY: fmt
fmt: ## 格式化代码
	@echo "Formatting code..."
	$(GO) fmt ./...
	$(GO) mod tidy

.PHONY: deps
deps: ## 下载依赖
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# ==============================================================================
# 发布目标 (GitHub Actions)
# ==============================================================================

.PHONY: release
release: ## 构建所有平台的发布包 (用于 GitHub Actions)
	@echo "Building release for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		echo "Building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o $(DIST_DIR)/$(SERVER_BIN)-$$os-$$arch ./cmd/server; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o $(DIST_DIR)/$(CLIENT_BIN)-$$os-$$arch ./cmd/client; \
	done
	@echo "✓ Release builds complete: $(DIST_DIR)/"

.PHONY: release-server
release-server: ## 仅构建服务端的发布包
	@echo "Building server release..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		echo "Building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o $(DIST_DIR)/$(SERVER_BIN)-$$os-$$arch ./cmd/server; \
	done
	@echo "✓ Server release builds complete: $(DIST_DIR)/"

.PHONY: release-client
release-client: ## 仅构建客户端的发布包
	@echo "Building client release..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		echo "Building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch $(GO_BUILD) -o $(DIST_DIR)/$(CLIENT_BIN)-$$os-$$arch ./cmd/client; \
	done
	@echo "✓ Client release builds complete: $(DIST_DIR)/"

.PHONY: release-current
release-current: ## 仅构建当前平台的发布包
	@echo "Building for current platform..."
	@mkdir -p $(DIST_DIR)
	$(GO_BUILD) -o $(DIST_DIR)/$(SERVER_BIN)-$(shell $(GO) env GOOS)-$(shell $(GO) env GOARCH) ./cmd/server
	$(GO_BUILD) -o $(DIST_DIR)/$(CLIENT_BIN)-$(shell $(GO) env GOOS)-$(shell $(GO) env GOARCH) ./cmd/client
	@echo "✓ Current platform build complete: $(DIST_DIR)/"

.PHONY: checksum
checksum: ## 为发布包生成校验和
	@echo "Generating checksums..."
	@cd $(DIST_DIR) && \
		sha256sum * > checksums.txt 2>/dev/null || \
		shasum -a 256 * > checksums.txt 2>/dev/null || \
		echo "Warning: Could not generate checksums"
	@echo "✓ Checksums generated: $(DIST_DIR)/checksums.txt"

.PHONY: package
package: ## 打包发布文件
	@echo "Packaging release..."
	@cd $(DIST_DIR) && \
		for file in $(SERVER_BIN)-* $(CLIENT_BIN)-*; do \
			if [ -f "$$file" ]; then \
				echo "Packing $$file..."; \
				tar czf $$file.tar.gz $$file; \
			fi; \
		done
	@echo "✓ Packages created: $(DIST_DIR)/*.tar.gz"

# ==============================================================================
# GitHub Actions 专用目标
# ==============================================================================

.PHONY: ci
ci: ## CI 流程: 清理 → 依赖 → 测试 → 构建
	@echo "Running CI pipeline..."
	$(MAKE) clean
	$(MAKE) deps
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) build
	@echo "✓ CI pipeline complete"

.PHONY: ci-release
ci-release: ## 完整发布流程 (用于 GitHub Actions)
	@echo "Running CI release pipeline..."
	$(MAKE) clean
	$(MAKE) deps
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) release
	$(MAKE) checksum
	$(MAKE) package
	@echo "✓ CI release pipeline complete"
	@echo ""
	@echo "Release artifacts:"
	@ls -lh $(DIST_DIR)/

.PHONY: version
version: ## 显示当前版本信息
	@echo "Project: $(PROJECT_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Git Branch: $(GIT_BRANCH)"
	@echo "Build Time: $(BUILD_TIME)"

.PHONY: info
info: ## 显示构建环境信息
	@echo "Go Version: $(shell $(GO) version)"
	@echo "GOOS: $(shell $(GO) env GOOS)"
	@echo "GOARCH: $(shell $(GO) env GOARCH)"
	@echo "Module: $(MODULE)"
	@echo "Build Dir: $(BUILD_DIR)"
	@echo "Dist Dir: $(DIST_DIR)"

# ==============================================================================
# 示例和测试
# ==============================================================================

.PHONY: example
example: ## 运行示例 (需要先启动服务端)
	@echo "Running example deployment..."
	@echo "Make sure server is running on port 8080 with token 'dev-token'"
	@echo ""
	@echo "Example commands:"
	@echo "  $(BUILD_DIR)/$(CLIENT_BIN) deploy -server http://localhost:8080 -token dev-token -file examples/hello.go -type exec -executable ./hello"
	@echo ""
	@echo "Building example..."
	@$(GO) build -o $(BUILD_DIR)/hello examples/hello.go

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像 (如果需要)
	@echo "Docker build target - Dockerfile needed"
	@echo "Example: docker build -t $(PROJECT_NAME):$(VERSION) ."

.PHONY: install-tools
install-tools: ## 安装开发工具
	@echo "Installing development tools..."
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "✓ Tools installed"

# ==============================================================================
# 文档
# ==============================================================================

.PHONY: docs
docs: ## 生成文档信息
	@echo "Project Documentation:"
	@echo "  - README.md"
	@echo "  - docs/ARCHITECTURE.md"
	@echo "  - docs/TESTING.md"
	@echo "  - docs/DEVELOPMENT.md"
	@echo "  - docs/PLAN.md"
	@echo ""
	@echo "Quick Start:"
	@echo "  1. make build          # Build binaries"
	@echo "  2. make server         # Start server (in another terminal)"
	@echo "  3. make example        # Deploy example app"
