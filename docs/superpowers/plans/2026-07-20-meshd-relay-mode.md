# meshd relay 模式 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 meshd 支持 `mode: relay` 配置,在此模式下不创建 TUN 设备、不分配 server IP,退化为纯中继,去掉 CAP_NET_ADMIN 依赖。

**Architecture:** `NewTunnelServer` 内部按 `cfg.Mode` 分支:relay 模式跳过 `CreateTUN`/`ConfigureInterface`,`tun` 字段为 nil,各方法加 nil 守卫。提取 `routeClientPacket` 对称于 `routePacket`,使 WS→TUN/WS 的单包路由可单测。默认 `mode: full` 保持现有行为。

**Tech Stack:** Go 1.x、`coder/websocket`、`modernc.org/sqlite`(纯 Go,CI 无 CGO)、YAML 配置、bash `install.sh`、systemd。

## Global Constraints

- 默认 `mode: "full"`,存量配置(`/etc/mesh/meshd.yaml` 无 `mode` 字段)行为零变化。
- relay 模式下 `tun_name` / `tun_mtu` 被忽略,不报错。
- `device.Allocate`、register 协议、`meshd init` **不动**。
- 测试用 `modernc.org/sqlite`(已在 `go.mod`),禁止引入 CGO 依赖。
- 提交信息中文,格式 `<类型>: <描述>`。
- 开发分支 `feat/meshd-relay-mode`(已建并已提交 spec)。

## File Structure

| 文件 | 责任 | 操作 |
|---|---|---|
| `internal/server/config/config.go` | 增加 `Mode` 字段、常量、`normalizeMode` 校验 | Modify |
| `internal/server/config/config_test.go` | mode 默认值/合法值/空值/非法值测试 | Modify |
| `internal/server/tunnel/server.go` | `NewTunnelServer` 分支、`Start`/`Close` 守卫、提取 `routeClientPacket` | Modify |
| `internal/server/tunnel/server_test.go` | relay 构造测试 + `routeClientPacket` 测试 + db helper | Modify |
| `install.sh` | `--mode` 参数、yaml 落 mode、systemd unit 条件去 caps | Modify |
| `docs/deploy/caddy-multi-app.md` | relay + Caddy 多应用部署文档 | Create |

---

## Task 1: config 增加 Mode 字段与校验

**Files:**
- Modify: `internal/server/config/config.go`
- Test: `internal/server/config/config_test.go`

**Interfaces:**
- Produces: `config.ModeFull == "full"`、`config.ModeRelay == "relay"` 常量;`Config.Mode string` 字段;`(*Config).normalizeMode()` 方法(在 `Load` 末尾调用)。Task 2 的 `NewTunnelServer` 依赖 `cfg.Mode == config.ModeRelay`。

- [ ] **Step 1: 写失败测试**

在 `internal/server/config/config_test.go` 末尾追加:

```go
func TestModeDefault(t *testing.T) {
	cfg := Default()
	if cfg.Mode != ModeFull {
		t.Fatalf("expected default Mode=%q, got %q", ModeFull, cfg.Mode)
	}
}

func TestModeLoadValues(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"mode: full\n", ModeFull},
		{"mode: relay\n", ModeRelay},
		{"mode: RELAY\n", ModeRelay},   // 大小写不敏感
		{"mode:  relay \n", ModeRelay}, // 容忍空白
		{"domain: x.com\n", ModeFull},  // 未指定 → 默认 full
		{"mode: foo\n", ModeFull},      // 非法 → 回退 full
		{"mode: \n", ModeFull},         // 空值 → full
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "cfg.yaml")
			os.WriteFile(p, []byte(tc.yaml), 0644)
			cfg, err := Load(p)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Mode != tc.want {
				t.Fatalf("yaml=%q: expected Mode=%q, got %q", tc.yaml, tc.want, cfg.Mode)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/config/ -run TestMode -v`
Expected: 编译失败,`undefined: ModeFull`(常量尚未定义)。

- [ ] **Step 3: 实现 config.go**

在 `internal/server/config/config.go` 中做三处改动。

(3a) 在 import 块后、`type Config` 前加常量:

```go
// Mode 取值。full = 创建 TUN、分配 server IP(server 作为 VPN 节点);
// relay = 纯中继,不创建 TUN、不分配 server IP,仅做客户端间转发。
const (
	ModeFull  = "full"
	ModeRelay = "relay"
)
```

