#!/usr/bin/env bash
# Email sender using SMTP2GO HTTP API
# Reliable routing: can bind to specific interface or use policy routing
set -euo pipefail

HOSTNAME=$(hostname)
TO="" SUBJECT="" MSG="" FROM="" NAME="" SMTP2GO_API_KEY=""
BIND_IFACE=""

die() { echo "Error: $1" >&2; exit 1; }
usage() {
    echo "Usage: $0 -t TO -s SUBJ -m MSG [-f FROM] [-n NAME] [-k API_KEY] [-i IFACE]"
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t) TO="$2"; shift 2 ;;
        -s) SUBJECT="$2"; shift 2 ;;
        -m) MSG="$2"; shift 2 ;;
        -f) FROM="$2"; shift 2 ;;
        -n) NAME="$2"; shift 2 ;;
        -k) SMTP2GO_API_KEY="$2"; shift 2 ;;
        -i) BIND_IFACE="$2"; shift 2 ;;
        *)  usage ;;
    esac
done

[[ -z "$TO" || -z "$MSG" ]] && die "Missing required arguments"
[[ -z "$FROM" ]] && FROM="${HOSTNAME}-mailer@goodkind.io"
[[ -z "$NAME" ]] && NAME="$HOSTNAME"

# Try to get API key from environment if not provided
if [[ -z "$SMTP2GO_API_KEY" ]]; then
    if [[ -r /etc/mwan-watchdog/watchdog.env ]]; then
        # shellcheck disable=SC1091
        . /etc/mwan-watchdog/watchdog.env
        SMTP2GO_API_KEY="${SMTP2GO_API_KEY:-}"
    fi
fi

[[ -z "$SMTP2GO_API_KEY" ]] && die "SMTP2GO_API_KEY not provided"

# Build curl command with optional interface binding
CURL_CMD="curl -sS --max-time 30"
if [[ -n "$BIND_IFACE" ]]; then
    CURL_CMD="$CURL_CMD --interface $BIND_IFACE"
fi

# Send email via SMTP2GO HTTP API
response=$(${CURL_CMD} \
    -X POST https://api.smtp2go.com/v3/email/send \
    -H "Content-Type: application/json" \
    -H "X-Smtp2go-Api-Key: $SMTP2GO_API_KEY" \
    -d @- <<EOF
{
    "sender": "$FROM",
    "to": ["$TO"],
    "subject": "$SUBJECT",
    "text_body": "$MSG\n\nHost: $HOSTNAME\nTime: $(date +'%Y-%m-%d %H:%M %Z')",
    "custom_headers": [
        {"header": "X-Sender-Name", "value": "$NAME"}
    ]
}
EOF
) || die "HTTP request failed"

# Check response
if echo "$response" | jq -e '.data.succeeded > 0' >/dev/null 2>&1; then
    echo "Email sent to $TO via SMTP2GO HTTP API"
    exit 0
else
    error_msg=$(echo "$response" | jq -r '.data.error // "Unknown error"' 2>/dev/null || echo "Unknown error")
    die "SMTP2GO API error: $error_msg"
fi
