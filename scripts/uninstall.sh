#!/bin/bash
set -euo pipefail

# Mesh VPN 卸载脚本
# 用法: sudo ./uninstall.sh

OS="$(uname -s)"

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: 需要 root 权限运行"
    exit 1
fi

echo "==> 停止服务"
case "$OS" in
    Darwin)
        launchctl unload /Library/LaunchDaemons/com.mesh.vpn.plist 2>/dev/null || true
        rm -f /Library/LaunchDaemons/com.mesh.vpn.plist
        ;;
    Linux)
        systemctl stop meshd 2>/dev/null || true
        systemctl stop mesh 2>/dev/null || true
        systemctl disable meshd 2>/dev/null || true
        systemctl disable mesh 2>/dev/null || true
        rm -f /etc/systemd/system/meshd.service /etc/systemd/system/mesh.service
        systemctl daemon-reload
        ip link del mesh0 2>/dev/null || true
        ;;
esac

echo "==> 删除二进制"
rm -f /usr/local/bin/meshd /usr/local/bin/mesh

echo "==> 删除数据"
rm -rf /etc/mesh

echo "==> 删除客户端配置"
REAL_USER="${SUDO_USER:-$(whoami)}"
REAL_HOME=$(eval echo "~$REAL_USER")
rm -rf "$REAL_HOME/.mesh"

echo ""
echo "=== 卸载完成 ==="
