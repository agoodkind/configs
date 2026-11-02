#!/usr/bin/env bash
set -euo pipefail

apt-get install --yes tayga nftables
# Debian starts the packaged translator before the managed configuration exists.
systemctl disable --now tayga.service
