#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${FAKE_CURL_ERROR-}" ]]; then
    printf '%s\n' "$FAKE_CURL_ERROR" >&2
    exit 22
fi

if [[ -e "$FAKE_CURL_COUNTER" ]]; then
    printf '%s\n' "curl called more than once" >&2
    exit 70
fi

printf '' >"$FAKE_CURL_COUNTER"
printf '%s' "${FAKE_CURL_BODY-}"
