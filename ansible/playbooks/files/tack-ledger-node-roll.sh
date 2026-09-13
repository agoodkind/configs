#!/usr/bin/env bash
# Starts (or recreates) this guest's ledger node and waits for it keyed on
# the node's own progress, not on a clock. Contract: exit 0 once the node's
# health check passes, exit 1 when the node stops making progress while
# still unhealthy or when its container exits, exit 64 for bad arguments.
#
# Why progress and not a budget (TACK-488): a node holding real data replays
# every open tablet log segment after a kill, and the time that takes grows
# with the data. A fixed wait is a number an operator raises as the system
# grows, and the day it is too short the deploy marks a node failed while it
# is still starting. The tserver logs one "Bootstrap complete" line per
# tablet as it replays, so that count is the high-water mark: a node whose
# count keeps rising is never given up on, and a node whose count has not
# moved for the stall window while the health check still fails is wedged.
#
# Usage: tack-ledger-node-roll.sh wait|nowait stall_seconds
#   wait    start the node and wait for it as described above
#   nowait  start the node and return at once (a certificate rotation, where
#           no node can become healthy until every peer has restarted)
set -euo pipefail

readonly CONTAINER=tack-yugabyte-1
readonly TSERVER_LOG=/home/yugabyte/var/logs/tserver/yb-tserver.INFO
readonly POLL_SECONDS=5

function usage() {
    echo "usage: $0 wait|nowait stall_seconds" >&2
}

function health_status() {
    docker inspect --format '{{.State.Health.Status}}' "$CONTAINER" 2>/dev/null || echo "missing"
}

function container_running() {
    local state
    state=$(docker inspect --format '{{.State.Status}}' "$CONTAINER" 2>/dev/null || echo "missing")
    [[ "$state" == "running" ]]
}

# The log does not exist until the tserver opens it, and grep exits 1 on a
# zero count, so both read as zero progress rather than as an error.
function bootstrap_count() {
    docker exec "$CONTAINER" grep -c 'Bootstrap complete' "$TSERVER_LOG" 2>/dev/null || echo 0
}

function main() {
    if [[ $# -ne 2 ]]; then
        usage
        exit 64
    fi
    local mode="$1"
    local stall_seconds="$2"
    if [[ "$mode" != "wait" && "$mode" != "nowait" ]]; then
        usage
        exit 64
    fi
    if ! [[ "$stall_seconds" =~ ^[0-9]+$ ]]; then
        usage
        exit 64
    fi

    docker compose up -d yugabyte

    if [[ "$mode" == "nowait" ]]; then
        echo "started without waiting (certificate rotation)"
        exit 0
    fi

    local started_at=$EPOCHSECONDS
    local last_progress_at=$EPOCHSECONDS
    local high_water=0
    local count
    local status
    while true; do
        status=$(health_status)
        if [[ "$status" == "healthy" ]]; then
            echo "healthy after $((EPOCHSECONDS - started_at)) s, $high_water tablets bootstrapped"
            exit 0
        fi
        if ! container_running; then
            echo "container $CONTAINER is not running (health $status)" >&2
            exit 1
        fi
        count=$(bootstrap_count)
        if (( count > high_water )); then
            high_water=$count
            last_progress_at=$EPOCHSECONDS
        fi
        if (( EPOCHSECONDS - last_progress_at > stall_seconds )); then
            echo "stalled: health $status, $high_water tablets bootstrapped, no progress for $stall_seconds s after $((EPOCHSECONDS - started_at)) s" >&2
            exit 1
        fi
        sleep "$POLL_SECONDS"
    done
}

main "$@"
