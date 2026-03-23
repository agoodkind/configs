#!/usr/bin/env bash
set -euo pipefail

# Modrinth mod updater for Crafty-managed Minecraft Fabric server.
# Discovers the server UUID from the Crafty servers directory,
# queries the Modrinth API for latest compatible versions,
# and downloads updated jars to the mods/ folder.
#
# Usage: update-mods.sh -d <servers_dir> [-l <log_file>] PROJECT_ID [PROJECT_ID ...]

function usage {
    echo "Usage: $0 -d <servers_dir> [-l <log_file>] PROJECT_ID [...]"
    exit 1
}

SERVERS_DIR=""
LOG_FILE="/var/log/mc-mod-update.log"
USER_AGENT="agoodkind/mc-mods (automated updater)"

while getopts "d:l:" opt; do
    case "${opt}" in
        d) SERVERS_DIR="${OPTARG}" ;;
        l) LOG_FILE="${OPTARG}" ;;
        *) usage ;;
    esac
done
shift $((OPTIND - 1))

if [[ -z "${SERVERS_DIR}" || $# -eq 0 ]]; then
    usage
fi

function log_msg {
    local ts
    ts="$(date +"%Y-%m-%d %H:%M:%S")"
    echo "${ts} $1" | tee -a "${LOG_FILE}"
}

function discover_server_uuid {
    local uuid_dir
    uuid_dir=$(ls -1 "${SERVERS_DIR}" | head -1)
    if [[ -z "${uuid_dir}" ]]; then
        log_msg "ERROR: No server found in ${SERVERS_DIR}"
        exit 1
    fi
    echo "${uuid_dir}"
}

function get_mc_version {
    local server_dir="$1"
    local version_dir
    version_dir=$(ls -1 "${server_dir}/versions" | head -1)
    if [[ -z "${version_dir}" ]]; then
        log_msg "ERROR: Cannot determine MC version"
        exit 1
    fi
    echo "${version_dir}"
}

# Extract a glob prefix from a filename by stripping everything
# from the first digit onward. E.g.:
#   lithium-fabric-0.21.3+mc1.21.11.jar  -> lithium-fabric-
#   Moonrise-Fabric-0.9.0-beta.4.jar     -> Moonrise-Fabric-
function filename_prefix {
    local name="$1"
    echo "${name}" | sed 's/[0-9].*//'
}

function get_latest_version {
    local project_id="$1"
    local mc_version="$2"

    local api_url
    api_url="https://api.modrinth.com/v2/project/${project_id}/version"
    api_url+="?loaders=%5B%22fabric%22%5D"
    api_url+="&game_versions=%5B%22${mc_version}%22%5D"

    local response
    response=$(curl -sH "User-Agent: ${USER_AGENT}" "${api_url}")

    local filename url
    filename=$(echo "${response}" \
        | jq -r '.[0].files[] | select(.primary) | .filename')
    url=$(echo "${response}" \
        | jq -r '.[0].files[] | select(.primary) | .url')

    if [[ -z "${filename}" || "${filename}" == "null" ]]; then
        return 1
    fi

    echo "${filename}|${url}"
}

function update_mod {
    local project_id="$1"
    local mods_dir="$2"
    local mc_version="$3"

    log_msg "Checking ${project_id}..."

    local result
    if ! result=$(get_latest_version "${project_id}" "${mc_version}"); then
        log_msg "WARNING: No version for ${project_id} on MC ${mc_version}"
        return 0
    fi

    local filename="${result%%|*}"
    local url="${result##*|}"

    if [[ -f "${mods_dir}/${filename}" ]]; then
        log_msg "${filename} already installed"
        return 0
    fi

    local prefix
    prefix=$(filename_prefix "${filename}")
    local old_jar
    for old_jar in "${mods_dir}"/${prefix}*.jar; do
        if [[ -f "${old_jar}" ]]; then
            log_msg "Removing old: $(basename "${old_jar}")"
            rm -f "${old_jar}"
        fi
    done

    log_msg "Downloading ${filename}..."
    if curl -sLo "${mods_dir}/${filename}" "${url}"; then
        chown crafty:crafty "${mods_dir}/${filename}"
        log_msg "Installed ${filename}"
        return 1
    else
        log_msg "ERROR: Failed to download ${filename}"
        return 0
    fi
}

function main {
    log_msg "Starting mod update check"

    local server_uuid
    server_uuid=$(discover_server_uuid)
    local server_dir="${SERVERS_DIR}/${server_uuid}"
    local mods_dir="${server_dir}/mods"
    local mc_version
    mc_version=$(get_mc_version "${server_dir}")

    log_msg "Server UUID: ${server_uuid}"
    log_msg "MC version: ${mc_version}"
    log_msg "Mods dir: ${mods_dir}"

    mkdir -p "${mods_dir}"

    local updated=0
    local project_id
    for project_id in "$@"; do
        if update_mod "${project_id}" "${mods_dir}" "${mc_version}"; then
            :
        else
            updated=$((updated + 1))
        fi
    done

    if [[ ${updated} -gt 0 ]]; then
        log_msg "${updated} mod(s) updated. Restart server to apply."
    else
        log_msg "All mods up to date."
    fi
}

main "$@"
