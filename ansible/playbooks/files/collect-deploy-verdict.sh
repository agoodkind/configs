#!/usr/bin/env bash
# Contract: exit 0 after collecting and validating this run's verdict, exit 1
# on budget expiry, exit 2 when the transient gate died without recording it,
# and exit 64 for invalid arguments.
set -euo pipefail

readonly CONNECT_TIMEOUT_SECONDS=6
readonly POLL_SECONDS=10

# Shared state for try_collect, set once by main: this run's verdict location
# and identity, scratch files for the remote read, and the ssh exit code of
# the last failed read.
VERDICT_PATH=""
EXPECTED_TRACE_ID=""
EXPECTED_BOOT_ID=""
STDOUT_FILE=""
STDERR_FILE=""
LAST_READ_RC=0

function usage() {
    echo "usage: $0 verdict_path unit_name expected_trace_id expected_boot_id budget_seconds address..." >&2
}

# try_collect reads the verdict from one address and validates it against
# this run's identity. Returns 0 after printing a valid verdict on stdout,
# 1 when the remote read failed (LAST_READ_RC carries the ssh exit code and
# STDERR_FILE the remote stderr), and 2 when a file was read but rejected as
# stale or invalid (already logged).
function try_collect() {
    local address="$1"
    local read_rc
    local validation_error

    LAST_READ_RC=0
    read_rc=0
    ssh -o BatchMode=yes -o ConnectTimeout="$CONNECT_TIMEOUT_SECONDS" \
        -o StrictHostKeyChecking=accept-new "root@${address}" \
        "cat -- '$VERDICT_PATH'" >"$STDOUT_FILE" 2>"$STDERR_FILE" || read_rc=$?
    if (( read_rc != 0 )); then
        LAST_READ_RC=$read_rc
        return 1
    fi

    if validation_error="$(python3 -c '
import json
import sys

try:
    with open(sys.argv[3], encoding="utf-8") as verdict_file:
        verdict = json.load(verdict_file)
except (OSError, json.JSONDecodeError) as error:
    print(f"invalid verdict: {error}")
    sys.exit(1)

if not isinstance(verdict, dict):
    print("invalid verdict: top-level JSON value is not an object")
    sys.exit(1)

if verdict.get("trace_id") != sys.argv[1]:
    print("stale verdict: trace_id mismatch")
    sys.exit(2)

if verdict.get("old_boot_id") != sys.argv[2]:
    print("stale verdict: old_boot_id mismatch")
    sys.exit(2)
' "$EXPECTED_TRACE_ID" "$EXPECTED_BOOT_ID" "$STDOUT_FILE" 2>&1)"; then
        cat "$STDOUT_FILE"
        echo "Collected deploy verdict from ${address}" >&2
        return 0
    fi

    if [[ "$validation_error" == *"stale verdict"* ]]; then
        echo "Stale-verdict rejection from ${address}: ${validation_error}" >&2
    else
        echo "Invalid deploy verdict from ${address}: ${validation_error}" >&2
    fi
    return 2
}

function main() {
    local unit_name
    local budget_seconds
    local deadline
    local now
    local remaining_seconds
    local address
    local address_list
    local temp_dir
    local status_stdout_file
    local status_stderr_file
    local cat_stderr
    local status_value
    local collect_rc
    local retry_rc
    local ssh_rc
    local status_rc
    local -a addresses

    if [[ "$#" -lt 6 ]]; then
        usage
        exit 64
    fi

    VERDICT_PATH="$1"
    unit_name="$2"
    EXPECTED_TRACE_ID="$3"
    EXPECTED_BOOT_ID="$4"
    budget_seconds="$5"
    shift 5
    addresses=("$@")

    if [[ ! "$budget_seconds" =~ ^[0-9]+$ ]]; then
        usage
        exit 64
    fi

    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT
    STDOUT_FILE="$temp_dir/stdout"
    STDERR_FILE="$temp_dir/stderr"
    status_stdout_file="$temp_dir/status-stdout"
    status_stderr_file="$temp_dir/status-stderr"

    now="$(date +%s)"
    deadline=$((now + budget_seconds))

    while true; do
        now="$(date +%s)"
        if (( now >= deadline )); then
            break
        fi

        for address in "${addresses[@]}"; do
            collect_rc=0
            try_collect "$address" || collect_rc=$?
            if (( collect_rc == 0 )); then
                exit 0
            fi
            if (( collect_rc == 2 )); then
                # A rejected verdict was already logged; poll again for a
                # fresh file rather than probing the unit.
                continue
            fi
            ssh_rc=$LAST_READ_RC

            cat_stderr="$(tr '\n' ' ' <"$STDERR_FILE" | cut -c1-240)"
            echo "Verdict read failed from ${address} with rc ${ssh_rc}: ${cat_stderr}" >&2

            # A transport failure returns 255. A reachable host returns cat's
            # nonzero exit code, regardless of the remote locale.
            if (( ssh_rc != 255 )); then
                if ssh -o BatchMode=yes -o ConnectTimeout="$CONNECT_TIMEOUT_SECONDS" \
                    -o StrictHostKeyChecking=accept-new "root@${address}" \
                    "systemctl is-active '$unit_name'" >"$status_stdout_file" \
                    2>"$status_stderr_file"; then
                    status_rc=0
                else
                    status_rc=$?
                fi

                status_value="$(tr -d '\r\n' <"$status_stdout_file")"
                if (( status_rc != 255 )) \
                    && [[ "$status_value" != "active" && "$status_value" != "activating" ]]; then
                    # The gate writes the verdict before its process exits, and
                    # systemd-run --collect garbage-collects the finished unit,
                    # so "inactive" also describes a gate that recorded its
                    # verdict in the gap since the failed read above. Re-read
                    # before ruling: only inactive AND still no verdict proves
                    # the gate died without recording one.
                    retry_rc=0
                    try_collect "$address" || retry_rc=$?
                    if (( retry_rc == 0 )); then
                        exit 0
                    fi
                    # A transport failure on the re-read observes nothing about
                    # the verdict, so it must not confirm death: that repeats
                    # the very mistake this branch exists to correct. Keep
                    # polling and let the deadline rule instead.
                    if (( LAST_READ_RC == 255 )); then
                        echo "Verdict re-read from ${address} lost connectivity with rc ${LAST_READ_RC}; not confirming gate death" >&2
                        continue
                    fi
                    status_value="${status_value:-unknown}"
                    echo "Deploy gate ${unit_name} is ${status_value} on ${address} without a verdict; gate death confirmed" >&2
                    exit 2
                fi

                status_value="${status_value} $(tr '\n' ' ' <"$status_stderr_file")"
                status_value="$(printf '%s' "$status_value" | cut -c1-240)"
                echo "Gate status probe failed from ${address} with rc ${status_rc}: ${status_value}" >&2
            fi
        done

        now="$(date +%s)"
        remaining_seconds=$((deadline - now))
        if (( remaining_seconds <= 0 )); then
            break
        fi
        if (( remaining_seconds < POLL_SECONDS )); then
            sleep "$remaining_seconds"
        else
            sleep "$POLL_SECONDS"
        fi
    done

    address_list="$(IFS=,; printf '%s' "${addresses[*]}")"
    echo "Timed out after ${budget_seconds}s waiting for deploy verdict from: ${address_list}" >&2
    exit 1
}

main "$@"
