#!/usr/bin/env bash
# T04: 连通性与路由场景（P0）
# 验证 mesh0 路由注入、ping server/peer、不存在 IP 100% 丢包、
# client-b 断开/重连、fping 并发不丢。
#
# 前提：容器已起、mesh0 已 up（由外部编排完成）。
set -euo pipefail
source "$(dirname "$0")/../lib/helpers.sh"

OUT="$RESULTS_DIR/01-connectivity"
mkdir -p "$OUT"
ASSERT_OK="$OUT/ok"; : >"$ASSERT_OK"
ASSERT_FAIL="$OUT/fail"; : >"$ASSERT_FAIL"
LOG="$OUT/01-connectivity.log"; : >"$LOG"
exec > >(tee -a "$LOG") 2>&1

echo "=== Scenario 01: connectivity & routing ==="

wait_for_server
wait_for_client mesh-client-a
wait_for_client mesh-client-b

# docker-compose.yml 里 client 服务用 ${MESH_TOKEN:?}，stop/start 等编排命令
# 解析 compose 文件时也需要这个 env 存在（值不校验）。读真实 token 注入。
export MESH_TOKEN="$(get_token)"

SERVER_IP="10.100.0.1"
# mesh IP 不保证顺序：动态读 mesh0 IPv4
A_IP=$(dex mesh-client-a ip -o -4 addr show mesh0 | awk '{print $4}' | cut -d/ -f1)
B_IP=$(dex mesh-client-b ip -o -4 addr show mesh0 | awk '{print $4}' | cut -d/ -f1)
echo "client-a=$A_IP client-b=$B_IP"

# 路由表：10.100.0.0/24 走 mesh0
# 注意：dex 是 host shell 函数，bash -c 子进程不继承，必须用 docker exec 二进制。
assert "client-a has mesh route" \
  bash -c "docker exec mesh-client-a ip route | grep -q '10.100.0.0/24.*mesh0'"

# ping server
assert "client-a ping server" \
  dex mesh-client-a ping -c 3 -W 2 "$SERVER_IP"

# ping peer
assert "client-a ping client-b" \
  dex mesh-client-a ping -c 3 -W 2 "$B_IP"

# 不存在的 IP 必须 100% 丢包
# ping 输出 "..., 100% packet loss, ..."，用 sed -E 提取百分号前的数字（BSD/GNU grep 通用）。
NONEXIST=$(dex mesh-client-a ping -c 3 -W 1 10.100.0.99 2>&1 || true)
LOSS=$(echo "$NONEXIST" | sed -nE 's/.*[^0-9]([0-9]+)% packet loss.*/\1/p' | tail -1)
[ -z "$LOSS" ] && LOSS=0
assert "unreachable IP 100% loss" \
  bash -c "[ '$LOSS' -eq 100 ]"

# 断开 client-b
docker compose -f "$COMPOSE" stop client-b >/dev/null
sleep 3
assert "client-b offline: a->b fails" \
  bash -c "! docker exec mesh-client-a ping -c 3 -W 2 $B_IP 2>/dev/null"

# 重启 client-b，恢复
docker compose -f "$COMPOSE" start client-b >/dev/null
wait_for_client mesh-client-b
assert "client-b reconnect: a->b works" \
  dex mesh-client-a ping -c 3 -W 3 "$B_IP"

# fping 并发不丢（容许 <5%）
# fping -q -c 输出 "xmt/rcv/%loss = 20/20/0%, ..."，sed -E 提取 loss 数值。
# dex 是函数在 bash -c 不可用 → 用 docker exec；grep -oP 在 BSD 不可用 → 用 sed -E。
assert "fping burst low loss" \
  bash -c "loss=\$(docker exec mesh-client-a fping -q -c 20 -p 100 $B_IP 2>&1 | sed -nE 's|.*= [0-9]+/[0-9]+/([0-9]+)%.*|\1|p' | head -1); [ -z \"\$loss\" ] && exit 1; [ \"\$loss\" -lt 5 ]"

OK=$(wc -l <"$ASSERT_OK" | tr -d ' ')
FAIL=$(wc -l <"$ASSERT_FAIL" | tr -d ' ')
echo "=== 01 result: PASS=$OK FAIL=$FAIL ==="
jq -n --arg ok "$OK" --arg fail "$FAIL" '{pass:($ok|tonumber), fail:($fail|tonumber)}' >"$OUT/01-connectivity.json"
test "$FAIL" -eq 0
