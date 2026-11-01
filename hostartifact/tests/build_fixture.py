from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import sys
import tarfile
from collections.abc import Sequence
from enum import StrEnum
from pathlib import Path

from hostartifact.manifest import (
    MANIFEST_MEMBER,
    Manifest,
    ManifestDirectory,
    ManifestFile,
    ManifestProgram,
    check_revision,
    manifest_bytes,
    member_name,
    schema_version_for,
)
from hostartifact.pack import (
    ASSET_NAME_PREFIX,
    ASSET_NAME_SUFFIX,
    COMPRESS_LEVEL,
    MANIFEST_MODE,
    OCTAL_BASE,
    PROGRAM_BUNDLE_PREFIX,
    PROGRAM_BUNDLE_SUFFIX,
    ROOT_ID,
    ArchiveMember,
    BuildResult,
    VerifiedProgram,
    read_program_result,
    verify_program,
    write_archive,
    write_program_bundles,
)
from hostartifact.projection import (
    REPOSITORY,
    BuildError,
    ProgramEntry,
    StrictModel,
    target_identifier,
)

PROGRAM_NAME = "build_fixture"
FAILURE_EXIT_CODE = 1
FIXTURE_ASSET_LABEL = "fixture"
ASSET_FIELD_SEPARATOR = "-"
NODE = "hypervisor"
ENVIRONMENT = "testbed"
ROOT_ACCOUNT = "root"
DIRECTORY_MODE = "0755"
FIRST_FILE_LABEL = "a"
SECOND_FILE_LABEL = "b"
FILE_NAME_SUFFIX = ".conf"
FIRST_FILE_MODE = "0644"
SECOND_FILE_MODE = "0640"
VERSION_ONE = "one"
VERSION_TWO = "two"
BAD_CONTENT = b"a bad\n"
DEFECT_CONTENT = b"x\n"
DEFECT_MODE = 0o644
SPECIAL_MEMBER_MODE = 0o4755
SIZE_INCREASE = 1
TRAVERSAL_FILE_NAME = "traversal.conf"
ABSOLUTE_FILE_NAME = "absolute.conf"
EXTRA_FILE_NAME = "extra.conf"
TRAVERSAL_MEMBER_PREFIX = "files/.."
RECOVERY_FILE_COUNT = 300
RECOVERY_FILE_PREFIX = "recovery-"
RECOVERY_INDEX_FORMAT = "03d"
RECOVERY_FILE_MODE = "0644"
ARCHIVE_TIME = 0
EMPTY_NAME = ""
PROGRAM_IDENTIFIER = "opnsensectl"
PROGRAM_REPOSITORY = "agoodkind/opnsensectl"
PROGRAM_RELEASE_TAG = "202609190337-7-7db20fb"
PROGRAM_ASSET = "opnsensectl_linux_amd64.tar.gz"
PROGRAM_ARCHIVE_SHA256 = (
    "29e65cb0cc7b7924478416ab98d7c22e8d3e1679e7cbcfdbff42323fde874399"
)
PROGRAM_MEMBER = "opnsensectl"
PROGRAM_SOURCE_REVISION = "7db20fb56fa01fa41bbb9ebeb03f29c281bce24a"
PROGRAM_SIGNER_REVISION = "6f29e0969c7c302aee9c1a23b3424c91f8c7796d"
PROGRAM_MODE = "0755"
WRONG_REVISION = "1" * 40
WRONG_DIGEST = "0" * 64


class FixtureCase(StrEnum):
    VALID_V1 = "valid-v1"
    VALID_V2 = "valid-v2"
    OTHER_TARGET = "other-target"
    DIGEST = "digest"
    SIZE = "size"
    SYMLINK = "symlink"
    HARDLINK = "hardlink"
    TRAVERSAL = "traversal"
    ABSOLUTE = "absolute"
    EXTRA = "extra"
    MISSING = "missing"
    DUPLICATE = "duplicate"
    MEMBER_MODE = "member-mode"
    RECOVERY_V1 = "recovery-v1"
    RECOVERY_V2 = "recovery-v2"
    PROGRAM_VALID = "program-valid"
    PROGRAM_WRONG_SOURCE = "program-wrong-source"
    PROGRAM_WRONG_SIGNER = "program-wrong-signer"
    PROGRAM_WRONG_ARCHIVE = "program-wrong-archive"
    PROGRAM_WRONG_MEMBER = "program-wrong-member"
    PROGRAM_COLLISION = "program-collision"


