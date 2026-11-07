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
        systemd-run --wait --unit="$unit_name" --property=Type=oneshot \
            /usr/bin/bash "$runtime_directory/activate.sh" --apply
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
if [[ -e /etc/systemd/system/vault-guest-network.service ]]; then
    systemctl disable --now vault-guest-network.service
fi
nft destroy table ip vault_guests
nft destroy table inet vault_guests
sysctl --load=/etc/sysctl.d/70-vault-guest-network.conf
systemctl enable --now systemd-networkd.service
networkctl reload

systemctl enable cloudflared.service
systemctl restart cloudflared.service
