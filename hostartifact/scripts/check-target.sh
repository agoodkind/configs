#!/usr/bin/env bash
# The script builds one target with build-target.sh.
# The script checks every archive member digest and compares each copied file with its checkout.
# For each program, the script checks the archive SHA-256, the member SHA-256, and the bundle file against the manifest.
# When the push changes the pin file of the target, the script also compares the manifest with the pinned release.
# Usage: check-target.sh <repository root> <workspace> <target directory> <base commit> <head commit>.
# Set GH_TOKEN and GITHUB_REPOSITORY before running it.

set -euo pipefail

readonly PIN_NAME="host-artifact.auto.tfvars.json"

compare_archive() {
  local target="$1"
  local archive
  local destination
  local repository
  local path
  local root
  local -A roots
  archive="$(jq -er .archive "$target/result.json")"
  mkdir -p "$target/extracted"
  tar -tvzf "$archive"
  tar -xzf "$archive" -C "$target/extracted"
  jq . "$target/extracted/manifest.json"
  jq -r '.files[] | "\(.sha256)  \(.member)"' "$target/extracted/manifest.json" > "$target/members.sha256"
  (cd "$target/extracted" && sha256sum --check --strict "$target/members.sha256")
  while IFS='=' read -r repository root; do
    roots["$repository"]="$root"
  done < <(cat "$target/asset-roots.txt" "$target/configs-root.txt")
  jq -r '.files[] | select(.source | startswith("asset:")) | (.source | ltrimstr("asset:") | split(":")) as $source | [.destination, $source[0], ($source[1:] | join(":"))] | @tsv' \
    "$target/projection.json" > "$target/assets.tsv"
  while IFS=$'\t' read -r destination repository path; do
    cmp "$target/extracted/files$destination" "${roots[$repository]}/$path"
    echo "Archive member matches the checkout file: $destination $repository $path"
  done < "$target/assets.tsv"
}

compare_programs() {
  local target="$1"
  local bundles="$target/output/program-bundles.json"
  local name
  local asset
  local sha256
  local member
  local member_sha256
  local bundle_asset
  local bundle_sha256
  jq -r '(.programs // [])[] | [.name, .asset, .sha256, .member, .member_sha256] | @tsv' \
    "$target/extracted/manifest.json" > "$target/programs.tsv"
  if [ ! -s "$target/programs.tsv" ]; then
    if [ -e "$bundles" ]; then
      echo "::error::The build wrote $bundles for a target that does not list programs in its manifest"
      exit 1
    fi
    echo "The manifest does not list any programs."
    return
  fi
  while IFS=$'\t' read -r name asset sha256 member member_sha256; do
    echo "$sha256  $target/programs/$name/$asset" | sha256sum --check --strict
    tar -xzf "$target/programs/$name/$asset" -O "$member" > "$target/programs/$name/member.bin"
    echo "$member_sha256  $target/programs/$name/member.bin" | sha256sum --check --strict
    bundle_asset="$(jq -er --arg name "$name" '.[$name].asset' "$bundles")"
    bundle_sha256="$(jq -er --arg name "$name" '.[$name].sha256' "$bundles")"
    echo "$bundle_sha256  $target/output/$bundle_asset" | sha256sum --check --strict
    cmp "$target/programs/$name/bundle.jsonl" "$target/output/$bundle_asset"
    echo "The archive SHA-256, member SHA-256, and bundle SHA-256 for program $name match the values in the manifest."
  done < "$target/programs.tsv"
  jq -e --slurpfile manifest "$target/extracted/manifest.json" '(keys | length) == ($manifest[0].programs | length)' \
    "$bundles" > /dev/null
}

check_pin() {
  local repository_root="$1"
  local workspace="$2"
  local target="$3"
  local base="$4"
  local head="$5"
  local pin="opentofu/$workspace/$PIN_NAME"
  local pinned_tag
  local pinned_asset
  local pinned_sha256
  git -C "$repository_root" diff --name-only "$base" "$head" -- "$pin" > "$target/pin-change.txt"
  if [ ! -s "$target/pin-change.txt" ] || [ ! -f "$repository_root/$pin" ]; then
    echo "This push does not change the pin file: $pin"
    return
  fi
  pinned_tag="$(jq -er .host_artifact.release_tag "$repository_root/$pin")"
  pinned_asset="$(jq -er .host_artifact.asset "$repository_root/$pin")"
  pinned_sha256="$(jq -er .host_artifact.sha256 "$repository_root/$pin")"
  gh release download "$pinned_tag" --repo "${GITHUB_REPOSITORY:?}" --pattern "$pinned_asset" \
    --dir "$target/pinned" < /dev/null
  echo "$pinned_sha256  $target/pinned/$pinned_asset" | sha256sum --check --strict
  tar -xzf "$target/pinned/$pinned_asset" -O manifest.json > "$target/pinned-manifest.json"
  jq -S 'del(.source_revision)' "$target/pinned-manifest.json" > "$target/pinned-compare.json"
  jq -S 'del(.source_revision)' "$target/extracted/manifest.json" > "$target/rendered-compare.json"
  if ! diff -u "$target/pinned-compare.json" "$target/rendered-compare.json"; then
    echo "::error::the pinned manifest differs from the new build: $pin"
    exit 1
  fi
  echo "The pinned manifest matches the new build: $pin"
}

main() {
  if [ "$#" -ne 5 ]; then
    echo "::error::usage: check-target.sh <repository root> <workspace> <target directory> <base commit> <head commit>"
    exit 2
  fi
  local repository_root="$1"
  local workspace="$2"
  local target="$3"
  local base="$4"
  local head="$5"
  "$(dirname "${BASH_SOURCE[0]}")/build-target.sh" "$repository_root" "$workspace" "$target" "$head"
  compare_archive "$target"
  compare_programs "$target"
  check_pin "$repository_root" "$workspace" "$target" "$base" "$head"
}

main "$@"