PROGRAM_CASES = frozenset(
    {
        FixtureCase.PROGRAM_VALID,
        FixtureCase.PROGRAM_WRONG_SOURCE,
        FixtureCase.PROGRAM_WRONG_SIGNER,
        FixtureCase.PROGRAM_WRONG_ARCHIVE,
        FixtureCase.PROGRAM_WRONG_MEMBER,
        FixtureCase.PROGRAM_COLLISION,
    }
)


class LinkKind(StrEnum):
    SYMBOLIC = "symbolic"
    HARD = "hard"


class FixtureTarget(StrictModel):
    workspace: str
    directory: str


class FixtureFile(StrictModel):
    path: str
    content: bytes
    mode: str


class LinkMember(StrictModel):
    name: str
    kind: LinkKind
    link_name: str
    mode: int


type FixtureMember = ArchiveMember | LinkMember


class FixtureArchive(StrictModel):
    target: FixtureTarget
    members: list[FixtureMember]


class ProgramInput(StrictModel):
    bundle: bytes
    member_sha256: str


class Arguments(StrictModel):
    case: FixtureCase
    expected_workspace: str
    source_revision: str
    output_dir: Path
    program_result: Path | None


TARGET_A = FixtureTarget(
    workspace="suburban/hostartifact-acceptance",
    directory="/srv/hostartifact-acceptance",
)
TARGET_B = FixtureTarget(
    workspace="suburban/hostartifact-acceptance-b",
    directory="/srv/hostartifact-acceptance-b",
)


def case_target(case: FixtureCase) -> FixtureTarget:
    if case == FixtureCase.OTHER_TARGET:
        return TARGET_B
    return TARGET_A


def destination(target: FixtureTarget, file_name: str) -> str:
    return f"{target.directory}/{file_name}"


def labeled_file(
    target: FixtureTarget, label: str, version: str, mode: str
) -> FixtureFile:
    return FixtureFile(
        path=destination(target, label + FILE_NAME_SUFFIX),
        content=f"{label} {version}\n".encode(),
        mode=mode,
    )


def two_files(target: FixtureTarget, version: str) -> list[FixtureFile]:
    first = labeled_file(target, FIRST_FILE_LABEL, version, FIRST_FILE_MODE)
    second = labeled_file(target, SECOND_FILE_LABEL, version, SECOND_FILE_MODE)
    return [first, second]


def recovery_files(target: FixtureTarget, version: str) -> list[FixtureFile]:
    files: list[FixtureFile] = []
    for index in range(RECOVERY_FILE_COUNT):
        number = f"{index:{RECOVERY_INDEX_FORMAT}}"
        file_name = RECOVERY_FILE_PREFIX + number + FILE_NAME_SUFFIX
        files.append(
            FixtureFile(
                path=destination(target, file_name),
                content=f"{version} {number}\n".encode(),
                mode=RECOVERY_FILE_MODE,
            )
        )
    return files


def manifest_file(fixture_file: FixtureFile) -> ManifestFile:
    return ManifestFile(
        member=member_name(fixture_file.path),
        path=fixture_file.path,
        size=len(fixture_file.content),
        sha256=hashlib.sha256(fixture_file.content).hexdigest(),
        mode=fixture_file.mode,
        owner=ROOT_ACCOUNT,
        group=ROOT_ACCOUNT,
    )


def enlarged(entry: ManifestFile) -> ManifestFile:
    return ManifestFile(
        member=entry.member,
        path=entry.path,
        size=entry.size + SIZE_INCREASE,
        sha256=entry.sha256,
        mode=entry.mode,
        owner=entry.owner,
        group=entry.group,
    )


def archive_member(fixture_file: FixtureFile) -> ArchiveMember:
    return ArchiveMember(
        name=member_name(fixture_file.path),
        content=fixture_file.content,
        mode=int(fixture_file.mode, OCTAL_BASE),
    )


def defect_member(name: str) -> ArchiveMember:
    return ArchiveMember(name=name, content=DEFECT_CONTENT, mode=DEFECT_MODE)


