#!/bin/sh
#
# Stands in for the configsctl script at the repository root. It records the
# arguments a rake task handed it, one per line, beside itself, and exits 0.

set -eu

printf '%s\n' "$@" >"$(dirname "$0")/configsctl-argv"
