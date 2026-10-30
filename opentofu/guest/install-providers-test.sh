#!/usr/bin/env bash

# Check that repeated installs preserve mirror files and conflicting pins fail
# without changing them. Missing archives require network access and an
# authenticated gh session for the first download.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly SCRIPT_DIR REPOSITORY_ROOT

readonly INSTALLER="${SCRIPT_DIR}/install-providers.sh"
readonly PIN_FILE="${SCRIPT_DIR}/providers.pin"
readonly ARCHIVE_DIR="${REPOSITORY_ROOT}/.make/releases"
readonly PVEGUEST_SOURCE="tofu.home.arpa/agoodkind/pveguest"
readonly ASSIGNED_VERSION="0.1.1"
readonly REPLACEMENT_VERSION="0.1.2"

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

mirror_digests() {
    local mirror_dir="$1"
    local output_path="$2"
    find "${mirror_dir}" -type f -exec shasum -a 256 {} + | sort > "${output_path}"
}

main() {
    WORK_DIR="$(mktemp -d)"
    trap cleanup EXIT

    local mirror_dir="${WORK_DIR}/mirror"
    local first_digests="${WORK_DIR}/first.sha256"
    local second_digests="${WORK_DIR}/second.sha256"
    local conflict_digests="${WORK_DIR}/conflict.sha256"
    local conflict_pin_file="${WORK_DIR}/conflict.pin"
    local conflict_stderr="${WORK_DIR}/conflict.stderr"

    "${INSTALLER}" "${PIN_FILE}" "${mirror_dir}" "${ARCHIVE_DIR}"
    mirror_digests "${mirror_dir}" "${first_digests}"

    "${INSTALLER}" "${PIN_FILE}" "${mirror_dir}" "${ARCHIVE_DIR}"
    mirror_digests "${mirror_dir}" "${second_digests}"
    if ! diff "${first_digests}" "${second_digests}" >&2; then
        fail "Second install changed mirror files mirror_dir=${mirror_dir}"
    fi

    local platform_dirs=("${mirror_dir}/${PVEGUEST_SOURCE}/${ASSIGNED_VERSION}"/*)
    if [[ ${#platform_dirs[@]} -ne 1 || ! -d "${platform_dirs[0]}" ]]; then
        fail "Mirror does not contain exactly one platform directory for pveguest 0.1.1 mirror_dir=${mirror_dir}"
    fi
    local platform="${platform_dirs[0]##*/}"
    local assigned_target="${platform_dirs[0]}/terraform-provider-pveguest_v${ASSIGNED_VERSION}"

    # Assign the host platform's pveguest 0.1.2 archive to mirror version 0.1.1.
    awk -v source="${PVEGUEST_SOURCE}" -v from="${REPLACEMENT_VERSION}" \
        -v to="${ASSIGNED_VERSION}" -v platform="${platform}" \
        '$1 == source && $4 == from && $5 == platform { $4 = to; print }' \
        "${PIN_FILE}" > "${conflict_pin_file}"
    if [[ ! -s "${conflict_pin_file}" ]]; then
        fail "Pin file does not contain a pveguest row for the requested version and platform pin_file=${PIN_FILE} version=${REPLACEMENT_VERSION} platform=${platform}"
    fi

    if "${INSTALLER}" "${conflict_pin_file}" "${mirror_dir}" "${ARCHIVE_DIR}" 2> "${conflict_stderr}"; then
        fail "Installer accepted different bytes for an installed version conflict_pin_file=${conflict_pin_file}"
    fi
    if ! grep -qF "target_path=${assigned_target}" "${conflict_stderr}"; then
        cat "${conflict_stderr}" >&2
        fail "Installer error does not identify the existing target assigned_target=${assigned_target}"
    fi
    mirror_digests "${mirror_dir}" "${conflict_digests}"
    if ! diff "${first_digests}" "${conflict_digests}" >&2; then
        fail "Rejected install changed mirror files mirror_dir=${mirror_dir}"
    fi

    echo "Provider installation checks passed platform=${platform}"
}

main "$@"
