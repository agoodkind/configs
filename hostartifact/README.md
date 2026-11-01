# hostartifact

The `hostartifact` package provides `python -m hostartifact`. The command builds the
nonsecret host artifact archive for one OpenTofu workspace selector and selects targets
that depend on a list of changed paths. Each target has a descriptor at
`opentofu/<selector>/host-artifact-target.json`.

## Prerequisites

- uv.
- Python 3.15. uv installs Python from `requires-python`.
- OpenTofu 1.13.2 for the tests and the render root.
- git for `affected --base-revision`.
- GitHub CLI, jq, and GNU sha256sum for test fixture preparation. GitHub CLI requires
  an authenticated account or `GH_TOKEN` with access to the pinned program release.

## Setup

From the repository root, run:

```sh
uv sync --frozen --project hostartifact
```

## Development

`make -C hostartifact check` runs ruff format check, ruff check, mypy, basedpyright,
`scripts/ban_object.py`, and pytest. `make -C hostartifact fmt` runs ruff format and
`ruff check --fix`. The test target downloads and verifies the pinned program release
in a temporary directory before running pytest. The target deletes that directory after
success, failure, or interruption.

## build

The `build` command accepts these arguments:

| Argument | Value |
|---|---|
| `--projection <file>` | The JSON value of `planned_values.outputs.projection.value` from a plan of `opentofu/render/host-artifact`. |
| `--repository-root <dir>` | The agoodkind/configs checkout. |
| `--source-revision <commit>` | A commit with 40 lowercase hexadecimal characters. |
| `--asset-root <owner>/<repository>=<dir>` | The verified checkout of one application repository. This argument is repeatable and cannot use agoodkind/configs as the repository. |
| `--program-result <file>` | Each occurrence supplies one verified program result from a JSON file. `build` requires exactly one result file for each program in the projection and rejects missing, unexpected, or duplicate results. |
| `--output-dir <dir>` | The directory that receives the archive. |

A template or asset source path must equal its normalized form. For example, use
`opnsense/config.toml.j2` instead of `./opnsense/config.toml.j2`.

Each descriptor requires `validation_file`, a repository-relative JSON file. The
selected target reads that file into the projection's required `validation` field.
The validation data requires `identity_variables` with nonempty `node` and `environment`
variable names, `variable_overrides` with caller values, and `forbidden_toml_keys` with
the keys to reject at every depth in rendered TOML. The builder merges the overrides
before checking variable names and adding the separately supplied `service_mapping`.
The builder rejects `vault_` variable names and a caller-defined `service_mapping`.
The archive manifest does not include validation data.

The command prints one JSON line on stdout with `archive`, `asset`, `sha256`, and `size`.
The asset file name is `hostconfig_<SHA-256 of the selector>.tar.gz`.

For a target with programs, `build` writes the archive, one `hostconfig_program-<name>.sigstore.jsonl` file per program, and `program-bundles.json` to the output directory. Each entry in `program-bundles.json` maps a program name to an object with `asset` for the bundle file name and `sha256` for the bundle SHA-256. `build` records the programs in a manifest with `schema_version` 2 and a `programs` list. For a target without programs, `build` writes only the archive and uses a manifest with `schema_version` 1.

Target builds and fixture builds remove obsolete program bundle files listed in a valid prior
`program-bundles.json` after validating the new inputs. They remove the index when the
new build omits programs. They reject unrecognized prior asset names and preserve
unrelated files and archives with different generated names.

## affected

The `affected` command accepts these arguments:

| Argument | Value |
|---|---|
| `--repository-root <dir>` | The agoodkind/configs checkout. |
| `--changed-files <file>` | A list of repository-relative paths, each followed by a NUL byte, as `git diff -z --name-only --no-renames <base> <head>` prints it. |
| `--workspace <selector>` | A candidate target selector. This argument is repeatable. |
| `--base-revision <commit>` | An optional commit with 40 lowercase hexadecimal characters. The command reads a template absent from the working tree from this commit to find its includes. |

The command prints a JSON array of selected selectors in sorted order on stdout. The
command prints each warning on stderr with the prefix `hostartifact: `.

