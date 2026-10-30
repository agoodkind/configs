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

A target depends on its descriptor, its pin file
`opentofu/<selector>/host-artifact.auto.tfvars.json`, each group variable file in the
descriptor, each template in the descriptor and every template it includes, and each
`asset:agoodkind/configs:` file. The command collects these paths from the descriptor
before it checks whether the files exist. A deleted or renamed input selects the target.
The build of that target reports the missing file.

A change under `.github/workflows/ci.yml`, `hostartifact/`,
`opentofu/modules/host-artifact-targets/`, or `opentofu/render/host-artifact/` selects
every candidate target. When the command cannot read a descriptor, it prints a warning
and selects that target only when its descriptor or pin file changed.

## Exit codes

Both commands use these exit codes:

| Code | Meaning |
|---|---|
| 0 | `build` wrote the archive or `affected` printed the selection, including a selection with warnings. |
| 1 | The command rejected its input and printed the reason on stderr with the prefix `hostartifact: `. |
| 2 | The arguments are not valid, and argparse printed usage on stderr. |

## CI scripts

`scripts/build-target.sh <repository root> <workspace> <target directory> <source revision>`
plans the render root, verifies each pinned application release, checks out the attested
source commits, and runs `build`.

`scripts/check-target.sh <repository root> <workspace> <target directory> <base commit>
<head commit>` runs `build-target.sh`, checks the archive member digests, compares copied
files with their checkouts, and compares the manifest with the pinned release when the
pin file changed.

Both scripts need `GH_TOKEN` and `GITHUB_REPOSITORY`.