(3b) 在 `Config` 结构体加字段(放在 `TunMTU` 之后、`TLSTestMode` 之前):

```go
	Mode string `yaml:"mode"`
```

(3c) 在 `Default()` 的字面量里加默认值(紧挨 `TunMTU: 1300,` 之后):

```go
		Mode:     ModeFull,
```

(3d) 加 `normalizeMode` 方法,并在 `Load` 末尾调用。把现有 `Load` 改为:

```go
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalizeMode()
	return cfg, nil
}

// normalizeMode 把 Mode 归一化为合法值。空或非法值回退 full 并打印告警,
// 避免配置笔误让进程进入未定义状态。warning 风格仿 server.loadCfg。
func (c *Config) normalizeMode() {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", ModeFull:
		c.Mode = ModeFull
	case ModeRelay:
		c.Mode = ModeRelay
	default:
		fmt.Fprintf(os.Stderr, "warning: unknown mode %q, falling back to %q\n", c.Mode, ModeFull)
		c.Mode = ModeFull
	}
}
```

(`strings` / `os` / `fmt` 已在 config.go 的 import 中,无需新增。)

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/config/ -v`
Expected: 全部 PASS,包括新增 `TestModeDefault` / `TestModeLoadValues` 与既有用例。

- [ ] **Step 5: 提交**

```bash
git add internal/server/config/config.go internal/server/config/config_test.go
git commit -m "feat(config): 新增 mode 配置项支持 full/relay"
```

---

## Task 2: tunnel/server.go 支持 relay 模式

**Files:**
- Modify: `internal/server/tunnel/server.go`(改 `NewTunnelServer`、`Start`、`clientReadLoop`、`Close`,新增 `routeClientPacket`)
- Test: `internal/server/tunnel/server_test.go`(加 db helper + 4 个测试)

**Interfaces:**
- Consumes: `config.ModeRelay`(Task 1 产出)。
- Produces: relay 模式下 `NewTunnelServer` 返回 `tun==nil`、`tunName==""`、`tunIP` 为零值;`routeClientPacket(pkt []byte)` 处理 WS→TUN/WS 单包路由,relay 模式下 `ts.tun==nil` 自动跳过"注入 TUN"分支。

- [ ] **Step 1: 加 db helper 与失败测试**

在 `internal/server/tunnel/server_test.go` 顶部 import 块改为(新增 `database/sql`、`path/filepath`、config、db 包):

```go
import (
	"database/sql"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxyu/mesh/internal/server/config"
	"github.com/maxyu/mesh/internal/server/db"
)
```

在文件末尾追加 helper 与测试:

```go
// setupTestDB 建一个已 migrate 的临时 sqlite,供需要 *sql.DB 的测试使用。
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return d
}

// TestNewTunnelServerRelayNoTUN 验证 relay 模式不创建 TUN、不分配 tunName。
// CI 可跑:relay 路径不触碰 /dev/net/tun。
func TestNewTunnelServerRelayNoTUN(t *testing.T) {
	d := setupTestDB(t)
	cfg := config.Default()
	cfg.Mode = config.ModeRelay

	ts, err := NewTunnelServer(d, cfg)
	if err != nil {
		t.Fatalf("NewTunnelServer relay: %v", err)
	}
	defer ts.Close()

	if ts.tun != nil {
		t.Fatalf("relay mode must not create TUN, got non-nil tun")
	}
	if ts.tunName != "" {
		t.Fatalf("relay mode tunName must be empty, got %q", ts.tunName)
	}
}

// TestCloseRelayNilTUN 验证 relay 模式(ts.tun==nil)下 Close 不 panic。
func TestCloseRelayNilTUN(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil
	if err := ts.Close(); err != nil {
		t.Fatalf("Close on nil tun should be no-op, got %v", err)
	}
}

// TestRouteClientPacketRelayDropsServerBound 验证 relay 模式(tun==nil)下,
// 发给传统 server IP(10.100.0.1)的包被丢弃并计入 routeMiss。
func TestRouteClientPacketRelayDropsServerBound(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil ⇒ relay 语义
	ts.routeClientPacket(makeIPv4Packet(netip.MustParseAddr("10.100.0.1")))
	if got := ts.routeMiss.Load(); got != 1 {
		t.Fatalf("relay: expected routeMiss=1 for server-bound packet, got %d", got)
	}
}

