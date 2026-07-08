#!/usr/bin/env bash
# T01: mesh 客户端 entrypoint
# 等待 server 就绪 -> join -> up（前台）
set -euo pipefail

# 等待 server 起来（自签证书，curl 必须 -k）
echo "waiting for server..."
for i in $(seq 1 60); do
  if curl -kfsS https://server:443/api/devices >/dev/null 2>&1; then
    echo "server is up"
    break
  fi
  sleep 1
done

: "${MESH_TOKEN:?MESH_TOKEN must be set}"
mesh join server --token "$MESH_TOKEN" --insecure

# 启动隧道（前台）
exec mesh up
