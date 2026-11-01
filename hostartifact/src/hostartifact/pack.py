from __future__ import annotations

import gzip
import hashlib
import io
import json
import re
import tarfile
import zlib
from functools import partial
from pathlib import Path
from typing import Final

from jinja2 import StrictUndefined
from pydantic import Field, TypeAdapter, ValidationError
lazy from pydantic import JsonValue

from hostartifact.manifest import (
    MANIFEST_MEMBER,
    REVISION_PATTERN,
    Manifest,
    ManifestDirectory,
    ManifestFile,
    ManifestProgram,
    check_revision,
    manifest_bytes,
    member_name,
    schema_version_for,
)
from hostartifact.projection import (
    PROGRAM_NAME_PATTERN,
    REPOSITORY,
    AssetSource,
    BuildError,
    ProgramEntry,
    Projection,
    StrictModel,
    check_entries,
    contained_file,
    parse_source,
    target_identifier,
)
from hostartifact.render import VariableResolver, render_template
from hostartifact.secrets import (
    check_rendered_file,
    check_variable_names,
)

ASSET_NAME_PREFIX = "hostconfig_"
ASSET_NAME_SUFFIX = ".tar.gz"
SERVICE_MAPPING_VARIABLE = "service_mapping"
MANIFEST_MODE = 0o644
OCTAL_BASE = 8
COMPRESS_LEVEL = 9
ROOT_ID = 0
PROGRAM_BUNDLE_PREFIX = "hostconfig_program-"
PROGRAM_BUNDLE_SUFFIX = ".sigstore.jsonl"
PROGRAM_BUNDLES_FILE = "program-bundles.json"
PROGRAM_ARCHIVE_MODE: Final = "r:gz"
SHA256_PATTERN = r"^[0-9a-f]{64}$"
READ_CHUNK_BYTES = 1024 * 1024
JSON_INDENT = 2


class ArchiveMember(StrictModel):
    name: str
    content: bytes
    mode: int


class BuildResult(StrictModel):
    archive: str
    asset: str
    sha256: str
    size: int


class ProgramResult(StrictModel):
    name: str
    release_tag: str = Field(min_length=1)
    asset: str
    sha256: str = Field(pattern=SHA256_PATTERN)
    source_revision: str = Field(pattern=REVISION_PATTERN.pattern)
    signer_revision: str = Field(pattern=REVISION_PATTERN.pattern)
    archive: Path
    bundle: Path


class ProgramBundle(StrictModel):
    asset: str
    sha256: str


class VerifiedProgram(StrictModel):
    manifest: ManifestProgram
    bundle_asset: str
    bundle_content: bytes


def read_program_result(path: Path) -> ProgramResult:
    try:
        return ProgramResult.model_validate_json(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, ValidationError) as error:
        raise BuildError(
            f"The build cannot read or validate program result {path}: {error}"
        ) from error


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as stream:
            for chunk in iter(partial(stream.read, READ_CHUNK_BYTES), b""):
                digest.update(chunk)
    except OSError as error:
        raise BuildError(f"The build cannot read {path}: {error}") from error
    return digest.hexdigest()