def program_manifest(
    case: FixtureCase, target: FixtureTarget, archive_member_sha256: str
) -> ManifestProgram:
    source_revision = PROGRAM_SOURCE_REVISION
    signer_revision = PROGRAM_SIGNER_REVISION
    archive_sha256 = PROGRAM_ARCHIVE_SHA256
    member_sha256 = archive_member_sha256
    path = destination(target, PROGRAM_IDENTIFIER)
    if case == FixtureCase.PROGRAM_WRONG_SOURCE:
        source_revision = WRONG_REVISION
    if case == FixtureCase.PROGRAM_WRONG_SIGNER:
        signer_revision = WRONG_REVISION
    if case == FixtureCase.PROGRAM_WRONG_ARCHIVE:
        archive_sha256 = WRONG_DIGEST
    if case == FixtureCase.PROGRAM_WRONG_MEMBER:
        member_sha256 = WRONG_DIGEST
    if case == FixtureCase.PROGRAM_COLLISION:
        path = destination(target, FIRST_FILE_LABEL + FILE_NAME_SUFFIX)
    return ManifestProgram(
        name=PROGRAM_IDENTIFIER,
        repository=PROGRAM_REPOSITORY,
        release_tag=PROGRAM_RELEASE_TAG,
        asset=PROGRAM_ASSET,
        sha256=archive_sha256,
        source_revision=source_revision,
        signer_revision=signer_revision,
        member=PROGRAM_MEMBER,
        member_sha256=member_sha256,
        path=path,
        mode=PROGRAM_MODE,
        owner=ROOT_ACCOUNT,
        group=ROOT_ACCOUNT,
    )


def manifest_member(
    target: FixtureTarget,
    source_revision: str,
    entries: list[ManifestFile],
    programs: list[ManifestProgram],
) -> ArchiveMember:
    directory = ManifestDirectory(
        path=target.directory, mode=DIRECTORY_MODE, owner=ROOT_ACCOUNT, group=ROOT_ACCOUNT
    )
    manifest = Manifest(
        schema_version=schema_version_for(programs),
        repository=REPOSITORY,
        source_revision=source_revision,
        workspace=target.workspace,
        node=NODE,
        environment=ENVIRONMENT,
        directories=[directory],
        files=entries,
        programs=programs,
    )
    return ArchiveMember(
        name=MANIFEST_MEMBER, content=manifest_bytes(manifest), mode=MANIFEST_MODE
    )


def assembled_archive(
    target: FixtureTarget,
    source_revision: str,
    entries: list[ManifestFile],
    file_members: Sequence[FixtureMember],
    programs: list[ManifestProgram] | None = None,
) -> FixtureArchive:
    listed_programs: list[ManifestProgram] = []
    if programs is not None:
        listed_programs = programs
    manifest = manifest_member(target, source_revision, entries, listed_programs)
    return FixtureArchive(target=target, members=[manifest, *file_members])


def matching_archive(
    target: FixtureTarget, source_revision: str, files: list[FixtureFile]
) -> FixtureArchive:
    entries = [manifest_file(item) for item in files]
    file_members = [archive_member(item) for item in files]
    return assembled_archive(target, source_revision, entries, file_members)


