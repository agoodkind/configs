#!/usr/bin/env bash
#
# Run home-route-tracker.sh DESTINATION GATEWAY INTERFACE INTERVAL_SECONDS
# FAILURE_THRESHOLD ROUTE_METRIC.

set -euo pipefail

readonly DESTINATION="$1"
readonly GATEWAY="$2"
readonly INTERFACE="$3"
readonly INTERVAL_SECONDS="$4"
readonly FAILURE_THRESHOLD="$5"
readonly ROUTE_METRIC="$6"
readonly PROBE_TIMEOUT_SECONDS=1
readonly INTERRUPT_EXIT_STATUS=130

INTERRUPTED=0
STOP_SIGNAL=""
CHILD_PIDS=()

handle_signal() {
    INTERRUPTED=1
    STOP_SIGNAL="$1"
}

route_installed() {
    local routes
    routes="$(ip -6 route show exact "${DESTINATION}" via "${GATEWAY}" dev "${INTERFACE}")"
    [[ -n "${routes}" ]]
}

install_route() {
    ip -6 route replace "${DESTINATION}" via "${GATEWAY}" dev "${INTERFACE}" \
        metric "${ROUTE_METRIC}" proto static
}

remove_route() {
    if route_installed; then
        ip -6 route del "${DESTINATION}" via "${GATEWAY}" dev "${INTERFACE}" \
            metric "${ROUTE_METRIC}" proto static
        echo "removed ${DESTINATION} via ${GATEWAY} dev ${INTERFACE}"
    fi
}

# The EXIT trap removes the route on every exit path because a stopped
# tracker cannot detect a failed next hop.
cleanup() {
    local child_pid
    trap - EXIT
    if [[ "${#CHILD_PIDS[@]}" -gt 0 ]]; then
        for child_pid in "${CHILD_PIDS[@]}"; do
            if kill -0 "${child_pid}" 2>/dev/null; then
                kill "${child_pid}"
            fi
        done
    fi
    remove_route
}

# A trapped signal ends the wait before the interval ends.
wait_for_interval() {
    local sleep_pid
    sleep "${INTERVAL_SECONDS}" &
    sleep_pid=$!
    CHILD_PIDS=("${sleep_pid}")
    if ! wait "${sleep_pid}"; then
        echo "the wait ended before ${INTERVAL_SECONDS} seconds" >&2
    fi
    CHILD_PIDS=()
}

main() {
    local failure_count=0

    trap cleanup EXIT
    trap 'handle_signal INT' INT
    trap 'handle_signal TERM' TERM

    while [[ "${INTERRUPTED}" -eq 0 ]]; do
        if ping -6 -c 1 -W "${PROBE_TIMEOUT_SECONDS}" -I "${INTERFACE}" "${GATEWAY}" >/dev/null; then
            failure_count=0
            install_route
        else
            failure_count=$((failure_count + 1))
            echo "probe ${failure_count} of ${FAILURE_THRESHOLD} to ${GATEWAY} on ${INTERFACE} failed" >&2
            if [[ "${failure_count}" -ge "${FAILURE_THRESHOLD}" ]]; then
                remove_route
            fi
        fi
        if [[ "${INTERRUPTED}" -eq 0 ]]; then
            wait_for_interval
        fi
    done

    if [[ "${STOP_SIGNAL}" == "INT" ]]; then
        exit "${INTERRUPT_EXIT_STATUS}"
    fi
}

main
