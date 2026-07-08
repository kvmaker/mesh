# Mesh e2e 测试（基于 Docker）— 设计 Spec

> 状态：等待用户 review
> 日期：2026-07-08
> 范围：仅设计，不含实施

## 1. 背景

当前 `tests/integration_test.go` 是单进程 Go 集成测试（HTTP API、SQLite、设备表），不验证：

- 真实 TUN 设备的创建与路由注入
- 真实跨主机（容器间）包转发
- 性能指标（RTT、吞吐、丢包）
- 链路恶化（延迟/抖动/丢包）下的行为
- server / client 故障下的重连与优雅退出

为补足这些维度，本 spec 设计一套基于 Docker 的 e2e 测试，输出可观测的指标与明确的 pass/fail。

## 2. 目标

- 验证 mesh VPN 的端到端正确性：TUN 路由、跨 client 转发、server 中继、设备上下线、ACME/自签证书下连通。
- 量化核心性能：RTT p50/p95/p99、TCP 吞吐、UDP 丢包率。
- 验证故障路径：server 重启、client 失联、SIGTERM 优雅退出。
- 提供 CI 可消费的 PASS/FAIL 报告。
- 与现有 `tests/integration_test.go` 解耦：后者负责快速集成验证，e2e 负责深度验证。

## 3. 非目标

- 不覆盖安全/加密协议的负面测试（无效 token 拒绝等）—— 留给单元测试。
- 不做 macOS host 上的 TUN e2e —— macOS Docker 不支持 utun，跨平台 TUN 验证统一在 Linux 容器内进行。
- 不做大规模（>50 client）负载测试 —— 本 spec 关注 2 client + 1 server 的核心场景。
- 不在 spec 内实现，只描述场景与接口。

## 4. 运行模型

### 4.1 容器拓扑

```yaml
# tests/e2e/docker-compose.yml
version: "3.9"
services:
  server:
    build: { context: ../.., dockerfile: tests/e2e/Dockerfile.server }
    image: mesh-e2e/server:dev
    privileged: true
    cap_add: [NET_ADMIN, NET_RAW]
    networks: [meshnet]

  client-a:
    build: { context: ../.., dockerfile: tests/e2e/Dockerfile.client }
    image: mesh-e2e/client:dev
    privileged: true
    cap_add: [NET_ADMIN, NET_RAW]
    devices: ["/dev/net/tun:/dev/net/tun"]
    networks: [meshnet]
    depends_on: [server]

  client-b:
    同 client-a
    depends_on: [server]

networks:
  meshnet:
    driver: bridge
```

要点：

- 全部 `privileged: true` + `NET_ADMIN/RAW`，TUN + tc 才能用。
- 通过 Docker bridge 网络通信，模拟"跨主机"。
- server / client / iperf3/nuttcp 都跑在容器内，host 不需装额外工具。
- host 是 macOS / Linux / CI ubuntu-runner 都能跑同一份 compose。

### 4.2 镜像基线

- 基础镜像：`ubuntu:24.04`。
- server 镜像：`meshd` 二进制 + bash + curl + jq + ca-certificates。
- client 镜像：`mesh` 二进制 + bash + iperf3 + nuttcp + iputils-ping + fping + iproute2 + jq + ca-certificates。

### 4.3 TUN 处理

- Linux 容器用 `/dev/net/tun` 创建 `mesh0`，`internal/tun/tun_linux.go` 已支持，无需 build tag。
- macOS host 不直接 e2e，但代码路径保留（`tun_darwin.go`）。

## 5. 目录结构

```text
tests/e2e/
├── docker-compose.yml
├── Dockerfile.client
├── Dockerfile.server
├── run.sh                    # 一键启动 / 收尾
├── lib/
│   ├── helpers.sh            # wait_ready / register_device / show_logs
│   └── metrics.sh            # 收集 RTT/吞吐/丢包
├── scenarios/
│   ├── 01-connectivity.sh    # P0
│   ├── 02-performance.sh     # P0
│   └── 03-failure.sh         # P1
├── fixtures/
│   ├── meshd.yaml            # server 配置
│   └── netem.sh              # tc netem 封装
└── results/
    └── <timestamp>/          # JSON + log 输出
```

