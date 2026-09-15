#!/usr/bin/env bash
# Stand-in for dpkg in the package-updater tests. It appends its argv to the
# file named by PACKAGE_UPDATER_RECORD and exits with FAKE_DPKG_EXIT_CODE.
set -euo pipefail

printf 'dpkg %s\n' "$*" >>"${PACKAGE_UPDATER_RECORD}"

exit "${FAKE_DPKG_EXIT_CODE}"
