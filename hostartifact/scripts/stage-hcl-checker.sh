#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  echo "usage: stage-hcl-checker.sh <repository root> <tool directory>" >&2
  exit 2
fi

readonly REPOSITORY_ROOT="$1"
readonly TOOL_DIRECTORY="$2"

source "$REPOSITORY_ROOT/configsctl"
mkdir -p "$TOOL_DIRECTORY"
GOBIN="$TOOL_DIRECTORY" go install "${CONFIGSCTL_PACKAGE}@${CONFIGSCTL_VERSION}"

if [[ ! -x "$TOOL_DIRECTORY/configsctl" ]]; then
  echo "the pinned configsctl build did not create $TOOL_DIRECTORY/configsctl" >&2
  exit 1
fi
