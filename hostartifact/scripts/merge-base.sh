#!/usr/bin/env bash
# The script prints the merge base of the main ref and the head commit on
# stdout. When git merge-base fails, the script prints an error on stderr
# with the base name, main ref, head commit, and git exit code. The script
# exits with the git exit code. select-targets.sh uses the base name
# selection, and the CI check job uses pin.
# Usage: merge-base.sh <base name> <repository root> <main ref> <head commit>

set -euo pipefail

main() {
  if [ "$#" -ne 4 ]; then
    echo "::error::usage: merge-base.sh <base name> <repository root> <main ref> <head commit>" >&2
    exit 2
  fi
  local purpose="$1"
  local repository_root="$2"
  local main_ref="$3"
  local head="$4"
  local exit_code=0
  git -C "$repository_root" merge-base "$main_ref" "$head" || exit_code=$?
  if [ "$exit_code" -ne 0 ]; then
    echo "::error::cannot compute the $purpose base: git merge-base $main_ref $head failed with exit_code=$exit_code" >&2
    exit "$exit_code"
  fi
}

main "$@"
