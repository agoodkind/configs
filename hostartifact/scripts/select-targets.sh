#!/usr/bin/env bash
# The script selects host artifact targets for one push. For a ref other
# than refs/heads/main, it compares the head with the merge base of the
# main ref and the head. For refs/heads/main, it compares the head with
# the before commit, or with the parent of the head when the before
# commit is all zeros or does not exist. The script writes the changed
# paths to <work directory>/changed.bin and runs
# python -m hostartifact affected with that base. The script prints the
# JSON array of selected workspaces on stdout.
# Usage: select-targets.sh <repository root> <ref> <main ref>
# <before commit> <head commit> <work directory> <workspace>...

set -euo pipefail

readonly MAIN_BRANCH_REF="refs/heads/main"
readonly ZERO_COMMIT="0000000000000000000000000000000000000000"

selection_base() {
  local repository_root="$1"
  local ref="$2"
  local main_ref="$3"
  local before="$4"
  local head="$5"
  if [ "$ref" != "$MAIN_BRANCH_REF" ]; then
    "$(dirname "${BASH_SOURCE[0]}")/merge-base.sh" selection "$repository_root" "$main_ref" "$head"
    return
  fi
  if [ "$before" != "$ZERO_COMMIT" ] && git -C "$repository_root" cat-file -e "$before^{commit}"; then
    echo "$before"
    return
  fi
  git -C "$repository_root" rev-parse "$head^"
}

main() {
  if [ "$#" -lt 6 ]; then
    echo "::error::usage: select-targets.sh <repository root> <ref> <main ref> <before commit> <head commit> <work directory> <workspace>..."
    exit 2
  fi
  local repository_root="$1"
  local ref="$2"
  local main_ref="$3"
  local before="$4"
  local head="$5"
  local work="$6"
  shift 6
  local base
  local workspace
  local workspace_arguments=()
  base="$(selection_base "$repository_root" "$ref" "$main_ref" "$before" "$head")"
  echo "The target selection compares changes with commit $base" >&2
  mkdir -p "$work"
  git -C "$repository_root" diff -z --name-only --no-renames "$base" "$head" > "$work/changed.bin"
  for workspace in "$@"; do
    workspace_arguments+=(--workspace "$workspace")
  done
  python -I -m hostartifact affected \
    --repository-root "$repository_root" \
    --changed-files "$work/changed.bin" \
    --base-revision "$base" \
    "${workspace_arguments[@]}"
}

main "$@"
