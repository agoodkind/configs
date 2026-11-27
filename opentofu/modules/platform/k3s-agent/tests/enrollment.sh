#!/usr/bin/env bash
set -euo pipefail

SCRIPT=$(cd "$(dirname "$0")/../files" && pwd)/enroll-worker.sh
TEST_DIR=$(mktemp -d)
trap 'rm -r "$TEST_DIR"' EXIT
BUILD=5b12a66b-cdb7-48ae-bae2-84f9031ea912
NODE="worker-127-$BUILD"
export TEST_DIR BUILD NODE

qm() {
    local vmid=$3
    local pass_stdin=$5
    local output=""
    local result=0
    local token input
    shift 8

    printf -v token 'K10%064d::abc123.0123456789abcdef' 0
    case "$vmid $*" in
        '127 cat /sys/class/dmi/id/product_uuid')
            output=$BUILD
            ;;
        "124 k3s token create --ttl 30m --description $NODE")
            output=$token
            echo issue >> "$TEST_DIR/events"
            ;;
        '127 install -m 0600 /dev/null /etc/rancher/k3s/agent-token') ;;
        '127 dd of=/etc/rancher/k3s/agent-token status=none')
            read -r input
            if [[ "$pass_stdin" != 1 || "$input" != "$token" ]]; then
                return 1
            fi
            echo deliver >> "$TEST_DIR/events"
            ;;
        '127 systemctl daemon-reload' | '127 systemctl restart --no-block k3s-agent.service') ;;
        "124 k3s kubectl wait --for=create node/$NODE --timeout=5m") ;;
        "124 k3s kubectl wait --for=condition=Ready node/$NODE --timeout=5m")
            result=$READY_EXIT
            echo ready >> "$TEST_DIR/events"
            ;;
        '124 k3s token delete abc123')
            echo revoke >> "$TEST_DIR/events"
            ;;
        "124 k3s kubectl delete node $NODE --ignore-not-found")
            echo remove >> "$TEST_DIR/events"
            ;;
        *)
            echo "Unexpected guest command" >&2
            return 1
            ;;
    esac
    jq -n --arg output "$output" --argjson status "$result" \
        '{exited: 1, exitcode: $status, "out-data": $output}'
}
export -f qm

run_worker_action() {
    bash "$SCRIPT" "$1" 124 127 "$2" > "$TEST_DIR/stdout" 2> "$TEST_DIR/stderr"
}

export READY_EXIT=0
run_worker_action enroll "$BUILD"
expected_events=$(printf '%s\n' issue deliver ready revoke)
[[ $(cat "$TEST_DIR/events") == "$expected_events" ]]
if grep -q 'abc123\.' "$TEST_DIR/stdout" "$TEST_DIR/stderr"; then
    echo "Worker enrollment printed the bootstrap token." >&2
    exit 1
fi

export READY_EXIT=1
: > "$TEST_DIR/events"
if run_worker_action enroll "$BUILD"; then
    echo "Enrollment succeeded after the readiness check failed." >&2
    exit 1
fi
[[ $(cat "$TEST_DIR/events") == "$expected_events" ]]

: > "$TEST_DIR/events"
if run_worker_action enroll 00000000-0000-0000-0000-000000000000; then
    echo "Enrollment accepted a different worker build." >&2
    exit 1
fi
[[ ! -s "$TEST_DIR/events" ]]

run_worker_action remove "$BUILD"
[[ $(cat "$TEST_DIR/events") == remove ]]
