#!/usr/bin/env bash
# The script plans the backendless render root to produce the projection.
# The script verifies each pinned application release with its checksum pin and gh attestation verify.
# The script checks out the attested source commit.
# The script checks each program archive against its pinned SHA-256.
# The script writes each program attestation bundle to disk with gh attestation download.
# The script verifies each program archive with its attestation bundle using gh attestation verify --bundle.
# The script runs python -m hostartifact build with one --program-result file per program and a PATH that contains only the uv environment and the OpenTofu directory.
# The CI publish job and check-target.sh run this script.
# Usage: build-target.sh <repository root> <workspace> <target directory> <source revision>.
# Set GH_TOKEN and GITHUB_REPOSITORY before running it.

set -euo pipefail

readonly RENDER_ROOT="opentofu/render/host-artifact"
readonly ANSIBLE_PROGRAMS=(ansible ansible-playbook ansible-vault ansible-inventory ansible-console)
readonly SHA1_PATTERN='^[0-9a-f]{40}$'
readonly SHA256_PATTERN='^[0-9a-f]{64}$'
readonly PROGRAM_CERT_IDENTITY='https://github.com/agoodkind/go-makefile/.github/workflows/_package.yml@refs/heads/main'
readonly PROGRAM_SOURCE_REF='refs/heads/main'
SUBJECT_CHECK="$(dirname "${BASH_SOURCE[0]}")/subject-check.jq"
readonly SUBJECT_CHECK

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

release_prefix() {
  local projection="$1"
  local repository="$2"
  jq -er --arg repository "$repository" '[.vars | to_entries[] | select((.key | endswith("_release_repo")) and .value == $repository) | .key | rtrimstr("_release_repo")] | if length == 1 then .[0] else error("the projection must pin exactly one release for \($repository)") end' "$projection"
}

release_tag() {
  local projection="$1"
  local prefix="$2"
  jq -er --arg key "${prefix}_release_tag" '.vars[$key]' "$projection"
}

release_checksum() {
  local projection="$1"
  local prefix="$2"
  local asset="$3"
  jq -er --arg key "${prefix}_release_assets" --arg asset "$asset" '.vars[$key][] | select(.name == $asset) | .checksum' "$projection"
}

download_release_asset() {
  local projection="$1"
  local prefix="$2"
  local repository="$3"
  local asset="$4"
  local destination="$5"
  local tag
  local checksum
  tag="$(release_tag "$projection" "$prefix")"
  checksum="$(release_checksum "$projection" "$prefix" "$asset")"
  mkdir -p "$destination"
  gh release download "$tag" --repo "$repository" --pattern "$asset" --dir "$destination" < /dev/null
  echo "${checksum#sha256:}  $destination/$asset" | sha256sum --check --strict
}

