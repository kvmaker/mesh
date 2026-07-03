#!/bin/bash
set -euo pipefail

# Mesh VPN 服务器安装脚本（Linux systemd）
# 用法: sudo ./install-server.sh [meshd 二进制路径] [配置文件路径]

BINARY="${1:-./meshd}"
CONFIG="${2:-}"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/etc/mesh"
CERT_DIR="/etc/mesh/certs"
SERVICE_FILE="/etc/systemd/system/meshd.service"

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: 需要 root 权限运行"
    exit 1
fi

if [ ! -f "$BINARY" ]; then
    echo "Error: 找不到 meshd 二进制: $BINARY"
    echo "用法: sudo ./install-server.sh <meshd二进制路径> [meshd.yaml路径]"
    exit 1
fi

echo "==> 安装 meshd 到 $INSTALL_DIR"
cp "$BINARY" "$INSTALL_DIR/meshd"
chmod +x "$INSTALL_DIR/meshd"

echo "==> 创建数据目录"
mkdir -p "$DATA_DIR" "$CERT_DIR"
chmod 700 "$DATA_DIR" "$CERT_DIR"

if [ -n "$CONFIG" ] && [ -f "$CONFIG" ]; then
    echo "==> 安装配置文件"
    cp "$CONFIG" "$DATA_DIR/meshd.yaml"
elif [ ! -f "$DATA_DIR/meshd.yaml" ]; then
    echo "==> 生成默认配置（请修改 domain 字段）"
    cat > "$DATA_DIR/meshd.yaml" << 'EOF'
domain: "your-domain.com"
listen_addr: ":443"
network: "10.100.0.0/24"
data_dir: "/etc/mesh"
cert_dir: "/etc/mesh/certs"
tun_name: "mesh0"
tun_mtu: 1300
EOF
    echo "    配置文件: $DATA_DIR/meshd.yaml"
fi

echo "==> 初始化数据库和 Token"
meshd init
echo ""

echo "==> 创建 systemd 服务"
cat > "$SERVICE_FILE" << 'EOF'
[Unit]
Description=Mesh VPN Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/meshd run
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable meshd

echo ""
echo "=== 安装完成 ==="
echo "  二进制: $INSTALL_DIR/meshd"
echo "  配置:   $DATA_DIR/meshd.yaml"
echo "  服务:   $SERVICE_FILE"
echo ""
echo "下一步:"
echo "  1. 编辑 $DATA_DIR/meshd.yaml 设置 domain"
echo "  2. 确保端口 443 和 80 开放"
echo "  3. 启动: sudo systemctl start meshd"
echo "  4. 查看 Token: meshd token show"