// TestRouteClientPacketRelayForwardsToClient 验证 relay 模式下,
// 客户端间转发不受影响(对称于 TestRoutePacket)。
func TestRouteClientPacketRelayForwardsToClient(t *testing.T) {
	ts := &TunnelServer{router: NewRouter()} // tun==nil ⇒ relay 语义
	dstIP := netip.MustParseAddr("10.100.0.5")
	cc := NewClientConn(nil, "dev1", dstIP, 4)
	t.Cleanup(cc.Close)
	ts.router.Register(dstIP, cc)

	ts.routeClientPacket(makeIPv4Packet(dstIP))
	if got := cc.QueueDepth.Load(); got != 1 {
		t.Fatalf("relay: expected packet forwarded to client, QueueDepth=%d", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/tunnel/ -run 'TestNewTunnelServerRelay|TestCloseRelay|TestRouteClientPacket' -v`
Expected: 编译失败,`undefined: routeClientPacket`(方法尚未提取);且 `NewTunnelServer` 在 relay 模式仍尝试创建 TUN,CI 上 `TestNewTunnelServerRelayNoTUN` 会因 CreateTUN 失败而 FAIL。

- [ ] **Step 3: 改 `NewTunnelServer` 加 mode 分支**

把 `internal/server/tunnel/server.go` 中现有 `NewTunnelServer` 整体替换为:

```go
// NewTunnelServer creates a TunnelServer. In full mode it initializes the TUN
// device and configures the network interface so the server itself joins the
// VPN subnet. In relay mode it skips TUN creation entirely — the server acts
// purely as a packet relay between clients and needs no CAP_NET_ADMIN.
func NewTunnelServer(db *sql.DB, cfg *config.Config) (*TunnelServer, error) {
	ts := &TunnelServer{
		db:     db,
		cfg:    cfg,
		router: NewRouter(),
	}
	if cfg.Mode == config.ModeRelay {
		// tun / tunName / tunIP 保持零值:tun==nil 即 relay 标志,
		// Start / routeClientPacket / Close 据此走 nil 守卫分支。
		return ts, nil
	}

	srvIP, err := serverIP(cfg.Network)
	if err != nil {
		return nil, err
	}
	dev, name, err := meshtun.CreateTUN(cfg.TunName, cfg.TunMTU)
	if err != nil {
		return nil, err
	}
	if err := meshtun.ConfigureInterface(name, srvIP, cfg.Network); err != nil {
		dev.Close()
		return nil, err
	}
	ts.tun = dev
	ts.tunName = name
	ts.tunIP = netip.MustParseAddr(srvIP)
	return ts, nil
}
```

- [ ] **Step 4: 改 `Start` 加 tun 守卫**

把现有 `Start` 替换为:

```go
// Start launches the TUN read loop (full mode only) and the periodic stats
// aggregation loop. In relay mode there is no TUN to read.
func (ts *TunnelServer) Start(ctx context.Context) {
	if ts.tun != nil {
		go ts.readTUN(ctx)
	}
	go ts.statsLoop(ctx)
}
```

- [ ] **Step 5: 提取 `routeClientPacket` 并改写 `clientReadLoop`**

把现有 `clientReadLoop` 整体替换为下面两个方法(路由决策拆出 `routeClientPacket`,对称于 `routePacket`):

```go
// clientReadLoop reads packets from a WebSocket client and dispatches each
// via routeClientPacket until the context is cancelled or the connection errors.
func (ts *TunnelServer) clientReadLoop(ctx context.Context, cc *ClientConn) {
	for {
		_, pkt, err := cc.Conn.Read(ctx)
		if err != nil {
			return
		}
		cc.RecordRx(len(pkt))
		ts.routeClientPacket(pkt)
	}
}

// routeClientPacket handles a single IP packet arriving from a client:
//   - destination is the server's own VPN IP (full mode only) → inject into TUN;
//   - destination is another registered client → forward over its WS;
//   - no route → routeMiss counter.
//
// In relay mode ts.tun==nil, so the first branch is skipped and every packet
// is either forwarded to another client or counted as a route miss. Packets
// addressed to the legacy server IP (e.g. 10.100.0.1) thus fall through to
// routeMiss — expected, since a relay server offers no in-VPN service.
func (ts *TunnelServer) routeClientPacket(pkt []byte) {
	dst, err := ExtractDstIP(pkt)
	if err != nil {
		ts.parseErr.Add(1)
		if debugPacket {
			log.Printf("packet parse error: %v (len=%d)", err, len(pkt))
		}
		return
	}

	if ts.tun != nil && dst == ts.tunIP {
		buf := make([]byte, meshtun.Offset()+len(pkt))
		copy(buf[meshtun.Offset():], pkt)
		bufs := [][]byte{buf}
		if _, err := ts.tun.Write(bufs, meshtun.Offset()); err != nil {
			log.Printf("write to TUN: %v", err)
		}
		return
	}

	if dest, ok := ts.router.Lookup(dst); ok {
		dest.Enqueue(Packet{Data: pkt})
		return
	}

	ts.routeMiss.Add(1)
	if debugPacket {
		log.Printf("no route for %s", dst)
	}
}
```

- [ ] **Step 6: 改 `Close` 加 nil 守卫**

把现有 `Close` 替换为:

```go
// Close shuts down the TUN device. No-op in relay mode (no TUN was created).
func (ts *TunnelServer) Close() error {
	if ts.tun == nil {
		return nil
	}
	return ts.tun.Close()
}
```

- [ ] **Step 7: 跑 tunnel 包全部测试确认通过**

Run: `go test ./internal/server/tunnel/ -v`
Expected: 全部 PASS,包括新增 4 个测试与既有 `TestServerIP` / `TestRoutePacket` / `TestRoutePacketNoRoute` / `TestRoutePacketMalformed` / `TestDebugPacketDefaultOff` / `TestTunnelServerStats`。

- [ ] **Step 8: 跑全量测试确认无回归**

Run: `go test ./...`
Expected: 全部 PASS(config / tunnel / api / device / token / db / common / version)。

- [ ] **Step 9: 提交**

```bash
git add internal/server/tunnel/server.go internal/server/tunnel/server_test.go
git commit -m "feat(tunnel): 支持 relay 模式,不创建 TUN、不分配 server IP"
```

---

## Task 3: install.sh 加 --mode 参数与 systemd unit 分支

**Files:**
- Modify: `install.sh`(改 `install_server` 函数,约第 116-184 行)

**Interfaces:**
- Consumes: Task 1/2 的 `mode` 配置语义。
- Produces: `install.sh server --mode relay --domain <d>` 生成带 `mode: relay` 的 yaml 和不含 `CAP_NET_ADMIN` 的 systemd unit。

> 说明:`install.sh` 是 bash 部署脚本,无 Go 单测。验证靠 `bash -n` 语法检查 + 人工 review 生成逻辑 + 真机部署验证。

- [ ] **Step 1: 语法检查基线(改动前)**

Run: `bash -n install.sh && echo OK`
Expected: 输出 `OK`(确认改动前语法正确,作为基线)。

- [ ] **Step 2: 改 `install_server` 解析 `--mode` 并写入 yaml**

在 `install.sh` 的 `install_server()` 中,把变量声明与参数解析段:

```bash
install_server() {
    local domain=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --domain) domain="$2"; shift 2 ;;
            *) shift ;;
        esac
    done
```

替换为:

```bash
install_server() {
    local domain=""
    local mode="full"

    while [ $# -gt 0 ]; do
        case "$1" in
            --domain) domain="$2"; shift 2 ;;
            --mode)   mode="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    case "$mode" in
        full|relay) ;;
        *) echo "Error: --mode 仅支持 full 或 relay,得到 $mode"; exit 1 ;;
    esac
