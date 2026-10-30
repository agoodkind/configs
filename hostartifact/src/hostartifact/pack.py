from __future__ import annotations

import gzip
import hashlib
import io
import tarfile
from pathlib import Path

from jinja2 import StrictUndefined
lazy from pydantic import JsonValue

from hostartifact.manifest import (
    MANIFEST_MEMBER,
    SCHEMA_VERSION,
    Manifest,
    ManifestDirectory,
    ManifestFile,
    check_revision,
    manifest_bytes,
    member_name,
)
from hostartifact.projection import (
    REPOSITORY,
    AssetSource,
    BuildError,
    Projection,
    StrictModel,
    check_entries,
    contained_file,
    parse_source,
    target_identifier,
)
from hostartifact.render import VariableResolver, render_template
from hostartifact.secrets import (
    EMBEDS_SECRETS_VARIABLE,
    check_rendered_file,
    check_variable_names,
)

ASSET_NAME_PREFIX = "hostconfig_"
ASSET_NAME_SUFFIX = ".tar.gz"
SERVICE_MAPPING_VARIABLE = "service_mapping"
NODE_VARIABLE = "mwan_proxmox_node"
ENVIRONMENT_VARIABLE = "mwan_environment"
MANIFEST_MODE = 0o644
OCTAL_BASE = 8
COMPRESS_LEVEL = 9
ROOT_ID = 0


class ArchiveMember(StrictModel):
    name: str
    content: bytes
    mode: int


class BuildResult(StrictModel):
    archive: str
    asset: str
    sha256: str
    size: int


def write_archive(path: Path, members: list[ArchiveMember]) -> None:
    with (
        path.open("wb") as raw_file,
        gzip.GzipFile(
            filename="",
            mode="wb",
            fileobj=raw_file,
            mtime=0,
            compresslevel=COMPRESS_LEVEL,
        ) as gzip_file,
        tarfile.open(fileobj=gzip_file, mode="w", format=tarfile.USTAR_FORMAT) as archive,
    ):
        for member in members:
            info = tarfile.TarInfo(member.name)
            info.size = len(member.content)
            info.mode = member.mode
            info.mtime = 0
            info.uid = ROOT_ID
            info.gid = ROOT_ID
            info.uname = ""
            info.gname = ""
            info.type = tarfile.REGTYPE
            archive.addfile(info, io.BytesIO(member.content))


def build_variables(projection: Projection) -> dict[str, JsonValue]:
    check_variable_names(projection.variables)
    if SERVICE_MAPPING_VARIABLE in projection.variables:
        raise BuildError(
            f"the projection variables must not define {SERVICE_MAPPING_VARIABLE}, "
            "because the projection supplies it as a separate field"
        )
    variables: dict[str, JsonValue] = dict(projection.variables)
    variables[SERVICE_MAPPING_VARIABLE] = projection.service_mapping
    variables[EMBEDS_SECRETS_VARIABLE] = False
    return variables


def check_identity(projection: Projection, resolver: VariableResolver) -> None:
    expected = {
        NODE_VARIABLE: projection.node,
        ENVIRONMENT_VARIABLE: projection.environment,
    }
    context = resolver.context(set(expected))
    for name, value in expected.items():
        if name not in context or isinstance(context[name], StrictUndefined):
            raise BuildError(f"the projection variables do not define {name}")
        if context[name] != value:
            raise BuildError(
                f"{name} is {context[name]!r} in the group variables, but the "
                f"projection sets {value!r}"
            )


def file_content(
    source: str,
    repository_root: Path,
    asset_roots: dict[str, Path],
    resolver: VariableResolver,
) -> bytes:
    parsed = parse_source(source)
    if isinstance(parsed, AssetSource):
        if parsed.repository not in asset_roots:
            raise BuildError(f"specify an --asset-root option for {parsed.repository}")
        root = asset_roots[parsed.repository]
        return contained_file(root, parsed.path, parsed.repository).read_bytes()
    template_file = contained_file(repository_root, parsed.path, REPOSITORY)
    return render_template(template_file, resolver)


def build_artifact(
    projection: Projection,
    repository_root: Path,
    source_revision: str,
    asset_roots: dict[str, Path],
    output_dir: Path,
) -> BuildResult:
    identifier = target_identifier(projection.workspace)
    check_revision(source_revision)
    check_entries(projection)
    if REPOSITORY in asset_roots:
        raise BuildError(
            f"--asset-root must not name {REPOSITORY}, because --repository-root is "
            "its checkout"
        )
    roots = dict(asset_roots)
    roots[REPOSITORY] = repository_root
    resolver = VariableResolver(build_variables(projection))
    check_identity(projection, resolver)

    members: list[ArchiveMember] = []
    manifest_files: list[ManifestFile] = []
    for entry in sorted(projection.files, key=lambda item: item.destination):
        content = file_content(entry.source, repository_root, roots, resolver)
        check_rendered_file(entry.destination, content)
        member = member_name(entry.destination)
        mode = int(entry.mode, OCTAL_BASE)
        members.append(ArchiveMember(name=member, content=content, mode=mode))
        manifest_files.append(
            ManifestFile(
                member=member,
                path=entry.destination,
                size=len(content),
                sha256=hashlib.sha256(content).hexdigest(),
                mode=entry.mode,
                owner=entry.owner,
                group=entry.group,
            )
        )
    directories: list[ManifestDirectory] = []
    for item in sorted(projection.directories, key=lambda directory: directory.path):
        directories.append(
            ManifestDirectory(
                path=item.path, mode=item.mode, owner=item.owner, group=item.group
            )
        )
    manifest = Manifest(
        schema_version=SCHEMA_VERSION,
        repository=REPOSITORY,
        source_revision=source_revision,
        workspace=projection.workspace,
        node=projection.node,
        environment=projection.environment,
        directories=directories,
        files=manifest_files,
    )
    manifest_member = ArchiveMember(
        name=MANIFEST_MEMBER, content=manifest_bytes(manifest), mode=MANIFEST_MODE
    )

    asset = f"{ASSET_NAME_PREFIX}{identifier}{ASSET_NAME_SUFFIX}"
    output_dir.mkdir(parents=True, exist_ok=True)
    archive_path = output_dir / asset
    write_archive(archive_path, [manifest_member, *members])
    archive_bytes = archive_path.read_bytes()
    return BuildResult(
        archive=str(archive_path),
        asset=asset,
        sha256=hashlib.sha256(archive_bytes).hexdigest(),
        size=len(archive_bytes),
    )
