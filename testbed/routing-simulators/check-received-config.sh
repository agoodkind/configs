#!/usr/bin/env bash
#
# Run check-received-config.sh EXPECTED_BYTES COMMAND [ARGUMENT...] with the
# configuration on stdin. The script writes stdin to a temp file, requires the
# file size to equal EXPECTED_BYTES, and runs COMMAND with the temp file path
# as its last argument. vtysh -C and nft -c accept an empty stdin. The size
# check fails an empty or short stdin before COMMAND runs.

set -euo pipefail

readonly EXPECTED_BYTES="$1"
shift
RECEIVED_FILE="$(mktemp)"
readonly RECEIVED_FILE
trap 'rm -f "${RECEIVED_FILE}"' EXIT

main() {
    local received_bytes
    cat >"${RECEIVED_FILE}"
    received_bytes="$(wc -c <"${RECEIVED_FILE}")"
    if [[ "${received_bytes}" -ne "${EXPECTED_BYTES}" ]]; then
        echo "stdin contained ${received_bytes} bytes, and the staged file contains ${EXPECTED_BYTES} bytes" >&2
        exit 1
    fi
    "$@" "${RECEIVED_FILE}"
}

main "$@"