```

- [ ] **Step 3: yaml heredoc 落 `mode` 字段**

把写 `/etc/mesh/meshd.yaml` 的 heredoc(该 heredoc 用未加引号的 `EOF`,变量会展开):

```bash
    if [ ! -f /etc/mesh/meshd.yaml ]; then
        cat > /etc/mesh/meshd.yaml << EOF
domain: "${domain}"
listen_addr: ":443"
network: "10.100.0.0/24"
data_dir: "/etc/mesh"
cert_dir: "/etc/mesh/certs"
tun_name: "mesh0"
tun_mtu: 1300
EOF
    fi
```

替换为(新增 `mode: "${mode}"` 一行):

```bash
    if [ ! -f /etc/mesh/meshd.yaml ]; then
        cat > /etc/mesh/meshd.yaml << EOF
domain: "${domain}"
mode: "${mode}"
listen_addr: ":443"
network: "10.100.0.0/24"
data_dir: "/etc/mesh"
cert_dir: "/etc/mesh/certs"
tun_name: "mesh0"
tun_mtu: 1300
EOF
    fi
```

- [ ] **Step 4: systemd unit 按 mode 条件生成 caps**

在该函数中,把写 `/etc/systemd/system/meshd.service` 的整段(原 heredoc 用加引号的 `'EOF'`,不展开变量)替换为下面这段(改用未加引号 `EOF`,通过 `caps_line` 变量控制特权行):

```bash
    echo "==> 配置 systemd 服务"
    local caps_line="AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE"
    if [ "$mode" = "relay" ]; then
        # relay 模式不创建 TUN,无需 CAP_NET_ADMIN/CAP_NET_RAW。
        # 若配合反代绑本地高位端口,CAP_NET_BIND_SERVICE 也不需要,一并去掉。
        caps_line="# relay 模式:无需特权能力(TUN/特权端口均不使用)"
    fi
    cat > /etc/systemd/system/meshd.service << EOF