def fixture_archive(
    case: FixtureCase, source_revision: str, program_member_digest: str
) -> FixtureArchive:
    target = case_target(case)
    version_two = two_files(target, VERSION_TWO)
    entries = [manifest_file(item) for item in version_two]
    first = archive_member(version_two[0])
    second = archive_member(version_two[1])
    match case:
        case FixtureCase.VALID_V1 | FixtureCase.OTHER_TARGET:
            version_one = two_files(target, VERSION_ONE)
            return matching_archive(target, source_revision, version_one)
        case FixtureCase.VALID_V2:
            return matching_archive(target, source_revision, version_two)
        case (
            FixtureCase.PROGRAM_VALID
            | FixtureCase.PROGRAM_WRONG_SOURCE
            | FixtureCase.PROGRAM_WRONG_SIGNER
            | FixtureCase.PROGRAM_WRONG_ARCHIVE
            | FixtureCase.PROGRAM_WRONG_MEMBER
            | FixtureCase.PROGRAM_COLLISION
        ):
            program = program_manifest(case, target, program_member_digest)
            return assembled_archive(
                target, source_revision, entries, [first, second], [program]
            )
        case FixtureCase.RECOVERY_V1:
            recovery_one = recovery_files(target, VERSION_ONE)
            return matching_archive(target, source_revision, recovery_one)
        case FixtureCase.RECOVERY_V2:
            recovery_two = recovery_files(target, VERSION_TWO)
            return matching_archive(target, source_revision, recovery_two)
        case FixtureCase.DIGEST:
            corrupted = ArchiveMember(
                name=first.name, content=BAD_CONTENT, mode=first.mode
            )
            return assembled_archive(
                target, source_revision, entries, [corrupted, second]
            )
        case FixtureCase.SIZE:
            enlarged_entries = [enlarged(entries[0]), entries[1]]
            return assembled_archive(
                target, source_revision, enlarged_entries, [first, second]
            )
        case FixtureCase.SYMLINK:
            symbolic = LinkMember(
                name=first.name,
                kind=LinkKind.SYMBOLIC,
                link_name=version_two[1].path,
                mode=first.mode,
            )
            return assembled_archive(target, source_revision, entries, [symbolic, second])
        case FixtureCase.HARDLINK:
            hard = LinkMember(
                name=first.name,
                kind=LinkKind.HARD,
                link_name=second.name,
                mode=first.mode,
            )
            return assembled_archive(target, source_revision, entries, [hard, second])
        case FixtureCase.TRAVERSAL:
            traversal_name = TRAVERSAL_MEMBER_PREFIX + destination(
                target, TRAVERSAL_FILE_NAME
            )
            traversal = defect_member(traversal_name)
            return assembled_archive(
                target, source_revision, entries, [first, second, traversal]
            )
        case FixtureCase.ABSOLUTE:
            absolute = defect_member(destination(target, ABSOLUTE_FILE_NAME))
            return assembled_archive(
                target, source_revision, entries, [first, second, absolute]
            )
        case FixtureCase.EXTRA:
            extra = defect_member(member_name(destination(target, EXTRA_FILE_NAME)))
            return assembled_archive(
                target, source_revision, entries, [first, second, extra]
            )
        case FixtureCase.MISSING:
            return assembled_archive(target, source_revision, entries, [second])
        case FixtureCase.DUPLICATE:
            return assembled_archive(
                target, source_revision, entries, [first, second, first]
            )
        case FixtureCase.MEMBER_MODE:
            special = ArchiveMember(
                name=first.name, content=first.content, mode=SPECIAL_MEMBER_MODE
            )
            return assembled_archive(target, source_revision, entries, [special, second])


def link_type(kind: LinkKind) -> bytes:
    if kind == LinkKind.SYMBOLIC:
        return tarfile.SYMTYPE
    return tarfile.LNKTYPE


def member_header(name: str, mode: int) -> tarfile.TarInfo:
    info = tarfile.TarInfo(name)
    info.mode = mode
    info.mtime = ARCHIVE_TIME
    info.uid = ROOT_ID
    info.gid = ROOT_ID
    info.uname = EMPTY_NAME
    info.gname = EMPTY_NAME
    return info


def write_link_archive(path: Path, members: Sequence[FixtureMember]) -> None:
    with (
        path.open("wb") as raw_file,
        gzip.GzipFile(
            filename=EMPTY_NAME,
            mode="wb",
            fileobj=raw_file,
            mtime=ARCHIVE_TIME,
            compresslevel=COMPRESS_LEVEL,
        ) as gzip_file,
        tarfile.open(fileobj=gzip_file, mode="w", format=tarfile.USTAR_FORMAT) as archive,
    ):
        for member in members:
            info = member_header(member.name, member.mode)
            if isinstance(member, LinkMember):
                info.type = link_type(member.kind)
                info.linkname = member.link_name
                archive.addfile(info)
            else:
                info.type = tarfile.REGTYPE
                info.size = len(member.content)
                archive.addfile(info, io.BytesIO(member.content))


def check_result_field(field: str, actual: str, expected: str) -> None:
    if actual != expected:
        raise BuildError(
            f"Program result field {field} contains {actual}, but the pinned "
            f"opnsensectl release uses {expected}"
        )


def fixture_program() -> ProgramEntry:
    return ProgramEntry(
        name=PROGRAM_IDENTIFIER,
        repository=PROGRAM_REPOSITORY,
        asset=PROGRAM_ASSET,
        member=PROGRAM_MEMBER,
        path=destination(TARGET_A, PROGRAM_IDENTIFIER),
        mode=PROGRAM_MODE,
        owner=ROOT_ACCOUNT,
        group=ROOT_ACCOUNT,
    )