驱动：bash + 简单 helper。  
理由：直接调 iperf3 / tc / ss / curl，跨平台问题少，CI 友好。

## 6. 关键设计选择

### 6.1 TLS / 证书

- 容器内 server 拿不到 Let's Encrypt 证书。
- 引入 `MESH_TEST_TLS=on` 开关（推荐在 `internal/config` 落地）：
  - server 跳过 `acme/autocert`，改用自签证书。
  - client `mesh join` 端允许自签（`InsecureSkipVerify`，已在 `internal/client/peers.go` 使用过）。
- 退出测试时无需清理证书目录，容器销毁即可。

### 6.2 网络模拟（tc netem）

`fixtures/netem.sh` 封装：

```bash
netem clean       <iface>
netem baseline    <iface>            # 0 干扰
netem wan         <iface>            # 80ms ± 10ms, 1% loss
netem bad         <iface>            # 200ms ± 50ms, 5% loss
netem satellite   <iface>            # 600ms ± 100ms, 2% loss
```

### 6.3 性能工具

- `iperf3`：TCP 吞吐（`1 stream`、`4 stream`）、UDP 模式。
- `nuttcp`：备用，覆盖长流 + 小包。
- `ping -c 200 -i 0.01`：RTT 分布。
- `fping -p 20 -c 50`：并行 ping 抖动。
- `ss -ti` / `netstat -s`：重传统计。

### 6.4 mesh 启动流程

server 容器：

```text
1. /usr/local/bin/meshd init
2. /usr/local/bin/meshd run
3. 等 :443 可达 / `/api/devices` 200
```

client 容器：

```text
1. wait_for_server (curl https://server:443/api/devices)
2. mesh join <server-domain> --token <tok>
3. mesh up
4. ip route show | grep 10.100.0.0/24
5. ping 10.100.0.1
```

### 6.5 失败注入

- server 容器：`docker compose kill server` / `docker compose restart server`。
- 链路：`tc qdisc change ... loss 50%`。
- client：`docker compose kill client-a`。

## 7. 场景拆分

### 7.1 场景 01：连通性与路由（P0）

```text
01.1 启动 server，等 /api/devices 可达
01.2 client-a join + up；client-b join + up
01.3 ip route 校验：10.100.0.0/24 dev mesh0
01.4 ping 10.100.0.1（server）必须通
01.5 ping client-b（10.100.0.3）必须通
01.6 ping 不存在的 10.100.0.99 必须 100% 丢包
01.7 kill client-b；client-a ping 10.100.0.3 应超时
01.8 server route table 移除
01.9 restart client-b；重新 join；client-a ping 恢复
01.10 fping -p 20 -c 50 不丢
```

判定：每个 case 必须 100% 符合预期，任意 fail → 整体 fail。

### 7.2 场景 02：性能与抖动（P0）

```text
02.1 baseline iperf3 -c 10.100.0.3 -t 30 -P 1
02.2 iperf3 -c 10.100.0.3 -t 30 -P 4
02.3 iperf3 -u -b 100M -t 30（UDP 100Mbps）
02.4 加 wan netem 后重测 02.1 / 02.3
02.5 加 bad netem 后重测 02.1
02.6 ping -c 200 -i 0.01 收集 RTT
02.7 5 分钟长流，验证 Tx/Rx 计数不漂移
```

输出 `02-performance.json`：

```json
{
  "tcp_1stream_mbps": 92.3,
  "tcp_4stream_mbps": 110.5,
  "udp_100m_loss_pct": 0.7,
  "wan_tcp_mbps": 78.4,
  "wan_udp_loss_pct": 1.4,
  "rtt_p50_ms": 81.2,
  "rtt_p95_ms": 95.0,
  "rtt_p99_ms": 110.4
}
```

