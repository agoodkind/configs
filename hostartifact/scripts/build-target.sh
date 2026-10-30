#!/usr/bin/env bash
# The script builds the host artifact for one workspace selector. The script
# plans the backendless render root to produce the projection, verifies each
# pinned application release with its checksum pin and gh attestation verify,
# checks out the attested source commit, and runs python -m hostartifact build
# with a PATH that contains only the uv environment and the OpenTofu
# directory. The CI publish job and check-target.sh run this script.
# Usage: build-target.sh <repository root> <workspace> <target directory>
# <source revision>.
# Set GH_TOKEN and GITHUB_REPOSITORY before running it.

set -euo pipefail

readonly RENDER_ROOT="opentofu/render/host-artifact"
readonly ANSIBLE_PROGRAMS=(ansible ansible-playbook ansible-vault ansible-inventory ansible-console)
readonly SHA1_PATTERN='^[0-9a-f]{40}$'

check_helper_path() {
  local directory
  local program
  for directory in "$@"; do
    for program in "${ANSIBLE_PROGRAMS[@]}"; do
      if [ -e "$directory/$program" ]; then
        echo "::error::the helper PATH contains an Ansible program: $directory/$program"
        exit 1
      fi
    done
  done
}

project_target() {
  local repository_root="$1"
  local workspace="$2"
  local target="$3"
  tofu -chdir="$repository_root/$RENDER_ROOT" init -backend=false -input=false > "$target/init.log"
  tofu -chdir="$repository_root/$RENDER_ROOT" plan -refresh=false -lock=false -input=false \
    -out="$target/projection.tfplan" -var "workspace=$workspace" > "$target/plan.log"
  tofu -chdir="$repository_root/$RENDER_ROOT" show -json "$target/projection.tfplan" > "$target/show.json"
  jq -e .planned_values.outputs.projection.value "$target/show.json" > "$target/projection.json"
}

verify_application() {
  local projection="$1"
  local repository="$2"
  local destination="$3"
  local roots_file="$4"
  local prefix
  local tag
  local asset
  local checksum
  local signer
  local digest
  prefix="$(jq -er --arg repository "$repository" '[.vars | to_entries[] | select((.key | endswith("_release_repo")) and .value == $repository) | .key | rtrimstr("_release_repo")] | if length == 1 then .[0] else error("the projection must pin exactly one release for \($repository)") end' "$projection")"
  tag="$(jq -er --arg key "${prefix}_release_tag" '.vars[$key]' "$projection")"
  asset="${prefix}_linux_amd64.tar.gz"
  checksum="$(jq -er --arg key "${prefix}_release_assets" --arg asset "$asset" '.vars[$key][] | select(.name == $asset) | .checksum' "$projection")"
  signer="$(jq -er '.vars.release_signer_repo' "$projection")"
  mkdir -p "$destination"
  gh release download "$tag" --repo "$repository" --pattern "$asset" --dir "$destination" < /dev/null
  echo "${checksum#sha256:}  $destination/$asset" | sha256sum --check --strict
  digest="$(gh attestation verify "$destination/$asset" --repo "$repository" --signer-repo "$signer" \
    --format json --jq '.[0].verificationResult.signature.certificate.sourceRepositoryDigest' < /dev/null)"
  if [[ ! "$digest" =~ $SHA1_PATTERN ]]; then
    echo "::error::the attestation did not return a source commit for $repository $tag"
    exit 1
  fi
  echo "Verified release source: $repository $tag $digest"
  git init --quiet "$destination/source"
  git -C "$destination/source" fetch --quiet --depth 1 "https://github.com/$repository" "$digest" < /dev/null
  git -C "$destination/source" checkout --quiet --detach "$digest"
  printf '%s=%s\n' "$repository" "$destination/source" >> "$roots_file"
}

verify_applications() {
  local repository_root="$1"
  local target="$2"
  local projection="$target/projection.json"
  local configs_repository
  local repository
  configs_repository="${GITHUB_REPOSITORY:?}"
  : > "$target/asset-roots.txt"
  jq -r '.files[].source | select(startswith("asset:")) | ltrimstr("asset:") | split(":")[0]' "$projection" \
    > "$target/asset-repositories.txt"
  sort -u "$target/asset-repositories.txt" > "$target/applications.txt"
  while read -r repository; do
    if [ "$repository" != "$configs_repository" ]; then
      verify_application "$projection" "$repository" "$target/applications/${repository//\//_}" "$target/asset-roots.txt"
    fi
  done < "$target/applications.txt"
  printf '%s=%s\n' "$configs_repository" "$repository_root" > "$target/configs-root.txt"
}

build_archive() {
  local repository_root="$1"
  local target="$2"
  local source_revision="$3"
  local helper_path="$4"
  local root
  local root_arguments=()
  while read -r root; do
    root_arguments+=(--asset-root "$root")
  done < "$target/asset-roots.txt"
  env -i PATH="$helper_path" python -I -m hostartifact build \
    --projection "$target/projection.json" \
    --repository-root "$repository_root" \
    --source-revision "$source_revision" \
    "${root_arguments[@]}" \
    --output-dir "$target/output" > "$target/result.json"
  jq . "$target/result.json"
}

main() {
  if [ "$#" -ne 4 ]; then
    echo "::error::usage: build-target.sh <repository root> <workspace> <target directory> <source revision>"
    exit 2
  fi
  local repository_root="$1"
  local workspace="$2"
  local target="$3"
  local source_revision="$4"
  local venv_bin="$repository_root/hostartifact/.venv/bin"
  local tofu_dir
  tofu_dir="$(dirname "$(command -v tofu)")"
  check_helper_path "$venv_bin" "$tofu_dir"
  mkdir -p "$target"
  project_target "$repository_root" "$workspace" "$target"
  verify_applications "$repository_root" "$target"
  build_archive "$repository_root" "$target" "$source_revision" "$venv_bin:$tofu_dir"
}

main "$@"
