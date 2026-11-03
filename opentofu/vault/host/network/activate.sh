#!/usr/bin/env bash
set -euo pipefail

dispatch_activation() {
    local script_name="${0##*/}"
    local unit_name
    local load_state
    local runtime_directory

    if [[ ! "$script_name" =~ ^vault-network-[[:xdigit:]]{64}\.sh$ ]]; then
        printf 'Run activation through the Vault host OpenTofu resource.\n' >&2
        return 1
    fi
    unit_name="${script_name%.sh}.service"
    load_state=$(systemctl show --property=LoadState --value "$unit_name")
    if [[ -z "$load_state" ]]; then
        printf 'systemctl did not return the activation unit state.\n' >&2
        return 1
    fi
    if [[ "$load_state" == not-found ]]; then
        runtime_directory=$(mktemp -d /run/vault-network-activation.XXXXXXXX)
        cp -- "$0" "$runtime_directory/activate.sh"
        systemd-run --unit="$unit_name" --property=Type=oneshot \
            --property=RemainAfterExit=yes /usr/bin/bash "$runtime_directory/activate.sh" --apply
        return
    fi
    systemctl start "$unit_name"
}

if [[ "$#" -eq 0 ]]; then
    dispatch_activation
    exit
fi
if [[ "$#" -ne 1 || "$1" != --apply ]]; then
    printf 'Use --apply only for the local activation job.\n' >&2
    exit 1
fi

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