判定：**软门槛**默认只报告；`--strict` 触发硬门槛：

```text
rtt_p95_ms < 200
tcp_1stream_mbps > 30
wan_udp_loss_pct < 5
```

### 7.3 场景 03：故障 / 重连 / 优雅退出（P1）

```text
03.1 docker kill server；观察 client 错误日志；2-5s 内重连尝试
03.2 server restart；client 自动恢复
03.3 重建连接后 ping / iperf 复测
03.4 长流中短暂 server 中断，client 重建，iperf 重新建立
03.5 满队列抗压：iperf3 + tc 丢包 30%，观察 drop 计数与吞吐变化
03.6 SIGTERM client：必须 <=2s 退出，不留 zombie goroutine
03.7 client 连续 join 10 次，server 必须正确处理
```

## 8. 结果收集与判定

```text
results/
└── 2026-07-08T10-30-00/
    ├── 01-connectivity.log
    ├── 01-connectivity.json
    ├── 02-performance.log
    ├── 02-performance.json
    ├── 03-failure.log
    ├── 03-failure.json
    └── summary.txt
```

`summary.txt`：

```text
[P0] 01-connectivity: PASS (9/9)
[P0] 02-performance: PASS (soft); tcp=92Mbps rtt_p95=95ms
[P1] 03-failure:      PASS (6/6)
Overall: PASS
```

## 9. CI 集成

GitHub Actions（在 `.github/workflows/` 下新增 `e2e.yml`）：

```yaml
e2e:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - run: docker compose -f tests/e2e/docker-compose.yml build
    - run: ./tests/e2e/run.sh --all
    - run: ./tests/e2e/run.sh --report | tee junit.xml
    - uses: actions/upload-artifact@v4
      with: { name: e2e-report, path: tests/e2e/results/ }
```

- push 触发软门槛（默认）。
- merge to master 触发硬门槛（`--strict`）。
- release tag 触发全量 + 严格。

## 10. 与现有 `tests/integration_test.go` 的边界

```text
tests/integration_test.go   单元 + HTTP API 集成（无 TUN / 无网络）
tests/e2e/                  完整 e2e（容器 + TUN + tc + 性能）
```

不重叠。前者跑得快（CI 默认每次 push），后者跑得慢（合并前 / release 前）。

## 11. 实施 TODO

实施时建议拆为以下 TODO（落到 `docs/todo/testing/`）：

- T00：MESH_TEST_TLS 开关 + server 自签支持。
- T01：Dockerfile.server / Dockerfile.client 与 docker-compose.yml。
- T02：lib/helpers.sh + lib/metrics.sh。
- T03：fixtures/netem.sh。
- T04：scenario 01-connectivity。
- T05：scenario 02-performance。
- T06：scenario 03-failure。
- T07：run.sh + 结果聚合 + summary.txt。
- T08：CI workflow。

## 12. 风险与缓解

| 风险 | 缓解 |
|------|------|
| `tc` 在 macOS host 不可用 | e2e 全在 Linux 容器内 |
| Docker privileged 模式安全风险 | 仅 CI/本地开发，文档明示 |
| 性能数据受 host 负载影响 | 软门槛 + 历史趋势对比 |
| server 拿不到 ACME 证书 | 引入 `MESH_TEST_TLS=on` 开关 |
| `mesh join` 在 TUN 起来前需要 DNS | 容器内 `/etc/hosts` 注入 server 别名 |
| 长时间 iperf 占用 CI 资源 | 默认 30s 短流，CI 用 `--quick` 模式 |

## 13. 参考资料

- 当前 `tests/integration_test.go`
- `internal/tunnel/router.go`、`internal/tunnel/server.go`（P02 已落地）
- `docs/todo/performance/performance.md`（性能目标基线）
- Tailscale / ZeroTier 的 e2e 思路（控制面与数据面分离 + 容器化拓扑）