def program_member_sha256(program: ProgramEntry, archive_path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with tarfile.open(archive_path, mode=PROGRAM_ARCHIVE_MODE) as archive:
            matches: list[tarfile.TarInfo] = []
            for item in archive.getmembers():
                if item.name == program.member:
                    matches.append(item)
            if not matches:
                raise BuildError(
                    f"Program {program.name}: the archive {archive_path} does not "
                    f"contain the member {program.member}"
                )
            if len(matches) > 1:
                raise BuildError(
                    f"Program {program.name}: the archive {archive_path} contains the "
                    f"member {program.member} more than once"
                )
            selected = matches[0]
            if not selected.isreg():
                raise BuildError(
                    f"Program {program.name}: the member {program.member} in "
                    f"{archive_path} must be a regular file and must not be a link or "
                    "another type"
                )
            extracted = archive.extractfile(selected)
            if extracted is None:
                raise BuildError(
                    f"Program {program.name}: the tar library cannot open the member "
                    f"{program.member} in {archive_path} for reading"
                )
            for chunk in iter(partial(extracted.read, READ_CHUNK_BYTES), b""):
                digest.update(chunk)
    except (tarfile.TarError, OSError, EOFError, zlib.error) as error:
        raise BuildError(
            f"Program {program.name}: the build cannot read the archive {archive_path} "
            f"as a gzip tar file: {error}"
        ) from error
    return digest.hexdigest()


def check_program_results(
    programs: list[ProgramEntry], results: dict[str, ProgramResult]
) -> None:
    expected = {program.name for program in programs}
    missing = sorted(expected - set(results))
    unexpected = sorted(set(results) - expected)
    if missing or unexpected:
        raise BuildError(
            "The program results do not match the projection programs: "
            f"programs [{', '.join(missing)}] do not have results, and "
            f"results [{', '.join(unexpected)}] do not match any program"
        )


def verify_program(program: ProgramEntry, result: ProgramResult) -> VerifiedProgram:
    if result.asset != program.asset:
        raise BuildError(
            f"Program {program.name}: the result records asset {result.asset}, but the "
            f"projection uses {program.asset}"
        )
    archive_path = result.archive
    archive_digest = file_sha256(archive_path)
    if archive_digest != result.sha256:
        raise BuildError(
            f"Program {program.name}: the archive {archive_path} has SHA-256 "
            f"{archive_digest}, but the result records {result.sha256}"
        )
    member_digest = program_member_sha256(program, archive_path)
    bundle_path = result.bundle
    try:
        bundle_content = bundle_path.read_bytes()
    except OSError as error:
        raise BuildError(
            f"Program {program.name}: the build cannot read the bundle {bundle_path}: "
            f"{error}"
        ) from error
    if not bundle_content:
        raise BuildError(
            f"Program {program.name}: the bundle {bundle_path} does not contain any data"
        )
    manifest_program = ManifestProgram(
        name=program.name,
        repository=program.repository,
        release_tag=result.release_tag,
        asset=program.asset,
        sha256=result.sha256,
        source_revision=result.source_revision,
        signer_revision=result.signer_revision,
        member=program.member,
        member_sha256=member_digest,
        path=program.path,
        mode=program.mode,
        owner=program.owner,
        group=program.group,
    )
    bundle_asset = f"{PROGRAM_BUNDLE_PREFIX}{program.name}{PROGRAM_BUNDLE_SUFFIX}"
    return VerifiedProgram(
        manifest=manifest_program,
        bundle_asset=bundle_asset,
        bundle_content=bundle_content,
    )


def previous_program_assets(output_dir: Path) -> list[str]:
    index_path = output_dir / PROGRAM_BUNDLES_FILE
    try:
        content = index_path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return []
    except (OSError, UnicodeDecodeError) as error:
        raise BuildError(
            f"The build cannot read program bundle index {index_path}: {error}"
        ) from error
    try:
        index = TypeAdapter(dict[str, ProgramBundle]).validate_json(content)
    except ValidationError as error:
        raise BuildError(
            f"The program bundle index {index_path} is not valid: {error}"
        ) from error
    assets: list[str] = []
    for name, bundle in index.items():
        expected_asset = f"{PROGRAM_BUNDLE_PREFIX}{name}{PROGRAM_BUNDLE_SUFFIX}"
        if (
            re.fullmatch(PROGRAM_NAME_PATTERN, name) is None
            or bundle.asset != expected_asset
        ):
            raise BuildError(
                f"The program bundle index {index_path} contains an unrecognized "
                f"program asset {bundle.asset!r} for {name!r}"
            )
        assets.append(bundle.asset)
    return assets


def write_program_bundles(output_dir: Path, programs: list[VerifiedProgram]) -> None:
    previous_assets = previous_program_assets(output_dir)
    current_assets = {program.bundle_asset for program in programs}
    try:
        for asset in previous_assets:
            if asset not in current_assets:
                (output_dir / asset).unlink(missing_ok=True)
        if not programs:
            (output_dir / PROGRAM_BUNDLES_FILE).unlink(missing_ok=True)
            return
    except OSError as error:
        raise BuildError(
            f"The build cannot remove previous program outputs in {output_dir}: {error}"
        ) from error
    index: dict[str, ProgramBundle] = {}
    for program in programs:
        (output_dir / program.bundle_asset).write_bytes(program.bundle_content)
        index[program.manifest.name] = ProgramBundle(
            asset=program.bundle_asset,
            sha256=hashlib.sha256(program.bundle_content).hexdigest(),
        )
    document = {name: bundle.model_dump(mode="json") for name, bundle in index.items()}
    text = json.dumps(document, indent=JSON_INDENT, sort_keys=True)
    (output_dir / PROGRAM_BUNDLES_FILE).write_text(text + "\n", encoding="utf-8")


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
    variables: dict[str, JsonValue] = dict(projection.variables)
    variables.update(projection.validation.variable_overrides)
    check_variable_names(variables)
    if SERVICE_MAPPING_VARIABLE in variables:
        raise BuildError(
            f"the projection variables must not define {SERVICE_MAPPING_VARIABLE}, "
            "because the projection supplies it as a separate field"
        )
    variables[SERVICE_MAPPING_VARIABLE] = projection.service_mapping
    return variables


def check_identity(projection: Projection, resolver: VariableResolver) -> None:
    expected = {
        projection.validation.identity_variables.node: projection.node,
        projection.validation.identity_variables.environment: projection.environment,
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
    program_results: dict[str, ProgramResult],
    output_dir: Path,
) -> BuildResult:
    identifier = target_identifier(projection.workspace)
    check_revision(source_revision)
    check_entries(projection)
    check_program_results(projection.programs, program_results)
    if REPOSITORY in asset_roots:
        raise BuildError(
            f"--asset-root must not name {REPOSITORY}, because --repository-root is "
            "its checkout"
        )
    roots = dict(asset_roots)
    roots[REPOSITORY] = repository_root
    resolver = VariableResolver(build_variables(projection))
    check_identity(projection, resolver)
    blocked_keys = frozenset(projection.validation.forbidden_toml_keys)

    members: list[ArchiveMember] = []
    manifest_files: list[ManifestFile] = []
    for entry in sorted(projection.files, key=lambda item: item.destination):
        content = file_content(entry.source, repository_root, roots, resolver)
        check_rendered_file(entry.destination, content, blocked_keys)
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
    verified_programs: list[VerifiedProgram] = []
    for program in sorted(projection.programs, key=lambda item: item.name):
        verified_programs.append(verify_program(program, program_results[program.name]))
    manifest_programs: list[ManifestProgram] = []
    for verified in verified_programs:
        manifest_programs.append(verified.manifest)
    manifest = Manifest(
        schema_version=schema_version_for(manifest_programs),
        repository=REPOSITORY,
        source_revision=source_revision,
        workspace=projection.workspace,
        node=projection.node,
        environment=projection.environment,
        directories=directories,
        files=manifest_files,
        programs=manifest_programs,
    )
    manifest_member = ArchiveMember(
        name=MANIFEST_MEMBER, content=manifest_bytes(manifest), mode=MANIFEST_MODE
    )

    asset = f"{ASSET_NAME_PREFIX}{identifier}{ASSET_NAME_SUFFIX}"
    output_dir.mkdir(parents=True, exist_ok=True)
    archive_path = output_dir / asset
    write_program_bundles(output_dir, verified_programs)
    write_archive(archive_path, [manifest_member, *members])
    archive_bytes = archive_path.read_bytes()
    return BuildResult(
        archive=str(archive_path),
        asset=asset,
        sha256=hashlib.sha256(archive_bytes).hexdigest(),
        size=len(archive_bytes),
    )
