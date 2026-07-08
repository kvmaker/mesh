#!/usr/bin/env bash
# T01: meshd 服务端 entrypoint
# 自签证书模式（MESH_TEST_TLS=on），init 后提取 token 供 e2e 测试读取
set -euo pipefail

export MESH_TEST_TLS=on

if [ -f /etc/mesh/mesh.db ]; then
  echo "already initialized, skip init"
else
  meshd init 2>&1 | tee /tmp/init.log
  # meshd init 输出 "Initialized.\nToken: xxx"，提取 token 写入文件供 e2e 读取
  # 用 sed 比 grep -oP 更可移植（不依赖 PCRE）
  sed -n 's/^Token: //p' /tmp/init.log > /etc/mesh/token 2>/dev/null || true
  if [ ! -s /etc/mesh/token ]; then
    echo "ERROR: failed to extract token from init output" >&2
    exit 1
  fi
fi

exec meshd run
