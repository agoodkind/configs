from __future__ import annotations

import json
import re
from typing import Final, Literal

from hostartifact.projection import BuildError, StrictModel

SCHEMA_VERSION: Final = 1
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


class Manifest(StrictModel):
    schema_version: Literal[1]
    repository: str
    source_revision: str
    workspace: str
    node: str
    environment: str
    directories: list[ManifestDirectory]
    files: list[ManifestFile]


def check_revision(revision: str) -> None:
    if REVISION_PATTERN.fullmatch(revision) is None:
        raise BuildError(
            f"source revision {revision!r} must be 40 lowercase hexadecimal characters"
        )


def member_name(destination: str) -> str:
    return FILES_PREFIX + destination.lstrip("/")


def manifest_bytes(manifest: Manifest) -> bytes:
    document = manifest.model_dump(mode="json")
    text = json.dumps(document, indent=JSON_INDENT, sort_keys=True)
    return (text + "\n").encode("utf-8")
