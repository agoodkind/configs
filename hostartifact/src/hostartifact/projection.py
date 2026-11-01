from __future__ import annotations

import hashlib
import json
import posixpath
import re
from pathlib import Path, PurePosixPath

from pydantic import BaseModel, ConfigDict, Field, JsonValue, ValidationError

REPOSITORY = "agoodkind/configs"
TEMPLATE_PREFIX = "template:"
ASSET_PREFIX = "asset:"
ASSET_FIELD_SEPARATOR = ":"
SELECTOR_SEPARATOR = "/"
HIDDEN_PREFIX = "."
CURRENT_DIRECTORY = "."
EMPTY_SELECTOR = ""
BACKSLASH = "\\"
EXCLUDED_DIRECTORIES = frozenset({"terraform.d", "modules"})
MODE_PATTERN = re.compile(r"^0[0-7]{3}$")
ACCOUNT_PATTERN = re.compile(r"^[a-z_][a-z0-9_-]*$")
REPOSITORY_PATTERN = re.compile(r"^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$")
PROGRAM_NAME_PATTERN = r"^[a-z][a-z0-9-]*$"
PROGRAM_ASSET_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
PARENT_DIRECTORY = ".."
ROOT_PATH = "/"
MINIMUM_VARIABLE_NAME_LENGTH = 1


class BuildError(Exception):
    pass


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class DirectoryEntry(StrictModel):
    path: str
    mode: str
    owner: str
    group: str


class FileEntry(StrictModel):
    destination: str
    source: str
    mode: str
    owner: str
    group: str


class ProgramEntry(StrictModel):
    name: str = Field(pattern=PROGRAM_NAME_PATTERN)
    repository: str = Field(pattern=REPOSITORY_PATTERN.pattern)
    asset: str
    member: str
    path: str
    mode: str
    owner: str
    group: str


class IdentityVariables(StrictModel):
    node: str = Field(min_length=MINIMUM_VARIABLE_NAME_LENGTH)
    environment: str = Field(min_length=MINIMUM_VARIABLE_NAME_LENGTH)


class ValidationPolicy(StrictModel):
    identity_variables: IdentityVariables
    variable_overrides: dict[str, JsonValue]
    forbidden_toml_keys: list[str]


class Projection(StrictModel):
    model_config = ConfigDict(extra="forbid", frozen=True, populate_by_name=True)

    workspace: str
    node: str
    environment: str
    validation: ValidationPolicy
    variables: dict[str, JsonValue] = Field(alias="vars")
    service_mapping: dict[str, JsonValue]
    directories: list[DirectoryEntry]
    files: list[FileEntry]
    programs: list[ProgramEntry] = Field(default_factory=list)


class TemplateSource(StrictModel):
    path: str


class AssetSource(StrictModel):
    repository: str
    path: str


def load_projection(path: Path) -> Projection:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as error:
        raise BuildError(f"cannot read projection {path}: {error}") from error
    try:
        return Projection.model_validate(json.loads(text))
    except (json.JSONDecodeError, ValidationError) as error:
        raise BuildError(f"projection {path} is not valid: {error}") from error


def excluded_directory(name: str) -> bool:
    return name.startswith(HIDDEN_PREFIX) or name in EXCLUDED_DIRECTORIES


def check_selector(workspace: str) -> None:
    is_clean = posixpath.normpath(workspace) == workspace
    is_empty = workspace in {EMPTY_SELECTOR, CURRENT_DIRECTORY}
    is_local = not workspace.startswith(ROOT_PATH) and not is_empty
    if not is_clean or not is_local or BACKSLASH in workspace:
        raise BuildError(
            f"workspace selector {workspace!r} must be a clean relative path inside "
            "the workspace tree"
        )
    if any(excluded_directory(part) for part in workspace.split(SELECTOR_SEPARATOR)):
        raise BuildError(
            f"workspace selector {workspace!r} contains an excluded directory"
        )


def target_identifier(workspace: str) -> str:
    check_selector(workspace)
    return hashlib.sha256(workspace.encode("utf-8")).hexdigest()


def check_absolute_path(path: str) -> None:
    pure_path = PurePosixPath(path)
    is_normalized = str(pure_path) == path
    if not pure_path.is_absolute() or not is_normalized or path == ROOT_PATH:
        raise BuildError(
            f"path {path!r} must be an absolute, normalized path below the root directory"
        )
    if PARENT_DIRECTORY in pure_path.parts:
        raise BuildError(f"path {path!r} must not contain a parent directory segment")


