from __future__ import annotations

import argparse
import sys
from pathlib import Path

from hostartifact.pack import build_artifact
from hostartifact.projection import BuildError, load_projection

BUILD_COMMAND = "build"
ASSET_ROOT_SEPARATOR = "="
FAILURE_EXIT_CODE = 1
PROGRAM_NAME = "hostartifact"


class BuildArguments(argparse.Namespace):
    command: str
    projection: Path
    repository_root: Path
    source_revision: str
    asset_root: list[str]
    output_dir: Path


def parse_arguments(arguments: list[str]) -> BuildArguments:
    parser = argparse.ArgumentParser(prog=PROGRAM_NAME)
    commands = parser.add_subparsers(dest="command", required=True)
    build = commands.add_parser(BUILD_COMMAND)
    build.add_argument("--projection", type=Path, required=True)
    build.add_argument("--repository-root", type=Path, required=True)
    build.add_argument("--source-revision", required=True)
    build.add_argument("--asset-root", action="append", default=[])
    build.add_argument("--output-dir", type=Path, required=True)
    return parser.parse_args(arguments, namespace=BuildArguments())


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


def main(arguments: list[str]) -> int:
    parsed = parse_arguments(arguments)
    try:
        result = build_artifact(
            projection=load_projection(parsed.projection),
            repository_root=parsed.repository_root,
            source_revision=parsed.source_revision,
            asset_roots=parse_asset_roots(parsed.asset_root),
            output_dir=parsed.output_dir,
        )
    except BuildError as error:
        print(f"{PROGRAM_NAME}: {error}", file=sys.stderr)
        return FAILURE_EXIT_CODE
    print(result.model_dump_json())
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
