from __future__ import annotations

import json
from collections.abc import Iterable
from pathlib import Path

from pydantic import ValidationError

from hostartifact.projection import (
    REPOSITORY,
    AssetSource,
    BuildError,
    DirectoryEntry,
    FileEntry,
    StrictModel,
    check_selector,
    contained_file,
    parse_source,
)
from hostartifact.render import template_files

WORKSPACES_DIRECTORY = "opentofu"
DESCRIPTOR_NAME = "host-artifact-target.json"
PIN_NAME = "host-artifact.auto.tfvars.json"
DIRECTORY_SUFFIX = "/"
# A change to any of these paths selects every target. The list contains the
# workflow, the helper, the render root, the target module, the contracts
# module, and the three group variable files that the contracts module reads.
PRODUCER_PATHS = (
    ".github/workflows/ci.yml",
    "ansible/inventory/group_vars/all/service_mapping.yml",
    "ansible/inventory/group_vars/all/vars.yml",
    "ansible/inventory/group_vars/proxy_servers.yml",
    "hostartifact/",
    "opentofu/modules/contracts/",
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
    environment_group_vars: str
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


def relative_paths(repository_root: Path, files: Iterable[Path]) -> set[str]:
    resolved_root = repository_root.resolve()
    return {file.resolve().relative_to(resolved_root).as_posix() for file in files}


def target_dependencies(repository_root: Path, workspace: str) -> set[str]:
    descriptor = load_descriptor(repository_root, workspace)
    dependencies = {descriptor_path(workspace), pin_path(workspace)}
    dependencies.add(descriptor.environment_group_vars)
    dependencies.update(descriptor.group_vars)
    for entry in descriptor.files:
        source = parse_source(entry.source)
        if isinstance(source, AssetSource):
            if source.repository == REPOSITORY:
                dependencies.add(source.path)
            continue
        template_file = contained_file(repository_root, source.path, REPOSITORY)
        dependencies.update(
            relative_paths(repository_root, template_files(template_file))
        )
    return dependencies


def producer_changed(changed: set[str]) -> bool:
    for path in changed:
        for producer_path in PRODUCER_PATHS:
            is_directory = producer_path.endswith(DIRECTORY_SUFFIX)
            if path == producer_path or (is_directory and path.startswith(producer_path)):
                return True
    return False


def affected_targets(
    repository_root: Path, workspaces: list[str], changed: set[str]
) -> AffectedTargets:
    for workspace in workspaces:
        check_selector(workspace)
    if producer_changed(changed):
        return AffectedTargets(workspaces=sorted(workspaces), warnings=[])
    affected: list[str] = []
    warnings: list[str] = []
    for workspace in sorted(workspaces):
        try:
            dependencies = target_dependencies(repository_root, workspace)
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
