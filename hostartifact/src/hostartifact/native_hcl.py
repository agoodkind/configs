from __future__ import annotations

import os
import subprocess
import tempfile
from pathlib import Path

from pydantic import BaseModel

from hostartifact.projection import REPOSITORY, BuildError, FileEntry
from hostartifact.secret_patterns import TEMPLATE_MARKERS

CONFIGS_ASSET_PREFIX = f"asset:{REPOSITORY}:"
HCL_SUFFIX = ".tf"
CHECK_COMMAND = "lint-hcl-sources"
REQUEST_NAME = "request.json"
CHECK_TIMEOUT_SECONDS = 30


class HclSourceFile(BaseModel):
    source: str
    destination: str
    content: str


class HclSourceRequest(BaseModel):
    files: list[HclSourceFile]


def raw_hcl_asset(entry: FileEntry, workspace: str) -> bool:
    workspace_source_prefix = f"{CONFIGS_ASSET_PREFIX}opentofu/{workspace}/"
    return (
        entry.destination.endswith(HCL_SUFFIX)
        and entry.source.startswith(workspace_source_prefix)
        and entry.source.endswith(HCL_SUFFIX)
    )


def hcl_source_files(
    files: list[tuple[FileEntry, bytes]], workspace: str
) -> list[HclSourceFile]:
    sources: list[HclSourceFile] = []
    for entry, content in files:
        if not raw_hcl_asset(entry, workspace):
            continue
        try:
            text = content.decode("utf-8")
        except UnicodeDecodeError as error:
            raise BuildError(
                f"{entry.destination} does not contain UTF-8 text: {error}"
            ) from error
        markers = [marker for marker in TEMPLATE_MARKERS if marker in text]
        if markers:
            raise BuildError(
                f"{entry.destination} contains unresolved template markers: "
                f"{' '.join(markers)}"
            )
        source = entry.source.removeprefix(CONFIGS_ASSET_PREFIX)
        sources.append(
            HclSourceFile(source=source, destination=entry.destination, content=text)
        )
    return sources


def check_hcl_sources(
    files: list[tuple[FileEntry, bytes]], workspace: str, checker: Path
) -> set[str]:
    if not checker.is_absolute():
        raise BuildError(f"HOSTARTIFACT_HCL_CHECKER must be an absolute path: {checker}")
    if not checker.is_file() or not os.access(checker, os.X_OK):
        raise BuildError(f"native HCL checker is missing or not executable: {checker}")
    sources = hcl_source_files(files, workspace)
    if not sources:
        return set()
    request = HclSourceRequest(files=sources)
    with tempfile.TemporaryDirectory(prefix="hostartifact-hcl-") as directory:
        request_path = Path(directory) / REQUEST_NAME
        request_path.write_text(request.model_dump_json(), encoding="utf-8")
        try:
            result = subprocess.run(
                [str(checker), CHECK_COMMAND, str(request_path)],
                capture_output=True,
                text=True,
                check=False,
                env={},
                timeout=CHECK_TIMEOUT_SECONDS,
            )
        except (OSError, subprocess.TimeoutExpired) as error:
            raise BuildError(f"native HCL checker failed: {error}") from error
    if result.returncode != 0:
        error_message = result.stderr.strip()
        if not error_message:
            error_message = f"exit status {result.returncode}"
        raise BuildError(f"native HCL checker rejected the source: {error_message}")
    return {source.destination for source in sources}
