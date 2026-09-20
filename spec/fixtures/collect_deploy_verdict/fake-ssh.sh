#!/usr/bin/env bash
# Stand-in for ssh in the collect-deploy-verdict tests. The collector passes the
# remote command as its last argument; this script's behavior comes from the
# environment:
#   FAKE_SSH_STATE_DIR        holds the read counter and the staged verdict.json
#   FAKE_SSH_CAT_FAILURES     leading verdict reads that fail with ENOENT
#   FAKE_SSH_TRANSPORT_AFTER  reads past this count fail with ssh's 255, the
#                             transport error, rather than reaching the host
# A verdict read past the failure budget prints the staged verdict. The unit
# status probe always reports inactive, as systemctl does for a transient unit
# that systemd-run --collect has already garbage-collected.
set -euo pipefail

remote_command="${!#}"
count_file="$FAKE_SSH_STATE_DIR/cat-count"
transport_after="${FAKE_SSH_TRANSPORT_AFTER:-0}"

if [[ "$remote_command" == cat* ]]; then
    printf '%s' "$remote_command" >"$FAKE_SSH_STATE_DIR/last-cat-command"
    count=0
    if [[ -f "$count_file" ]]; then
        count="$(cat "$count_file")"
    fi
    count=$((count + 1))
    printf '%s' "$count" >"$count_file"
    if (( transport_after > 0 && count > transport_after )); then
        echo "ssh: connect to host 192.0.2.10 port 22: Connection timed out" >&2
        exit 255
    fi
    if (( count <= FAKE_SSH_CAT_FAILURES )); then
        echo "cat: no such file or directory" >&2
        exit 1
    fi
    cat "$FAKE_SSH_STATE_DIR/verdict.json"
    exit 0
fi

if [[ "$remote_command" == systemctl* ]]; then
    echo "inactive"
    exit 3
fi

echo "fake ssh: unexpected remote command: $remote_command" >&2
exit 64
