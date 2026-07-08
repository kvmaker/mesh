# bug TODO 索引

> e2e 测试、codereview 或线上反馈发现的 mesh 缺陷修复。与 performance（优化）和 architecture（演进）区分：bug 是"功能坏了"，必须修。

每个条目都有独立详细分析文档，路径见下表。

| ID | 标题 | 优先级 | 状态 | 详细分析 |
|----|------|--------|------|----------|
| B00 | Linux TUN IFF_VNET_HDR offload 导致 TCP checksum 损坏（TCP over mesh 全断） | P0 | [x] | [B00.md](B00.md) |
| B01 | TunnelClient main loop 卡 tun.Read，无背景流量时 SIGTERM 不退出/不自动重连 | P1 | [x] | [B01.md](B01.md) |

---

## 各条目摘要

### B00. Linux TUN IFF_VNET_HDR offload 导致 TCP checksum 损坏

mesh 在 Linux 上所有 TCP over mesh 流量完全不可用（~0 Mbps），UDP/ICMP 正常。根因：`golang.zx2c4.com/wireguard/tun` 的 `CreateTUN` 强制 `IFF_VNET_HDR`，kernel 对 TCP 包不软件算 checksum（留给 offload），mesh 读 TUN 后直接转发不修正，对端 kernel 因 checksum incorrect 丢弃所有 TCP 数据段。e2e T05 性能场景抓包定位。修复涉及架构决策（与 T04 offset 同源），待 brainstorm 方案。

### B01. TunnelClient main loop 卡 tun.Read，SIGTERM/重连依赖外部流量

`connect` 主循环 `select{ctx.Done; default} + tun.Read`，一旦阻塞在 `tun.Read`（无 TUN 入流量）就不响应 ctx cancel。后果：无背景流量时 SIGTERM 不退出（实测 80s+）、server 故障后不自动重连（死锁）。e2e T06 发现，workaround 是场景脚本主动 ping 产生流量。修复方向：ctx cancel 时 close TUN fd 触发 Read 返回。

---

## 推荐执行顺序

1. ~~B00 — TCP checksum（P0）~~ ✅ 已修复（commit 4b4a943，TCP 877Mbps）
2. ~~B01 — main loop ctx cancel 死锁（P1）~~ ✅ 已修复（commit a3868b8，SIGTERM 116ms）