[Unit]
Description=Mesh VPN Server (mode=${mode})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/meshd run
Restart=always
RestartSec=5
${caps_line}

[Install]
WantedBy=multi-user.target
EOF
```

- [ ] **Step 5: 语法检查改动后**

Run: `bash -n install.sh && echo OK`
Expected: 输出 `OK`。

- [ ] **Step 6: 验证用法说明同步**

把 `install.sh` 顶部 `usage()` 中的示例段:

```bash
  # 服务器
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --domain vpn.example.com
```

替换为(补充 `--mode` 说明):

```bash
  # 服务器(默认 full 模式,server 作为 VPN 节点)
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --domain vpn.example.com

  # 服务器(relay 模式,纯中继,不创建 TUN,可降权/容器化)
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --mode relay --domain vpn.example.com
```

- [ ] **Step 7: 再次语法检查**

Run: `bash -n install.sh && echo OK`
Expected: 输出 `OK`。

- [ ] **Step 8: 提交**

```bash
git add install.sh
git commit -m "feat(install): install.sh 支持 --mode,relay 模式去掉 CAP_NET_ADMIN"
```

---

## Task 4: 部署文档 caddy-multi-app.md

**Files:**
- Create: `docs/deploy/caddy-multi-app.md`

**Interfaces:**
- Consumes: Task 1-3 产出的 relay 模式 + install.sh `--mode`。

- [ ] **Step 1: 创建文档**

创建 `docs/deploy/caddy-multi-app.md`,内容如下:

````markdown
# 单机多应用部署:Caddy + meshd(relay)+ aigw

## 场景

单台公网主机(如 `gz-ubuntu`)同时部署 meshd 与其它 web 应用(如 `aigw`)。meshd 默认独占 443/80 且自带 Let's Encrypt,与其它应用冲突。解法:引入 Caddy 作统一入口,meshd 切到 **relay 模式** 退到本地端口。

## 前提:meshd relay 模式

relay 模式下 meshd **不创建 TUN、不分配 server IP、不需要 CAP_NET_ADMIN**,仅做客户端间包中转。代价:客户端无法访问 server 本机服务(10.100.0.1 不可达),server 也无法主动连客户端。纯中继场景下这些都是可接受的。

## 架构

```
Internet → Caddy :443/:80 ─┬─→ meshd  127.0.0.1:8443  (relay, plain HTTP)
                            └─→ aigw   127.0.0.1:8080  (plain HTTP)
```

## 部署步骤

### 1. 安装 meshd(relay 模式)

```bash
curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh \
  | sudo bash -s -- server --mode relay --domain mesh.example.com
```

生成的 `/etc/mesh/meshd.yaml` 含 `mode: relay`,systemd unit 不含 `CAP_NET_ADMIN`。

### 2. meshd 监听本地端口

编辑 `/etc/mesh/meshd.yaml`,把 `listen_addr` 改为本地端口(让 Caddy 反代):

```yaml
mode: relay
listen_addr: "127.0.0.1:8443"
```

> relay 模式下 meshd 仍自带 TLS(autocert)。若希望 TLS 完全交由 Caddy 管理,后续可加 `tls_mode: none` 配置(当前未实现,Caddy 可改用 SNI 四层透传,此处从略)。

### 3. 安装 Caddy

```bash
sudo apt install caddy
```

### 4. 配置 Caddy

编辑 `/etc/caddy/Caddyfile`:

```caddyfile
mesh.example.com {
    reverse_proxy 127.0.0.1:8443   # meshd,WebSocket 自动支持
}

