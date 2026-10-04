#!/usr/bin/env bash
# sshd provides the requested command in SSH_ORIGINAL_COMMAND.
# Use this authorized_keys entry:
#   command="/usr/local/sbin/tack-ops-gate /root/tack",restrict ssh-ed25519 ...
set -euo pipefail

LOG_TAG="tack-ops-gate"
REFUSED_STATUS=126
# The gate passes validated argument tokens directly to docker compose.
SAFE_WORD='^[A-Za-z0-9_@%+=:,./-]+$'

install_dir="${1:-}"
original_command="${SSH_ORIGINAL_COMMAND:-}"

refuse() {
    local reason="$1"
    logger -t "$LOG_TAG" -- "refused (${reason}): ${original_command}"
    printf '%s: refused: %s. This key runs only "./server ops <command>" and "docker compose run --rm tack-ops ops <command>".\n' \
        "$LOG_TAG" "$reason" >&2
    exit "$REFUSED_STATUS"
}

if [[ -z "$install_dir" ]]; then
    refuse "the forced command does not specify an installation directory"
fi
if [[ -z "$original_command" ]]; then
    refuse "an interactive shell"
fi

read -r -a words <<< "$original_command"
for word in "${words[@]}"; do
    if [[ ! "$word" =~ $SAFE_WORD ]]; then
        refuse "a word with a quote or a shell operator"
    fi
done

ops_arguments=()
service="tack-ops"
if [[ "${words[0]}" == "./server" && "${words[1]:-}" == "ops" ]]; then
    ops_arguments=("${words[@]:2}")
elif [[ "${words[*]:0:4}" == "docker compose run --rm" && "${words[5]:-}" == "ops" ]]; then
    service="${words[4]}"
    ops_arguments=("${words[@]:6}")
else
    refuse "not an ops command"
fi
if [[ "$service" != "tack-ops" && "$service" != "app" ]]; then
    refuse "compose service ${service}"
fi
if [[ "${#ops_arguments[@]}" -eq 0 ]]; then
    refuse "the request does not specify an ops command"
fi

logger -t "$LOG_TAG" -- "accepted: ${original_command}"
cd "$install_dir" || exit 1
exec docker compose run --rm "$service" ops "${ops_arguments[@]}"
