#!/usr/bin/env bash
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
    echo "This uninstaller must run as root"
    exit 1
fi

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/vaultfleet"

echo "==> Stopping VaultFleet Agent..."
if command -v systemctl &>/dev/null && systemctl list-unit-files vaultfleet-agent.service &>/dev/null; then
    systemctl stop vaultfleet-agent 2>/dev/null || true
    systemctl disable vaultfleet-agent 2>/dev/null || true
    rm -f /etc/systemd/system/vaultfleet-agent.service
    systemctl daemon-reload
elif command -v rc-service &>/dev/null && [ -f /etc/init.d/vaultfleet-agent ]; then
    rc-service vaultfleet-agent stop 2>/dev/null || true
    rc-update del vaultfleet-agent default 2>/dev/null || true
    rm -f /etc/init.d/vaultfleet-agent
else
    pkill -f "${INSTALL_DIR}/vaultfleet-agent" 2>/dev/null || true
fi

echo "==> Removing binaries..."
rm -f "${INSTALL_DIR}/vaultfleet-agent"
rm -f "${INSTALL_DIR}/restic" "${INSTALL_DIR}/restic-real"
rm -f "${INSTALL_DIR}/rclone"

echo "==> Removing configuration..."
rm -rf "${CONFIG_DIR}"

echo "==> VaultFleet Agent uninstalled successfully!"