aigw.example.com {
    reverse_proxy 127.0.0.1:8080   # aigw
}
```

重载:`sudo systemctl reload caddy`。Caddy 自动为两个域名签发并续签证书。

### 5. 部署 aigw

按 aigw 自身方式部署,监听 `127.0.0.1:8080`,用 systemd 管理。

## 升级维护

```bash
# 升级 meshd
sudo install -m 755 /tmp/meshd-new /usr/local/bin/meshd && sudo systemctl restart meshd

# 各服务独立重启、独立看日志,互不影响
systemctl status meshd aigw caddy
journalctl -u meshd -f
```

## 限制

- relay 模式下,客户端发给 10.100.0.1 的包被丢弃(server 不提供本机 VPN 服务)。
- server 无法主动发起连接到客户端 IP。
````

- [ ] **Step 2: 提交**

```bash
git add docs/deploy/caddy-multi-app.md
git commit -m "docs(deploy): 新增 Caddy 多应用部署文档(relay 模式)"
```

---

## 完成后

- 跑全量测试:`go test ./...`
- 按 CLAUDE.md 流程,合并回 master 前用 coderabbit 做完整 codereview。

---

# 范围扩展:tls_mode: none(2026-07-20 实施评审后批准)

> 原 Task 1-4 完成 relay 模式(去 TUN)基础。评审发现 relay 未解决 meshd 独占 443/80 + 自带 TLS(I-1),且 install.sh 去 CAP 不彻底(M1)。以下 Task 5-7 扩展范围,让 relay + Caddy 反代真正可用。

## Task 5: config 加 TLSMode 字段

**Files:**
- Modify: `internal/server/config/config.go`
- Test: `internal/server/config/config_test.go`

**Interfaces:**
- Produces: `config.TLSAutocert == "autocert"`、`config.TLSNone == "none"` 常量;`Config.TLSMode string` 字段;`(*Config).normalizeTLSMode()`。Task 6 的 `serveMode` 依赖 `cfg.TLSMode == config.TLSNone`。

- [ ] **Step 1: 写失败测试**

在 `internal/server/config/config_test.go` 末尾追加:

```go
func TestTLSModeDefault(t *testing.T) {
	if Default().TLSMode != TLSAutocert {
		t.Fatalf("expected default TLSMode=%q, got %q", TLSAutocert, Default().TLSMode)
	}
}

