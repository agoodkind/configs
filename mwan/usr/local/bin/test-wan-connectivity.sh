#!/bin/bash
# Test WAN connectivity for mwan multi-WAN setup
# Tests IPv4 static NAT, IPv6-PD delegation, and NPT

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ATT_IFACE="eth1.3242"
WEBPASS_IFACE="eth2"
INTERNAL_PREFIX="3d06:bad:b01::/56"

log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=== MWAN Connectivity Test ==="
echo ""

# 1. Check IPv6-PD WAN addresses
log "1. Checking IPv6-PD WAN addresses..."

att_v6=$(ip -6 addr show dev "$ATT_IFACE" scope global | \
    grep -oP 'inet6 \K[0-9a-f:]+/64' | head -1 || true)
webpass_v6=$(ip -6 addr show dev "$WEBPASS_IFACE" scope global | \
    grep -oP 'inet6 \K[0-9a-f:]+/64' | head -1 || true)

if [ -n "$att_v6" ]; then
    log "  AT&T WAN IPv6: $att_v6 ✓"
else
    error "  AT&T WAN IPv6: NOT FOUND"
    warn "    Check: journalctl -u dhcpcd | grep -i 'ia_pd2'"
fi

if [ -n "$webpass_v6" ]; then
    log "  Webpass WAN IPv6: $webpass_v6 ✓"
else
    error "  Webpass WAN IPv6: NOT FOUND"
    warn "    Check: journalctl -u dhcpcd | grep -i 'ia_pd4'"
fi

echo ""

# 2. Check IPv4 DHCP addresses
log "2. Checking IPv4 DHCP addresses..."

att_v4=$(ip -4 addr show dev "$ATT_IFACE" | \
    grep -oP 'inet \K[0-9.]+/[0-9]+' | grep -v '127\.' | head -1 || true)
webpass_v4=$(ip -4 addr show dev "$WEBPASS_IFACE" | \
    grep -oP 'inet \K[0-9.]+/[0-9]+' | grep -v '127\.' | head -1 || true)

if [ -n "$att_v4" ]; then
    log "  AT&T WAN IPv4: $att_v4 ✓"
else
    error "  AT&T WAN IPv4: NOT FOUND"
fi

if [ -n "$webpass_v4" ]; then
    log "  Webpass WAN IPv4: $webpass_v4 ✓"
else
    error "  Webpass WAN IPv4: NOT FOUND"
fi

echo ""

# 3. Check NPT rules
log "3. Checking NPT rules..."

att_npt=$(nft list ruleset ip6 nat | \
    grep -A2 "oif $ATT_IFACE" | grep "snat.*prefix" || true)
webpass_npt=$(nft list ruleset ip6 nat | \
    grep -A2 "oif $WEBPASS_IFACE" | grep "snat.*prefix" || true)

if [ -n "$att_npt" ]; then
    log "  AT&T NPT: Configured ✓"
    echo "    $att_npt"
else
    error "  AT&T NPT: NOT FOUND"
    warn "    Run: /etc/dhcpcd.exit-hook manually or check logs"
fi

if [ -n "$webpass_npt" ]; then
    log "  Webpass NPT: Configured ✓"
    echo "    $webpass_npt"
else
    error "  Webpass NPT: NOT FOUND"
    warn "    Run: /etc/dhcpcd.exit-hook manually or check logs"
fi

echo ""

# 4. Check 1:1 NAT rules
log "4. Checking 1:1 IPv4 NAT rules..."

att_nat=$(nft list ruleset ip nat | \
    grep -A1 "oif $ATT_IFACE" | grep "snat to 104.57.226" || true)
webpass_nat=$(nft list ruleset ip nat | \
    grep -A1 "oif $WEBPASS_IFACE" | grep "snat to 136.25.91" || true)

if [ -n "$att_nat" ]; then
    log "  AT&T 1:1 NAT: Configured ✓"
    echo "    (found rules for 104.57.226.x)"
else
    error "  AT&T 1:1 NAT: NOT FOUND"
fi

if [ -n "$webpass_nat" ]; then
    log "  Webpass 1:1 NAT: Configured ✓"
    echo "    (found rules for 136.25.91.x)"
else
    error "  Webpass 1:1 NAT: NOT FOUND"
fi

echo ""

# 5. Test IPv6 connectivity
log "5. Testing IPv6 connectivity..."

if [ -n "$att_v6" ]; then
    att_addr="${att_v6%/*}"
    if ping6 -c 2 -W 2 -I "$ATT_IFACE" 2001:4860:4860::8888 >/dev/null 2>&1; then
        log "  AT&T IPv6: PASS (via $ATT_IFACE)"
    else
        error "  AT&T IPv6: FAIL (cannot reach 2001:4860:4860::8888)"
    fi
else
    warn "  AT&T IPv6: SKIP (no address)"
fi

if [ -n "$webpass_v6" ]; then
    webpass_addr="${webpass_v6%/*}"
    if ping6 -c 2 -W 2 -I "$WEBPASS_IFACE" 2001:4860:4860::8888 >/dev/null 2>&1; then
        log "  Webpass IPv6: PASS (via $WEBPASS_IFACE)"
    else
        error "  Webpass IPv6: FAIL (cannot reach 2001:4860:4860::8888)"
    fi
else
    warn "  Webpass IPv6: SKIP (no address)"
fi

echo ""

# 6. Test IPv4 connectivity with static NAT
log "6. Testing IPv4 connectivity via static NAT..."

# Test from internal network perspective
# Source IPs will be NAT'd to static external IPs
if [ -n "$att_v4" ]; then
    if ping -c 2 -W 2 -I "$ATT_IFACE" 8.8.8.8 >/dev/null 2>&1; then
        log "  AT&T IPv4: PASS (via $ATT_IFACE)"
        log "    Static NAT will translate 10.250.250.x -> 104.57.226.x"
    else
        error "  AT&T IPv4: FAIL (cannot reach 8.8.8.8)"
    fi
else
    warn "  AT&T IPv4: SKIP (no address)"
fi

if [ -n "$webpass_v4" ]; then
    if ping -c 2 -W 2 -I "$WEBPASS_IFACE" 8.8.8.8 >/dev/null 2>&1; then
        log "  Webpass IPv4: PASS (via $WEBPASS_IFACE)"
        log "    Static NAT will translate 10.250.250.x -> 136.25.91.x"
    else
        error "  Webpass IPv4: FAIL (cannot reach 8.8.8.8)"
    fi
else
    warn "  Webpass IPv4: SKIP (no address)"
fi

echo ""

# 7. Test NPT with traceroute (shows path)
log "7. Testing NPT with traceroute (check external IP)..."
warn "  Manual test from internal host:"
warn "    IPv6: traceroute6 -s <internal_ipv6> 2001:4860:4860::8888"
warn "    Check external IP matches delegated prefix"

echo ""
log "Test complete!"
echo ""
warn "To test from internal network (OPNsense side):"
warn "  IPv4: curl -4 https://api.ipify.org (should show 104.57.226.x or 136.25.91.x)"
warn "  IPv6: curl -6 https://api64.ipify.org (should show delegated prefix)"


