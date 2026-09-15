#!/bin/sh
# shellcheck shell=sh
# mwan-opnsense-instances.sh stops or counts the mwan-opnsense instances on the
# OPNsense guest, whether or not the rc.d pidfile names them.
#
# The rc.d status and start read only the supervisor pidfile, and the start
# deletes that pidfile before it launches daemon(8), which defeats the lock
# daemon(8) would otherwise use to refuse a second copy. An instance the pidfile
# does not name is therefore invisible to rc.d, and a start adds a second reader
# on the serial port. This script finds instances by command line instead. It
# runs on the guest under /bin/sh, because OPNsense ships no bash.
#
# An instance is a daemon(8) supervisor, titled "daemon: <target>[<pid>]", and
# the daemon under it, which runs as the run shim under /bin/sh and then as the
# daemon binary. The current rc.d script supervises the run shim; older ones
# supervised the daemon binary directly.
#
# Usage:
#   mwan-opnsense-instances.sh stop RC_SCRIPT RUN_SHIM DAEMON_BINARY WAIT_SECONDS
#   mwan-opnsense-instances.sh check-one RUN_SHIM DAEMON_BINARY PIDFILE
#
# Contract: stop exits 0 when no instance remains. check-one exits 0 and prints
# the pids when exactly one supervisor runs, the pidfile names it, and exactly
# one daemon runs under it. Both exit 1 otherwise, and 64 on invalid arguments.
set -eu

readonly EXIT_USAGE=64
readonly KIND_SUPERVISOR="supervisor"
readonly KIND_DAEMON="daemon"
readonly KIND_ANY="any"

usage() {
    echo "usage: $0 stop RC_SCRIPT RUN_SHIM DAEMON_BINARY WAIT_SECONDS" >&2
    echo "       $0 check-one RUN_SHIM DAEMON_BINARY PIDFILE" >&2
}

log() {
    echo "mwan-opnsense-instances: $*" >&2
}

# list_instances prints "pid ppid kind" for every instance process. Each pattern
# anchors at the start of the command line, so a pager or editor that names one
# of these paths is never matched.
list_instances() {
    local run_shim="$1"
    local daemon_binary="$2"
    local listing
    local ps_exit=0
    local pid
    local ppid
    local command

    listing="$(ps -A -ww -o pid= -o ppid= -o command=)" || ps_exit=$?
    if [ "${ps_exit}" -ne 0 ]; then
        log "ps exited ${ps_exit}; cannot list instances"
        return 1
    fi
    while read -r pid ppid command; do
        case "${command}" in
            "daemon: ${run_shim}["* | "daemon: ${daemon_binary}"*)
                echo "${pid} ${ppid} ${KIND_SUPERVISOR}"
                ;;
            "${run_shim}"* | "/bin/sh ${run_shim}"* | "${daemon_binary}"*)
                echo "${pid} ${ppid} ${KIND_DAEMON}"
                ;;
        esac
    done <<EOF
${listing}
EOF
}

# pids_of prints the pids of the instances of KIND, or of every instance when
# KIND is any.
pids_of() {
    local kind="$1"
    local run_shim="$2"
    local daemon_binary="$3"
    local instances
    local pid
    local _ppid
    local instance_kind

    instances="$(list_instances "${run_shim}" "${daemon_binary}")" || return 1
    while read -r pid _ppid instance_kind; do
        if [ -z "${pid}" ]; then
            continue
        fi
        if [ "${kind}" = "${KIND_ANY}" ] || [ "${kind}" = "${instance_kind}" ]; then
            echo "${pid}"
        fi
    done <<EOF
${instances}
EOF
}

# signal_instances sends SIGNAL_NAME to every instance of KIND. A process can
# exit between the listing and the kill, so a failed kill is logged and left to
# the wait that follows, which decides the outcome.
signal_instances() {
    local signal_name="$1"
    local kind="$2"
    local run_shim="$3"
    local daemon_binary="$4"
    local pids
    local pid

    pids="$(pids_of "${kind}" "${run_shim}" "${daemon_binary}")" || return 1
    if [ -z "${pids}" ]; then
        return 0
    fi
    log "sending ${signal_name} to ${kind} pids $(echo "${pids}" | tr '\n' ' ')"
    while read -r pid; do
        kill "-${signal_name}" "${pid}" || log "kill -${signal_name} ${pid} failed; it may have exited"
    done <<EOF
${pids}
EOF
}

