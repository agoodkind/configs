#!/usr/bin/env bash
# Stage FRR for the sit6 guest's first install. Runs on vault, which has IPv6
# internet the guest lacks until FRR announces its prefix. Resolves FRR against
# the guest's own apt sources and dpkg status in a private apt root, so the
# versions match what the guest's apt expects, then pushes the fresh package
# lists and the package files into the guest for an install with no download.
#
# Usage: stage-frr-packages.sh VMID WORK_DIR
set -euo pipefail

readonly VMID="$1"
readonly WORK_DIR="$2"
readonly GUEST_SOURCES=/etc/apt/sources.list.d/debian.sources

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT INT TERM

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR/sources.list.d" "$WORK_DIR/state/lists/partial" \
    "$WORK_DIR/cache/archives/partial"
pct pull "$VMID" "$GUEST_SOURCES" "$WORK_DIR/sources.list.d/debian.sources"
pct pull "$VMID" /var/lib/dpkg/status "$WORK_DIR/status"

apt_options=(
    -o "Dir::Etc::SourceList=/dev/null"
    -o "Dir::Etc::SourceParts=$WORK_DIR/sources.list.d"
    -o "Dir::Etc::Preferences=/dev/null"
    -o "Dir::Etc::PreferencesParts=/dev/null"
    -o "Dir::State=$WORK_DIR/state"
    -o "Dir::State::status=$WORK_DIR/status"
    -o "Dir::Cache=$WORK_DIR/cache"
    -o "APT::Architecture=amd64"
)

apt-get "${apt_options[@]}" -qq update
apt-get "${apt_options[@]}" -y -qq --download-only install frr

for list_file in "$WORK_DIR"/state/lists/*; do
    if [[ -f "$list_file" && "$(basename "$list_file")" != "lock" ]]; then
        pct push "$VMID" "$list_file" "/var/lib/apt/lists/$(basename "$list_file")"
    fi
done
for package_file in "$WORK_DIR"/cache/archives/*.deb; do
    pct push "$VMID" "$package_file" "/var/cache/apt/archives/$(basename "$package_file")"
    echo "staged $(basename "$package_file")"
done
