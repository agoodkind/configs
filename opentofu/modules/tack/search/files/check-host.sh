#!/usr/bin/env bash
set -euo pipefail
readonly REQUIRED_MAX_MAP_COUNT=262144
read -r current_max_map_count < /proc/sys/vm/max_map_count
if (( current_max_map_count < REQUIRED_MAX_MAP_COUNT )); then
    printf 'OpenSearch requires host vm.max_map_count >= %s; the current value is %s.\n' "$REQUIRED_MAX_MAP_COUNT" "$current_max_map_count" >&2
    exit 1
fi
