#!/usr/bin/env bash

# Check that repeated installs preserve mirror files and conflicting pins fail
# without changing them. Check concurrent installs with conflicting pins.
# Missing archives require network access and an authenticated gh session
# for the first download.

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
readonly RACE_ROUNDS=5

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
    # Record files with mode 0755 to detect permission changes.
    find "${mirror_dir}" -type f -perm 0755 | sort | sed 's/^/mode_0755 /' >> "${output_path}"
}

archive_member_path() {
    local pin_file="$1"
    local platform="$2"
    local output_dir="$3"
    local row_source row_repository row_tag row_rest
    read -r row_source row_repository row_tag row_rest < "${pin_file}"
    local binary_name="terraform-provider-${row_source##*/}"
    local archive_path="${ARCHIVE_DIR}/${binary_name}/${row_tag}/${binary_name}_${platform}.tar.gz"
    mkdir -p "${output_dir}"
    tar -xzf "${archive_path}" -C "${output_dir}" "${binary_name}"
    printf '%s\n' "${output_dir}/${binary_name}"
}

run_race_round() {
    local round="$1"
    local platform="$2"
    local assigned_pin_file="$3"
    local conflict_pin_file="$4"
    local race_dir="${WORK_DIR}/race-${round}"
    local race_mirror="${race_dir}/mirror"
    local target_dir="${race_mirror}/${PVEGUEST_SOURCE}/${ASSIGNED_VERSION}/${platform}"
    local target_path="${target_dir}/terraform-provider-pveguest_v${ASSIGNED_VERSION}"
    mkdir -p "${race_dir}"

    "${INSTALLER}" "${assigned_pin_file}" "${race_mirror}" "${ARCHIVE_DIR}" \
        > "${race_dir}/assigned.log" 2>&1 &
    local assigned_pid=$!
    "${INSTALLER}" "${conflict_pin_file}" "${race_mirror}" "${ARCHIVE_DIR}" \
        > "${race_dir}/conflict.log" 2>&1 &
    local conflict_pid=$!

    local assigned_exit_code=0
    local conflict_exit_code=0
    wait "${assigned_pid}" || assigned_exit_code=$?
    wait "${conflict_pid}" || conflict_exit_code=$?

    local winner_pin_file loser_log
    if [[ ${assigned_exit_code} -eq 0 && ${conflict_exit_code} -ne 0 ]]; then
        winner_pin_file="${assigned_pin_file}"
        loser_log="${race_dir}/conflict.log"
    elif [[ ${conflict_exit_code} -eq 0 && ${assigned_exit_code} -ne 0 ]]; then
        winner_pin_file="${conflict_pin_file}"
        loser_log="${race_dir}/assigned.log"
    else
        cat "${race_dir}/assigned.log" "${race_dir}/conflict.log" >&2
        fail "The installers did not produce exactly one success round=${round} assigned_exit_code=${assigned_exit_code} conflict_exit_code=${conflict_exit_code}"
    fi

    if ! grep -qF "target_path=${target_path}" "${loser_log}"; then
        cat "${loser_log}" >&2
        fail "The failing installer did not identify the target path round=${round} target_path=${target_path}"
    fi

    local winner_member
    winner_member="$(archive_member_path "${winner_pin_file}" "${platform}" "${race_dir}/winner")"
    if ! cmp -s "${winner_member}" "${target_path}"; then
        fail "The target binary differs from the winning archive member round=${round} target_path=${target_path}"
    fi
    if [[ -z "$(find "${target_path}" -perm 0755)" ]]; then
        fail "The target binary does not have mode 0755 round=${round} target_path=${target_path}"
    fi

    local extra_entries
    extra_entries="$(find "${target_dir}" -mindepth 1 ! -path "${target_path}")"
    if [[ -n "${extra_entries}" ]]; then
        echo "${extra_entries}" >&2
        fail "The target directory contains entries other than the target binary round=${round} target_dir=${target_dir}"
    fi
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
    local assigned_pin_file="${WORK_DIR}/assigned.pin"

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

    # Run five rounds with two installers assigning different pveguest archives to
    # version 0.1.1 in a new mirror. Require one winner and check its binary, mode,
    # error from the other installer, and removal of temporary files.
    awk -v source="${PVEGUEST_SOURCE}" -v version="${ASSIGNED_VERSION}" \
        -v platform="${platform}" \
        '$1 == source && $4 == version && $5 == platform { print }' \
        "${PIN_FILE}" > "${assigned_pin_file}"
    if [[ ! -s "${assigned_pin_file}" ]]; then
        fail "Pin file does not contain a pveguest row for the requested version and platform pin_file=${PIN_FILE} version=${ASSIGNED_VERSION} platform=${platform}"
    fi
    local round
    for ((round = 1; round <= RACE_ROUNDS; round++)); do
        run_race_round "${round}" "${platform}" "${assigned_pin_file}" "${conflict_pin_file}"
    done

    echo "Provider installation checks passed platform=${platform}"
}

main "$@"
