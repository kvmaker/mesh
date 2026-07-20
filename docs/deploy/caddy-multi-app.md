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
- **切换 mode**:`install.sh` 仅在 `/etc/mesh/meshd.yaml` 不存在时生成它(幂等,不覆写已有配置)。若已在 full 模式部署后想切到 relay,需手动编辑 yaml 将 `mode: full` 改为 `mode: relay`,再重新跑 `install.sh server --mode relay` 以刷新 systemd unit(去掉 `CAP_NET_ADMIN`)并 `sudo systemctl restart meshd`。
