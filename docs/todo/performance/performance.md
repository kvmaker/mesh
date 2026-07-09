# performance TODO 索引

> 当前中转架构下的程序级性能优化，重点降低延迟抖动、避免慢连接阻塞、提升吞吐、减少 GC 和日志开销，并为后续多路径 / 多 gateway / P2P 演进预留边界。

每个条目都有独立详细分析文档，路径见下表。

| ID | 标题 | 优先级 | 状态 | 详细分析 |
|----|------|--------|------|----------|
| P00 | 热路径日志降噪 | P1 | [x] | [P00.md](P00.md) |
| P01 | 本地 mesh 地址发夹短路 | P1 | [ ] | [P01.md](P01.md) |
| P02 | 异步转发队列与单写者模型 | P1 | [x] | [P02.md](P02.md) |
| P03 | 队列背压、丢包策略与统计 | P1 | [ ] | [P03.md](P03.md) |
| P04 | 转发路径可观测性与状态展示 | P1 | [ ] | [P04.md](P04.md) |
| P05 | TUN 批量读写 | P2 | [ ] | [P05.md](P05.md) |
| P06 | buffer pool 降低热路径 GC | P2 | [ ] | [P06.md](P06.md) |
| P07 | WebSocket frame 协议扩展与批处理 | P2 | [ ] | [P07.md](P07.md) |
| P08 | 多 WebSocket lane 并行转发 | P3 | [ ] | [P08.md](P08.md) |
| P09 | 多 gateway relay path 与 PathManager | P3 | [ ] | [P09.md](P09.md) |

---

## 各条目摘要

### P00. 热路径日志降噪

去掉 `internal/tunnel/server.go` 默认每包日志，仅在 `MESH_DEBUG_PACKET=1` 时打印；增加周期性聚合统计日志。降低热路径 I/O 与格式化开销。

### P01. 本地 mesh 地址发夹短路

client TUN → WS 转发前判断 `dst == localIP`，本机地址直接丢弃（或写回 TUN），不送往 server。避免 `ping 自己 mesh IP` 走两轮 server 中继。

### P02. 异步转发队列与单写者模型

每个 `ClientConn` 增加 bounded `SendQueue`；连接建立后启动独立 `writeLoop`；转发路径只 enqueue。避免慢 peer 阻塞读循环、降低并发写风险。

### P03. 队列背压、丢包策略与统计

队列必须 bounded；满时按 `drop_tail` / `drop_head` 策略丢包；统计 `drop_packets`、`queue_depth`、`queue_max_depth`。避免延迟雪崩。

### P04. 转发路径可观测性与状态展示

per peer 增加 `rx/tx/bytes/drops/queue/rtt/last_active`；server 全局 `active_clients/route_miss/write_errors/tun_errors`；`mesh status` 与 `meshd stats` 展示。

### P05. TUN 批量读写

将 TUN read buffer 数量从 1 扩到 16/32；单次 read 多个包后逐个 route/enqueue。降低 syscall 与调度开销。

### P06. buffer pool 降低热路径 GC

引入 `Packet{Buf, Len, Release}` 和 `sync.Pool`；enqueue/写完后再 Release。降低 GC 频率与尾延迟。

### P07. WebSocket frame 协议扩展与批处理

引入 frame header `{version, type, payload}`；类型包括 `IPPacket/Batch/Ping/Pong/Control`；批处理 16 包/64KB/1ms。兼容旧协议（首字节高 4 bit 判别）。

### P08. 多 WebSocket lane 并行转发

每个 client 建多条 data lane；按内层五元组 hash 选 lane，避免 TCP 乱序。控制 lane 1 条 + data lane N 条，缓解单外层 TCP 队头阻塞。

### P09. 多 gateway relay path 与 PathManager

引入 `PathManager` 抽象 `relay/direct/peer_relay`；多 gateway 同时连接；选最低 RTT healthy gateway，失败切换。MVP 假设所有 client 都连同一组 gateway。

---

## 推荐执行顺序

1. P00 — 热路径日志降噪
2. P01 — 本地 mesh 地址发夹短路
3. P02 — 异步转发队列与单写者模型
4. P03 — 队列背压、丢包策略与统计
5. P04 — 转发路径可观测性与状态展示
6. P05 — TUN 批量读写
7. P06 — buffer pool 降低热路径 GC
8. P07 — WebSocket frame 协议扩展与批处理
9. P08 — 多 WebSocket lane 并行转发
10. P09 — 多 gateway relay path 与 PathManager
