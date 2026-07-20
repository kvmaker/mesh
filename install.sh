#!/bin/bash
set -euo pipefail

# Mesh VPN 一键安装脚本
# 用法:
#   服务器: curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --domain your.domain.com
#   客户端: curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- client

REPO="kvmaker/mesh"
INSTALL_DIR="/usr/local/bin"
VERSION=""

usage() {
    cat << 'EOF'
Mesh VPN 安装脚本

用法:
  install.sh server --domain <domain>   安装服务器（meshd + systemd 服务）
  install.sh client                     安装客户端（mesh + 后台服务）
  install.sh uninstall                  卸载

示例:
  # 服务器(默认 full 模式,server 作为 VPN 节点)
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --domain vpn.example.com

  # 服务器(relay 模式,纯中继,不创建 TUN,可降权/容器化)
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --mode relay --domain vpn.example.com

  # 客户端（macOS/Linux）
  curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- client
EOF
    exit 1
}

detect_os() {
    case "$(uname -s)" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      echo "unsupported"; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)            echo "unsupported"; exit 1 ;;
    esac
}

get_latest_version() {
    # 方法1: GitHub API（可能被限流）
    local v=""
    if command -v curl &>/dev/null; then
        v=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    fi
    if [ -n "$v" ]; then echo "$v"; return; fi

    # 方法2: 从 redirect URL 解析（不需要 API 配额）
    if command -v curl &>/dev/null; then
        v=$(curl -fsSI "https://github.com/$REPO/releases/latest" 2>/dev/null | grep -i "^location:" | sed -E 's|.*/v([^[:space:]]+).*|\1|')
    fi
    if [ -n "$v" ]; then echo "$v"; return; fi

    # 方法3: 使用 gh CLI
    if command -v gh &>/dev/null; then
        v=$(gh release view --repo "$REPO" --json tagName -q '.tagName' 2>/dev/null | sed 's/^v//')
    fi
    echo "$v"
}

download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget &>/dev/null; then
        wget -qO "$dest" "$url"
    else
        echo "Error: 需要 curl 或 wget"
        exit 1
    fi
}

install_binary() {
    local binary="$1" os arch version filename url tmpdir

    os=$(detect_os)
    arch=$(detect_arch)

    if [ -n "$VERSION" ]; then
        version="$VERSION"
    else
        version=$(get_latest_version)
    fi

    if [ -z "$version" ]; then
        echo "Error: 无法获取最新版本号。请使用 --version 指定，如: install.sh --version 2.1.0 client"
        exit 1
    fi

    filename="${binary}_${version}_${os}_${arch}.tar.gz"
    url="https://github.com/$REPO/releases/download/v${version}/${filename}"

    echo "  版本: v${version}"
    echo "  平台: ${os}/${arch}"
    echo "  下载: ${url}"

    tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" EXIT

    download "$url" "$tmpdir/$filename"
    tar -xzf "$tmpdir/$filename" -C "$tmpdir"

    cp "$tmpdir/$binary" "$INSTALL_DIR/$binary"
    chmod +x "$INSTALL_DIR/$binary"
    echo "  安装: $INSTALL_DIR/$binary"
}

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

    if [ -z "$domain" ]; then
        echo "Error: 需要指定 --domain"
        echo "用法: install.sh server --domain your.domain.com"
        exit 1
    fi

    echo "==> 安装 meshd"
    install_binary "meshd"

    echo "==> 创建配置"
    mkdir -p /etc/mesh/certs
    chmod 700 /etc/mesh /etc/mesh/certs

    local tls_mode_line=""
    local listen_addr_line="listen_addr: \":443\""
    if [ "$mode" = "relay" ]; then
        tls_mode_line="tls_mode: \"none\""
        listen_addr_line="listen_addr: \"127.0.0.1:8443\""
    fi
    if [ ! -f /etc/mesh/meshd.yaml ]; then
        cat > /etc/mesh/meshd.yaml << EOF
