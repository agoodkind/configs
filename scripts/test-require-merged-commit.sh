#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ROOT="$(mktemp -d)"
readonly SCRIPT_DIR TEST_ROOT

cleanup() {
    rm -rf "${TEST_ROOT}"
}
trap cleanup EXIT

git init --quiet --bare --initial-branch=main "${TEST_ROOT}/origin.git"
git init --quiet --initial-branch=main "${TEST_ROOT}/checkout"
git -C "${TEST_ROOT}/checkout" remote add origin "${TEST_ROOT}/origin.git"
mkdir "${TEST_ROOT}/hooks"
git -C "${TEST_ROOT}/checkout" config core.hooksPath "${TEST_ROOT}/hooks"
git -C "${TEST_ROOT}/checkout" config user.name "$(git config user.name)"
git -C "${TEST_ROOT}/checkout" config user.email "$(git config user.email)"
printf 'merged\n' > "${TEST_ROOT}/checkout/state"
git -C "${TEST_ROOT}/checkout" add state
git -C "${TEST_ROOT}/checkout" commit --quiet -m 'Add merged state'
git -C "${TEST_ROOT}/checkout" push --quiet -u origin main

(
    cd "${TEST_ROOT}/checkout"
    "${SCRIPT_DIR}/require-merged-commit.sh"
)

git -C "${TEST_ROOT}/checkout" switch --quiet -c unmerged
printf 'unmerged\n' > "${TEST_ROOT}/checkout/state"
git -C "${TEST_ROOT}/checkout" commit --quiet -am 'Add unmerged state'
if (
    cd "${TEST_ROOT}/checkout"
    "${SCRIPT_DIR}/require-merged-commit.sh"
); then
    printf 'An unmerged commit passed the deploy gate.\n' >&2
    exit 1
fi

git -C "${TEST_ROOT}/checkout" switch --quiet main
printf 'uncommitted\n' > "${TEST_ROOT}/checkout/state"
if (
    cd "${TEST_ROOT}/checkout"
    "${SCRIPT_DIR}/require-merged-commit.sh"
); then
    printf 'Uncommitted changes passed the deploy gate.\n' >&2
    exit 1
fi