A target depends on its descriptor, its declared validation file, its pin file
`opentofu/<selector>/host-artifact.auto.tfvars.json`, each group variable file in the
descriptor, each template in the descriptor and every template it includes, and each
`asset:agoodkind/configs:` file. The command collects these paths from the descriptor
before it checks whether the files exist. The command normalizes each declared template,
configs asset, validation file, and group file path before it compares the path with
the changed paths.
A deleted or renamed input selects the target. The build of that target reports the
missing file.

A change under `.github/workflows/ci.yml`, `hostartifact/`,
`opentofu/modules/host-artifact-targets/`, or `opentofu/render/host-artifact/` selects
every candidate target. When the command cannot read a descriptor, it prints a warning
and selects that target only when its descriptor or pin file changed.

## build_fixture.py

[build_fixture.py](tests/build_fixture.py) creates fixed archives for host API acceptance.
The command replaces an existing archive with the same generated name in the output
directory. This repository-root invocation selects a valid case:

```sh
uv run --frozen --project hostartifact python hostartifact/tests/build_fixture.py \
  --case valid-v1 \
  --expected-workspace suburban/hostartifact-acceptance \
  --source-revision '<commit>' \
  --output-dir /tmp/fixture
```

The command requires these arguments:

| Argument | Meaning |
|---|---|
| `--case` | One case from the cases table. |
| `--expected-workspace` | The fixed selector for the case. Another value causes rejection and does not change the archive. |
| `--source-revision` | A commit with 40 lowercase hexadecimal characters. The manifest records this value. |
| `--output-dir` | The directory that receives the archive. |
| `--program-result <file>` | The local program verification result. |

The generator applies these requirements to program inputs:

| Input | Requirement |
|---|---|
| Result file | Program cases require the `result.json` that `verify_program` writes after checking the release archive SHA-256 and attestation. Other cases reject this option. |
| Release identity | The result must match the pinned `opnsensectl` program name, release tag, asset, archive SHA-256, source revision, and signer revision. |
| Archive | The generator reads the local archive from the result's `archive` field and hashes it. It rejects a digest that differs from the pinned release before creating output. |
| Archive member | The archive must contain exactly one regular `opnsensectl` member. The generator rejects a missing member, duplicate members, links, and other member types. |
| Member digest | The generator computes the member SHA-256 and writes it as `member_sha256` in the manifest. |
| Bundle | The generator reads the local bundle from the result's `bundle` field and writes the bundle and `program-bundles.json` to the output directory. |

The command rejects an unknown case, an invalid source revision, or a mismatched expected
workspace before it creates the output directory. Case definitions fix the payload,
paths, and identity. Fixture construction does not run OpenTofu, read target descriptors,
inventory, or vault values, or use the network.

The targets use these selectors and directories:

| Target | Selector | Directory |
|---|---|---|
| A | `suburban/hostartifact-acceptance` | `/srv/hostartifact-acceptance` |
| B | `suburban/hostartifact-acceptance-b` | `/srv/hostartifact-acceptance-b` |

Each defect case starts from `valid-v2` and changes one item.

The command accepts these cases:

| Case | Purpose |
|---|---|
| `valid-v1` | The first valid version of the two target A files. |
| `valid-v2` | A second valid version that replaces both target A files. |
| `other-target` | A valid archive for target B, for a request that selects target A. |
| `digest` | A member has content that does not match its manifest digest. |
| `size` | A manifest size does not match its member. |
| `symlink` | A symbolic link replaces a file member. |
| `hardlink` | A hard link replaces a file member. |
| `traversal` | An extra member name contains a parent directory component. |
| `absolute` | An extra member has an absolute name. |
| `extra` | The manifest does not list an extra member. |
| `missing` | A manifest file does not have a member. |
| `duplicate` | A member name appears twice. |
| `member-mode` | A member has setuid mode bits that differ from the manifest mode. |
| `recovery-v1` | The first version of 300 target A files for interrupted installation and recovery. |
| `recovery-v2` | The second version of 300 target A files for interrupted installation and recovery. |

All six program cases start from `valid-v2` and add `opnsensectl` at `/srv/hostartifact-acceptance/opnsensectl`.

