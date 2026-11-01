#!/usr/bin/env bash

set -euo pipefail

# shellcheck source=build-target.sh
source "$(dirname "${BASH_SOURCE[0]}")/build-target.sh"

readonly FIXTURE_PROGRAM_NAME="opnsensectl"
readonly FIXTURE_PROGRAM_REPOSITORY="agoodkind/opnsensectl"
readonly FIXTURE_PROGRAM_RELEASE_TAG="202609190337-7-7db20fb"
readonly FIXTURE_PROGRAM_ASSET="opnsensectl_linux_amd64.tar.gz"
readonly FIXTURE_PROGRAM_SHA256="29e65cb0cc7b7924478416ab98d7c22e8d3e1679e7cbcfdbff42323fde874399"

write_projection() {
  local projection="$1"
  jq -n \
    --arg name "$FIXTURE_PROGRAM_NAME" \
    --arg repository "$FIXTURE_PROGRAM_REPOSITORY" \
    --arg tag "$FIXTURE_PROGRAM_RELEASE_TAG" \
    --arg asset "$FIXTURE_PROGRAM_ASSET" \
    --arg checksum "sha256:$FIXTURE_PROGRAM_SHA256" \
    '{
      programs: [{name: $name, repository: $repository, asset: $asset}],
      vars: {
        opnsensectl_release_repo: $repository,
        opnsensectl_release_tag: $tag,
        opnsensectl_release_assets: [{name: $asset, checksum: $checksum}]
      }
    }' > "$projection"
}

main() {
  if [ "$#" -ne 1 ]; then
    echo "::error::usage: build-fixture-program.sh <work directory>"
    exit 2
  fi
  local work="$1"
  mkdir -p "$work"
  write_projection "$work/projection.json"
  verify_program "$work/projection.json" "$FIXTURE_PROGRAM_NAME" "$work" "$work/results.txt"
}

main "$@"
