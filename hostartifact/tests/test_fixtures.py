from __future__ import annotations

import gzip
import hashlib
import json
import subprocess
import sys
import tarfile
from pathlib import Path

import pytest
from pydantic import BaseModel, ConfigDict, JsonValue

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
GENERATOR = REPOSITORY_ROOT / "hostartifact/tests/build_fixture.py"
SOURCE_REVISION = "0123456789abcdef0123456789abcdef01234567"
UPPERCASE_REVISION = "0123456789ABCDEF0123456789ABCDEF01234567"
SHORT_REVISION = SOURCE_REVISION[:-1]
EMPTY_REVISION = ""
CONFIGS_REPOSITORY = "agoodkind/configs"
TARGET_A = "suburban/hostartifact-acceptance"
TARGET_B = "suburban/hostartifact-acceptance-b"
NORMAL_WORKSPACE = "suburban/mwan"
DIRECTORY_A = "/srv/hostartifact-acceptance"
DIRECTORY_B = "/srv/hostartifact-acceptance-b"
NODE = "hypervisor"
ENVIRONMENT = "testbed"
ROOT_ACCOUNT = "root"
DIRECTORY_MODE = "0755"
SCHEMA_VERSION = 1
MANIFEST_MEMBER = "manifest.json"
MANIFEST_MODE = 0o644
FILES_PREFIX = "files/"
OCTAL_BASE = 8
PERMISSION_BITS = 0o7777
FIRST_FILE = "a.conf"
SECOND_FILE = "b.conf"
FIRST_LABEL = "a"
SECOND_LABEL = "b"
FIRST_MODE = "0644"
SECOND_MODE = "0640"
VERSION_ONE = "one"
VERSION_TWO = "two"
BAD_CONTENT = b"a bad\n"
DEFECT_CONTENT = b"x\n"
DEFECT_MODE = 0o644
SPECIAL_MODE = 0o4755
SIZE_INCREASE = 1
NO_SIZE_CHANGE = 0
TRAVERSAL_MEMBER = "files/../srv/hostartifact-acceptance/traversal.conf"
ABSOLUTE_MEMBER = "/srv/hostartifact-acceptance/absolute.conf"
EXTRA_MEMBER = "files/srv/hostartifact-acceptance/extra.conf"
RECOVERY_FILE_COUNT = 300
MEMBER_LIMIT = 512
ROOT_ID = 0
ARCHIVE_TIME = 0
EMPTY_NAME = ""
INVALID_ARGUMENTS_EXIT_CODE = 2
REJECTED_INPUT_EXIT_CODE = 1
ERROR_PREFIX = "build_fixture: "
GZIP_FLAGS_OFFSET = 3
GZIP_FILENAME_FLAG = 0x08
GZIP_MTIME = slice(4, 8)
ZERO_MTIME = b"\0\0\0\0"
USTAR_MAGIC = slice(257, 265)
USTAR_MAGIC_VALUE = b"ustar\x0000"
NO_LINK = ""
NO_CONTENT = b""
OTHER_TARGET_CASE = "other-target"
VALID_ONE_CASE = "valid-v1"
ALL_CASES = (
    VALID_ONE_CASE,
    "valid-v2",
    OTHER_TARGET_CASE,
    "digest",
    "size",
    "symlink",
    "hardlink",
    "traversal",
    "absolute",
    "extra",
    "missing",
    "duplicate",
    "member-mode",
    "recovery-v1",
    "recovery-v2",
)

type JsonObject = dict[str, JsonValue]


class ExpectedFile(BaseModel):
    model_config = ConfigDict(frozen=True)

    path: str
    content: bytes
    mode: str


class TarMember(BaseModel):
    model_config = ConfigDict(frozen=True)

    name: str
    type: bytes
    link_name: str
    mode: int
    content: bytes


def as_object(value: JsonValue) -> JsonObject:
    assert isinstance(value, dict)
    return value


def as_text(value: JsonValue) -> str:
    assert isinstance(value, str)
    return value


