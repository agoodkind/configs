#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

readonly DPKG_CONFOLD=--force-confold
readonly STOPPED_EXIT_CODE=143

STOP_REQUESTED=0

# The unit sets KillMode=process, so a stop sends SIGTERM to this script alone.
# A handler, rather than an ignored signal that apt-get and dpkg would inherit,
# makes bash finish waiting for the running apt-get or dpkg before it runs, and
# run_step then exits instead of starting the next step.
request_stop() {
    STOP_REQUESTED=1
    printf 'package-updater: stop requested, finishing the current step\n'
}

run_step() {
    if [[ "${STOP_REQUESTED}" -eq 1 ]]; then
        printf 'package-updater: stopped before: %s\n' "$*"
        exit "${STOPPED_EXIT_CODE}"
    fi
    "$@"
}

# A run killed mid-configure leaves packages unpacked or half configured, and
# apt-get refuses to upgrade until dpkg configures them. Configuring them first
# lets the next run heal that state. A failure here needs an operator, so the
# run stops before apt-get changes anything else.
configure_pending_packages() {
    printf 'package-updater: running dpkg --configure -a\n'
    if dpkg "${DPKG_CONFOLD}" --configure -a; then
        printf 'package-updater: dpkg --configure -a exit_code=0\n'
    else
        local configure_exit_code=$?
        printf 'package-updater: dpkg --configure -a failed exit_code=%s, skipping the upgrade\n' \
            "${configure_exit_code}" >&2
        exit "${configure_exit_code}"
    fi
}

trap request_stop TERM

run_step configure_pending_packages
run_step apt-get update
run_step apt-get -o "Dpkg::Options::=${DPKG_CONFOLD}" full-upgrade -y
run_step apt-get autoremove -y
run_step apt-get autoclean
