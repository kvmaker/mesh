# bug TODO 索引

> e2e 测试、codereview 或线上反馈发现的 mesh 缺陷修复。与 performance（优化）和 architecture（演进）区分：bug 是"功能坏了"，必须修。

每个条目都有独立详细分析文档，路径见下表。

| ID | 标题 | 优先级 | 状态 | 详细分析 |
|----|------|--------|------|----------|
| B00 | Linux TUN IFF_VNET_HDR offload 导致 TCP checksum 损坏（TCP over mesh 全断） | P0 | [ ] | [B00.md](B00.md) |

---

## 各条目摘要

### B00. Linux TUN IFF_VNET_HDR offload 导致 TCP checksum 损坏

mesh 在 Linux 上所有 TCP over mesh 流量完全不可用（~0 Mbps），UDP/ICMP 正常。根因：`golang.zx2c4.com/wireguard/tun` 的 `CreateTUN` 强制 `IFF_VNET_HDR`，kernel 对 TCP 包不软件算 checksum（留给 offload），mesh 读 TUN 后直接转发不修正，对端 kernel 因 checksum incorrect 丢弃所有 TCP 数据段。e2e T05 性能场景抓包定位。修复涉及架构决策（与 T04 offset 同源），待 brainstorm 方案。

---

## 推荐执行顺序

1. B00 — TCP checksum（P0，阻塞 mesh Linux TCP 可用性）
