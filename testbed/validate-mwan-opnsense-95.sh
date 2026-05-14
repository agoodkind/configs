#!/usr/bin/env bash
# MWAN-95 Step 5 validation: 50/50 success three runs in a row.
#
# Drives 50 independent gRPC probes against the bridge daemon's local
# unix socket. The bridge multiplexes them onto the persistent
# upstream connection that holds /dev/ttyV0.1 open inside the
# OPNsense guest. Three consecutive 50/50 runs pass MWAN-95 acceptance.
#
# Run from suburban (the Proxmox host that owns VM 101).
#
# Usage:
#   sudo ./validate-mwan-opnsense-95.sh                 # default bridge socket
#   sudo MWAN_OPNSENSE_SOCK=/tmp/foo ./validate-mwan-opnsense-95.sh
#
# Exits 0 if all three runs hit 50/50. Exits 1 with a per-run summary
# line otherwise.

set -u

readonly DEFAULT_SOCK=/var/run/mwan-opnsense.sock
readonly DEFAULT_PROBE=/usr/local/bin/mwan
readonly RUN_COUNT=3
readonly PROBE_COUNT=50

bridge_sock=${MWAN_OPNSENSE_SOCK:-$DEFAULT_SOCK}
probe_bin=${MWAN_OPNSENSE_PROBE:-$DEFAULT_PROBE}

if [[ ! -S $bridge_sock ]]; then
    printf 'validate: bridge socket %s does not exist or is not a socket\n' "$bridge_sock" >&2
    printf 'validate: hint: is mwan-opnsense-host running? (systemctl status mwan-opnsense-host)\n' >&2
    exit 1
fi

if [[ ! -x $probe_bin ]]; then
    printf 'validate: probe binary %s is not executable\n' "$probe_bin" >&2
    exit 1
fi

# One probe = one fresh process invoking opnsense-probe -op version.
# This stresses the lifecycle that broke pre-MWAN-95: every probe
# is an independent gRPC client that dials, handshakes, calls one
# RPC, and tears down. The bridge keeps the upstream conn alive.
run_probe() {
    local target="unix://$bridge_sock"
    "$probe_bin" opnsense-probe -target "$target" -op version > /dev/null 2>&1
}

# Default probe binary is the main `mwan` binary; the OPNsense-probe
# logic is a subcommand. Override via MWAN_OPNSENSE_PROBE if the path
# differs.

run_one_round() {
    local round=$1
    local pass=0
    local fail=0
    local start
    start=$(date +%s)
    for ((i = 1; i <= PROBE_COUNT; i++)); do
        if run_probe; then
            pass=$((pass + 1))
        else
            fail=$((fail + 1))
        fi
    done
    local end
    end=$(date +%s)
    local elapsed=$((end - start))
    printf 'round %d: %d/%d pass, %d fail, %ds\n' \
        "$round" "$pass" "$PROBE_COUNT" "$fail" "$elapsed"
    [[ $pass -eq $PROBE_COUNT ]]
}

overall=0
for ((round = 1; round <= RUN_COUNT; round++)); do
    if ! run_one_round "$round"; then
        overall=1
    fi
done

if [[ $overall -eq 0 ]]; then
    printf 'PASS: %d consecutive %d/%d rounds against %s\n' \
        "$RUN_COUNT" "$PROBE_COUNT" "$PROBE_COUNT" "$bridge_sock"
else
    printf 'FAIL: at least one round did not hit %d/%d\n' \
        "$PROBE_COUNT" "$PROBE_COUNT" >&2
fi
exit "$overall"
