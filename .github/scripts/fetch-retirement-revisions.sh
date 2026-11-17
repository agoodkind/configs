#!/usr/bin/env bash
set -euo pipefail

: "${BASE_SHA:?The pull request base SHA is required}"
: "${HEAD_SHA:?The pull request head SHA is required}"
: "${PR_NUMBER:?The pull request number is required}"

git fetch --no-tags origin "$BASE_SHA" "refs/pull/$PR_NUMBER/head"
git cat-file -e "$BASE_SHA^{commit}"
git cat-file -e "$HEAD_SHA^{commit}"