def case_workspace(case: str) -> str:
    if case == OTHER_TARGET_CASE:
        return TARGET_B
    return TARGET_A


def run_fixture(*arguments: str) -> subprocess.CompletedProcess[str]:
    command = [sys.executable, str(GENERATOR), *arguments]
    return subprocess.run(command, capture_output=True, text=True, check=False)


def build(case: str, output_dir: Path) -> JsonObject:
    built = run_fixture(
        "--case",
        case,
        "--expected-workspace",
        case_workspace(case),
        "--source-revision",
        SOURCE_REVISION,
        "--output-dir",
        str(output_dir),
    )
    assert built.returncode == 0, built.stderr
    return as_object(json.loads(built.stdout))


def regular(name: str, content: bytes, mode: int) -> TarMember:
    return TarMember(
        name=name, type=tarfile.REGTYPE, link_name=NO_LINK, mode=mode, content=content
    )


def file_member(expected: ExpectedFile) -> TarMember:
    name = FILES_PREFIX + expected.path.lstrip("/")
    return regular(name, expected.content, int(expected.mode, OCTAL_BASE))


def labeled_file(
    directory: str, file_name: str, label: str, version: str, mode: str
) -> ExpectedFile:
    content = f"{label} {version}\n".encode()
    return ExpectedFile(path=f"{directory}/{file_name}", content=content, mode=mode)


def two_files(directory: str, version: str) -> list[ExpectedFile]:
    first = labeled_file(directory, FIRST_FILE, FIRST_LABEL, version, FIRST_MODE)
    second = labeled_file(directory, SECOND_FILE, SECOND_LABEL, version, SECOND_MODE)
    return [first, second]


def recovery_files(version: str) -> list[ExpectedFile]:
    files: list[ExpectedFile] = []
    for index in range(RECOVERY_FILE_COUNT):
        number = f"{index:03d}"
        path = f"{DIRECTORY_A}/recovery-{number}.conf"
        content = f"{version} {number}\n".encode()
        files.append(ExpectedFile(path=path, content=content, mode=FIRST_MODE))
    return files


def manifest_entry(expected: ExpectedFile, size_increase: int) -> JsonObject:
    return {
        "member": FILES_PREFIX + expected.path.lstrip("/"),
        "path": expected.path,
        "size": len(expected.content) + size_increase,
        "sha256": hashlib.sha256(expected.content).hexdigest(),
        "mode": expected.mode,
        "owner": ROOT_ACCOUNT,
        "group": ROOT_ACCOUNT,
    }


def expected_manifest(
    workspace: str, directory: str, files: list[ExpectedFile], first_size_increase: int
) -> JsonObject:
    entries: list[JsonValue] = [manifest_entry(files[0], first_size_increase)]
    entries.extend(manifest_entry(item, NO_SIZE_CHANGE) for item in files[1:])
    owned_directory: JsonObject = {
        "path": directory,
        "mode": DIRECTORY_MODE,
        "owner": ROOT_ACCOUNT,
        "group": ROOT_ACCOUNT,
    }
    return {
        "schema_version": SCHEMA_VERSION,
        "repository": CONFIGS_REPOSITORY,
        "source_revision": SOURCE_REVISION,
        "workspace": workspace,
        "node": NODE,
        "environment": ENVIRONMENT,
        "directories": [owned_directory],
        "files": entries,
    }


def read_headers(archive_path: Path) -> list[tarfile.TarInfo]:
    with tarfile.open(archive_path, mode="r:gz") as archive:
        return archive.getmembers()


def read_members(archive_path: Path) -> list[TarMember]:
    observed: list[TarMember] = []
    with tarfile.open(archive_path, mode="r:gz") as archive:
        for member in archive.getmembers():
            content = NO_CONTENT
            if member.isreg():
                extracted = archive.extractfile(member)
                assert extracted is not None
                content = extracted.read()
            else:
                assert member.size == 0
            observed.append(
                TarMember(
                    name=member.name,
                    type=member.type,
                    link_name=member.linkname,
                    mode=member.mode & PERMISSION_BITS,
                    content=content,
                )
            )
    return observed


