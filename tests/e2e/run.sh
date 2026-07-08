#!/usr/bin/env bash
# T07: e2e 一键入口与结果聚合
#
# 用法：
#   bash tests/e2e/run.sh [--all|--quick|--strict|--scenario N ...]
#
# 选项：
#   --all         跑全部场景 01 02 03（默认）
#   --quick       只跑 01 02，跳过最慢的 03 故障
#   --strict      传给场景脚本，启用性能硬门槛（02）
#   --scenario N  追加指定场景（可多次）
#
# 编排要点（来自 T04-T06 实战）：
#   1. 单独起 server（触发 meshd init 生成真实 token 写 /etc/mesh/token）
#   2. 从 server 容器读真实 token
#   3. 用真实 token 串行起 client-a、client-b（同时 join 偶发 HTTP 500）
#   4. 跑场景 → 收日志 → 聚合 summary.txt
#   5. trap 保证容器 always down
#
# 环境变量：
#   DOCKER_HOST   本机 colima 需 export，CI 不用（run.sh 不硬编码）
set -euo pipefail
E2E_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$E2E_DIR/lib/helpers.sh"

STRICT="${STRICT:-0}"
SCENARIOS=()

usage() { echo "Usage: $0 [--all|--quick|--strict|--scenario N]"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --all)      SCENARIOS=(01 02 03);;
    --quick)    SCENARIOS=(01 02);;      # 跳过最慢的 03 故障
    --strict)   STRICT=1; shift; continue;;
    --scenario) SCENARIOS+=("$2"); shift 2; continue;;
    *)          usage;;
  esac
  shift || true
done
[ ${#SCENARIOS[@]} -eq 0 ] && SCENARIOS=(01 02 03)

TS="$(date -u +%Y-%m-%dT%H-%M-%S)"
export RESULTS_DIR="$E2E_DIR/results/$TS"
mkdir -p "$RESULTS_DIR"

echo "=== e2e run $TS (scenarios: ${SCENARIOS[*]}, strict: $STRICT) ==="

# ---- 正确编排：分步起容器 ----
# 1. 起 server（MESH_TOKEN=dummy 仅满足 compose 解析；server 自身不用）
MESH_TOKEN=dummy docker compose -f "$COMPOSE" up -d --build server
trap 'docker compose -f "$COMPOSE" down >/dev/null 2>&1 || true' EXIT

# 2. 等 server + 读真实 token
wait_for_server
REAL_TOKEN=$(get_token)

# 3. 串行起 client（带真实 token；同时 join 偶发 HTTP 500）
#    必须加 --no-deps：否则 compose 会因 depends_on 把 server 一起 recreate，
#    server 重新 init 生成新 token，client 用旧 token join 报 "invalid token"。
MESH_TOKEN="$REAL_TOKEN" docker compose -f "$COMPOSE" up -d --build --no-deps client-a
wait_for_client mesh-client-a
MESH_TOKEN="$REAL_TOKEN" docker compose -f "$COMPOSE" up -d --build --no-deps client-b
wait_for_client mesh-client-b

# 导出给场景脚本（它们用 docker compose stop/start 需 MESH_TOKEN 存在）
export MESH_TOKEN="$REAL_TOKEN"

# ---- 跑场景 ----
OVERALL=0
for s in "${SCENARIOS[@]}"; do
  echo "--- scenario $s ---"
  if ! STRICT="$STRICT" bash "$E2E_DIR/scenarios/${s}-"*.sh; then
    OVERALL=1
  fi
done

dump_logs "$RESULTS_DIR"

# ---- 聚合 summary.txt ----
{
  for s in "${SCENARIOS[@]}"; do
    # 找该场景的 json（目录名 0N-xxx/0N-xxx.json）
    json=$(find "$RESULTS_DIR" -path "*0${s#0}-*/*.json" 2>/dev/null | head -1)
    [ -z "$json" ] && json=$(find "$RESULTS_DIR" -path "*${s}-*/*.json" 2>/dev/null | head -1)
    if [ -n "$json" ]; then
      pf=$(jq -r '.pass // "?"' "$json"); fl=$(jq -r '.fail // "?"' "$json")
      st="PASS"; [ "$fl" != "0" ] && [ "$fl" != "?" ] && st="FAIL"
      echo "[${st}] scenario $s: pass=$pf fail=$fl"
    else
      echo "[?] scenario $s: no result json"
    fi
  done
  perf="$RESULTS_DIR/02-performance/02-performance.json"
  if [ -f "$perf" ]; then
    echo "[PERF] $(jq -r '"tcp_1stream=\(.tcp_1stream_mbps)Mbps wan_tcp=\(.wan_tcp_mbps)Mbps rtt_avg=\(.rtt_avg_ms)ms (注:TCP 受 B00 影响可能为0)"' "$perf")"
  fi
  [ "$OVERALL" -eq 0 ] && echo "Overall: PASS" || echo "Overall: FAIL"
} | tee "$RESULTS_DIR/summary.txt"

exit $OVERALL
