#!/usr/bin/env bash

# Check repeat installation, conflicting pins, concurrent installation, and
# recovery from staging leftovers with the real installer and OpenTofu.
# The test requires tofu on PATH. The first download of missing archives
# requires network access and an authenticated gh session.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly SCRIPT_DIR REPOSITORY_ROOT

readonly INSTALLER="${SCRIPT_DIR}/install-providers.sh"
readonly PIN_FILE="${SCRIPT_DIR}/providers.pin"
readonly LOCK_ROOT_FIXTURE="${SCRIPT_DIR}/testdata/pveguest-lock-root/main.tf"
readonly ARCHIVE_DIR="${REPOSITORY_ROOT}/.make/releases"
readonly PVEGUEST_SOURCE="tofu.home.arpa/agoodkind/pveguest"
readonly ASSIGNED_VERSION="0.1.1"
readonly REPLACEMENT_VERSION="0.1.2"
readonly RACE_ROUNDS=5
readonly WATCH_INTERVAL_SECONDS=0.05
readonly DIFFERENT_BINARY_MESSAGE="A different binary exists at the target"

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

record_target_state() {
    local target_path="$1"
    local output_path="$2"
    local inode_line digest_line
    if ! inode_line="$(ls -i "${target_path}" 2> /dev/null)"; then
        return 0
    fi
    if ! digest_line="$(shasum -a 256 "${target_path}" 2> /dev/null)"; then
        return 0
    fi
    local inode
    read -r inode _ <<< "${inode_line}"
    echo "inode=${inode} sha256=${digest_line%% *}" >> "${output_path}"
}

