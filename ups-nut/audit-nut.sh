#!/usr/bin/env bash
# NUT Config Audit Script - Phase 1
# Run on each target host (vault, suburban) to collect current config state
# Usage: ssh root@<host> 'bash -s' < audit-nut.sh > audit-<hostname>.txt 2>&1

set -e

HOSTNAME=$(hostname -s)
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S %Z')

echo "====================================================================="
echo "NUT Configuration Audit - $HOSTNAME"
echo "Timestamp: $TIMESTAMP"
echo "====================================================================="
echo

# --- Basic host info ---
echo "# HOST INFORMATION"
echo "Hostname: $(hostname -f)"
echo "IPv6 Address: $(ip -6 addr show | grep -oP '(?<=inet6\s)\d[a-f0-9:]+' | grep -v '^fe80' | head -1)"
echo "Kernel: $(uname -r)"
echo

# --- NUT Installation Status ---
echo "# NUT INSTALLATION STATUS"
which upsd upsc upsmon upssched 2>/dev/null || echo "Warning: Some NUT commands not found"
echo "NUT Version:"
upsd -V 2>/dev/null || echo "  (upsd not available)"
echo

# --- Service Status ---
echo "# SERVICE STATUS"
systemctl status upsd --no-pager || echo "upsd not running"
echo
systemctl status upsmon --no-pager || echo "upsmon not running"
echo

# --- /etc/nut/ Contents ---
echo "# /etc/nut/ DIRECTORY LISTING"
ls -la /etc/nut/ 2>/dev/null || echo "Directory not found or not readable"
echo

# --- nut.conf ---
echo "# /etc/nut/nut.conf"
echo "--- BEGIN ---"
cat /etc/nut/nut.conf 2>/dev/null || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- ups.conf ---
echo "# /etc/nut/ups.conf"
echo "--- BEGIN ---"
cat /etc/nut/ups.conf 2>/dev/null || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- upsd.conf ---
echo "# /etc/nut/upsd.conf"
echo "--- BEGIN ---"
cat /etc/nut/upsd.conf 2>/dev/null || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- upsd.users (redacted) ---
echo "# /etc/nut/upsd.users (CREDENTIALS REDACTED)"
echo "--- BEGIN ---"
cat /etc/nut/upsd.users 2>/dev/null | sed 's/password = .*/password = [REDACTED]/' | sed 's/instcmds = .*/instcmds = [REDACTED]/' || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- upsmon.conf ---
echo "# /etc/nut/upsmon.conf"
echo "--- BEGIN ---"
cat /etc/nut/upsmon.conf 2>/dev/null || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- upssched.conf ---
echo "# /etc/nut/upssched.conf"
echo "--- BEGIN ---"
cat /etc/nut/upssched.conf 2>/dev/null || echo "(Not found or not readable)"
echo "--- END ---"
echo

# --- Notification Scripts ---
echo "# NOTIFICATION SCRIPTS"
for script in /usr/local/bin/nut-notify /usr/bin/upssched-cmd /opt/scripts/upssched-cmd; do
    if [ -f "$script" ]; then
        echo "Found: $script"
        echo "  Permissions: $(ls -la "$script" | awk '{print $1, $3, $4}')"
        echo "  Size: $(du -h "$script" | cut -f1)"
    fi
done
echo

# --- USB Devices ---
echo "# USB DEVICES (potential UPS connections)"
ls -la /dev/ttyUSB* /dev/hidraw* 2>/dev/null || echo "(No USB devices found)"
echo

# --- UPS Status (if available) ---
echo "# UPS STATUS (upsc query)"
if which upsc > /dev/null 2>&1; then
    upsc 2>/dev/null | head -50 || echo "(No UPS data available)"
else
    echo "upsc command not available"
fi
echo

# --- Recent Logs ---
echo "# RECENT LOGS (upsd, upsmon)"
echo "--- upsd logs (last 30 lines) ---"
journalctl -u upsd -n 30 --no-pager 2>/dev/null || echo "(No upsd logs)"
echo
echo "--- upsmon logs (last 30 lines) ---"
journalctl -u upsmon -n 30 --no-pager 2>/dev/null || echo "(No upsmon logs)"
echo

# --- Network Listeners ---
echo "# NETWORK LISTENERS (NUT ports)"
netstat -tlnp 2>/dev/null | grep -E ":3493|nut" || echo "(No NUT listeners detected)"
echo

# --- dpkg-dist Originals ---
echo "# PACKAGE ORIGINALS (.dpkg-dist)"
find /etc/nut/ -name "*.dpkg-dist" 2>/dev/null | while read f; do
    echo "Found: $f"
    echo "--- BEGIN ---"
    cat "$f"
    echo "--- END ---"
done
echo

# --- Summary ---
echo "====================================================================="
echo "Audit complete. Timestamp: $TIMESTAMP"
echo "====================================================================="

