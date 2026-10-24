#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPTS_REPO="https://github.com/agoodkind/scripts.git"
readonly REQUIRED_ARGUMENT_COUNT=1
readonly USAGE_EXIT_STATUS=2

clone_repo() {
    local scripts_dir="$1"
    if [[ -d "${scripts_dir}" ]]; then
        echo "[scripts-update] The updater removes the existing scripts directory because it has no .git directory. scripts_dir=${scripts_dir}"
        rm -rf "${scripts_dir}"
    fi
    echo "[scripts-update] The updater clones agoodkind/scripts into the scripts directory. scripts_dir=${scripts_dir}"
    git clone "${SCRIPTS_REPO}" "${scripts_dir}"
}

register_safe_directory() {
    local scripts_dir="$1"
    if ! git config --system --get-all safe.directory | grep -Fx -- "${scripts_dir}" > /dev/null; then
        git config --system --add safe.directory "${scripts_dir}"
    fi
}

update_repo() {
    local scripts_dir="$1"
    echo "[scripts-update] Starting update of ${scripts_dir}"
    register_safe_directory "${scripts_dir}"
    echo "[scripts-update] Fetching from origin"
    git -C "${scripts_dir}" fetch origin
    echo "[scripts-update] Resetting to origin/main"
    git -C "${scripts_dir}" reset --hard origin/main
    echo "[scripts-update] Updated to $(git -C "${scripts_dir}" rev-parse --short HEAD)"
}

main() {
    if [[ $# -ne ${REQUIRED_ARGUMENT_COUNT} || -z "$1" ]]; then
        echo "[scripts-update] The updater requires exactly one nonempty scripts directory argument. argument_count=$#" >&2
        exit "${USAGE_EXIT_STATUS}"
    fi
    local scripts_dir="$1"

    if [[ -d "${scripts_dir}/.git" ]]; then
        update_repo "${scripts_dir}"
    else
        clone_repo "${scripts_dir}"
    fi
}

main "$@"
