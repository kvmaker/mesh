#!/usr/bin/env bash
# 指标采集与 JSON 输出。
#
# 提供：
#   - rtt_stats       : ping RTT 统计，输出 "avg loss_pct"
#   - iperf_tcp       : iperf3 TCP 吞吐（Mbps）
#   - iperf_udp_loss  : iperf3 UDP 丢包率
#   - json_set        : 往 JSON 结果文件写一个 key/value
#
# 依赖：容器内 ping / iperf3 / jq，host 端 jq。

# ping RTT 统计：输出 "avg loss_pct"
# 用法: rtt_stats <client-container> <dst-ip> [count]
# - -i 0.05 间隔（20pps），-q 安静模式
# - ping -q 最后输出: "rtt min/avg/max/mdev = x/y/z/w ms"，取 avg（斜杠分隔第 2 值）
# - sed -nE 用 POSIX ERE，跨平台兼容（macOS BSD grep 不支持 grep -oP）
rtt_stats() {
  local c="$1" dst="$2" count="${3:-200}"
  local tmp avg loss
  tmp=$(dex "$c" ping -c "$count" -i 0.05 -q "$dst" 2>&1) || true
  avg=$(echo "$tmp" | sed -nE 's|.*rtt min/avg/max/mdev = [^/]+/([^/]+)/.*|\1|p' | tail -1)
  loss=$(echo "$tmp" | sed -nE 's|.*[^0-9]([0-9]+)% packet loss.*|\1|p' | tail -1)
  echo "${avg:-0} ${loss:-0}"
}

# iperf3 TCP 吞吐（Mbps）
# 用法: iperf_tcp <client-container> <dst-ip> [streams] [time]
iperf_tcp() {
  local client="$1" dst="$2" streams="${3:-1}" time="${4:-10}"
  dex "$client" iperf3 -c "$dst" -t "$time" -P "$streams" -J 2>/dev/null \
    | jq -r '.end.sum_received.bits_per_second / 1e6' 2>/dev/null || echo "0"
}

# iperf3 UDP 丢包率
# 用法: iperf_udp_loss <client-container> <dst-ip> [rate] [time]
iperf_udp_loss() {
  local client="$1" dst="$2" rate="${3:-100M}" time="${4:-10}"
  dex "$client" iperf3 -c "$dst" -u -b "$rate" -t "$time" -J 2>/dev/null \
    | jq -r '.end.sum.lost_percent // 100' 2>/dev/null || echo "100"
}

# 往 JSON 结果文件写一个 key/value（val 须是合法 JSON 数字/字符串）
# 用法: json_set <file> <key> <val>
# - 数字直接传:  json_set r.json rtt_avg 12.34
# - 字符串加引号: json_set r.json unit '"ms"'
json_set() {
  local file="$1" key="$2" val="$3"
  jq --arg k "$key" --argjson v "$val" '.[$k] = $v' "$file" >"$file.tmp" && mv "$file.tmp" "$file"
}