# wait_until_gone polls once a second until no instance remains, and returns 1
# when WAIT_SECONDS pass first or the listing fails.
wait_until_gone() {
    local wait_seconds="$1"
    local run_shim="$2"
    local daemon_binary="$3"
    local waited=0
    local pids

    while :; do
        pids="$(pids_of "${KIND_ANY}" "${run_shim}" "${daemon_binary}")" || return 1
        if [ -z "${pids}" ]; then
            return 0
        fi
        if [ "${waited}" -ge "${wait_seconds}" ]; then
            return 1
        fi
        sleep 1
        waited=$((waited + 1))
    done
}

stop_instances() {
    local rc_script="$1"
    local run_shim="$2"
    local daemon_binary="$3"
    local wait_seconds="$4"
    local rc_exit=0
    local remaining

    # The rc.d stop ends the instance the pidfile names and clears the pidfiles.
    "${rc_script}" stop || rc_exit=$?
    if [ "${rc_exit}" -ne 0 ]; then
        log "rc.d stop exited ${rc_exit}; sweeping every instance regardless"
    fi

    # A TERM to a daemon(8) supervisor is forwarded to its daemon and turns off
    # the respawn, while a TERM to the daemon alone makes the supervisor start
    # another. Supervisors therefore get the first signal.
    signal_instances TERM "${KIND_SUPERVISOR}" "${run_shim}" "${daemon_binary}" || return 1
    if wait_until_gone "${wait_seconds}" "${run_shim}" "${daemon_binary}"; then
        log "no instance remains"
        return 0
    fi

    # What is left is a daemon whose supervisor is gone or ignored the TERM.
    signal_instances TERM "${KIND_DAEMON}" "${run_shim}" "${daemon_binary}" || return 1
    if wait_until_gone "${wait_seconds}" "${run_shim}" "${daemon_binary}"; then
        log "no instance remains"
        return 0
    fi

    # KILL goes to supervisors before daemons for the same respawn reason.
    signal_instances KILL "${KIND_SUPERVISOR}" "${run_shim}" "${daemon_binary}" || return 1
    signal_instances KILL "${KIND_DAEMON}" "${run_shim}" "${daemon_binary}" || return 1
    if wait_until_gone "${wait_seconds}" "${run_shim}" "${daemon_binary}"; then
        log "no instance remains"
        return 0
    fi

    remaining="$(list_instances "${run_shim}" "${daemon_binary}")" || return 1
    log "instances still running after KILL (pid ppid kind):"
    echo "${remaining}" >&2
    return 1
}

check_one() {
    local run_shim="$1"
    local daemon_binary="$2"
    local pidfile="$3"
    local instances
    local tracked_pid=""
    local supervisor_count=0
    local daemon_count=0
    local supervisor_pid=""
    local daemon_pid=""
    local daemon_parent=""
    local pid
    local ppid
    local kind

    instances="$(list_instances "${run_shim}" "${daemon_binary}")" || return 1
    while read -r pid ppid kind; do
        case "${kind}" in
            "${KIND_SUPERVISOR}")
                supervisor_count=$((supervisor_count + 1))
                supervisor_pid="${pid}"
                ;;
            "${KIND_DAEMON}")
                daemon_count=$((daemon_count + 1))
                daemon_pid="${pid}"
                daemon_parent="${ppid}"
                ;;
        esac
    done <<EOF
${instances}
EOF

    if [ -f "${pidfile}" ]; then
        tracked_pid="$(cat "${pidfile}")"
    fi

    if [ "${supervisor_count}" -eq 1 ] && [ "${daemon_count}" -eq 1 ] \
        && [ "${supervisor_pid}" = "${tracked_pid}" ] \
        && [ "${daemon_parent}" = "${supervisor_pid}" ]; then
        echo "supervisor=${supervisor_pid} daemon=${daemon_pid}"
        return 0
    fi

    log "want one supervisor named by ${pidfile} with one daemon under it;" \
        "found ${supervisor_count} supervisor(s) and ${daemon_count} daemon(s)," \
        "and the pidfile names '${tracked_pid}' (pid ppid kind follow)"
    echo "${instances}" >&2
    return 1
}

main() {
    local mode

    if [ "$#" -lt 1 ]; then
        usage
        exit "${EXIT_USAGE}"
    fi
    mode="$1"
    shift

    case "${mode}" in
        stop)
            if [ "$#" -ne 4 ]; then
                usage
                exit "${EXIT_USAGE}"
            fi
            case "$4" in
                "" | *[!0-9]*)
                    usage
                    exit "${EXIT_USAGE}"
                    ;;
            esac
            stop_instances "$@"
            ;;
        check-one)
            if [ "$#" -ne 3 ]; then
                usage
                exit "${EXIT_USAGE}"
            fi
            check_one "$@"
            ;;
        *)
            usage
            exit "${EXIT_USAGE}"
            ;;
    esac
}

main "$@"
