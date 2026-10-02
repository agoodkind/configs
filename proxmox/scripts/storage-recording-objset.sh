#!/usr/bin/env bash
# storage-recording-objset.sh appends one JSON line per ZFS dataset with its
# cumulative write and read counters from /proc/spl/kstat/zfs/<pool>/objset-*
# and the change since the previous run. storage-recording-objset.service runs
# it every minute on a hypervisor that enables storage recording (TACK-483).
#
# Usage: storage-recording-objset.sh STATE_FILE LOG_FILE KSTAT_DIR POOL...
#
# A delta is null for a dataset absent from the previous run, and for a
# counter lower than its previous value (the pool was imported again).
set -euo pipefail

TEMPORARY_STATE=""

function cleanup {
    if [[ -n "$TEMPORARY_STATE" ]]; then
        rm -f "$TEMPORARY_STATE"
    fi
}

trap cleanup EXIT

# read_counters prints "dataset writes nwritten reads nread", tab separated,
# for every objset kstat of each pool.
function read_counters {
    local kstat_dir="$1"
    shift
    local pool
    local objset
    for pool in "$@"; do
        if [[ ! -d "$kstat_dir/$pool" ]]; then
            printf 'storage-recording-objset: no kstat directory %s/%s\n' "$kstat_dir" "$pool" >&2
            return 1
        fi
        for objset in "$kstat_dir/$pool"/objset-0x*; do
            if [[ ! -f "$objset" ]]; then
                continue
            fi
            awk '
                $1 == "dataset_name" { name = $3 }
                $1 == "writes" { writes = $3 }
                $1 == "nwritten" { nwritten = $3 }
                $1 == "reads" { reads = $3 }
                $1 == "nread" { nread = $3 }
                END { if (name != "") printf "%s\t%s\t%s\t%s\t%s\n", name, writes, nwritten, reads, nread }
            ' "$objset"
        done
    done
}

function main {
    if (( $# < 4 )); then
        printf 'usage: %s STATE_FILE LOG_FILE KSTAT_DIR POOL...\n' "$0" >&2
        return 2
    fi
    local state_file="$1"
    local log_file="$2"
    local kstat_dir="$3"
    shift 3
    local now
    now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    TEMPORARY_STATE="$(mktemp "${state_file}.XXXXXX")"
    read_counters "$kstat_dir" "$@" > "$TEMPORARY_STATE"

    local previous_state=/dev/null
    if [[ -f "$state_file" ]]; then
        previous_state="$state_file"
    fi
    awk -F '\t' -v OFS='\t' -v previous_file="$previous_state" '
        FILENAME == previous_file { previous[$1] = $2 OFS $3 OFS $4 OFS $5; next }
        { if ($1 in previous) { print $0, previous[$1] } else { print $0 } }
    ' "$previous_state" "$TEMPORARY_STATE" \
        | jq -Rc --arg time "$now" '
            def delta($current; $previous):
                if $previous == null or $current < $previous then null else $current - $previous end;
            split("\t") as $field
            | ($field | map(tonumber? // .)) as $value
            | {
                time: $time,
                dataset: $field[0],
                writes: $value[1],
                nwritten: $value[2],
                reads: $value[3],
                nread: $value[4],
                writes_delta: delta($value[1]; $value[5]),
                nwritten_delta: delta($value[2]; $value[6]),
                reads_delta: delta($value[3]; $value[7]),
                nread_delta: delta($value[4]; $value[8])
              }
        ' >> "$log_file"
    mv "$TEMPORARY_STATE" "$state_file"
    TEMPORARY_STATE=""
}

main "$@"
