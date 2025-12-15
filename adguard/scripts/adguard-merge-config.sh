#!/usr/bin/env bash
set -euo pipefail

CFG_PATH="${ADGUARD_CONFIG_PATH:-}"
if [[ -z "${CFG_PATH}" ]]; then
  echo "ERROR: ADGUARD_CONFIG_PATH is required" >&2
  exit 2
fi

if ! command -v yq >/dev/null 2>&1; then
  echo "ERROR: yq is required" >&2
  exit 2
fi

# Inputs are passed as JSON strings.
TLS_JSON="${ADGUARD_TLS_JSON:-}"
REWRITES_JSON="${ADGUARD_REWRITES_JSON:-[]}"
DOMAIN_LC="${ADGUARD_MANAGED_DOMAIN_LC:-}"

before="$(sha256sum "${CFG_PATH}" | cut -d' ' -f1)"

# Merge TLS keys into .tls (without touching other keys).
if [[ -n "${TLS_JSON}" ]]; then
  yq -yi --argjson tls "${TLS_JSON}" '
    .tls |= (. // {})
    | .tls.enabled = ($tls.enabled | not | not)
    | .tls.server_name = ($tls.server_name // .tls.server_name // "")
    | .tls.force_https = ($tls.force_https | not | not)
    | .tls.port_https = ($tls.port_https // .tls.port_https)
    | .tls.port_dns_over_tls = ($tls.port_dns_over_tls // .tls.port_dns_over_tls)
    | .tls.port_dns_over_quic = ($tls.port_dns_over_quic // .tls.port_dns_over_quic)
    | .tls.certificate_path = ($tls.certificate_path // .tls.certificate_path // "")
    | .tls.private_key_path = ($tls.private_key_path // .tls.private_key_path // "")
  ' "${CFG_PATH}"
fi

# Merge filtering.rewrites: remove loopback self-rewrites and ensure managed ones exist+enabled.
# DOMAIN_LC is optional; if provided, we remove 127.0.0.1/::1 rewrites for that domain.
yq -yi --arg dom_lc "${DOMAIN_LC}" --argjson managed "${REWRITES_JSON}" '
  .filtering |= (. // {})
  | .filtering.rewrites |= (. // [])
  | .filtering.rewrites = (
      .filtering.rewrites
      | (if ($dom_lc | length) > 0 then
          map(select((.domain|ascii_downcase) != ($dom_lc|ascii_downcase) or ((.answer|tostring) != "127.0.0.1" and (.answer|tostring) != "::1")))
        else
          .
        end)
      | ( . as $r
          | ($managed | map((.domain|ascii_downcase) + "|" + (.answer|tostring))) as $keys
          | ($r | map(
              (((.domain|ascii_downcase) + "|" + (.answer|tostring)) as $k
                | if ($keys | index($k)) != null then .enabled = true else . end)
            ))
        )
      | . + $managed
      | unique_by((.domain|ascii_downcase) + "|" + (.answer|tostring))
    )
' "${CFG_PATH}"

after="$(sha256sum "${CFG_PATH}" | cut -d' ' -f1)"

if [[ "${before}" != "${after}" ]]; then
  echo "changed=1"
else
  echo "changed=0"
fi
