#!/usr/bin/env bash
# T01: mesh 客户端 entrypoint
# 等待 server 就绪 -> join -> up（前台）
#
# 幂等：容器重启（docker compose stop/start、restart）时 config.json 仍在，
# mesh join 会拒绝并报 "already registered"。检测到已注册则跳过 join 直接 up，
# 这样容器重启能自愈，e2e reconnect 场景也能通过。
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
if ! curl -kfsS https://server:443/api/devices >/dev/null 2>&1; then
  echo "ERROR: server did not become ready within 60s" >&2
  exit 1
fi

# 已注册则跳过 join（容器重启场景）。config 路径 = $HOME/.mesh/config.json
# （见 internal/client/config.go ConfigDir）。
if [ -f "$HOME/.mesh/config.json" ]; then
  echo "already registered, skip join"
else
  : "${MESH_TOKEN:?MESH_TOKEN must be set}"
  mesh join server --token "$MESH_TOKEN" --insecure
fi

# 启动隧道（前台）
exec mesh up
