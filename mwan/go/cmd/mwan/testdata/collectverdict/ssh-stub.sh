#!/usr/bin/env bash
# Stands in for ssh on PATH under the collect-deploy-verdict.sh tests. The
# collector passes the remote command as the last argument; behavior comes
# from the environment:
#   STUB_STATE_DIR     holds the read counter and the staged verdict.json
#   STUB_CAT_FAILURES  number of leading verdict reads that fail with ENOENT
# A verdict read past the failure budget prints the staged verdict. The unit
# status probe always reports inactive, as systemctl does for a transient
# unit that systemd-run --collect has already garbage-collected.
set -euo pipefail

remote_command="${!#}"
count_file="$STUB_STATE_DIR/cat-count"

if [[ "$remote_command" == cat* ]]; then
    count=0
    if [[ -f "$count_file" ]]; then
        count="$(cat "$count_file")"
    fi
    count=$((count + 1))
    printf '%s' "$count" >"$count_file"
    if (( count <= STUB_CAT_FAILURES )); then
        echo "cat: /run/mwan-deploy-gate/trace-1.json: No such file or directory" >&2
        exit 1
    fi
    cat "$STUB_STATE_DIR/verdict.json"
    exit 0
fi

if [[ "$remote_command" == systemctl* ]]; then
    echo "inactive"
    exit 3
fi

echo "ssh stub: unexpected remote command: $remote_command" >&2
exit 64
