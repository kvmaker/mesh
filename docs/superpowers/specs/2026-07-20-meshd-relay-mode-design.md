# meshd relay 模式设计

- 日期: 2026-07-20
- 状态: 设计已批准,待实施
- 分支: `feat/meshd-relay-mode`

## 1. 背景与动机

meshd 当前在 `NewTunnelServer` 中**无条件**创建 TUN 设备(`mesh0`)并分配 server VPN IP(`10.100.0.1`),使 server 本机成为 VPN 子网的一个节点。这带来两个痛点:

- **CAP_NET_ADMIN 强依赖**:创建/配置 TUN 接口需要该能力,阻碍容器化部署,增大特权攻击面。
- **多应用部署困难**:在同一主机(如 `gz-ubuntu`)上与其它 web 应用(`aigw` 等)共存时,meshd 的特权需求与降权运行的统一网关(Caddy)形成不对称,meshd 无法优雅地退到反代之后。

但 meshd 的核心职责——**客户端之间的包中转**——完全不依赖 TUN(`clientReadLoop` 中客户端→客户端走 WS→WS 直转,不经 mesh0)。TUN 仅服务于"server 本机进出 VPN"这一额外能力(参见前序讨论的路径 ②③)。

因此引入 **relay 模式**:meshd 退化为纯中继,不创建 TUN、不分配 server IP,从而去掉 CAP_NET_ADMIN 依赖,适配降权 / 容器化 / 多应用共存场景。

## 2. 目标与非目标

**目标**
- 支持 `mode: relay` 配置,meshd 不创建 TUN、不分配 server IP。
- 默认 `mode: full` 保持现有行为,存量部署零改动(向后兼容)。
- `install.sh` / systemd 联动:relay 模式生成的 unit 去掉 `CAP_NET_ADMIN`。
- 提供多应用部署文档(Caddy 示例)。

**非目标(YAGNI)**
- 不改变客户端协议(register 响应、WS 隧道格式不变)。
- 不改变 IP 分配逻辑(`device.Allocate` 不动)。
- 不引入新的转发模式(如 NAT 网关、L3 路由)。
- 不在 relay 模式下提供"server 本机接入 VPN"的替代方案。

## 3. 配置设计

`internal/server/config/config.go`:

```go
type Config struct {
    Domain     string `yaml:"domain"`
    ListenAddr string `yaml:"listen_addr"`
    Network    string `yaml:"network"`
    DataDir    string `yaml:"data_dir"`
    CertDir    string `yaml:"cert_dir"`
    TunName    string `yaml:"tun_name"`
    TunMTU     int    `yaml:"tun_mtu"`
    Mode       string `yaml:"mode"` // 新增:"full"(默认) | "relay"

    TLSTestMode bool `yaml:"-"`
}
```

- `Default()` 设 `Mode: "full"`。
- `Load()` 解析后校验 `Mode ∈ {"full", "relay"}`,空值视为 `"full"`,非法值复用 `server.loadCfg` 现有 warning 机制回退 `"full"` 并打印告警。
- relay 模式下 `tun_name` / `tun_mtu` 字段被忽略(文档注明,不报错)。

## 4. 架构与组件改动

实现结构采用 **Approach A**:`NewTunnelServer` 内部按 mode 分支。理由:现有测试 `server_test.go:55` 已按 `tun=nil` 构造 `TunnelServer`,nil 鲁棒性本就是隐含契约,改动最集中。(对比 Approach B 双工厂 `NewTunnelServerFull/Relay`,`clientReadLoop`/`Close` 仍要处理 `tun=nil`,收益不抵重复代码,pass。)

### 4.1 `internal/server/tunnel/server.go`

| 位置 | full 模式(不变) | relay 模式(新增) |
|---|---|---|
| `NewTunnelServer:67` | CreateTUN + ConfigureInterface + 解析 tunIP | 跳过 TUN 创建,`tun=nil, tunName="", tunIP=netip.Addr{}` |
| `Start:91` | `go readTUN` + `go statsLoop` | 仅 `go statsLoop`(`if ts.tun != nil` 守卫 readTUN) |
| `clientReadLoop:254` | `if dst == ts.tunIP { 写 TUN }` | `if ts.tun != nil && dst == ts.tunIP`,否则 relay 下该包走 routeMiss 丢弃 |
| `Close:275` | `return ts.tun.Close()` | `if ts.tun != nil { return ts.tun.Close() }; return nil` |

### 4.2 不变项
- `device.Allocate`(`ip.go:32` 从 `.2` 起,`.1` 天然不被分配)。
- register 协议(`handler_register.go`,客户端无感知)。
- `meshd init`(不涉及 TUN)。

## 5. 数据流

relay 模式下三条路径:

