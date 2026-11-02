#!/usr/bin/env bash
set -euo pipefail

readonly VM_CONFIGURATION=/etc/pve/qemu-server/101.conf
readonly LAN_DEVICE=/sys/class/net/enp2s0f1np1/device
readonly EXPECTED_VF=/sys/bus/pci/devices/0000:02:0a.0
readonly SERVICES=(mwan-opnsense-host.service mwan-opnsense-drain.service sriov-vf-lan.service)

if [[ -e "$VM_CONFIGURATION" ]]; then
    printf 'VM101 still exists. Apply the vault/opnsense retirement plan before retiring its host services and LAN virtual function.\n' >&2
    exit 1
fi

if [[ ! -r "$LAN_DEVICE/sriov_numvfs" ]]; then
    printf 'The LAN port enp2s0f1np1 does not expose its virtual function count. Verify the current interface and PCI assignment before retirement.\n' >&2
    exit 1
fi
read -r VF_COUNT <"$LAN_DEVICE/sriov_numvfs"
case "$VF_COUNT" in
0) ;;
1)
    if [[ ! "$LAN_DEVICE/virtfn0" -ef "$EXPECTED_VF" ]]; then
        printf 'The LAN virtual function does not match 0000:02:0a.0. Verify its current PCI assignment before retirement.\n' >&2
        exit 1
    fi
    ;;
*)
    printf 'The LAN port has %s virtual functions. Verify their consumers before retiring the VM101 allocation.\n' "$VF_COUNT" >&2
    exit 1
    ;;
esac

main() {
    local configuration
    local grep_status
    local service
    local load_state
    shopt -s nullglob
    for configuration in /etc/pve/qemu-server/*.conf; do
        if grep -Eq '^hostpci[0-9]+:[[:space:]]*(host=)?(0000:)?02:0a(\.0)?([,;[:space:]]|$)' "$configuration"; then
            printf 'The LAN virtual function remains assigned in %s. Remove that assignment before retiring the allocation.\n' "$configuration" >&2
            exit 1
        else
            grep_status=$?
            if [[ "$grep_status" -ne 1 ]]; then
                printf 'The retirement check could not read %s.\n' "$configuration" >&2
                exit "$grep_status"
            fi
        fi
    done

    for service in "${SERVICES[@]}"; do
        if read -r load_state < <(systemctl show --property=LoadState --value "$service"); then
            wait "$!"
        else
            wait "$!"
            printf 'systemctl did not return the LoadState for %s.\n' "$service" >&2
            return 1
        fi
        if [[ "$load_state" == not-found ]]; then
            printf '%s is not installed.\n' "$service"
            continue
        fi
        systemctl disable --now "$service"
    done
    if [[ "$VF_COUNT" -eq 1 ]]; then
        printf '0\n' >"$LAN_DEVICE/sriov_numvfs"
    fi
    read -r VF_COUNT <"$LAN_DEVICE/sriov_numvfs"
    if [[ "$VF_COUNT" -ne 0 ]]; then
        printf 'The LAN virtual function allocation remains active.\n' >&2
        exit 1
    fi
}

main
