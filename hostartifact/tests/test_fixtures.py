from __future__ import annotations

import gzip
import hashlib
import json
import os
import shutil
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
PROGRAM_SCHEMA_VERSION = 2
PROGRAM_NAME = "opnsensectl"
PROGRAM_REPOSITORY = "agoodkind/opnsensectl"
PROGRAM_RELEASE_TAG = "202609190337-7-7db20fb"
PROGRAM_ASSET = "opnsensectl_linux_amd64.tar.gz"
PROGRAM_ARCHIVE_SHA256 = (
    "29e65cb0cc7b7924478416ab98d7c22e8d3e1679e7cbcfdbff42323fde874399"
)
PROGRAM_SOURCE_REVISION = "7db20fb56fa01fa41bbb9ebeb03f29c281bce24a"
PROGRAM_SIGNER_REVISION = "6f29e0969c7c302aee9c1a23b3424c91f8c7796d"
PROGRAM_PATH = f"{DIRECTORY_A}/opnsensectl"
FIXTURE_PROGRAM_RESULT_ENV = "HOSTARTIFACT_FIXTURE_PROGRAM_RESULT"
TAMPERED_ARCHIVE_SUFFIX = b"tampered"
PROGRAM_BUNDLE_ASSET = "hostconfig_program-opnsensectl.sigstore.jsonl"
PROGRAM_BUNDLES_FILE = "program-bundles.json"
WRONG_REVISION = "1" * 40
WRONG_DIGEST = "0" * 64
LEGACY_TAR_DIGESTS = {
    VALID_ONE_CASE: "549d0949df1fd8d74c9ccf40cde59e43eab7e18d14793968e28426c5555a9ea5",
    "valid-v2": "dd4d1fc28092ef71160bfcb7fc37fec9caa06468e155872fc5e095131f7eab3c",
    OTHER_TARGET_CASE: "e4d1ec6d766a282d5c97a3dfb678a4c3447339a2efe0b0feb8551e9b5ad9bec4",
    "digest": "5b96d24c69b207b614a7c53ae35db49d3d3153b98ea4f67c7a932fcea9b43fd9",
    "size": "068a57d1540b5aaa91400850659d3581f84769e319696b711c9962019fa7d1a7",
    "symlink": "41c5a548aa89e436cd3744083060541e9f1309bacab9ae73d5a732ca8518ef85",
    "hardlink": "ba77f28e80968b65ad3a3788af7169db4f73f979b49724f532b390643051c18b",
    "traversal": "83f9c34ae837fe838bf9b65a774629ecb5381c6ba6cc41deb0030f2382623f14",
    "absolute": "3232c8133eb1af74434dcbca11a485c8eef0876727e866be5a779eddfcb70cfd",
    "extra": "4fa87de4e713a74431549b9533c595bcc3e575933a4aaefb9463c21f0c26479b",
    "missing": "da4d2998bd169aa50b10ca784ce4988dbc07352623554756af198e15151c014c",
    "duplicate": "00f893a302f5a34c3eabe1496e5249a64e24caa7544e65bf7764462d5e9714bc",
    "member-mode": "8c47471b73c658df8f47eb13f4215fb63575675fcecfe6c19c0c833e70c68b7d",
    "recovery-v1": "6bba5edf498a695095a425487f9534e7b9ca33bcd874ce3df37756d606f13e14",
    "recovery-v2": "a94689edd13445c63dbef7cff3e993f72b20c46353f2b684f71645a2a642f99d",
}
LEGACY_CASES = tuple(LEGACY_TAR_DIGESTS)
PROGRAM_CASES = (
    "program-valid",
    "program-wrong-source",
    "program-wrong-signer",
    "program-wrong-archive",
    "program-wrong-member",
    "program-collision",
)
ALL_CASES = (*LEGACY_CASES, *PROGRAM_CASES)

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


def qualified_program_result() -> JsonObject:
    result_path = os.environ.get(FIXTURE_PROGRAM_RESULT_ENV)
    if result_path is None:
        pytest.fail(
            f"{FIXTURE_PROGRAM_RESULT_ENV} is required. Run make -C hostartifact test "
            "or make -C hostartifact check from the repository root.",
            pytrace=False,
        )
    path = Path(result_path)
    return as_object(json.loads(path.read_text(encoding="utf-8")))


