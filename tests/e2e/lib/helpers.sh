#!/usr/bin/env bash
# 通用辅助函数。所有场景脚本 source 本文件。
#
# 提供：
#   - dex              : 在指定容器内执行命令
#   - wait_for_server  : 等 server 容器内 meshd 可达（自签，curl -k）
#   - wait_for_client  : 等 client 容器内 mesh0 起来并有 IPv4
#   - get_token        : 从 server 容器读 /etc/mesh/token（T01 entrypoint 已写入）
#   - dump_logs        : 收集所有容器最近日志到 results 目录
#   - assert           : 断言命令退出码符合预期，写 ok/fail 计数

# 项目根目录（本文件位于 tests/e2e/lib/，往上两级到 e2e 目录，再往上两级到仓库根）
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$E2E_DIR/../.." && pwd)"
COMPOSE="$E2E_DIR/docker-compose.yml"
RESULTS_DIR="${RESULTS_DIR:-$E2E_DIR/results}"

# 断言计数文件（assert 函数使用）。调用方应在场景脚本开头初始化：
#   ASSERT_OK="$RESULTS_DIR/<scenario>.ok"; ASSERT_FAIL="$RESULTS_DIR/<scenario>.fail"
# 未设置时给个默认，避免 set -u 报错。
ASSERT_OK="${ASSERT_OK:-/tmp/mesh-e2e-ok.$$}"
ASSERT_FAIL="${ASSERT_FAIL:-/tmp/mesh-e2e-fail.$$}"

# 在指定容器内执行命令
dex() { docker exec "$@"; }

# 等 server 容器内 meshd 可达（自签，curl -k）
# 用 server 的 compose 网络 alias "server" 访问，避免依赖 host 端口映射。
wait_for_server() {
  echo "waiting for server meshd..."
  local i
  for i in $(seq 1 60); do
    # 探活用无需鉴权的封面页 GET /（/api/devices 现在需要 Bearer 凭证，
    # 未授权会 401 导致 curl -f 失败，不适合做健康检查）。
    if dex mesh-server curl -kfsS https://server:443/ >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "ERROR: server did not become ready" >&2
  return 1
}

# 等 client 容器内 mesh0 起来并有 IPv4
# 用法: wait_for_client <client-container>
wait_for_client() {
  local c="$1"
  local i
  for i in $(seq 1 60); do
    if dex "$c" ip -o addr show mesh0 2>/dev/null | grep -q inet; then
      return 0
    fi
    sleep 1
  done
  echo "ERROR: client $c mesh0 did not come up" >&2
  return 1
}

# 从 server 容器读 /etc/mesh/token（T01 entrypoint 已写入）
get_token() {
  dex mesh-server cat /etc/mesh/token
}

# 收集所有容器最近日志到 results 目录
# 用法: dump_logs <out-dir>
dump_logs() {
  local out="$1"
  mkdir -p "$out"
  local c
  for c in mesh-server mesh-client-a mesh-client-b; do
    docker logs "$c" >"$out/$c.log" 2>&1 || true
  done
}

# 断言：命令退出码符合预期。写 ok/fail 计数到 $ASSERT_OK/$ASSERT_FAIL 文件
# 用法: assert "<label>" <cmd...>
# 用 `if "$@"; then` 形式，命令失败不会触发 set -e 退出脚本。
assert() {
  local label="$1"; shift
  if "$@"; then
    echo "  [PASS] $label"
    echo "1" >>"$ASSERT_OK"
  else
    echo "  [FAIL] $label"
    echo "1" >>"$ASSERT_FAIL"
  fi
}
