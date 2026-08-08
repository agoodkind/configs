#!/usr/bin/env bash
# Deploy-time reachability probes for the MWAN VM, run on the Proxmox host that
# owns the guest. The host is the only vantage point that stays up while the
# guest reboots, so deploy-mwan.yml delegates these probes here.
#
# Modes:
#   check-egress                one-shot internet probe, used before the deploy
#                               to decide whether a rollback snapshot is worth
#                               taking. Either address family answering is
#                               enough, because this only gates the snapshot.
#   wait-reboot <vmid> <old_boot_id> <seconds>
#                               wait until the guest's /proc/sys/kernel/random/
#                               boot_id, read through the QEMU guest agent,
#                               differs from <old_boot_id>. A changed boot_id
#                               proves the kernel rebooted regardless of how
#                               short the observable down window was, which a
#                               ping-cadence probe can miss entirely. A guest
#                               whose boot_id never changes is still serving
#                               the config the deploy just wrote.
#   wait-egress <seconds>       wait until internet egress returns after the
#                               reboot. IPv6 decides the verdict because IPv6 is
#                               the primary family here, and the IPv4 result is
#                               reported alongside it every cycle.
#
# Every mode exits 0 on success and 1 on timeout. A timeout prints the last
# probe failure, so the deploy log names the cause and not only the verdict.

set -euo pipefail

readonly EGRESS_TARGET_V6="2606:4700:4700::1111"
readonly EGRESS_TARGET_V4="1.1.1.1"
readonly PROBE_TIMEOUT_SECONDS=2
readonly POLL_INTERVAL_SECONDS=1
readonly REBOOT_POLL_INTERVAL_SECONDS=3
readonly UUID_PATTERN='[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}'

last_probe_error=""

# probe sends one ping of the given family and records the failure text, so a
# later timeout can report why every attempt failed instead of only that it did.
probe() {
    local family="$1"
    local target="$2"
    local output
    if output=$(ping "$family" -c 1 -W "$PROBE_TIMEOUT_SECONDS" "$target" 2>&1); then
        return 0
    fi
    last_probe_error="ping $family $target: $(printf '%s' "$output" | tr '\n' ' ')"
    return 1
}

check_egress() {
    local reachable_v6="no"
    local reachable_v4="no"
    if probe -6 "$EGRESS_TARGET_V6"; then
        reachable_v6="yes"
    fi
    if probe -4 "$EGRESS_TARGET_V4"; then
        reachable_v4="yes"
    fi
    printf 'egress probe: ipv6=%s ipv4=%s\n' "$reachable_v6" "$reachable_v4"
    if [[ "$reachable_v6" == "yes" || "$reachable_v4" == "yes" ]]; then
        return 0
    fi
    printf 'no egress on either family; last probe error: %s\n' "$last_probe_error"
    return 1
}

wait_reboot() {
    local vmid="$1"
    local old_boot_id="$2"
    local budget_seconds="$3"
    local deadline=$(($(date +%s) + budget_seconds))
    local raw
    local current
    while [[ "$(date +%s)" -lt "$deadline" ]]; do
        # The guest agent is unreachable through shutdown and early boot, so a
        # failed exec is a normal poll outcome while the reboot is in flight.
        if raw=$(qm guest exec "$vmid" --timeout 5 -- \
            cat /proc/sys/kernel/random/boot_id 2>&1); then
            # The exec response is JSON whose only UUID is the boot_id, so a
            # pattern match extracts it without depending on the JSON layout.
            current=$(printf '%s' "$raw" | grep -oE "$UUID_PATTERN" || true)
            if [[ -n "$current" && "$current" != "$old_boot_id" ]]; then
                printf 'guest %s rebooted: boot_id %s changed to %s\n' \
                    "$vmid" "$old_boot_id" "$current"
                return 0
            fi
        else
            last_probe_error="qm guest exec $vmid: $(printf '%s' "$raw" | tr '\n' ' ')"
        fi
        sleep "$REBOOT_POLL_INTERVAL_SECONDS"
    done
    printf 'guest %s boot_id never changed from %s within %ds, so the reboot never fired; last agent error: %s\n' \
        "$vmid" "$old_boot_id" "$budget_seconds" "${last_probe_error:-none}"
    return 1
}

wait_egress() {
    local budget_seconds="$1"
    local deadline=$(($(date +%s) + budget_seconds))
    local reachable_v6
    local reachable_v4
    while [[ "$(date +%s)" -lt "$deadline" ]]; do
        reachable_v6="no"
        reachable_v4="no"
        if probe -6 "$EGRESS_TARGET_V6"; then
            reachable_v6="yes"
        fi
        if probe -4 "$EGRESS_TARGET_V4"; then
            reachable_v4="yes"
        fi
        if [[ "$reachable_v6" == "yes" ]]; then
            printf 'egress restored: ipv6=%s ipv4=%s\n' "$reachable_v6" "$reachable_v4"
            return 0
        fi
        sleep "$POLL_INTERVAL_SECONDS"
    done
    printf 'no IPv6 egress within %ds; last probe error: %s\n' \
        "$budget_seconds" "$last_probe_error"
    return 1
}

usage() {
    printf 'usage: %s check-egress | wait-reboot <vmid> <old_boot_id> <seconds> | wait-egress <seconds>\n' \
        "$(basename "$0")" >&2
    exit 1
}

main() {
    if [[ $# -lt 1 ]]; then
        usage
    fi
    local mode="$1"
    shift
    if [[ "$mode" == "check-egress" ]]; then
        if [[ $# -ne 0 ]]; then
            usage
        fi
        check_egress
    elif [[ "$mode" == "wait-reboot" ]]; then
        if [[ $# -ne 3 ]]; then
            usage
        fi
        wait_reboot "$1" "$2" "$3"
    elif [[ "$mode" == "wait-egress" ]]; then
        if [[ $# -ne 1 ]]; then
            usage
        fi
        wait_egress "$1"
    else
        usage
    fi
}

main "$@"