| 路径 | 行为 |
|---|---|
| 客户端 A → 客户端 B | ✅ 不变。WS→WS 直转,不经 mesh0。 |
| 客户端 A → server IP(`10.100.0.1`) | ❌ 丢弃。`dst != ts.tunIP`(零值),Lookup 失败,routeMiss 计数 +1。**预期行为**:relay 模式 server 不提供本机 VPN 服务。 |
| server 本机 → 客户端 | ❌ 不可用。无 mesh0,server 协议栈无 VPN 路由,包发不出。**预期行为**。 |

## 6. 错误处理与降级

- **非法 mode 值**:Load 后校验失败 → warning + 回退 full(复用 `server.loadCfg:42-50` 现有机制)。绝不因配置笔误进入未定义状态。
- **relay 模式误配 TUN 相关字段**:忽略,不报错(字段语义上仅 full 模式生效)。
- **relay 模式收到发给 server 的包**:静默丢弃 + routeMiss 计数,由 statsLoop 聚合输出(与现有未知路由包处理一致)。

## 7. 测试策略

### 7.1 `internal/server/config/config_test.go`
- `mode` 默认值为 `"full"`。
- 合法值 `full` / `relay` 正确解析。
- 空字符串 → `"full"`。
- 非法值(如 `"foo"`)→ 回退 `"full"`(若校验产生 warning 则一并断言)。

### 7.2 `internal/server/tunnel/server_test.go`
- **relay 模式 `NewTunnelServer` 返回 `tun==nil`**:CI 可跑(不创建真 TUN)。断言 `ts.tun == nil`、`ts.tunName == ""`。
- **`clientReadLoop` 在 `tun==nil` 时不尝试注入 TUN**:构造 `tun=nil` 的 TunnelServer,注入 dst=`10.100.0.1`(传统 server IP 地址,relay 模式未分配)的包,断言 routeMiss +1、无 panic。
- **`clientReadLoop` 在 `tun==nil` 时正常转发客户端间包**:对照测试,确保中转不受影响。
- 现有 `TestServerIP` / `TestRoutePacket` / `TestRoutePacketNoRoute` 等不受影响。

## 8. 部署侧联动

### 8.1 `install.sh`
- `install_server` 新增 `--mode <full|relay>` 参数,默认 `full`。
- 写 `/etc/mesh/meshd.yaml` 时落 `mode: "${mode}"`。
- 生成的 systemd unit:
  - **full**:保留现有 `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE`。
  - **relay**:去掉 `CAP_NET_ADMIN CAP_NET_RAW`(核心收益落地);`CAP_NET_BIND_SERVICE` 视是否仍绑 443 决定(配合 Caddy 反代时 meshd 绑本地高位端口,可一并去掉)。

### 8.2 文档
新增 `docs/deploy/caddy-multi-app.md`:relay 模式 + Caddy 多应用部署示例,衔接 `gz-ubuntu` 场景(meshd + aigw 共存)。

## 9. 向后兼容性

- 存量 `/etc/mesh/meshd.yaml` 无 `mode` 字段 → 视为 `full` → 行为零变化。
- 存量客户端配置、register 协议、WS 隧道格式 → 零变化。
- 切换 mode 是纯服务端配置变更,客户端无需感知或升级。

## 10. 未覆盖范围 / 后续

- relay 模式的 Docker 镜像化(去 CAP_NET_ADMIN 后更易打包)——后续独立工作。
- 若未来 relay 模式需要 server 主动探活客户端,再评估轻量方案(当前 YAGNI)。

## 11. 范围扩展(2026-07-20 实施评审后发现并经用户批准)

原 spec 范围仅含"去 TUN"。实施评审(I-1)发现:relay 模式未解决 meshd 独占 `:443`/`:80` + 自带 TLS(autocert)的问题,导致文档描述的"Caddy 反代 meshd 到本地端口"架构跑不通(autocert 无法完成 ACME + Caddy 明文反代与 meshd TLS 握手失败)。另发现 M1:install.sh relay 仅去掉 `AmbientCapabilities`(未授),root 启动仍继承 CAP_NET_ADMIN。经用户批准扩展范围:

- **新增 `tls_mode: autocert | none` 配置**(默认 `autocert`,向后兼容)。
- `tls_mode: none` 时 meshd 走纯 HTTP(`http.Server.ListenAndServe`),不启动 autocert、不监听 `:80`,适用于 relay 模式配合反向代理(Caddy)。
- **install.sh relay 模式默认落 `tls_mode: none`**,systemd unit 加 `CapabilityBoundingSet=!CAP_NET_ADMIN CAP_NET_RAW` 显式 deny(修正 M1),并去掉 `CAP_NET_BIND_SERVICE`(relay 绑本地高位端口)。
- 文档(`caddy-multi-app.md`)更新为真实的 plain HTTP 架构,消除 I-1 矛盾。

**不变项(仍不动):** 客户端协议、`device.Allocate`、`meshd init`、full 模式行为。

`TLSTestMode`(env `MESH_TEST_TLS`,e2e 自签)优先级高于 `tls_mode`:测试模式仍走自签,不受 yaml `tls_mode` 影响。