verify_application() {
  local projection="$1"
  local repository="$2"
  local destination="$3"
  local roots_file="$4"
  local prefix
  local tag
  local asset
  local signer
  local digest
  prefix="$(release_prefix "$projection" "$repository")"
  tag="$(release_tag "$projection" "$prefix")"
  asset="${prefix}_linux_amd64.tar.gz"
  signer="$(jq -er '.vars.release_signer_repo' "$projection")"
  download_release_asset "$projection" "$prefix" "$repository" "$asset" "$destination"
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

download_program_bundle() {
  local destination="$1"
  local repository="$2"
  local asset="$3"
  local sha256="$4"
  local bundle="$5"
  local downloaded="sha256:$sha256.jsonl"
  # gh attestation download does not accept an output directory flag and writes the bundle file in the current directory.
  pushd "$destination" > /dev/null
  gh attestation download "$asset" --repo "$repository" < /dev/null
  popd > /dev/null
  if [ ! -s "$destination/$downloaded" ]; then
    echo "::error::gh attestation download did not write $destination/$downloaded"
    exit 1
  fi
  mv "$destination/$downloaded" "$bundle"
}

verify_program() {
  local projection="$1"
  local name="$2"
  local destination="$3"
  local results_file="$4"
  local repository
  local asset
  local prefix
  local tag
  local sha256
  local bundle="$destination/bundle.jsonl"
  local verification="$destination/verification.json"
  local source_revision
  local signer_revision
  repository="$(jq -er --arg name "$name" '.programs[] | select(.name == $name) | .repository' "$projection")"
  asset="$(jq -er --arg name "$name" '.programs[] | select(.name == $name) | .asset' "$projection")"
  prefix="$(release_prefix "$projection" "$repository")"
  tag="$(release_tag "$projection" "$prefix")"
  sha256="$(release_checksum "$projection" "$prefix" "$asset")"
  sha256="${sha256#sha256:}"
  if [[ ! "$sha256" =~ $SHA256_PATTERN ]]; then
    echo "::error::The pinned checksum for program $name is not a SHA-256 digest"
    exit 1
  fi
  download_release_asset "$projection" "$prefix" "$repository" "$asset" "$destination"
  download_program_bundle "$destination" "$repository" "$asset" "$sha256" "$bundle"
  gh attestation verify "$destination/$asset" --bundle "$bundle" --repo "$repository" \
    --cert-identity "$PROGRAM_CERT_IDENTITY" --source-ref "$PROGRAM_SOURCE_REF" \
    --deny-self-hosted-runners --format json < /dev/null > "$verification"
  if ! jq -e --arg asset "$asset" --arg sha256 "$sha256" -f "$SUBJECT_CHECK" "$verification" > /dev/null; then
    echo "::error::A verified statement for program $name does not list $asset with SHA-256 $sha256"
    exit 1
  fi
  source_revision="$(jq -er '[.[].verificationResult.signature.certificate.sourceRepositoryDigest] | unique | if length == 1 then .[0] else error("The statements list different source commits") end' "$verification")"
  signer_revision="$(jq -er '[.[].verificationResult.signature.certificate.buildSignerDigest] | unique | if length == 1 then .[0] else error("The statements list different signer commits") end' "$verification")"
  if [[ ! "$source_revision" =~ $SHA1_PATTERN ]] || [[ ! "$signer_revision" =~ $SHA1_PATTERN ]]; then
    echo "::error::The attestation for program $name did not return both a source commit with 40 characters and a signer commit with 40 characters"
    exit 1
  fi
  jq -n --arg name "$name" --arg release_tag "$tag" --arg asset "$asset" --arg sha256 "$sha256" \
    --arg source_revision "$source_revision" --arg signer_revision "$signer_revision" \
    --arg archive "$destination/$asset" --arg bundle "$bundle" \
    '{name: $name, release_tag: $release_tag, asset: $asset, sha256: $sha256, source_revision: $source_revision, signer_revision: $signer_revision, archive: $archive, bundle: $bundle}' \
    > "$destination/result.json"
  echo "The script verified program $name from $repository at $tag with source commit $source_revision and signer commit $signer_revision."
  echo "$destination/result.json" >> "$results_file"
}

verify_programs() {
  local target="$1"
  local projection="$target/projection.json"
  local name
  : > "$target/program-results.txt"
  jq -r '.programs[].name' "$projection" > "$target/program-names.txt"
  while read -r name; do
    verify_program "$projection" "$name" "$target/programs/$name" "$target/program-results.txt"
  done < "$target/program-names.txt"
}

build_archive() {
  local repository_root="$1"
  local target="$2"
  local source_revision="$3"
  local helper_path="$4"
  local root
  local result
  local root_arguments=()
  local result_arguments=()
  while read -r root; do
    root_arguments+=(--asset-root "$root")
  done < "$target/asset-roots.txt"
  while read -r result; do
    result_arguments+=(--program-result "$result")
  done < "$target/program-results.txt"
  env -i PATH="$helper_path" python -I -m hostartifact build \
    --projection "$target/projection.json" \
    --repository-root "$repository_root" \
    --source-revision "$source_revision" \
    "${root_arguments[@]}" \
    "${result_arguments[@]}" \
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
  verify_programs "$target"
  build_archive "$repository_root" "$target" "$source_revision" "$venv_bin:$tofu_dir"
}

# Only direct shell execution runs main. Other scripts can source its functions.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
