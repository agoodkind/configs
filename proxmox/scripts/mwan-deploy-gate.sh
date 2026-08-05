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
#   wait-down <addr> <seconds>  wait until <addr> misses two consecutive pings,
#                               which proves the scheduled reboot took the guest
#                               down. A guest that never drops is still serving
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
readonly CONSECUTIVE_MISSES_FOR_DOWN=2

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

wait_down() {
    local target="$1"
    local budget_seconds="$2"
    local deadline=$(($(date +%s) + budget_seconds))
    local misses=0
    while [[ "$(date +%s)" -lt "$deadline" ]]; do
        if probe -6 "$target"; then
            misses=0
        else
            misses=$((misses + 1))
            if [[ "$misses" -ge "$CONSECUTIVE_MISSES_FOR_DOWN" ]]; then
                printf 'guest %s went down after %d consecutive missed pings\n' \
                    "$target" "$misses"
                return 0
            fi
        fi
        sleep "$POLL_INTERVAL_SECONDS"
    done
    printf 'guest %s stayed reachable for %ds, so the reboot never fired\n' \
        "$target" "$budget_seconds"
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
    printf 'usage: %s check-egress | wait-down <addr> <seconds> | wait-egress <seconds>\n' \
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
    elif [[ "$mode" == "wait-down" ]]; then
        if [[ $# -ne 2 ]]; then
            usage
        fi
        wait_down "$1" "$2"
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
