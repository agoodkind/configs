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

## Setup

From the repository root, run:

```sh
uv sync --frozen --project hostartifact
```

## Development

`make -C hostartifact check` runs ruff format check, ruff check, mypy, basedpyright,
`scripts/ban_object.py`, and pytest. `make -C hostartifact fmt` runs ruff format and
`ruff check --fix`.

## build

The `build` command accepts these arguments:

| Argument | Value |
|---|---|
| `--projection <file>` | The JSON value of `planned_values.outputs.projection.value` from a plan of `opentofu/render/host-artifact`. |
| `--repository-root <dir>` | The agoodkind/configs checkout. |
| `--source-revision <commit>` | A commit with 40 lowercase hexadecimal characters. |
| `--asset-root <owner>/<repository>=<dir>` | The verified checkout of one application repository. This argument is repeatable and cannot use agoodkind/configs as the repository. |
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

`build_fixture.py` builds one fixed acceptance archive for the host artifact install
API. The command runs from the repository root:

```sh
uv run --project hostartifact python hostartifact/tests/build_fixture.py \
  --case <case> --expected-workspace <selector> \
  --source-revision <commit> --output-dir <directory>
```

The command accepts these arguments:

| Argument | Value |
|---|---|
| `--case <case>` | One case identifier from the cases table. |
| `--expected-workspace <selector>` | The fixed selector for the selected case. This value does not change the archive. |
| `--source-revision <commit>` | A commit with 40 lowercase hexadecimal characters. The manifest uses this value for `source_revision`. |
| `--output-dir <directory>` | The directory that receives the archive. The command creates the directory. |

The command rejects an unknown case, an invalid source revision, or a mismatched
expected workspace before creating the output directory. argparse rejects abbreviated
and unknown options. The command uses constants for every file path and byte in the
archive. It does not accept payload, destination, node, environment, repository,
workflow, or manifest inputs. It does not read target descriptors, OpenTofu,
application releases, inventory, or vault values.

Both targets use node `hypervisor`, environment `testbed`, and repository
`agoodkind/configs`. The following table lists their fixed selectors and paths:

| Field | Target A | Target B |
|---|---|---|
| Selector | `suburban/hostartifact-acceptance` | `suburban/hostartifact-acceptance-b` |
| Directory | `/srv/hostartifact-acceptance` | `/srv/hostartifact-acceptance-b` |
| First file | `/srv/hostartifact-acceptance/a.conf` | `/srv/hostartifact-acceptance-b/a.conf` |
| Second file | `/srv/hostartifact-acceptance/b.conf` | `/srv/hostartifact-acceptance-b/b.conf` |

Each directory uses mode `0755`. Each `a.conf` uses mode `0644`, and each `b.conf`
uses mode `0640`. Every directory and file uses owner `root` and group `root`. Each
manifest lists its target directory. The archive does not contain a directory member.
The manifest does not list the shared parent directory `/srv`.

Every case uses target A except `other-target`. Each defect case changes only the
stated item from the `valid-v2` manifest and members. The cases are:

| Case | Archive content |
|---|---|
| `valid-v1` | `a.conf` contains `a one\n`, and `b.conf` contains `b one\n`. The manifest sizes and digests match. |
| `valid-v2` | `a.conf` contains `a two\n`, and `b.conf` contains `b two\n`. The manifest sizes and digests match. |
| `other-target` | The archive uses the `valid-v1` contents for target B. |
| `digest` | The `a.conf` member contains `a bad\n`, which has the same length as `a two\n`. |
| `size` | The manifest records an `a.conf` size one byte larger than the member. |
| `symlink` | The `a.conf` member is a symbolic link to `/srv/hostartifact-acceptance/b.conf`. |
| `hardlink` | The `a.conf` member is a hard link to `files/srv/hostartifact-acceptance/b.conf`. |
| `traversal` | The archive adds the regular member `files/../srv/hostartifact-acceptance/traversal.conf` containing `x\n`. |
| `absolute` | The archive adds the regular member `/srv/hostartifact-acceptance/absolute.conf` containing `x\n`. |
| `extra` | The archive adds the regular member `files/srv/hostartifact-acceptance/extra.conf` containing `x\n`. The manifest does not list it. |
| `missing` | The archive omits the `a.conf` member. The manifest still lists both files. |
| `duplicate` | The archive repeats the `a.conf` member after `b.conf`. |
| `member-mode` | The `a.conf` member uses mode `04755`. The manifest retains mode `0644`. |
| `recovery-v1` | The archive contains 300 files from `/srv/hostartifact-acceptance/recovery-000.conf` through `/srv/hostartifact-acceptance/recovery-299.conf`. Each file uses mode `0644` and contains `one <number>\n`, with a three-digit number. |
| `recovery-v2` | The archive contains the same 300 files, each with `two <number>\n`. |

The command prints one `BuildResult` JSON line on stdout with `archive`, `asset`,
`sha256`, and `size`. The asset name is
`hostconfig_fixture-<case>-<SHA-256 of the selector>.tar.gz`.

The archive puts `manifest.json` first. It uses USTAR format, modification time zero,
numeric owner and group zero, and empty owner and group names. The gzip header uses
modification time zero and an empty file name. Two runs with the same case and revision
produce identical bytes. The `symlink` and `hardlink` cases use the standard library
`tarfile` module directly because the production writer accepts only regular members.

The command uses these exit codes:

| Code | Meaning |
|---|---|
| 0 | The command wrote the archive. |
| 1 | The command rejected the source revision or expected workspace and printed the reason on stderr with the prefix `build_fixture: `. |
| 2 | argparse rejected the arguments and printed usage on stderr. |

The `host artifact` job accepts the optional `fixture_case` workflow input, which
defaults to `none`. With `none`, the job runs `build-target.sh` for `workspace`.
Otherwise, the job builds the selected fixture and requires `workspace` to equal its
fixed selector. The job runs only for a manual dispatch on `refs/heads/main` and
requires `revision` to equal the workflow commit. It attests the archive, publishes the
archive and attestation bundle as the only two assets of a new release, and writes the
pin JSON to the job summary. A fixture release tag starts with
`hostconfig-fixture-<case>-`. The fixture build does not change a pin file or host
policy.

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
