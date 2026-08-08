#!/usr/bin/env bash
# Contract: exit 0 after collecting and validating this run's verdict, exit 1
# on budget expiry, exit 2 when the transient gate died without recording it,
# and exit 64 for invalid arguments.
set -euo pipefail

readonly CONNECT_TIMEOUT_SECONDS=6
readonly POLL_SECONDS=10

function usage() {
    echo "usage: $0 verdict_path unit_name expected_trace_id expected_boot_id budget_seconds address..." >&2
}

function main() {
    local verdict_path
    local unit_name
    local expected_trace_id
    local expected_boot_id
    local budget_seconds
    local deadline
    local now
    local remaining_seconds
    local address
    local address_list
    local temp_dir
    local stdout_file
    local stderr_file
    local status_stdout_file
    local status_stderr_file
    local cat_stderr
    local status_value
    local validation_error
    local ssh_rc
    local status_rc
    local -a addresses

    if [[ "$#" -lt 6 ]]; then
        usage
        exit 64
    fi

    verdict_path="$1"
    unit_name="$2"
    expected_trace_id="$3"
    expected_boot_id="$4"
    budget_seconds="$5"
    shift 5
    addresses=("$@")

    if [[ ! "$budget_seconds" =~ ^[0-9]+$ ]]; then
        usage
        exit 64
    fi

    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT
    stdout_file="$temp_dir/stdout"
    stderr_file="$temp_dir/stderr"
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
            if ssh -o BatchMode=yes -o ConnectTimeout="$CONNECT_TIMEOUT_SECONDS" \
                -o StrictHostKeyChecking=accept-new "root@${address}" \
                "cat -- '$verdict_path'" >"$stdout_file" 2>"$stderr_file"; then
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
' "$expected_trace_id" "$expected_boot_id" "$stdout_file" 2>&1)"; then
                    cat "$stdout_file"
                    echo "Collected deploy verdict from ${address}" >&2
                    exit 0
                fi

                if [[ "$validation_error" == *"stale verdict"* ]]; then
                    echo "Stale-verdict rejection from ${address}: ${validation_error}" >&2
                else
                    echo "Invalid deploy verdict from ${address}: ${validation_error}" >&2
                fi
                continue
            else
                ssh_rc=$?
            fi

            cat_stderr="$(tr '\n' ' ' <"$stderr_file" | cut -c1-240)"
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
