#!/usr/bin/env bash

set -euo pipefail

if ! CHECKOUT_STATUS="$(git status --porcelain --untracked-files=normal)"; then
    printf 'Deploy refused: could not inspect checkout changes.\n' >&2
    exit 1
fi

if [[ -n "${CHECKOUT_STATUS}" ]]; then
    printf 'Deploy refused: commit or remove checkout changes before deploying.\n' >&2
    exit 1
fi

if ! git fetch --quiet origin refs/heads/main:refs/remotes/origin/main; then
    printf 'Deploy refused: could not verify the current remote main branch.\n' >&2
    exit 1
fi

if ! git merge-base --is-ancestor HEAD refs/remotes/origin/main; then
    printf 'Deploy refused: HEAD is not merged into origin/main.\n' >&2
    exit 1
fi
