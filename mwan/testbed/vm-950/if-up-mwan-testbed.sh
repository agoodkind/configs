#!/bin/sh
# MWAN testbed persistent config. Runs on interface up.
# Deploy to: /etc/network/if-up.d/mwan-testbed (chmod +x)
# Also runs via: mwan-testbed-routes.service

# PD addresses for NPT source (needed on WAN interfaces)
ip -6 addr replace 3d06:bad:b01:220::1/60 dev enwebpass0 nodad 2>/dev/null
ip -6 addr replace 3d06:bad:b01:230::1/60 dev enatt0 nodad 2>/dev/null
ip -6 addr replace 3d06:bad:b01:240::1/60 dev enmbrains0 nodad 2>/dev/null

# IPv4 fwmark rules
ip rule add fwmark 1 table 100 priority 100 2>/dev/null || true
ip rule add fwmark 2 table 200 priority 200 2>/dev/null || true
ip rule add fwmark 3 table 300 priority 300 2>/dev/null || true

# IPv6 fwmark rules
ip -6 rule add fwmark 1 table 100 priority 100 2>/dev/null || true
ip -6 rule add fwmark 2 table 200 priority 200 2>/dev/null || true
ip -6 rule add fwmark 3 table 300 priority 300 2>/dev/null || true

# IPv6 source rules for PD prefixes
ip -6 rule add from 3d06:bad:b01:230::/60 table 100 priority 55 2>/dev/null || true
ip -6 rule add from 3d06:bad:b01:220::/60 table 200 priority 56 2>/dev/null || true
ip -6 rule add from 3d06:bad:b01:240::/60 table 300 priority 57 2>/dev/null || true

# Per-table IPv4 routes
ip route replace default via 10.240.205.1 dev enatt0 table 100 2>/dev/null
ip route replace default via 10.240.204.1 dev enwebpass0 table 200 2>/dev/null
ip route replace default via 10.240.206.1 dev enmbrains0 table 300 2>/dev/null

# Per-table IPv6 routes (ISP LXC link-locals)
ip -6 route replace default via fe80::be24:11ff:fed4:3ca4 dev enatt0 table 100 2>/dev/null
ip -6 route replace default via fe80::be24:11ff:fe7f:de4e dev enwebpass0 table 200 2>/dev/null
ip -6 route replace default via fe80::be24:11ff:fe87:1f3a dev enmbrains0 table 300 2>/dev/null

# Internal reach-back in all tables
for tbl in 100 200 300; do
    ip route replace 10.250.250.0/29 dev enmwanbr0 table $tbl 2>/dev/null
    ip -6 route replace 3d06:bad:b01:201::2/128 dev enmwanbr0 table $tbl 2>/dev/null
    ip -6 route replace 3d06:bad:b01:210::/60 via fe80::be24:11ff:fe07:a172 dev enmwanbr0 table $tbl 2>/dev/null
done

# Main table defaults
ip route replace default via 10.240.204.1 dev enwebpass0 metric 10 2>/dev/null
ip -6 route replace default via fe80::be24:11ff:fe7f:de4e dev enwebpass0 metric 10 2>/dev/null
