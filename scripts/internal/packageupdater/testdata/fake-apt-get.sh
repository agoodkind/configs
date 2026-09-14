#!/usr/bin/env bash
# Stand-in for apt-get in the package-updater tests. It appends its argv to the
# file named by PACKAGE_UPDATER_RECORD. The update verb waits for the file named
# by FAKE_APT_GET_UPDATE_GATE, so a test can signal the script mid-step. The
# wait is bounded so a test that fails before opening the gate leaves no
# process polling behind it.
set -euo pipefail

readonly GATE_POLL_SECONDS=0.05
readonly GATE_MAX_POLLS=400

printf 'apt-get %s\n' "$*" >>"${PACKAGE_UPDATER_RECORD}"

if [[ "${1}" == "update" ]]; then
    poll_count=0
    while [[ ! -e "${FAKE_APT_GET_UPDATE_GATE}" ]]; do
        poll_count=$((poll_count + 1))
        if [[ "${poll_count}" -gt "${GATE_MAX_POLLS}" ]]; then
            printf 'fake apt-get: update gate %s never opened\n' "${FAKE_APT_GET_UPDATE_GATE}" >&2
            exit 1
        fi
        sleep "${GATE_POLL_SECONDS}"
    done
    printf 'apt-get update finished\n' >>"${PACKAGE_UPDATER_RECORD}"
fi
