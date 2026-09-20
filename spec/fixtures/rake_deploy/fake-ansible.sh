#!/bin/sh
#
# Stands in for every ansible entry point. A rake task that reaches one has
# bypassed configsctl, so this records the call and fails the run.

set -eu

printf '%s %s\n' "$(basename "$0")" "$*" >>"${RAKE_DEPLOY_ANSIBLE_LOG}"
exit 1
