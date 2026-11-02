#!/usr/bin/env bash
set -euo pipefail

systemctl daemon-reload
nft --check --file /etc/vault-guest-network.nft
systemd-analyze verify /etc/systemd/system/tayga.service /etc/systemd/system/vault-guest-network.service
systemctl disable --now mwan-ifmgr.service mwan-watchdog.service
ifreload --all
sysctl --load=/etc/sysctl.d/70-vault-guest-network.conf
systemctl enable --now systemd-networkd.service
networkctl reload
networkctl reconfigure mbrains

# MWAN installed these source and tunnel-user rules for its separate routing table.
ip -4 rule flush priority 5
ip -4 rule flush priority 6
ip -6 rule flush priority 5
ip -6 rule flush priority 6

systemctl enable tayga.service vault-guest-network.service
systemctl restart vault-guest-network.service tayga.service
systemctl enable --now cloudflared.service
