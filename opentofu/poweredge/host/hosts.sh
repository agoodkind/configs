#!/usr/bin/env bash
set -euo pipefail

HOSTS_FILE=/etc/hosts
NEW_LINE="10.230.0.254 poweredge.management.collingwood.goodkind.io poweredge"
TEMP_FILE="$(mktemp)"

awk -v new_line="${NEW_LINE}" '
  $0 ~ /^10\.230\.0\.254[[:space:]]/ { print new_line; next }
  { print }
' "${HOSTS_FILE}" >"${TEMP_FILE}"

cat "${TEMP_FILE}" >"${HOSTS_FILE}"
rm -f "${TEMP_FILE}"

if ! grep -qxF "${NEW_LINE}" "${HOSTS_FILE}"; then
  echo "${HOSTS_FILE} does not contain the line: ${NEW_LINE}" >&2
  exit 1
fi
