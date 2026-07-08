#!/usr/bin/env bash
# T05: 性能与抖动场景（P0）
# iperf3 TCP 1流/4流吞吐、UDP 丢包；baseline vs wan(netem 80ms±10ms/1%loss) 对比；
# RTT 统计；可选 STRICT 硬门槛（env STRICT=1 启用）。
#
# 默认非 STRICT：只采集并报告数值，exit 0（避免 CI 因性能波动假失败）。
# STRICT=1 时按硬门槛判定（tcp_1stream≥30 Mbps，wan_udp_loss≤5%）。
#
# 前提：容器已起、mesh0 已 up（由外部编排完成）。
set -euo pipefail
source "$(dirname "$0")/../lib/helpers.sh"
source "$(dirname "$0")/../lib/metrics.sh"

OUT="$RESULTS_DIR/02-performance"
mkdir -p "$OUT"
JSON="$OUT/02-performance.json"
echo '{}' >"$JSON"
LOG="$OUT/02-performance.log"; : >"$LOG"
exec > >(tee -a "$LOG") 2>&1

echo "=== Scenario 02: performance ==="
wait_for_client mesh-client-a
wait_for_client mesh-client-b
B_IP=$(dex mesh-client-b ip -o -4 addr show mesh0 | awk '{print $4}' | cut -d/ -f1)
echo "client-b mesh0 = $B_IP"

# 合法数字校验：非空且仅含数字/点号则原样返回，否则返回默认值。
# 防御 metrics 边界 case（iperf3 异常退出输出非 JSON）导致 jq --argjson 失败。
num_or() { case "$1" in ''|*[!0-9.]*) echo "$2";; *) echo "$1";; esac; }

# 起 iperf3 server（client-b，daemon）。先清残留再 -s -D 后台运行。
dex mesh-client-b sh -c 'pkill iperf3 2>/dev/null || true; iperf3 -s -D'
sleep 1

# ---- baseline（无 netem 干扰）----
dex mesh-client-b bash -c 'source /usr/local/bin/netem-preset.sh; netem baseline'
echo "-- baseline TCP 1 stream --"
TCP1=$(num_or "$(iperf_tcp mesh-client-a "$B_IP" 1 10)" 0)
json_set "$JSON" tcp_1stream_mbps "$TCP1"
echo "tcp_1stream=$TCP1 Mbps"

echo "-- baseline TCP 4 streams --"
TCP4=$(num_or "$(iperf_tcp mesh-client-a "$B_IP" 4 10)" 0)
json_set "$JSON" tcp_4stream_mbps "$TCP4"
echo "tcp_4stream=$TCP4 Mbps"

echo "-- baseline UDP 100M --"
UDPL=$(num_or "$(iperf_udp_loss mesh-client-a "$B_IP" 100M 10)" 100)
json_set "$JSON" udp_100m_loss_pct "$UDPL"
echo "udp_100m_loss=$UDPL %"

# ---- wan netem（80ms±10ms, 1% loss）----
# netem 加在 client-b eth0：影响 client-b↔client-a 之间的 mesh 加密流量（双向）。
dex mesh-client-b bash -c 'source /usr/local/bin/netem-preset.sh; netem wan'
echo "-- wan TCP 1 stream --"
WAN_TCP=$(num_or "$(iperf_tcp mesh-client-a "$B_IP" 1 10)" 0)
json_set "$JSON" wan_tcp_mbps "$WAN_TCP"
echo "wan_tcp=$WAN_TCP Mbps"

echo "-- wan UDP 100M --"
WAN_UDP=$(num_or "$(iperf_udp_loss mesh-client-a "$B_IP" 100M 10)" 100)
json_set "$JSON" wan_udp_loss_pct "$WAN_UDP"
echo "wan_udp_loss=$WAN_UDP %"

# ---- RTT（恢复 baseline 后采集）----
dex mesh-client-b bash -c 'source /usr/local/bin/netem-preset.sh; netem baseline'
echo "-- RTT baseline (100 samples) --"
RTT=$(rtt_stats mesh-client-a "$B_IP" 100)
AVG=$(num_or "$(echo "$RTT" | awk '{print $1}')" 0)
LOSS=$(num_or "$(echo "$RTT" | awk '{print $2}')" 100)
json_set "$JSON" rtt_avg_ms "$AVG"
json_set "$JSON" rtt_loss_pct "$LOSS"
echo "rtt_avg=$AVG ms  rtt_loss=$LOSS %"

# ---- STRICT 硬门槛（可选，env STRICT=1 启用）----
# 用 `if ! awk; then` 形式，避免 set -e 在 awk 非零退出时提前终止脚本。
STRICT_RC=0
if [ "${STRICT:-0}" = "1" ]; then
  if ! awk -v tcp="$TCP1" -v udp="$WAN_UDP" '
        BEGIN{
          ok=1
          if (tcp+0 < 30) {print "STRICT FAIL: tcp_1stream="tcp" < 30"; ok=0}
          if (udp+0 > 5)  {print "STRICT FAIL: wan_udp_loss="udp" > 5"; ok=0}
          exit (ok?0:1)
        }'; then
    STRICT_RC=1
  fi
fi

# 收尾：关 iperf3 server，清 netem
dex mesh-client-b pkill iperf3 2>/dev/null || true
dex mesh-client-b bash -c 'source /usr/local/bin/netem-preset.sh; netem baseline'

echo "=== 02 done ==="
cat "$JSON"

# 非 STRICT 模式永远 pass（只报告）；STRICT 模式按门槛返回
if [ "${STRICT:-0}" = "1" ]; then
  exit "$STRICT_RC"
fi
exit 0
