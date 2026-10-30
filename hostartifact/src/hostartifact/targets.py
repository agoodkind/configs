from __future__ import annotations

import json
import posixpath
import shutil
import subprocess
from pathlib import Path

from jinja2 import TemplateSyntaxError, meta
from pydantic import ValidationError

from hostartifact.manifest import check_revision
from hostartifact.projection import (
    ASSET_FIELD_SEPARATOR,
    ASSET_PREFIX,
    REPOSITORY,
    TEMPLATE_PREFIX,
    BuildError,
    DirectoryEntry,
    FileEntry,
    StrictModel,
    check_selector,
)
from hostartifact.render import new_environment

WORKSPACES_DIRECTORY = "opentofu"
DESCRIPTOR_NAME = "host-artifact-target.json"
PIN_NAME = "host-artifact.auto.tfvars.json"
DIRECTORY_SUFFIX = "/"
GIT_PROGRAM = "git"
# A change to any of these paths selects every target. The list contains
# the workflow, the helper, the target module, and the render root.
# Each descriptor lists every group variable file of its target.
PRODUCER_PATHS = (
    ".github/workflows/ci.yml",
    "hostartifact/",
    "opentofu/modules/host-artifact-targets/",
    "opentofu/render/host-artifact/",
)


class SecretField(StrictModel):
    name: str
    storage_path: str
    mode: str
    owner: str
    group: str
    pattern: str
    min_length: int
    max_length: int
    consumer_units: list[str]


class TargetDescriptor(StrictModel):
    node: str
    environment: str
    group_vars: list[str]
    directories: list[DirectoryEntry]
    files: list[FileEntry]
    secret_fields: list[SecretField]


class AffectedTargets(StrictModel):
    workspaces: list[str]
    warnings: list[str]


def descriptor_path(workspace: str) -> str:
    return f"{WORKSPACES_DIRECTORY}/{workspace}/{DESCRIPTOR_NAME}"


def pin_path(workspace: str) -> str:
    return f"{WORKSPACES_DIRECTORY}/{workspace}/{PIN_NAME}"


def load_descriptor(repository_root: Path, workspace: str) -> TargetDescriptor:
    path = repository_root / descriptor_path(workspace)
    try:
        return TargetDescriptor.model_validate(
            json.loads(path.read_text(encoding="utf-8"))
        )
    except (OSError, json.JSONDecodeError, ValidationError) as error:
        raise BuildError(f"target descriptor {path} is not valid: {error}") from error


class SourceReader:
    def __init__(self, repository_root: Path, base_revision: str | None) -> None:
        self._repository_root = repository_root
        self._base_revision = base_revision
        self._git: str | None = None
        if base_revision is not None:
            check_revision(base_revision)
            self._git = shutil.which(GIT_PROGRAM)
            if self._git is None:
                raise BuildError(
                    f"--base-revision requires the {GIT_PROGRAM} program on PATH"
                )

    def read(self, relative_path: str) -> str | None:
        path = self._repository_root / relative_path
        if path.is_file():
            return path.read_text(encoding="utf-8")
        if self._base_revision is None or self._git is None:
            return None
        command = [
            self._git,
            "-C",
            str(self._repository_root),
            "show",
            f"{self._base_revision}:{relative_path}",
        ]
        shown = subprocess.run(command, capture_output=True, check=False)
        if shown.returncode != 0:
            return None
        return shown.stdout.decode("utf-8")


def template_dependencies(
    reader: SourceReader, template_path: str, warnings: list[str]
) -> set[str]:
    environment = new_environment(None)
    found: set[str] = set()
    pending = [template_path]
    while pending:
        current = pending.pop()
        if current in found:
            continue
        found.add(current)
        source = reader.read(current)
        if source is None:
            continue
        try:
            tree = environment.parse(source)
        except TemplateSyntaxError as error:
            warnings.append(
                f"cannot parse template {current} to find its includes: {error}"
            )
            continue
        for included in meta.find_referenced_templates(tree):
            if included is None:
                warnings.append(
                    f"template {current} includes a template by a computed name that "
                    "the command cannot follow"
                )
                continue
            pending.append(
                posixpath.normpath(posixpath.join(posixpath.dirname(current), included))
            )
    return found


def target_dependencies(
    repository_root: Path, workspace: str, reader: SourceReader, warnings: list[str]
) -> set[str]:
    descriptor = load_descriptor(repository_root, workspace)
    dependencies = {descriptor_path(workspace), pin_path(workspace)}
    dependencies.update(posixpath.normpath(path) for path in descriptor.group_vars)
    for entry in descriptor.files:
        source = entry.source
        if source.startswith(TEMPLATE_PREFIX):
            template_path = posixpath.normpath(source.removeprefix(TEMPLATE_PREFIX))
            dependencies.update(template_dependencies(reader, template_path, warnings))
        elif source.startswith(ASSET_PREFIX):
            repository, _, path = source.removeprefix(ASSET_PREFIX).partition(
                ASSET_FIELD_SEPARATOR
            )
            if repository == REPOSITORY:
                dependencies.add(posixpath.normpath(path))
    return dependencies


def producer_changed(changed: set[str]) -> bool:
    for path in changed:
        for producer_path in PRODUCER_PATHS:
            is_directory = producer_path.endswith(DIRECTORY_SUFFIX)
            if path == producer_path or (is_directory and path.startswith(producer_path)):
                return True
    return False


def affected_targets(
    repository_root: Path,
    workspaces: list[str],
    changed: set[str],
    base_revision: str | None,
) -> AffectedTargets:
    for workspace in workspaces:
        check_selector(workspace)
    reader = SourceReader(repository_root, base_revision)
    if producer_changed(changed):
        return AffectedTargets(workspaces=sorted(workspaces), warnings=[])
    affected: list[str] = []
    warnings: list[str] = []
    for workspace in sorted(workspaces):
        try:
            dependencies = target_dependencies(
                repository_root, workspace, reader, warnings
            )
        except BuildError as error:
            warnings.append(
                f"{workspace}: cannot read the target dependencies: {error}; the "
                "command selects this target only when its descriptor or pin file "
                "changes"
            )
            dependencies = {descriptor_path(workspace), pin_path(workspace)}
        if dependencies & changed:
            affected.append(workspace)
    return AffectedTargets(workspaces=affected, warnings=warnings)
