#!/bin/bash
# 服务端启动示例

# 设置你的认证令牌
TOKEN="your-secret-token-here"

# 启动服务端
./dzjjy-server \
  -token "$TOKEN" \
  -port 8080 \
  -upload ./uploads \
  -work ./workspace