| Case | Purpose |
|---|---|
| `program-valid` | The host installs the program. |
| `program-wrong-source` | Only `source_revision` differs from the attested source revision, and the host rejects the program during verification. |
| `program-wrong-signer` | Only `signer_revision` differs from the attested signer revision, and the host rejects the program during verification. |
| `program-wrong-archive` | Only `sha256` differs from the SHA-256 of the downloaded release archive. The provider rejects the hash mismatch. |
| `program-wrong-member` | Only `member_sha256` differs from the SHA-256 of the archive member, and the host rejects the archive. |
| `program-collision` | Only `path` changes to `/srv/hostartifact-acceptance/a.conf`, the destination of another file in the archive. The provider rejects the repeated path during its manifest check in the plan. |

The command prints one `BuildResult` JSON line with `archive`, `asset`, `sha256`, and
`size`. The asset name uses
`hostconfig_fixture-<case>-<SHA-256 of the selector>.tar.gz`. Repeated builds with the
same case and revision produce identical bytes.

The command returns these exit codes:

| Code | Meaning |
|---|---|
| 0 | The command wrote the archive. |
| 1 | The command rejected the source revision or expected workspace and printed the reason on stderr with the prefix `build_fixture: `. |
| 2 | argparse rejected the arguments, including an unknown case, and printed usage on stderr. |

For fixture publication, the `host artifact` job in [ci.yml](../.github/workflows/ci.yml) runs only for a manual dispatch on `refs/heads/main` when `revision` equals the workflow commit. With the optional `fixture_case` input at its default value of `none`, the job runs [build-target.sh](scripts/build-target.sh) for `workspace`. A different value builds that fixture with `workspace` as the expected selector. For a program case, the `host artifact` job first runs `build-fixture-program.sh` to verify the opnsensectl release bundle. After attesting the fixture archive, the job publishes a fixture release with a tag starting with `hostconfig-fixture-<case>-`. A program fixture release includes the archive, its attestation bundle, and `hostconfig_program-opnsensectl.sigstore.jsonl`. Other fixture releases include only the archive and its attestation bundle. A target build release also includes each retained program bundle. Pin JSON in the job summary lists the program bundle in `program_bundles` for program fixtures and sets `program_bundles` to `{}` for other fixtures. Publication does not change a pin file or host policy.

A main-branch publication request has this form:

```sh
gh workflow run ci.yml --ref main \
  -f workspace=suburban/hostartifact-acceptance \
  -f revision='<main commit>' \
  -f fixture_case=valid-v1
```

## Exit codes

The `build` and `affected` commands use these exit codes:

| Code | Meaning |
|---|---|
| 0 | `build` wrote the archive or `affected` printed the selection, including a selection with warnings. |
| 1 | The command rejected its input and printed the reason on stderr with the prefix `hostartifact: `. |
| 2 | The arguments are not valid, and argparse printed usage on stderr. |

## CI scripts

`scripts/build-target.sh <repository root> <workspace> <target directory>
<source revision>` plans the render root, verifies each pinned application release,
checks out the attested source commits, and runs `build`.

`scripts/check-target.sh <repository root> <workspace> <target directory> <base commit>
<head commit>` runs `build-target.sh`, checks the archive member digests, compares copied
files with their checkouts, and compares the manifest with the pinned release when the
pin file changed.

`scripts/select-targets.sh` selects targets for a push. Run it with:

```sh
scripts/select-targets.sh <repository root> <ref> <main ref> <before commit> \
  <head commit> <work directory> <workspace>...
```

For a ref other than `refs/heads/main`, the script uses the merge base of the main ref
and the head as the base commit. For `refs/heads/main`, the script uses the before commit
of the push when that commit exists and is not all zeros. Otherwise, the script uses
the parent of the head. The script writes a NUL-terminated list of changed paths to
`<work directory>/changed.bin` using `git diff -z --name-only --no-renames` from the
base to the head. It runs `python -m hostartifact affected` with `--base-revision` set
to the base and prints the JSON array of selected workspaces on stdout.

`scripts/merge-base.sh <base name> <repository root> <main ref> <head commit>` prints
`git merge-base <main ref> <head>` on stdout. When `git merge-base` fails, the script
prints an error on stderr with the base name, main ref, head commit, and git exit code.
The script exits with the git exit code. `select-targets.sh` uses the base name
`selection`, and the CI check job uses `pin`.

The CI check job selects targets with `select-targets.sh` using `origin/main` as the
main ref. The pin check uses the before commit of the push when that commit exists.
Otherwise, the pin check uses the merge base with `origin/main`.

The build and check scripts need `GH_TOKEN` and `GITHUB_REPOSITORY`.
