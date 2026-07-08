#!/usr/bin/env bash
# tc netem 预设封装。在容器内对 eth0 设置丢包/延迟模拟。
#
# 本文件为库，场景脚本（或容器内手工排障）source 后调用：
#   source /usr/local/bin/netem-preset.sh
#   netem wan
#
# 预设：
#   clean     清除所有 qdisc（恢复默认）
#   baseline  等价 clean（无干扰基线）
#   wan       典型公网：80ms±10ms 延迟，1% 丢包
#   bad       恶劣网络：200ms±50ms 延迟，5% 丢包
#   satellite 卫星链路：600ms±100ms 延迟，2% 丢包
#
# 依赖：iproute2（tc），容器需 NET_ADMIN + privileged（compose 已配置）。
# 注意：root qdisc 不存在时 `tc qdisc del` 会报错，统一 `|| true` 忽略。

netem() {
  local preset="${1:-clean}"
  case "$preset" in
    clean|baseline)
      tc qdisc del dev eth0 root 2>/dev/null || true
      ;;
    wan)
      tc qdisc del dev eth0 root 2>/dev/null || true
      tc qdisc add dev eth0 root netem delay 80ms 10ms loss 1%
      ;;
    bad)
      tc qdisc del dev eth0 root 2>/dev/null || true
      tc qdisc add dev eth0 root netem delay 200ms 50ms loss 5%
      ;;
    satellite)
      tc qdisc del dev eth0 root 2>/dev/null || true
      tc qdisc add dev eth0 root netem delay 600ms 100ms loss 2%
      ;;
    *)
      echo "unknown netem preset: $preset" >&2
      return 1
      ;;
  esac
}
