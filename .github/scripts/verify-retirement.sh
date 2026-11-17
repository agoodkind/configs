#!/usr/bin/env bash
set -euo pipefail

: "${BASE_SHA:?The pull request base SHA is required}"
: "${HEAD_SHA:?The pull request head SHA is required}"
exec "$HOME/go/bin/configsctl" ci retirement --base "$BASE_SHA" --head "$HEAD_SHA"
