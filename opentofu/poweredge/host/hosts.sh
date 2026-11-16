#!/usr/bin/env bash
set -euo pipefail

hosts_file=/etc/hosts
new_line="10.230.0.254 poweredge.management.collingwood.goodkind.io poweredge"
temp_file="$(mktemp)"

awk -v new_line="${new_line}" '
  $0 ~ /^10\.230\.0\.254[[:space:]]/ { print new_line; next }
  { print }
' "${hosts_file}" >"${temp_file}"

cat "${temp_file}" >"${hosts_file}"
rm -f "${temp_file}"

if ! grep -qxF "${new_line}" "${hosts_file}"; then
  echo "${hosts_file} does not contain the line: ${new_line}" >&2
  exit 1
fi