domain: "${domain}"
mode: "${mode}"
${tls_mode_line}
${listen_addr_line}
network: "10.100.0.0/24"
data_dir: "/etc/mesh"
cert_dir: "/etc/mesh/certs"
tun_name: "mesh0"
tun_mtu: 1300
EOF
    fi

    echo "==> 初始化"
    meshd init

    echo "==> 配置 systemd 服务"
    local caps_line="AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE"
    if [ "$mode" = "relay" ]; then
        # relay 模式:不创建 TUN、纯 HTTP 绑本地端口。
        # CapabilityBoundingSet 显式 deny,确保即便以 root 启动也不具备
        # CAP_NET_ADMIN/CAP_NET_RAW(防被攻破后创建 TUN/抓包)。
        caps_line="CapabilityBoundingSet=!CAP_NET_ADMIN CAP_NET_RAW"
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

    systemctl daemon-reload
    systemctl enable meshd
    systemctl start meshd

    echo ""
    echo "=== 服务器安装完成 ==="
    echo "  域名:  ${domain}"
    echo "  Token: $(meshd token show)"
    echo "  状态:  systemctl status meshd"
    echo ""
    echo "客户端安装命令:"
    echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/master/install.sh | sudo bash -s -- client"
}

install_client() {
    local os
    os=$(detect_os)

    echo "==> 安装 mesh"
    install_binary "mesh"

    case "$os" in
        darwin) install_client_macos ;;
        linux)  install_client_linux ;;
    esac

    echo ""
    echo "=== 客户端安装完成 ==="
    echo ""
    echo "使用方法:"
    echo "  1. 注册: mesh join <domain> --token <token>"
    echo "  2. 启动: sudo mesh up （或启动后台服务）"
}

install_client_macos() {
    local plist="/Library/LaunchDaemons/com.mesh.vpn.plist"
    local real_user="${SUDO_USER:-$(whoami)}"
    local real_home
    real_home=$(eval echo "~$real_user")

    echo "==> 配置 launchd 服务"
    cat > "$plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.mesh.vpn</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/mesh</string>
        <string>up</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>${real_home}</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/mesh.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/mesh.log</string>
</dict>
</plist>
EOF

    echo "  服务: $plist"
    echo "  日志: /tmp/mesh.log"
    echo ""
    echo "注册后启动:"
    echo "  sudo launchctl load $plist"
}

install_client_linux() {
    echo "==> 配置 systemd 服务"
    cat > /etc/systemd/system/mesh.service << 'EOF'
[Unit]
Description=Mesh VPN Client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/mesh up
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable mesh
    echo "  服务: /etc/systemd/system/mesh.service"
    echo ""
    echo "注册后启动:"
    echo "  sudo systemctl start mesh"
}

do_uninstall() {
    local os
    os=$(detect_os)

    echo "==> 停止服务"
    case "$os" in
        darwin)
            launchctl unload /Library/LaunchDaemons/com.mesh.vpn.plist 2>/dev/null || true
            rm -f /Library/LaunchDaemons/com.mesh.vpn.plist
            ;;
        linux)
            systemctl stop meshd mesh 2>/dev/null || true
            systemctl disable meshd mesh 2>/dev/null || true
            rm -f /etc/systemd/system/meshd.service /etc/systemd/system/mesh.service
            systemctl daemon-reload
            ip link del mesh0 2>/dev/null || true
            ;;
    esac

    echo "==> 删除二进制"
    rm -f /usr/local/bin/meshd /usr/local/bin/mesh

    echo "==> 删除数据"
    rm -rf /etc/mesh

    local real_user="${SUDO_USER:-$(whoami)}"
    local real_home
    real_home=$(eval echo "~$real_user")
    rm -rf "$real_home/.mesh"

    echo "=== 卸载完成 ==="
}

# --- main ---
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: 需要 root 权限，请使用 sudo 运行"
    exit 1
fi

# 解析全局参数
while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        *) break ;;
    esac
done

case "${1:-}" in
    server)   shift; install_server "$@" ;;
    client)   shift; install_client "$@" ;;
    uninstall) do_uninstall ;;
    *)        usage ;;
esac
