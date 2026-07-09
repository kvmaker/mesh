# Mesh TODO 索引

> Last updated: 2026-07-09

## 类别

| 代码 | 类别 | 说明 |
|------|------|------|
| P | performance | 当前中转架构下的程序级性能、延迟、吞吐和可观测性优化 |
| A | architecture | 参考 Tailscale / ZeroTier / Cloudflare One 的 Mesh 架构演进方向 |
| B | bug | e2e/codereview/线上发现的 mesh 缺陷修复（功能坏了，必须修） |

## 状态标记

- `[ ]` 未开始
- `[~]` 进行中
- `[x]` 已完成
- `[!]` 阻塞（注明原因）

## 优先级

- **P0**：关键，阻塞其他工作
- **P1**：高价值，尽快做
- **P2**：正常优先级
- **P3**：锦上添花 / 研究向

## 当前进度

| 类别 | 已完成 | 总数 | 完成率 |
|------|--------|------|--------|
| performance | 2 | 10 | 20% |
| architecture | 0 | 12 | 0% |
| bug | 2 | 2 | 100% |

## 目录结构

```text
docs/todo/
├── README.md
├── performance/
│   ├── performance.md           # 类别索引 + 条目摘要
│   ├── P00.md                   # 详细分析
│   ├── P01.md
│   ├── P02.md
│   ├── P03.md
│   ├── P04.md
│   ├── P05.md
│   ├── P06.md
│   ├── P07.md
│   ├── P08.md
│   └── P09.md
└── architecture/
    ├── architecture.md          # 类别索引 + 条目摘要
    ├── A00.md                   # 详细分析
    ├── A01.md
    ├── A02.md
    ├── A03.md
    ├── A04.md
    ├── A05.md
    ├── A06.md
    ├── A07.md
    ├── A08.md
    ├── A09.md
    ├── A10.md
    └── A11.md
```

> 约定：每个 TODO 都有对应的独立 `.md` 详细分析文件。类别总览文件 `performance.md` / `architecture.md` 保持纯索引 + 摘要，避免与详细分析文档重复维护。

## 推荐执行顺序

### performance（先做地基）

1. P00 — 热路径日志降噪
2. P01 — 本地 mesh 地址发夹短路
3. P02 — 异步转发队列与单写者模型
4. P03 — 队列背压、丢包策略与统计
5. P04 — 转发路径可观测性与状态展示
6. P05 — TUN 批量读写
7. P06 — buffer pool 降低热路径 GC
8. P07 — WebSocket frame 协议扩展与批处理
9. P08 — 多 WebSocket lane 并行转发
10. P09 — 多 gateway relay path 与 PathManager（程序级版本）

### architecture（在 performance 地基上扩展架构）

1. A00 — 控制面 / 数据面模块解耦
2. A02 — PathManager 统一路径抽象
3. A01 — Gateway map 与 home relay 选择
4. A03 — Peer Relay
5. A04 — LAN direct 按需直连
6. A09 — ACL / 默认 deny（与 A03/A04 同步审视）
7. A05 — 公网 NAT traversal direct
8. A06 — MagicDNS
9. A07 — Subnet Router
10. A08 — Exit Node
11. A10 — 端到端加密数据面
12. A11 — QUIC / MASQUE 数据通道