def read_manifest(member: TarMember) -> JsonObject:
    header = (member.name, member.type, member.link_name, member.mode)
    assert header == (MANIFEST_MEMBER, tarfile.REGTYPE, NO_LINK, MANIFEST_MODE)
    return as_object(json.loads(member.content))


@pytest.mark.parametrize("case", ALL_CASES)
def test_each_case_builds_identical_bytes_with_fixed_metadata(
    tmp_path: Path, case: str
) -> None:
    first = build(case, tmp_path / "first")
    second = build(case, tmp_path / "second")

    archive_path = Path(as_text(first["archive"]))
    archive_bytes = archive_path.read_bytes()
    archive_digest = hashlib.sha256(archive_bytes).hexdigest()
    identifier = hashlib.sha256(case_workspace(case).encode()).hexdigest()
    asset = f"hostconfig_fixture-{case}-{identifier}.tar.gz"
    assert first["asset"] == second["asset"] == asset
    assert archive_path == tmp_path / "first" / asset
    assert first["sha256"] == second["sha256"] == archive_digest
    assert first["size"] == len(archive_bytes)
    assert Path(as_text(second["archive"])).read_bytes() == archive_bytes
    assert archive_bytes[GZIP_FLAGS_OFFSET] & GZIP_FILENAME_FLAG == 0
    assert archive_bytes[GZIP_MTIME] == ZERO_MTIME
    assert gzip.decompress(archive_bytes)[USTAR_MAGIC] == USTAR_MAGIC_VALUE
    headers = read_headers(archive_path)
    assert len(headers) <= MEMBER_LIMIT
    for header in headers:
        assert (header.uid, header.gid, header.mtime) == (ROOT_ID, ROOT_ID, ARCHIVE_TIME)
        assert (header.uname, header.gname) == (EMPTY_NAME, EMPTY_NAME)


@pytest.mark.parametrize(
    ("case", "workspace", "directory", "version"),
    [
        (VALID_ONE_CASE, TARGET_A, DIRECTORY_A, VERSION_ONE),
        ("valid-v2", TARGET_A, DIRECTORY_A, VERSION_TWO),
        (OTHER_TARGET_CASE, TARGET_B, DIRECTORY_B, VERSION_ONE),
    ],
)
def test_valid_case_contains_exactly_its_two_files(
    tmp_path: Path, case: str, workspace: str, directory: str, version: str
) -> None:
    files = two_files(directory, version)

    result = build(case, tmp_path)

    members = read_members(Path(as_text(result["archive"])))
    manifest = read_manifest(members[0])
    assert manifest == expected_manifest(workspace, directory, files, NO_SIZE_CHANGE)
    assert members[1:] == [file_member(item) for item in files]


VERSION_TWO_FILES = two_files(DIRECTORY_A, VERSION_TWO)
FIRST = file_member(VERSION_TWO_FILES[0])
SECOND = file_member(VERSION_TWO_FILES[1])


def defect(name: str) -> TarMember:
    return regular(name, DEFECT_CONTENT, DEFECT_MODE)


def link(link_type: bytes, link_name: str) -> TarMember:
    return TarMember(
        name=FIRST.name,
        type=link_type,
        link_name=link_name,
        mode=FIRST.mode,
        content=NO_CONTENT,
    )


