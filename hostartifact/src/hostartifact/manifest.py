from __future__ import annotations

import json
import re
from typing import Final, Literal

from pydantic import Field

from hostartifact.projection import BuildError, StrictModel

SCHEMA_VERSION: Final = 1
PROGRAM_SCHEMA_VERSION: Final = 2
PROGRAMS_FIELD = "programs"
MANIFEST_MEMBER = "manifest.json"
FILES_PREFIX = "files/"
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")
JSON_INDENT = 2


class ManifestDirectory(StrictModel):
    path: str
    mode: str
    owner: str
    group: str


class ManifestFile(StrictModel):
    member: str
    path: str
    size: int
    sha256: str
    mode: str
    owner: str
    group: str


class ManifestProgram(StrictModel):
    name: str
    repository: str
    release_tag: str
    asset: str
    sha256: str
    source_revision: str
    signer_revision: str
    member: str
    member_sha256: str
    path: str
    mode: str
    owner: str
    group: str


class Manifest(StrictModel):
    schema_version: Literal[1, 2]
    repository: str
    source_revision: str
    workspace: str
    node: str
    environment: str
    directories: list[ManifestDirectory]
    files: list[ManifestFile]
    programs: list[ManifestProgram] = Field(default_factory=list)


def schema_version_for(programs: list[ManifestProgram]) -> Literal[1, 2]:
    if programs:
        return PROGRAM_SCHEMA_VERSION
    return SCHEMA_VERSION


def check_revision(revision: str) -> None:
    if REVISION_PATTERN.fullmatch(revision) is None:
        raise BuildError(
            f"source revision {revision!r} must be 40 lowercase hexadecimal characters"
        )


def member_name(destination: str) -> str:
    return FILES_PREFIX + destination.lstrip("/")


def manifest_bytes(manifest: Manifest) -> bytes:
    if manifest.schema_version == SCHEMA_VERSION:
        document = manifest.model_dump(mode="json", exclude={PROGRAMS_FIELD})
    else:
        document = manifest.model_dump(mode="json")
    text = json.dumps(document, indent=JSON_INDENT, sort_keys=True)
    return (text + "\n").encode("utf-8")
