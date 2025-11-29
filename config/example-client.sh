#!/bin/bash
# 客户端使用示例

SERVER="http://localhost:8080"
TOKEN="your-secret-token-here"

echo "=== 示例 1: 部署 Python 应用（runtime 类型）==="
./dzjjy-client deploy \
  -server "$SERVER" \
  -token "$TOKEN" \
  -file examples/hello.py \
  -type runtime \
  -executable python3 \
  -entry hello.py

# 查询状态
echo -e "\n查询应用状态..."
./dzjjy-client status \
  -server "$SERVER" \
  -token "$TOKEN"

# 等待一段时间
sleep 3

# 停止应用
echo -e "\n停止应用..."
./dzjjy-client stop \
  -server "$SERVER" \
  -token "$TOKEN"

echo -e "\n=== 示例 2: 部署 NodeJS 应用（runtime 类型）==="
./dzjjy-client deploy \
  -server "$SERVER" \
  -token "$TOKEN" \
  -file examples/hello.js \
  -type runtime \
  -executable node \
  -entry hello.js

# 查询状态
echo -e "\n查询应用状态..."
./dzjjy-client status \
  -server "$SERVER" \
  -token "$TOKEN"

# 停止应用
sleep 3
echo -e "\n停止应用..."
./dzjjy-client stop \
  -server "$SERVER" \
  -token "$TOKEN"

echo -e "\n=== 示例 3: 部署 Go 源码（runtime 类型）==="
./dzjjy-client deploy \
  -server "$SERVER" \
  -token "$TOKEN" \
  -file examples/hello.go \
  -type runtime \
  -executable go \
  -entry hello.go \
  -args "run"

# 查询状态
echo -e "\n查询应用状态..."
./dzjjy-client status \
  -server "$SERVER" \
  -token "$TOKEN"