def qualified_program_bundle() -> bytes:
    result = qualified_program_result()
    return Path(as_text(result["bundle"])).read_bytes()


def qualified_member_sha256() -> str:
    result = qualified_program_result()
    members = read_members(Path(as_text(result["archive"])))
    member = next(item for item in members if item.name == PROGRAM_NAME)
    return hashlib.sha256(member.content).hexdigest()


def write_program_result(directory: Path, overrides: JsonObject) -> Path:
    directory.mkdir(parents=True, exist_ok=True)
    result = qualified_program_result()
    archive = directory / PROGRAM_ASSET
    bundle = directory / "bundle.jsonl"
    shutil.copyfile(as_text(result["archive"]), archive)
    shutil.copyfile(as_text(result["bundle"]), bundle)
    result["archive"] = str(archive)
    result["bundle"] = str(bundle)
    result.update(overrides)
    result_file = directory / "result.json"
    result_file.write_text(json.dumps(result), encoding="utf-8")
    return result_file


def fixture_arguments(case: str, output_dir: Path) -> list[str]:
    return [
        "--case",
        case,
        "--expected-workspace",
        case_workspace(case),
        "--source-revision",
        SOURCE_REVISION,
        "--output-dir",
        str(output_dir),
    ]


def build(case: str, output_dir: Path) -> JsonObject:
    arguments = fixture_arguments(case, output_dir)
    if case in PROGRAM_CASES:
        result_file = write_program_result(output_dir.parent / "program-input", {})
        arguments.extend(["--program-result", str(result_file)])
    built = run_fixture(*arguments)
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


@pytest.mark.parametrize("case", LEGACY_CASES)
def test_legacy_case_keeps_its_tar_bytes(tmp_path: Path, case: str) -> None:
    result = build(case, tmp_path)

    archive_bytes = Path(as_text(result["archive"])).read_bytes()
    tar_digest = hashlib.sha256(gzip.decompress(archive_bytes)).hexdigest()
    assert tar_digest == LEGACY_TAR_DIGESTS[case]


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


def expected_program(overrides: JsonObject) -> JsonObject:
    program: JsonObject = {
        "name": PROGRAM_NAME,
        "repository": PROGRAM_REPOSITORY,
        "release_tag": PROGRAM_RELEASE_TAG,
        "asset": PROGRAM_ASSET,
        "sha256": PROGRAM_ARCHIVE_SHA256,
        "source_revision": PROGRAM_SOURCE_REVISION,
        "signer_revision": PROGRAM_SIGNER_REVISION,
        "member": PROGRAM_NAME,
        "member_sha256": qualified_member_sha256(),
        "path": PROGRAM_PATH,
        "mode": "0755",
        "owner": ROOT_ACCOUNT,
        "group": ROOT_ACCOUNT,
    }
    program.update(overrides)
    return program


@pytest.mark.parametrize(
    ("case", "overrides"),
    [
        ("program-valid", {}),
        ("program-wrong-source", {"source_revision": WRONG_REVISION}),
        ("program-wrong-signer", {"signer_revision": WRONG_REVISION}),
        ("program-wrong-archive", {"sha256": WRONG_DIGEST}),
        ("program-wrong-member", {"member_sha256": WRONG_DIGEST}),
        ("program-collision", {"path": f"{DIRECTORY_A}/{FIRST_FILE}"}),
    ],
)
def test_program_case_differs_from_program_valid_only_by_its_field(
    tmp_path: Path, case: str, overrides: JsonObject
) -> None:
    result = build(case, tmp_path)

    members = read_members(Path(as_text(result["archive"])))
    manifest = read_manifest(members[0])
    expected = expected_manifest(TARGET_A, DIRECTORY_A, VERSION_TWO_FILES, NO_SIZE_CHANGE)
    expected["schema_version"] = PROGRAM_SCHEMA_VERSION
    expected_programs: list[JsonValue] = [expected_program(overrides)]
    expected["programs"] = expected_programs
    assert manifest == expected
    assert members[1:] == [FIRST, SECOND]


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


