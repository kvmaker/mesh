# architecture TODO 索引

> 参考 Tailscale、ZeroTier、Cloudflare One / WARP 等成熟方案梳理的 Mesh 演进方向。重点是控制面 / 数据面分离、relay 降级为 fallback、direct / peer relay 引入、ACL / DNS / 安全策略统一、可观测性完善。

每个条目都有独立详细分析文档，路径见下表。

| ID | 标题 | 优先级 | 状态 | 详细分析 |
|----|------|--------|------|----------|
| A00 | 控制面与数据面职责解耦 | P1 | [ ] | [A00.md](A00.md) |
| A01 | Gateway map 与 home relay 选择 | P1 | [ ] | [A01.md](A01.md) |
| A02 | PathManager 统一路径抽象 | P1 | [ ] | [A02.md](A02.md) |
| A03 | Peer Relay：让优质节点作为专用中继 | P2 | [ ] | [A03.md](A03.md) |
| A04 | LAN direct：同局域网按需直连 | P2 | [ ] | [A04.md](A04.md) |
| A05 | 公网 direct / NAT traversal | P2 | [ ] | [A05.md](A05.md) |
| A06 | MagicDNS：设备名解析 | P2 | [ ] | [A06.md](A06.md) |
| A07 | Subnet Router：节点宣告子网路由 | P3 | [ ] | [A07.md](A07.md) |
| A08 | Exit Node：全流量出口 | P3 | [ ] | [A08.md](A08.md) |
| A09 | ACL / 身份驱动策略 | P2 | [ ] | [A09.md](A09.md) |
| A10 | 端到端加密数据面（relay 盲转密文） | P3 | [ ] | [A10.md](A10.md) |
| A11 | QUIC / MASQUE 数据通道实验 | P3 | [ ] | [A11.md](A11.md) |

---

## 各条目摘要

### A00. 控制面与数据面职责解耦

把 `meshd` 拆为 `internal/control`（注册、IP 分配、peer 列表、policy 下发、gateway map）、`internal/relay`（盲转数据）、`internal/path`（PathManager）。保留单进程多模块，接口清晰后可独立部署。后续所有架构演进都依赖此分层。

### A01. Gateway map 与 home relay 选择

`server_domain` 演进为 `gateways[]`；注册响应携带 gateway map；client 周期性探测 RTT/丢包/队列深度，选 home + backup relay。模拟 Tailscale DERP map / ZeroTier roots+moons 的就近接入。

### A02. PathManager 统一路径抽象

`PeerRoute` / `Path` / `PathManager`；策略：health → RTT → queue depth → 手动优先级；熔断与恢复。MVP 只暴露 `relay` path，接口预留 `peer_relay / direct / lan_direct`。

### A03. Peer Relay

设备表增加 relay 角色字段；PathManager 增加 `peer_relay` 类型；路径优先级 `direct > peer_relay > server relay`；ACL 限制中继范围，避免成为跳板。

### A04. LAN direct

设备注册时上报 LAN 候选 endpoint；按需探测（仅当 peer 实际有持续流量才发起）；成功切 direct，失败回 relay。默认关闭，按需开启。

### A05. 公网 direct / NAT traversal

设备上报 UDP endpoint 与 NAT 类型；双方同时打洞；失败熔断回 relay；symmetric NAT 标记 `relay_only`。最高收益，最复杂。

### A06. MagicDNS

控制面派生 `*.mesh` 域名；客户端拦截 system DNS；内部 DNS 通道（DoH over control plane 或 UDP/53 over mesh tunnel）；本地缓存 + TTL + 设备状态联动。

### A07. Subnet Router

节点声明 `advertised_routes: [192.168.1.0/24]`；控制面审核后下发；客户端动态调整 TUN 路由表；配合 ACL 限制可访问范围。

### A08. Exit Node

节点声明 `exit_node: true`；客户端 `mesh up --exit-node <peer>` 切换默认路由到 mesh TUN；强依赖 ACL 与审计。

### A09. ACL / 身份驱动策略

设备增加 tag；策略语法 `src/dst/proto/ports/action`；数据面在转发前匹配策略；控制面下发可见 peer 子集；默认 deny。

### A10. 端到端加密数据面

设备长期密钥 + per-peer 短期 session key；数据面包 `{version, dst_mesh_ip, nonce, encrypted_ip_packet, auth_tag}`；server 仍可看 `dst_mesh_ip` 路由，但看不到 IP payload；可选 Noise/WireGuard 风格握手。

### A11. QUIC / MASQUE 数据通道

引入 `Transport` 抽象；QUIC 作为 fast path，WebSocket/TLS 保留为 fallback；transport 协商握手；QUIC 提供 0-RTT 与 connection migration；与 A10 协同（QUIC 仅 transport security）；UDP 屏蔽时自动回退。

---

## 推荐执行顺序

1. A00 — 控制面 / 数据面模块解耦
2. A02 — PathManager 统一路径抽象
3. A01 — Gateway map 与 home relay 选择
4. A03 — Peer Relay
5. A04 — LAN direct 按需直连
6. A09 — ACL（与 A03/A04 同步审视）
7. A05 — 公网 NAT traversal direct
8. A06 — MagicDNS
9. A07 — Subnet Router
10. A08 — Exit Node
11. A10 — 端到端加密数据面
12. A11 — QUIC / MASQUE 数据通道

---

## 关联成熟方案参考

- Tailscale Control and data planes：https://tailscale.com/docs/concepts/control-data-planes
- Tailscale NAT traversal 改进：https://tailscale.com/blog/nat-traversal-improvements-pt-1
- Tailscale DERP servers：https://tailscale.com/docs/reference/derp-servers
- Tailscale Peer Relays：https://tailscale.com/blog/peer-relays-international-networks
- ZeroTier Protocol：https://docs.zerotier.com/protocol
- ZeroTier Private Root Servers：https://docs.zerotier.com/roots
- Cloudflare One Client architecture：https://developers.cloudflare.com/cloudflare-one/team-and-resources/devices/cloudflare-one-client/configure/route-traffic/client-architecture
- Cloudflare post-quantum WARP：https://blog.cloudflare.com/post-quantum-warp
