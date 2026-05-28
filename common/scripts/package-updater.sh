#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

APT_GET=/usr/bin/apt-get

"${APT_GET}" update
"${APT_GET}" -o Dpkg::Options::=--force-confold full-upgrade -y
"${APT_GET}" autoremove -y
"${APT_GET}" autoclean
