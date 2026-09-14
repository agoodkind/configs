#!/usr/bin/env bash
# Stand-in for apt-get in the package-updater tests. It appends its argv to the
# file named by PACKAGE_UPDATER_RECORD. The update verb waits for the file named
# by FAKE_APT_GET_UPDATE_GATE, so a test can signal the script mid-step.
set -euo pipefail

printf 'apt-get %s\n' "$*" >>"${PACKAGE_UPDATER_RECORD}"

if [[ "${1}" == "update" ]]; then
    while [[ ! -e "${FAKE_APT_GET_UPDATE_GATE}" ]]; do
        sleep 0.05
    done
    printf 'apt-get update finished\n' >>"${PACKAGE_UPDATER_RECORD}"
fi