@pytest.mark.parametrize("case", PROGRAM_CASES)
def test_program_case_publishes_the_verified_bundle(tmp_path: Path, case: str) -> None:
    result = build(case, tmp_path / "output")

    output_dir = tmp_path / "output"
    assert sorted(path.name for path in output_dir.iterdir()) == sorted(
        [as_text(result["asset"]), PROGRAM_BUNDLES_FILE, PROGRAM_BUNDLE_ASSET]
    )
    bundle_bytes = (output_dir / PROGRAM_BUNDLE_ASSET).read_bytes()
    assert bundle_bytes == qualified_program_bundle()
    index = json.loads((output_dir / PROGRAM_BUNDLES_FILE).read_text(encoding="utf-8"))
    assert index == {
        PROGRAM_NAME: {
            "asset": PROGRAM_BUNDLE_ASSET,
            "sha256": hashlib.sha256(bundle_bytes).hexdigest(),
        }
    }


def test_legacy_case_writes_no_bundle_files(tmp_path: Path) -> None:
    result = build(VALID_ONE_CASE, tmp_path / "output")

    names = [path.name for path in (tmp_path / "output").iterdir()]
    assert names == [as_text(result["asset"])]


@pytest.mark.parametrize("case", PROGRAM_CASES)
def test_program_case_without_a_result_creates_no_output(
    tmp_path: Path, case: str
) -> None:
    output_dir = tmp_path / "output"

    rejected = run_fixture(*fixture_arguments(case, output_dir))

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stdout == ""
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert "--program-result" in rejected.stderr
    assert not output_dir.exists()


def test_legacy_case_with_a_result_creates_no_output(tmp_path: Path) -> None:
    output_dir = tmp_path / "output"
    result_file = write_program_result(tmp_path / "input", {})

    rejected = run_fixture(
        *fixture_arguments(VALID_ONE_CASE, output_dir),
        "--program-result",
        str(result_file),
    )

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert not output_dir.exists()


@pytest.mark.parametrize(
    "overrides",
    [
        pytest.param({"release_tag": "other-tag"}, id="release-tag"),
        pytest.param({"asset": "other.tar.gz"}, id="asset"),
        pytest.param({"sha256": WRONG_DIGEST}, id="archive-digest"),
        pytest.param({"source_revision": WRONG_REVISION}, id="source-revision"),
        pytest.param({"signer_revision": WRONG_REVISION}, id="signer-revision"),
        pytest.param({"bundle": "absent-bundle.jsonl"}, id="missing-bundle"),
    ],
)
def test_program_result_that_differs_from_the_pinned_release_creates_no_output(
    tmp_path: Path, overrides: JsonObject
) -> None:
    output_dir = tmp_path / "output"
    result_file = write_program_result(tmp_path / "input", overrides)

    rejected = run_fixture(
        *fixture_arguments("program-valid", output_dir),
        "--program-result",
        str(result_file),
    )

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert not output_dir.exists()


def test_tampered_program_archive_creates_no_output(tmp_path: Path) -> None:
    output_dir = tmp_path / "output"
    result_file = write_program_result(tmp_path / "input", {})
    archive_path = tmp_path / "input" / PROGRAM_ASSET
    with archive_path.open("ab") as archive:
        archive.write(TAMPERED_ARCHIVE_SUFFIX)

    rejected = run_fixture(
        *fixture_arguments("program-valid", output_dir),
        "--program-result",
        str(result_file),
    )

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stdout == ""
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert str(archive_path) in rejected.stderr
    assert "has SHA-256" in rejected.stderr
    assert not output_dir.exists()


def test_program_result_with_an_empty_bundle_creates_no_output(tmp_path: Path) -> None:
    output_dir = tmp_path / "output"
    result_file = write_program_result(tmp_path / "input", {})
    (tmp_path / "input" / "bundle.jsonl").write_bytes(b"")

    rejected = run_fixture(
        *fixture_arguments("program-valid", output_dir),
        "--program-result",
        str(result_file),
    )

    assert rejected.returncode == REJECTED_INPUT_EXIT_CODE
    assert rejected.stderr.startswith(ERROR_PREFIX)
    assert not output_dir.exists()


def test_legacy_rebuild_removes_only_previous_program_companions(tmp_path: Path) -> None:
    output = tmp_path / "output"
    first = build("program-valid", output)
    unrelated = output / "keep.txt"
    unrelated.write_bytes(BAD_CONTENT)

    rebuilt = build(VALID_ONE_CASE, output)

    assert sorted(path.name for path in output.iterdir()) == sorted(
        [as_text(first["asset"]), as_text(rebuilt["asset"]), unrelated.name]
    )
    assert unrelated.read_bytes() == BAD_CONTENT