def load_program_input(
    case: FixtureCase, result_path: Path | None
) -> ProgramInput | None:
    if result_path is None:
        if case in PROGRAM_CASES:
            raise BuildError(
                f"{case.value} is a program case and requires --program-result"
            )
        return None
    if case not in PROGRAM_CASES:
        raise BuildError(
            f"{case.value} is not a program case and does not accept --program-result"
        )
    result = read_program_result(result_path)
    check_result_field("name", result.name, PROGRAM_IDENTIFIER)
    check_result_field("release_tag", result.release_tag, PROGRAM_RELEASE_TAG)
    check_result_field("asset", result.asset, PROGRAM_ASSET)
    check_result_field("sha256", result.sha256, PROGRAM_ARCHIVE_SHA256)
    check_result_field("source_revision", result.source_revision, PROGRAM_SOURCE_REVISION)
    check_result_field("signer_revision", result.signer_revision, PROGRAM_SIGNER_REVISION)
    verified = verify_program(fixture_program(), result)
    return ProgramInput(
        bundle=verified.bundle_content,
        member_sha256=verified.manifest.member_sha256,
    )


def write_fixture_bundle(
    case: FixtureCase,
    target: FixtureTarget,
    program_input: ProgramInput,
    output_dir: Path,
) -> None:
    bundle_asset = f"{PROGRAM_BUNDLE_PREFIX}{PROGRAM_IDENTIFIER}{PROGRAM_BUNDLE_SUFFIX}"
    verified = VerifiedProgram(
        manifest=program_manifest(case, target, program_input.member_sha256),
        bundle_asset=bundle_asset,
        bundle_content=program_input.bundle,
    )
    write_program_bundles(output_dir, [verified])


def build_fixture(
    case: FixtureCase,
    source_revision: str,
    output_dir: Path,
    program_input: ProgramInput | None,
) -> BuildResult:
    check_revision(source_revision)
    member_digest = EMPTY_NAME
    if program_input is not None:
        member_digest = program_input.member_sha256
    fixture = fixture_archive(case, source_revision, member_digest)
    identifier = target_identifier(fixture.target.workspace)
    asset_stem = ASSET_FIELD_SEPARATOR.join((FIXTURE_ASSET_LABEL, case.value, identifier))
    asset = f"{ASSET_NAME_PREFIX}{asset_stem}{ASSET_NAME_SUFFIX}"
    output_dir.mkdir(parents=True, exist_ok=True)
    archive_path = output_dir / asset
    regular_members = [
        item for item in fixture.members if isinstance(item, ArchiveMember)
    ]
    if len(regular_members) == len(fixture.members):
        write_archive(archive_path, regular_members)
    else:
        write_link_archive(archive_path, fixture.members)
    if program_input is not None:
        write_fixture_bundle(case, fixture.target, program_input, output_dir)
    archive_bytes = archive_path.read_bytes()
    return BuildResult(
        archive=str(archive_path),
        asset=asset,
        sha256=hashlib.sha256(archive_bytes).hexdigest(),
        size=len(archive_bytes),
    )


def check_expected_workspace(case: FixtureCase, expected_workspace: str) -> None:
    workspace = case_target(case).workspace
    if expected_workspace != workspace:
        raise BuildError(
            f"case {case.value} uses workspace {workspace!r}, but "
            f"--expected-workspace is {expected_workspace!r}"
        )


def parse_arguments(arguments: list[str]) -> Arguments:
    parser = argparse.ArgumentParser(prog=PROGRAM_NAME, allow_abbrev=False)
    parser.add_argument(
        "--case", required=True, choices=[item.value for item in FixtureCase]
    )
    parser.add_argument("--expected-workspace", required=True)
    parser.add_argument("--source-revision", required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--program-result", type=Path, default=None)
    parsed = parser.parse_args(arguments)
    return Arguments.model_validate(vars(parsed))


def main(arguments: list[str]) -> int:
    parsed = parse_arguments(arguments)
    case = parsed.case
    try:
        check_expected_workspace(case, parsed.expected_workspace)
        program_input = load_program_input(case, parsed.program_result)
        result = build_fixture(
            case, parsed.source_revision, parsed.output_dir, program_input
        )
    except BuildError as error:
        print(f"{PROGRAM_NAME}: {error}", file=sys.stderr)
        return FAILURE_EXIT_CODE
    print(result.model_dump_json())
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
