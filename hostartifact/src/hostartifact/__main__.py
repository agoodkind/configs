from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from hostartifact.pack import build_artifact
from hostartifact.projection import BuildError, load_projection
from hostartifact.targets import affected_targets

BUILD_COMMAND = "build"
AFFECTED_COMMAND = "affected"
ASSET_ROOT_SEPARATOR = "="
CHANGED_PATH_SEPARATOR = "\0"
FAILURE_EXIT_CODE = 1
PROGRAM_NAME = "hostartifact"


class Arguments(argparse.Namespace):
    command: str
    projection: Path
    repository_root: Path
    source_revision: str
    asset_root: list[str]
    output_dir: Path
    changed_files: Path
    workspace: list[str]
    base_revision: str | None


def parse_arguments(arguments: list[str]) -> Arguments:
    parser = argparse.ArgumentParser(prog=PROGRAM_NAME)
    commands = parser.add_subparsers(dest="command", required=True)
    build = commands.add_parser(BUILD_COMMAND)
    build.add_argument("--projection", type=Path, required=True)
    build.add_argument("--repository-root", type=Path, required=True)
    build.add_argument("--source-revision", required=True)
    build.add_argument("--asset-root", action="append", default=[])
    build.add_argument("--output-dir", type=Path, required=True)
    affected = commands.add_parser(AFFECTED_COMMAND)
    affected.add_argument("--repository-root", type=Path, required=True)
    affected.add_argument("--changed-files", type=Path, required=True)
    affected.add_argument("--workspace", action="append", default=[])
    affected.add_argument("--base-revision")
    return parser.parse_args(arguments, namespace=Arguments())


def parse_asset_roots(values: list[str]) -> dict[str, Path]:
    roots: dict[str, Path] = {}
    for value in values:
        repository, separator, directory = value.partition(ASSET_ROOT_SEPARATOR)
        if not separator or not repository or not directory:
            raise BuildError(
                f"--asset-root {value!r} must have the form "
                "<owner>/<repository>=<directory>"
            )
        if repository in roots:
            raise BuildError(f"--asset-root names {repository} more than once")
        roots[repository] = Path(directory)
    return roots


def read_changed_paths(path: Path) -> set[str]:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as error:
        raise BuildError(f"cannot read changed path list {path}: {error}") from error
    return {item for item in text.split(CHANGED_PATH_SEPARATOR) if item}


def run_build(parsed: Arguments) -> str:
    result = build_artifact(
        projection=load_projection(parsed.projection),
        repository_root=parsed.repository_root,
        source_revision=parsed.source_revision,
        asset_roots=parse_asset_roots(parsed.asset_root),
        output_dir=parsed.output_dir,
    )
    return result.model_dump_json()


def run_affected(parsed: Arguments) -> str:
    result = affected_targets(
        repository_root=parsed.repository_root,
        workspaces=parsed.workspace,
        changed=read_changed_paths(parsed.changed_files),
        base_revision=parsed.base_revision,
    )
    for warning in result.warnings:
        print(f"{PROGRAM_NAME}: {warning}", file=sys.stderr)
    return json.dumps(result.workspaces)


def main(arguments: list[str]) -> int:
    parsed = parse_arguments(arguments)
    try:
        if parsed.command == AFFECTED_COMMAND:
            output = run_affected(parsed)
        else:
            output = run_build(parsed)
    except BuildError as error:
        print(f"{PROGRAM_NAME}: {error}", file=sys.stderr)
        return FAILURE_EXIT_CODE
    print(output)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