@pytest.mark.parametrize(
    ("case", "size_increase", "file_members"),
    [
        (
            "digest",
            NO_SIZE_CHANGE,
            [regular(FIRST.name, BAD_CONTENT, FIRST.mode), SECOND],
        ),
        ("size", SIZE_INCREASE, [FIRST, SECOND]),
        (
            "symlink",
            NO_SIZE_CHANGE,
            [link(tarfile.SYMTYPE, VERSION_TWO_FILES[1].path), SECOND],
        ),
        ("hardlink", NO_SIZE_CHANGE, [link(tarfile.LNKTYPE, SECOND.name), SECOND]),
        ("traversal", NO_SIZE_CHANGE, [FIRST, SECOND, defect(TRAVERSAL_MEMBER)]),
        ("absolute", NO_SIZE_CHANGE, [FIRST, SECOND, defect(ABSOLUTE_MEMBER)]),
        ("extra", NO_SIZE_CHANGE, [FIRST, SECOND, defect(EXTRA_MEMBER)]),
        ("missing", NO_SIZE_CHANGE, [SECOND]),
        ("duplicate", NO_SIZE_CHANGE, [FIRST, SECOND, FIRST]),
        (
            "member-mode",
            NO_SIZE_CHANGE,
            [regular(FIRST.name, FIRST.content, SPECIAL_MODE), SECOND],
        ),
    ],
)
def test_defect_case_differs_from_valid_v2_only_by_its_defect(
    tmp_path: Path, case: str, size_increase: int, file_members: list[TarMember]
) -> None:
    result = build(case, tmp_path)

    members = read_members(Path(as_text(result["archive"])))
    manifest = read_manifest(members[0])
    assert manifest == expected_manifest(
        TARGET_A, DIRECTORY_A, VERSION_TWO_FILES, size_increase
    )
    assert members[1:] == file_members


@pytest.mark.parametrize(
    ("case", "version"), [("recovery-v1", VERSION_ONE), ("recovery-v2", VERSION_TWO)]
)
def test_recovery_case_contains_three_hundred_target_a_files(
    tmp_path: Path, case: str, version: str
) -> None:
    files = recovery_files(version)

    result = build(case, tmp_path)

    members = read_members(Path(as_text(result["archive"])))
    manifest = read_manifest(members[0])
    assert manifest == expected_manifest(TARGET_A, DIRECTORY_A, files, NO_SIZE_CHANGE)
    assert len(members) == RECOVERY_FILE_COUNT + 1
    assert members[1:] == [file_member(item) for item in files]


@pytest.mark.parametrize(
    ("arguments", "exit_code"),
    [
        pytest.param(
            ["--case", "unknown", "--expected-workspace", TARGET_A],
            INVALID_ARGUMENTS_EXIT_CODE,
            id="unknown-case",
        ),
        pytest.param(
            ["--case", VALID_ONE_CASE, "--expected-workspace", TARGET_A, "--node", NODE],
            INVALID_ARGUMENTS_EXIT_CODE,
            id="identity-override",
        ),
        pytest.param(
            ["--case", OTHER_TARGET_CASE, "--expected-workspace", TARGET_A],
            REJECTED_INPUT_EXIT_CODE,
            id="other-target-with-target-a",
        ),
        pytest.param(
            ["--case", VALID_ONE_CASE, "--expected-workspace", TARGET_B],
            REJECTED_INPUT_EXIT_CODE,
            id="valid-v1-with-target-b",
        ),
        pytest.param(
            ["--case", VALID_ONE_CASE, "--expected-workspace", NORMAL_WORKSPACE],
            REJECTED_INPUT_EXIT_CODE,
            id="normal-workspace",
        ),
    ],
)
def test_rejected_selection_creates_no_output(
    tmp_path: Path, arguments: list[str], exit_code: int
) -> None:
    output_dir = tmp_path / "output"

    rejected = run_fixture(
        *arguments, "--source-revision", SOURCE_REVISION, "--output-dir", str(output_dir)
    )

    assert rejected.returncode == exit_code
    assert rejected.stdout == ""
    assert not output_dir.exists()
    if exit_code == REJECTED_INPUT_EXIT_CODE:
        assert rejected.stderr.startswith(ERROR_PREFIX)


@pytest.mark.parametrize("revision", [UPPERCASE_REVISION, SHORT_REVISION, EMPTY_REVISION])
def test_invalid_source_revision_creates_no_output(tmp_path: Path, revision: str) -> None:
    output_dir = tmp_path / "output"

    rejected = run_fixture(
        "--case",
        VALID_ONE_CASE,
        "--expected-workspace",
        TARGET_A,
        "--source-revision",
        revision,
        "--output-dir",
        str(output_dir),
    )

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stdout == ""
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert not output_dir.exists()
