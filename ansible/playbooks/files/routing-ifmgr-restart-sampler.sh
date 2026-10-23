#!/usr/bin/env bash
# Prints one line for each sample: the epoch time, the source, and the value.
# An upstream source reports present or absent for its BGP route to the home
# prefix. The gateway source reports the number of BGP default routes.
set -euo pipefail

DURATION_SECONDS="$1"
INTERVAL_SECONDS="$2"
HOME_PREFIX="$3"
GATEWAY_VMID="$4"
shift 4
UPSTREAM_VMIDS=("$@")

sample_upstream() {
  local vmid="$1"
  local routes
  routes="$(pct exec "${vmid}" -- ip -j -6 route show exact "${HOME_PREFIX}" proto bgp)"
  local state="present"
  if [[ "${routes//[[:space:]]/}" == "[]" ]]; then
    state="absent"
  fi
  printf '%s upstream-%s %s\n' "$(date +%s.%N)" "${vmid}" "${state}"
}

sample_gateway() {
  local output
  output="$(qm guest exec "${GATEWAY_VMID}" -- ip -6 route show default proto bgp)"
  # qm guest exec exits 0 when the guest command fails.
  if ! grep -Eq '"exitcode"[[:space:]]*:[[:space:]]*0[,}[:space:]]' <<<"${output}"; then
    printf 'ip -6 route show in guest %s failed: %s\n' "${GATEWAY_VMID}" "${output}" >&2
    exit 1
  fi
  local count=0
  local remaining="${output}"
  while [[ "${remaining}" == *default* ]]; do
    count=$((count + 1))
    remaining="${remaining#*default}"
  done
  printf '%s gateway %s\n' "$(date +%s.%N)" "${count}"
}

main() {
  local end_seconds
  end_seconds=$(($(date +%s) + DURATION_SECONDS))
  while [[ "$(date +%s)" -lt "${end_seconds}" ]]; do
    local vmid
    for vmid in "${UPSTREAM_VMIDS[@]}"; do
      sample_upstream "${vmid}"
    done
    sample_gateway
    sleep "${INTERVAL_SECONDS}"
  done
}

main