func TestTLSModeLoadValues(t *testing.T) {
	cases := []struct {
		yaml string
		want string
	}{
		{"tls_mode: autocert\n", TLSAutocert},
		{"tls_mode: none\n", TLSNone},
		{"tls_mode: NONE\n", TLSNone},
		{"tls_mode:  none \n", TLSNone},
		{"domain: x.com\n", TLSAutocert}, // 未指定 → 默认
		{"tls_mode: foo\n", TLSAutocert}, // 非法 → 回退
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "cfg.yaml")
			os.WriteFile(p, []byte(tc.yaml), 0644)
			cfg, err := Load(p)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.TLSMode != tc.want {
				t.Fatalf("yaml=%q: expected TLSMode=%q, got %q", tc.yaml, tc.want, cfg.TLSMode)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/config/ -run TestTLSMode -v`
Expected: 编译失败,`undefined: TLSAutocert`。

- [ ] **Step 3: 实现 config.go**

(3a) 把现有常量块扩为:

```go
const (
	ModeFull    = "full"
	ModeRelay   = "relay"
	TLSAutocert = "autocert"
	TLSNone     = "none"
)
```

(3b) 在 `Config` 结构体 `Mode` 字段后加:

```go
	TLSMode string `yaml:"tls_mode"`
```

(3c) 在 `Default()` 字面量 `Mode: ModeFull,` 后加:

```go
		TLSMode:  TLSAutocert,
```

(3d) 在 `Load` 中 `cfg.normalizeMode()` 后加一行 `cfg.normalizeTLSMode()`,并新增方法:

```go
// normalizeTLSMode 把 TLSMode 归一化。空或非法值回退 autocert 并打印告警。
// TLSTestMode(env)优先级更高,此处只处理 yaml 的 tls_mode。
func (c *Config) normalizeTLSMode() {
	switch strings.ToLower(strings.TrimSpace(c.TLSMode)) {
	case "", TLSAutocert:
		c.TLSMode = TLSAutocert
	case TLSNone:
		c.TLSMode = TLSNone
	default:
		fmt.Fprintf(os.Stderr, "warning: unknown tls_mode %q, falling back to %q\n", c.TLSMode, TLSAutocert)
		c.TLSMode = TLSAutocert
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/config/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/server/config/config.go internal/server/config/config_test.go
git commit -m "feat(config): 新增 tls_mode 配置项支持 autocert/none"
```

---

## Task 6: api/server.go 支持 tls_mode: none

**Files:**
- Modify: `internal/server/api/server.go`(提取 `serveMode` + 改 `ListenAndServeTLS`)
- Test: `internal/server/api/api_test.go`

**Interfaces:**
- Consumes: `config.TLSNone` / `config.TLSAutocert`(Task 5)、`config.Config.TLSTestMode`(既有)。
- Produces: `(*Server).serveMode() string` 返回 `"selfsigned" | "plain" | "autocert"`;`ListenAndServeTLS` 在 `"plain"` 时走 `srv.ListenAndServe()`(纯 HTTP,不启动 autocert、不监听 :80)。

- [ ] **Step 1: 写失败测试**

在 `internal/server/api/api_test.go` 末尾追加:

```go
func TestServeMode(t *testing.T) {
	cases := []struct {
		name     string
		testMode bool
		tlsMode  string
		want     string
	}{
		{"autocert_default", false, config.TLSAutocert, "autocert"},
		{"plain_none", false, config.TLSNone, "plain"},
		{"selfsigned_testmode", true, config.TLSAutocert, "selfsigned"},
		{"testmode_overrides_none", true, config.TLSNone, "selfsigned"}, // TLSTestMode 优先
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: &config.Config{TLSMode: tc.tlsMode, TLSTestMode: tc.testMode}}
			if got := s.serveMode(); got != tc.want {
				t.Fatalf("serveMode(): expected %q, got %q", tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/api/ -run TestServeMode -v`
Expected: 编译失败,`undefined: s.serveMode`。

- [ ] **Step 3: 提取 `serveMode` 并改写 `ListenAndServeTLS`**

在 `internal/server/api/server.go` 中,新增 `serveMode` 方法(放在 `ListenAndServeTLS` 前):

```go
// serveMode 决定 HTTPS 服务如何启动:
//   - "selfsigned": TLSTestMode(e2e 测试,内存自签证书)
//   - "plain":      tls_mode: none(relay + 反向代理,纯 HTTP,不启动 autocert)
//   - "autocert":   默认(Let's Encrypt + :80 ACME challenge)
//
// TLSTestMode(env MESH_TEST_TLS)优先级高于 yaml tls_mode:测试模式恒走自签。
func (s *Server) serveMode() string {
	if s.cfg.TLSTestMode {
		return "selfsigned"
	}
	if s.cfg.TLSMode == config.TLSNone {
		return "plain"
	}
	return "autocert"
}
```

把现有 `ListenAndServeTLS` 整体替换为(用 `serveMode` 分发,新增 `"plain"` 分支):

```go
func (s *Server) ListenAndServeTLS(ctx context.Context) error {
	srv := &http.Server{
		Addr:                s.cfg.ListenAddr,
		Handler:             s.Handler(),
		ReadHeaderTimeout:   10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	switch s.serveMode() {
	case "selfsigned":
		tlsCfg, err := s.selfSignedTLSConfig()
		if err != nil {
			return fmt.Errorf("self-signed cert: %w", err)
		}
		srv.TLSConfig = tlsCfg
		return srv.ListenAndServeTLS("", "")
	case "plain":
		// tls_mode: none — 纯 HTTP,适用于 relay 模式配合反向代理(Caddy)。
		// 不启动 autocert,不监听 :80。
		return srv.ListenAndServe()
	default: // "autocert"
		m := &autocert.Manager{
			Cache:      autocert.DirCache(s.cfg.CertDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
		}
		srv.TLSConfig = &tls.Config{
			GetCertificate: m.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		go func() {
			if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
				log.Printf("ACME HTTP-01 listener on :80 failed: %v", err)
			}
		}()
		return srv.ListenAndServeTLS("", "")
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/api/ -v`
Expected: 全部 PASS(含新增 `TestServeMode` 与既有用例)。

- [ ] **Step 5: 跑全量测试确认无回归**

Run: `go test ./...`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/server/api/server.go internal/server/api/api_test.go
git commit -m "feat(api): 支持 tls_mode none,纯 HTTP 模式供 relay 反代使用"
```

---

## Task 7: install.sh + 文档收尾(修 I-1 与 M1)

**Files:**
- Modify: `install.sh`(relay 模式落 `tls_mode: none` + unit `CapabilityBoundingSet` deny)
- Modify: `docs/deploy/caddy-multi-app.md`(消除 I-1 矛盾,真实 plain HTTP 架构)

**Interfaces:**
- Consumes: Task 5/6 的 `tls_mode: none` + `serveMode`。

- [ ] **Step 1: install.sh — relay 模式 yaml 落 `tls_mode: none`**

在 `install.sh` 的 `install_server()` 中,把写 yaml 的 heredoc 改为(full 模式不写 tls_mode 走默认 autocert;relay 模式落 none):

```bash
    local tls_mode_line=""
    if [ "$mode" = "relay" ]; then
        tls_mode_line="tls_mode: \"none\""
    fi
    if [ ! -f /etc/mesh/meshd.yaml ]; then
        cat > /etc/mesh/meshd.yaml << EOF
domain: "${domain}"
mode: "${mode}"
${tls_mode_line}
listen_addr: ":443"
network: "10.100.0.0/24"
data_dir: "/etc/mesh"
cert_dir: "/etc/mesh/certs"
tun_name: "mesh0"
tun_mtu: 1300
EOF
    fi
```

> 注:full 模式 `${tls_mode_line}` 为空,yaml 该行为空行(yaml 合法);relay 模式展开为 `tls_mode: "none"`。

- [ ] **Step 2: install.sh — relay unit 加 CapabilityBoundingSet deny(修 M1)**

把 systemd unit 的 `caps_line` 逻辑改为(full 保留 AmbientCapabilities;relay 改为 CapabilityBoundingSet 显式 deny + 去掉 CAP_NET_BIND_SERVICE):

```bash
    local caps_line="AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE"
    if [ "$mode" = "relay" ]; then
        # relay 模式:不创建 TUN、纯 HTTP 绑本地端口。
        # CapabilityBoundingSet 显式 deny,确保即便以 root 启动也不具备
        # CAP_NET_ADMIN/CAP_NET_RAW(防被攻破后创建 TUN/抓包)。
        caps_line="CapabilityBoundingSet=!CAP_NET_ADMIN CAP_NET_RAW"
    fi
```

(`Description=Mesh VPN Server (mode=${mode})` 与 heredoc 其余部分不变。)

- [ ] **Step 3: install.sh 语法检查**

Run: `bash -n install.sh && echo OK`
Expected: `OK`。

- [ ] **Step 4: 文档 — 消除 I-1 矛盾,改为真实 plain HTTP 架构**

编辑 `docs/deploy/caddy-multi-app.md`:

(4a) 把"## 前提:meshd relay 模式"段中的限制说明保留,但把那条矛盾的引用块:

```
> relay 模式下 meshd 仍自带 TLS(autocert)。若希望 TLS 完全交由 Caddy 管理,后续可加 `tls_mode: none` 配置(当前未实现,Caddy 可改用 SNI 四层透传,此处从略)。
```

替换为:

```
> relay 模式下 install.sh 默认写入 `tls_mode: none`,meshd 走纯 HTTP,由 Caddy 统一终止 TLS 并签发证书。meshd 不启动 autocert、不监听 :80。
```

(4b) 把"### 2. meshd 监听本地端口"段中的 yaml 示例与说明:

```
编辑 `/etc/mesh/meshd.yaml`,把 `listen_addr` 改为本地端口(让 Caddy 反代):

```yaml
mode: relay
listen_addr: "127.0.0.1:8443"
```

> relay 模式下 meshd 仍自带 TLS(autocert)...
```

替换为:

```
`install.sh --mode relay` 生成的 yaml 已含 `mode: relay` 与 `tls_mode: none`。编辑 `/etc/mesh/meshd.yaml`,把 `listen_addr` 改为本地端口(让 Caddy 反代):

```yaml
mode: relay
tls_mode: none
listen_addr: "127.0.0.1:8443"
```

meshd 现在是纯 HTTP 服务,Caddy 明文反代即可,无 TLS 握手问题。
```

(4c) 在"### 1. 安装 meshd(relay 模式)"的命令块后,补一句说明 install.sh 已自动落 tls_mode: none 与去 CAP 的 unit。

(4d) 在"## 限制"节,保留现有三条 bullet(含 mode 切换提示)。

- [ ] **Step 5: 提交**

```bash
git add install.sh docs/deploy/caddy-multi-app.md
git commit -m "feat(install,docs): relay 模式联动 tls_mode none 与 CapabilityBoundingSet,修正 Caddy 反代架构"
```
