#!/usr/bin/env bash

# Check that the installer test detects a link function that replaces a target.
# The test requires tofu on PATH. The first download of missing archives
# requires network access and an authenticated gh session.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly SCRIPT_DIR REPOSITORY_ROOT

readonly INSTALLER="${SCRIPT_DIR}/install-providers.sh"
readonly REPLACING_LINK="${SCRIPT_DIR}/testdata/replacing-link-no-replace.sh"
readonly MAIN_CALL='main "$@"'
readonly RACE_FAILURE_PATTERN='The installers did not produce exactly one success|The target inode or bytes changed during the race|The failing installer did not report the different binary error for the target'

WORK_DIR=""

cleanup() {
    if [[ -n "${WORK_DIR}" ]]; then
        rm -rf "${WORK_DIR}"
    fi
}

fail() {
    echo "$1" >&2
    exit 1
}

main() {
    WORK_DIR="$(mktemp -d)"
    trap cleanup EXIT

    local guest_dir="${WORK_DIR}/opentofu/guest"
    local mutant_installer="${guest_dir}/install-providers.sh"
    local test_log="${WORK_DIR}/mutant-test.log"
    mkdir -p "${guest_dir}"
    cp "${SCRIPT_DIR}/install-providers-test.sh" "${SCRIPT_DIR}/providers.pin" "${guest_dir}/"
    cp -R "${SCRIPT_DIR}/testdata" "${guest_dir}/testdata"
    ln -s "${REPOSITORY_ROOT}/.make" "${WORK_DIR}/.make"

    # Append the fixture after the installer functions and call main afterward.
    # Bash uses the fixture's later link_no_replace definition.
    awk -v main_call="${MAIN_CALL}" '$0 != main_call { print }' "${INSTALLER}" > "${mutant_installer}"
    cat "${REPLACING_LINK}" >> "${mutant_installer}"
    printf '%s\n' "${MAIN_CALL}" >> "${mutant_installer}"
    chmod 0755 "${mutant_installer}"

    if "${guest_dir}/install-providers-test.sh" > "${test_log}" 2>&1; then
        cat "${test_log}" >&2
        fail "The installer test passed with the replacing installer mutant_installer=${mutant_installer}"
    fi
    if ! grep -qE "${RACE_FAILURE_PATTERN}" "${test_log}"; then
        cat "${test_log}" >&2
        fail "The installer test failed without reporting a race failure mutant_installer=${mutant_installer}"
    fi
    grep -E "${RACE_FAILURE_PATTERN}" "${test_log}"
    echo "The installer test detected the replacing installer"
}

main "$@"