# Record the target inode and SHA-256 every 0.05 seconds and once after the
# installers exit to detect a change during the race.
watch_target() {
    local target_path="$1"
    local output_path="$2"
    local stop_path="$3"
    while [[ ! -e "${stop_path}" ]]; do
        record_target_state "${target_path}" "${output_path}"
        sleep "${WATCH_INTERVAL_SECONDS}"
    done
    record_target_state "${target_path}" "${output_path}"
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
    local states_path="${race_dir}/target-states"
    local stop_path="${race_dir}/stop-watch"
    mkdir -p "${race_dir}"

    watch_target "${target_path}" "${states_path}" "${stop_path}" &
    local watch_pid=$!
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
    : > "${stop_path}"
    wait "${watch_pid}"

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

    local distinct_states
    distinct_states="$(sort -u "${states_path}")"
    if [[ "${distinct_states}" == *$'\n'* ]]; then
        echo "${distinct_states}" >&2
        fail "The target inode or bytes changed during the race round=${round} target_path=${target_path}"
    fi

    if ! grep -qF "${DIFFERENT_BINARY_MESSAGE}" "${loser_log}" ||
        ! grep -qF "target_path=${target_path}" "${loser_log}"; then
        cat "${loser_log}" >&2
        fail "The failing installer did not report the different binary error for the target round=${round} target_path=${target_path}"
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

lock_root() {
    local root_dir="$1"
    local mirror_dir="$2"
    local platform="$3"
    mkdir -p "${root_dir}"
    cp "${LOCK_ROOT_FIXTURE}" "${root_dir}/main.tf"
    tofu -chdir="${root_dir}" providers lock -fs-mirror="${mirror_dir}" \
        -platform="${platform}" "${PVEGUEST_SOURCE}" > "${root_dir}/lock.log" 2>&1
}

init_readonly_root() {
    local root_dir="$1"
    local mirror_dir="$2"
    local lock_file="$3"
    mkdir -p "${root_dir}"
    cp "${LOCK_ROOT_FIXTURE}" "${root_dir}/main.tf"
    cp "${lock_file}" "${root_dir}/.terraform.lock.hcl"
    tofu -chdir="${root_dir}" init -backend=false -input=false -lockfile=readonly \
        -plugin-dir="${mirror_dir}" > "${root_dir}/init.log" 2>&1
}

# Check that staging leftovers do not change OpenTofu checksums, that cleanup
# deletes only the exited process's run directory, and that the installer rejects
# an old temporary file inside the package directory without changing the mirror.
run_killed_run_case() {
    local platform="$1"
    local assigned_pin_file="$2"
    local replacement_member="$3"
    local case_dir="${WORK_DIR}/killed-run"
    local case_mirror="${case_dir}/mirror"
    local staging_root="${case_dir}/.mirror.staging"
    local target_dir="${case_mirror}/${PVEGUEST_SOURCE}/${ASSIGNED_VERSION}/${platform}"
    local reference_lock="${case_dir}/reference-root/.terraform.lock.hcl"

    "${INSTALLER}" "${assigned_pin_file}" "${case_mirror}" "${ARCHIVE_DIR}"
    lock_root "${case_dir}/reference-root" "${case_mirror}" "${platform}"

    local dead_pid
    true &
    dead_pid=$!
    wait "${dead_pid}"
    local stale_run_dir="${staging_root}/run.${dead_pid}.killed"
    local live_run_dir="${staging_root}/run.$$.live"
    local foreign_entry="${staging_root}/operator-notes"
    local stale_candidate="${stale_run_dir}/terraform-provider-pveguest_v${ASSIGNED_VERSION}.leftover"
    mkdir -p "${stale_run_dir}" "${live_run_dir}" "${foreign_entry}"
    cp "${replacement_member}" "${stale_candidate}"
    chmod 0755 "${stale_candidate}"

    if ! init_readonly_root "${case_dir}/leftover-init-root" "${case_mirror}" "${reference_lock}"; then
        cat "${case_dir}/leftover-init-root/init.log" >&2
        fail "tofu init failed with the reference lockfile while staging leftovers were present case_dir=${case_dir}"
    fi
    lock_root "${case_dir}/leftover-lock-root" "${case_mirror}" "${platform}"
    if ! diff "${reference_lock}" "${case_dir}/leftover-lock-root/.terraform.lock.hcl" >&2; then
        fail "The lockfile generated with staging leftovers differs from the reference lockfile case_dir=${case_dir}"
    fi

    "${INSTALLER}" "${assigned_pin_file}" "${case_mirror}" "${ARCHIVE_DIR}"
    if [[ -e "${stale_run_dir}" ]]; then
        fail "The installer kept the run directory of an exited process stale_run_dir=${stale_run_dir}"
    fi
    if [[ ! -d "${live_run_dir}" || ! -d "${foreign_entry}" ]]; then
        fail "The installer deleted a live run directory or an unrelated entry staging_root=${staging_root}"
    fi
    if ! init_readonly_root "${case_dir}/rerun-init-root" "${case_mirror}" "${reference_lock}"; then
        cat "${case_dir}/rerun-init-root/init.log" >&2
        fail "tofu init failed with the reference lockfile after the installer ran again case_dir=${case_dir}"
    fi

    local package_leftover="${target_dir}/.terraform-provider-pveguest_v${ASSIGNED_VERSION}.Y0VvPz"
    local package_before="${case_dir}/package-before.sha256"
    local package_after="${case_dir}/package-after.sha256"
    local package_stderr="${case_dir}/package-leftover.stderr"
    cp "${replacement_member}" "${package_leftover}"
    mirror_digests "${case_mirror}" "${package_before}"
    if "${INSTALLER}" "${assigned_pin_file}" "${case_mirror}" "${ARCHIVE_DIR}" 2> "${package_stderr}"; then
        fail "The installer accepted an extra file in the package directory target_dir=${target_dir}"
    fi
    if ! grep -qF "${package_leftover}" "${package_stderr}"; then
        cat "${package_stderr}" >&2
        fail "The installer error does not list the extra file package_leftover=${package_leftover}"
    fi
    mirror_digests "${case_mirror}" "${package_after}"
    if ! diff "${package_before}" "${package_after}" >&2; then
        fail "The failed installer changed mirror files or modes target_dir=${target_dir}"
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

    # Run five races between installers that assign different binaries to the same
    # target and check that the target does not change after its creation.
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

    local replacement_member
    replacement_member="$(archive_member_path "${conflict_pin_file}" "${platform}" "${WORK_DIR}/replacement")"
    run_killed_run_case "${platform}" "${assigned_pin_file}" "${replacement_member}"

    echo "Provider installation checks passed platform=${platform}"
}

main "$@"
