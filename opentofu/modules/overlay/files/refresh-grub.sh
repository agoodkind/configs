#!/usr/bin/env bash

set -euo pipefail

update-grub
proxmox-boot-tool refresh
