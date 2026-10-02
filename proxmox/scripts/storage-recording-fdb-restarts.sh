#!/usr/bin/env bash
# storage-recording-fdb-restarts.sh appends one JSON line per data guest with
# the Docker restart count, start time, and state of its FoundationDB
# container, read through pct exec. storage-recording-fdb-restarts.service runs
# it daily on a hypervisor that enables storage recording (TACK-483).
#
# Usage: storage-recording-fdb-restarts.sh LOG_FILE CONTAINER VMID...
#
# A guest that cannot be read gets a line with its exit code and error text,
# the other guests are still read, and the script then exits 1.
set -euo pipefail

readonly INSPECT_FORMAT='{{.RestartCount}} {{.State.StartedAt}} {{.State.Status}}'

function main {
    if (( $# < 3 )); then
        printf 'usage: %s LOG_FILE CONTAINER VMID...\n' "$0" >&2
        return 2
    fi
    local log_file="$1"
    local container="$2"
    shift 2
    local failures=0
    local vmid
    for vmid in "$@"; do
        local now
        now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        local output
        local exit_code=0
        output="$(pct exec "$vmid" -- docker inspect --format "$INSPECT_FORMAT" "$container" 2>&1)" || exit_code=$?
        if (( exit_code != 0 )); then
            failures=$((failures + 1))
            jq -cn --arg time "$now" --arg vmid "$vmid" --arg container "$container" \
                --argjson exit_code "$exit_code" --arg error "$output" \
                '{time: $time, vmid: $vmid, container: $container, exit_code: $exit_code, error: $error}' >> "$log_file"
            printf 'storage-recording-fdb-restarts: vmid %s exit_code=%s: %s\n' "$vmid" "$exit_code" "$output" >&2
            continue
        fi
        local restart_count
        local started_at
        local state
        read -r restart_count started_at state <<<"$output"
        jq -cn --arg time "$now" --arg vmid "$vmid" --arg container "$container" \
            --argjson restart_count "$restart_count" --arg started_at "$started_at" --arg state "$state" \
            '{time: $time, vmid: $vmid, container: $container, restart_count: $restart_count, started_at: $started_at, state: $state}' \
            >> "$log_file"
    done
    if (( failures > 0 )); then
        return 1
    fi
}

main "$@"
