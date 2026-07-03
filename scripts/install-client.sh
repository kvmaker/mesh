#!/bin/bash
set -euo pipefail

# Mesh VPN 客户端安装脚本（macOS launchd / Linux systemd）
# 用法: sudo ./install-client.sh <mesh 二进制路径>

BINARY="${1:-./mesh}"
OS="$(uname -s)"

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: 需要 root 权限运行"
    exit 1
fi

if [ ! -f "$BINARY" ]; then
    echo "Error: 找不到 mesh 二进制: $BINARY"
    echo "用法: sudo ./install-client.sh <mesh二进制路径>"
    exit 1
fi

echo "==> 安装 mesh 到 /usr/local/bin"
cp "$BINARY" /usr/local/bin/mesh
chmod +x /usr/local/bin/mesh

case "$OS" in
    Darwin)
        install_macos
        ;;
    Linux)
        install_linux
        ;;
    *)
        echo "Error: 不支持的系统: $OS"
        exit 1
        ;;
esac

install_macos() {
    PLIST="/Library/LaunchDaemons/com.mesh.vpn.plist"
    # 获取实际用户（sudo 下 $SUDO_USER 是原始用户）
    REAL_USER="${SUDO_USER:-$(whoami)}"
    REAL_HOME=$(eval echo "~$REAL_USER")

    echo "==> 创建 launchd 服务 (用户: $REAL_USER, HOME: $REAL_HOME)"
    cat > "$PLIST" << EOF
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
        <string>${REAL_HOME}</string>
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

    echo ""
    echo "=== 安装完成 ==="
    echo "  二进制: /usr/local/bin/mesh"
    echo "  服务:   $PLIST"
    echo "  日志:   /tmp/mesh.log"
    echo ""
    echo "下一步:"
    echo "  1. 注册: mesh join <domain> --token <token>"
    echo "  2. 启动服务: sudo launchctl load $PLIST"
    echo "  3. 停止服务: sudo launchctl unload $PLIST"
}

install_linux() {
    SERVICE_FILE="/etc/systemd/system/mesh.service"

    echo "==> 创建 systemd 服务"
    cat > "$SERVICE_FILE" << 'EOF'
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

    echo ""
    echo "=== 安装完成 ==="
    echo "  二进制: /usr/local/bin/mesh"
    echo "  服务:   $SERVICE_FILE"
    echo ""
    echo "下一步:"
    echo "  1. 注册: mesh join <domain> --token <token>"
    echo "  2. 启动服务: sudo systemctl start mesh"
    echo "  3. 停止服务: sudo systemctl stop mesh"
}

case "$OS" in
    Darwin)
        install_macos
        ;;
    Linux)
        install_linux
        ;;
esac