def check_relative_path(path: str) -> None:
    pure_path = PurePosixPath(path)
    if pure_path.is_absolute() or PARENT_DIRECTORY in pure_path.parts or not path:
        raise BuildError(
            f"source path {path!r} must be a relative path without a parent directory "
            "segment"
        )
    if posixpath.normpath(path) != path:
        raise BuildError(
            f"source path {path!r} must equal its normalized form "
            f"{posixpath.normpath(path)!r}"
        )


def check_metadata(path: str, mode: str, owner: str, group: str) -> None:
    if MODE_PATTERN.fullmatch(mode) is None:
        raise BuildError(
            f"{path}: mode {mode!r} must be four octal digits that start with 0"
        )
    for account in (owner, group):
        if ACCOUNT_PATTERN.fullmatch(account) is None:
            raise BuildError(
                f"{path}: account name {account!r} is not a valid user or group name"
            )


def check_program_archive_names(program: ProgramEntry) -> None:
    if PROGRAM_ASSET_PATTERN.fullmatch(program.asset) is None:
        raise BuildError(
            f"Program {program.name}: asset {program.asset!r} must use a release asset "
            "file name that does not contain a directory"
        )
    member = program.member
    pure_member = PurePosixPath(member)
    is_plain = (
        bool(member)
        and not pure_member.is_absolute()
        and PARENT_DIRECTORY not in pure_member.parts
        and posixpath.normpath(member) == member
        and member != CURRENT_DIRECTORY
    )
    if not is_plain:
        raise BuildError(
            f"Program {program.name}: member {member!r} must use a normalized relative "
            "path that does not contain a parent directory segment"
        )


def check_entries(projection: Projection) -> None:
    seen_directories: set[str] = set()
    for directory in projection.directories:
        check_absolute_path(directory.path)
        check_metadata(directory.path, directory.mode, directory.owner, directory.group)
        if directory.path in seen_directories:
            raise BuildError(
                f"the projection lists directory {directory.path} more than once"
            )
        seen_directories.add(directory.path)
    seen_destinations: set[str] = set()
    for entry in projection.files:
        check_absolute_path(entry.destination)
        check_metadata(entry.destination, entry.mode, entry.owner, entry.group)
        if entry.destination in seen_destinations:
            raise BuildError(
                f"the projection lists destination {entry.destination} more than once"
            )
        seen_destinations.add(entry.destination)
    seen_program_names: set[str] = set()
    seen_program_paths: set[str] = set()
    for program in projection.programs:
        check_absolute_path(program.path)
        check_metadata(program.path, program.mode, program.owner, program.group)
        check_program_archive_names(program)
        if program.name in seen_program_names:
            raise BuildError(
                f"the projection lists program name {program.name} more than once"
            )
        seen_program_names.add(program.name)
        if program.path in seen_program_paths:
            raise BuildError(
                f"the projection lists program path {program.path} more than once"
            )
        seen_program_paths.add(program.path)
        if program.path in seen_destinations:
            raise BuildError(
                f"the projection lists {program.path} as both the path for program "
                f"{program.name} and a file destination"
            )


def parse_source(source: str) -> TemplateSource | AssetSource:
    if source.startswith(TEMPLATE_PREFIX):
        template_path = source.removeprefix(TEMPLATE_PREFIX)
        check_relative_path(template_path)
        return TemplateSource(path=template_path)
    if source.startswith(ASSET_PREFIX):
        fields = source.removeprefix(ASSET_PREFIX).split(ASSET_FIELD_SEPARATOR, 1)
        if len(fields) != 2 or REPOSITORY_PATTERN.fullmatch(fields[0]) is None:
            raise BuildError(
                f"asset source {source!r} must have the form "
                "asset:<owner>/<repository>:<path>"
            )
        check_relative_path(fields[1])
        return AssetSource(repository=fields[0], path=fields[1])
    raise BuildError(f"source {source!r} must start with template: or asset:")


def contained_file(root: Path, relative_path: str, description: str) -> Path:
    resolved_root = root.resolve()
    candidate = (resolved_root / relative_path).resolve()
    if not candidate.is_relative_to(resolved_root) or not candidate.is_file():
        raise BuildError(
            f"{relative_path} is not a regular file in the {description} checkout "
            f"at {root}"
        )
    return candidate
