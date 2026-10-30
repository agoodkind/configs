#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly SCRIPT_DIR REPOSITORY_ROOT

readonly DEFAULT_PIN_FILE="${SCRIPT_DIR}/providers.pin"
# configsctl selects this shared mirror for every workspace.
readonly DEFAULT_MIRROR_DIR="${REPOSITORY_ROOT}/opentofu/terraform.d/plugins"
readonly DEFAULT_ARCHIVE_DIR="${REPOSITORY_ROOT}/.make/releases"
readonly SHA256_PATTERN='^[0-9a-f]{64}$'
readonly PINNED_ARGUMENT_COUNT=3

WORK_DIR=""
CANDIDATE_PATH=""

usage() {
    echo "usage: $0 [<pin_file> <mirror_dir> <archive_dir>]"
}

remove_candidate() {
    if [[ -n "${CANDIDATE_PATH}" ]]; then
        rm -f "${CANDIDATE_PATH}"
        CANDIDATE_PATH=""
    fi
}

cleanup() {
    remove_candidate
    if [[ -n "${WORK_DIR}" ]]; then
        rm -rf "${WORK_DIR}"
    fi
}

host_platform() {
    local kernel_name machine_name os arch
    kernel_name="$(uname -s)"
    machine_name="$(uname -m)"
    case "${kernel_name}" in
        Darwin) os="darwin" ;;
        Linux) os="linux" ;;
        *)
            echo "Unsupported host kernel kernel_name=${kernel_name}" >&2
            return 1
            ;;
    esac
    case "${machine_name}" in
        arm64 | aarch64) arch="arm64" ;;
        x86_64) arch="amd64" ;;
        *)
            echo "Unsupported host machine type machine_name=${machine_name}" >&2
            return 1
            ;;
    esac
    printf '%s_%s\n' "${os}" "${arch}"
}

file_sha256() {
    local path="$1"
    local digest_line
    digest_line="$(shasum -a 256 "${path}")"
    printf '%s\n' "${digest_line%% *}"
}

# Create a hard link only when the target path does not exist.
# Linux uses GNU ln -T to treat the target as a file, even if it is a directory.
# Darwin uses /bin/ln to select BSD ln from the base system, not GNU ln from PATH.
# BSD ln does not support -T, so install_provider rejects a directory target first.
link_no_replace() {
    local os="$1"
    local candidate_path="$2"
    local target_path="$3"
    case "${os}" in
        linux) ln -T "${candidate_path}" "${target_path}" ;;
        darwin) /bin/ln "${candidate_path}" "${target_path}" ;;
        *)
            echo "Cannot create a provider link on the host operating system os=${os}" >&2
            return 1
            ;;
    esac
}

