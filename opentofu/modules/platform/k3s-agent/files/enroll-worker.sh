#!/usr/bin/env bash
set -euo pipefail

action=${1:?supply enroll or remove}
server=${2:?supply the server VM ID}
worker=${3:?supply the worker VM ID}
build_uuid=${4:?supply the worker SMBIOS UUID}
node="worker-$worker-$build_uuid"
token_path=/etc/rancher/k3s/agent-token

guest() {
    local vmid=$1 pass_stdin=$2
    shift 2
    qm guest exec "$vmid" --pass-stdin "$pass_stdin" --timeout 330 -- "$@" |
        jq -erj --arg vm "$vmid" --arg command "$1" '
            if .exited == 1 and .exitcode == 0 and (.["out-truncated"] != 1)
            then .["out-data"] // ""
            else error("VM " + $vm + " failed to complete " + $command + " (exit " + (.exitcode | tostring) + ")")
            end'
}

kubectl() {
    guest "$server" 0 k3s kubectl "$@"
}

case "$action" in
    remove)
        kubectl delete node "$node" --ignore-not-found
        exit 0
        ;;
    enroll) ;;
    *) echo "Use enroll or remove." >&2; exit 1 ;;
esac

actual_uuid=$(guest "$worker" 0 cat /sys/class/dmi/id/product_uuid | tr '[:upper:]' '[:lower:]')
if [[ "$actual_uuid" != "$build_uuid" ]]; then
    echo "VM $worker has a different SMBIOS UUID. Refresh the worker plan." >&2
    exit 1
fi

token=$(guest "$server" 0 k3s token create --ttl 30m --description "$node")
if [[ ! "$token" =~ ^K10[0-9a-f]{64}::([a-z0-9]{6})\.[a-z0-9]{16}$ ]]; then
    echo "K3s returned an invalid secure bootstrap token." >&2
    exit 1
fi
token_id=${BASH_REMATCH[1]}
revoke_token() {
    guest "$server" 0 k3s token delete "$token_id" >/dev/null || {
        echo "K3s could not revoke the worker token. It expires after 30 minutes." >&2
        exit 1
    }
}
trap revoke_token EXIT

# The guest agent supplies the token on stdin. Only root can read the file.
guest "$worker" 0 install -m 0600 /dev/null "$token_path"
printf '%s\n' "$token" | guest "$worker" 1 dd "of=$token_path" status=none
unset token
guest "$worker" 0 systemctl daemon-reload
guest "$worker" 0 systemctl restart --no-block k3s-agent.service
kubectl wait --for=create "node/$node" --timeout=5m
kubectl wait --for=condition=Ready "node/$node" --timeout=5m