install_provider() {
    local source_address="$1"
    local repository="$2"
    local tag="$3"
    local version="$4"
    local platform="$5"
    local expected_sha256="$6"
    local mirror_dir="$7"
    local archive_dir="$8"

    local name="${source_address##*/}"
    local binary_name="terraform-provider-${name}"
    local asset_name="${binary_name}_${platform}.tar.gz"
    local release_dir="${archive_dir}/${binary_name}/${tag}"
    local archive_path="${release_dir}/${asset_name}"
    local unpack_dir="${WORK_DIR}/${name}"
    local target_dir="${mirror_dir}/${source_address}/${version}/${platform}"
    local target_path="${target_dir}/${binary_name}_v${version}"
    local actual_sha256

    if [[ ! -f "${archive_path}" ]]; then
        mkdir -p "${release_dir}"
        gh release download "${tag}" --repo "${repository}" \
            --pattern "${asset_name}" --dir "${release_dir}"
    fi

    actual_sha256="$(file_sha256 "${archive_path}")"
    if [[ "${actual_sha256}" != "${expected_sha256}" ]]; then
        echo "Archive SHA-256 differs from pinned value archive_path=${archive_path} expected_sha256=${expected_sha256} actual_sha256=${actual_sha256}" >&2
        return 1
    fi

    mkdir -p "${unpack_dir}" "${target_dir}"
    tar -xzf "${archive_path}" -C "${unpack_dir}" "${binary_name}"

    if [[ -d "${target_path}" ]]; then
        echo "Cannot publish the binary because the target path is a directory target_path=${target_path}" >&2
        return 1
    fi

    # OpenTofu links initialized roots to the mirror. Replacing an existing binary
    # changes the executable those roots run and breaks their lockfile checksums.
    # Copy the binary to a temporary file in the target directory, then create a hard
    # link at the target path. The link fails if another installer created the target,
    # so concurrent installers cannot replace an existing binary.
    CANDIDATE_PATH="$(mktemp "${target_dir}/.${binary_name}_v${version}.XXXXXX")"
    cp "${unpack_dir}/${binary_name}" "${CANDIDATE_PATH}"
    chmod 0755 "${CANDIDATE_PATH}"

    local install_action link_error
    if link_error="$(link_no_replace "${platform%%_*}" "${CANDIDATE_PATH}" "${target_path}" 2>&1)"; then
        install_action="installed"
    elif [[ -e "${target_path}" ]]; then
        if ! cmp -s "${CANDIDATE_PATH}" "${target_path}"; then
            remove_candidate
            echo "A different binary exists at the target. If providers.pin assigns a different release archive to an installed mirror version, give the release a new mirror version in providers.pin. If the target file is damaged or partial, delete it and run the installer again source_address=${source_address} version=${version} platform=${platform} tag=${tag} target_path=${target_path}" >&2
            return 1
        fi
        install_action="reused"
    else
        remove_candidate
        echo "The link command failed and the target file does not exist target_path=${target_path} link_error=${link_error}" >&2
        return 1
    fi
    remove_candidate

    if [[ ! -x "${target_path}" ]] || ! cmp -s "${unpack_dir}/${binary_name}" "${target_path}"; then
        echo "Binary is not executable or mismatches archive member target_path=${target_path}" >&2
        return 1
    fi
    echo "Script ${install_action} one provider source_address=${source_address} tag=${tag} target_path=${target_path}"
}

main() {
    local pin_file mirror_dir archive_dir
    if [[ $# -eq 0 ]]; then
        pin_file="${DEFAULT_PIN_FILE}"
        mirror_dir="${DEFAULT_MIRROR_DIR}"
        archive_dir="${DEFAULT_ARCHIVE_DIR}"
    elif [[ $# -eq ${PINNED_ARGUMENT_COUNT} ]]; then
        pin_file="$1"
        mirror_dir="$2"
        archive_dir="$3"
    else
        usage >&2
        exit 2
    fi

    local platform
    platform="$(host_platform)"
    WORK_DIR="$(mktemp -d)"
    trap cleanup EXIT

    local line_number=0
    local seen_sources=""
    local installed_sources=""
    local source_address repository tag version row_platform expected_sha256 extra_field
    # The loop reads the pin file on file descriptor 3 because gh and tar read standard input.
    while read -r source_address repository tag version row_platform expected_sha256 extra_field <&3; do
        line_number=$((line_number + 1))
        if [[ -z "${source_address}" || "${source_address}" == \#* ]]; then
            continue
        fi
        if [[ -n "${extra_field}" || ! "${expected_sha256}" =~ ${SHA256_PATTERN} ]]; then
            echo "Pin row contains an extra field or an invalid SHA-256 pin_file=${pin_file} line_number=${line_number}" >&2
            exit 1
        fi
        if [[ " ${seen_sources} " != *" ${source_address} "* ]]; then
            seen_sources="${seen_sources} ${source_address}"
        fi
        if [[ "${row_platform}" != "${platform}" ]]; then
            continue
        fi
        install_provider "${source_address}" "${repository}" "${tag}" "${version}" \
            "${platform}" "${expected_sha256}" "${mirror_dir}" "${archive_dir}"
        installed_sources="${installed_sources} ${source_address}"
    done 3< "${pin_file}"

    if [[ -z "${seen_sources}" ]]; then
        echo "Pin file has no provider row pin_file=${pin_file}" >&2
        exit 1
    fi
    for source_address in ${seen_sources}; do
        if [[ " ${installed_sources} " != *" ${source_address} "* ]]; then
            echo "Pin file has no row for the provider on the host platform pin_file=${pin_file} source_address=${source_address} platform=${platform}" >&2
            exit 1
        fi
    done
}

main "$@"
