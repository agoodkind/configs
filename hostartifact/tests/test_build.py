from __future__ import annotations

import copy
import hashlib
import json
import shutil
import subprocess
import sys
import tarfile
import tomllib
from collections.abc import Callable
from pathlib import Path

import pytest
from pydantic import JsonValue

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
RENDER_ROOT = "opentofu/render/host-artifact"
COPIED_PATHS = (
    RENDER_ROOT,
    "opentofu/modules/contracts",
    "opentofu/modules/host-artifact-targets",
    "ansible/inventory/group_vars",
    "mwan/config",
    "opnsense",
    "proxmox/services",
)
IGNORED_COPIES = (".terraform", "*.tfplan", "*.tfstate")
SUBURBAN = "suburban/mwan"
NESTED_SELECTORS = (
    "suburban/simulators-astound",
    "suburban/simulators/astound",
    "suburban-simulators/astound",
)
VAULT_GROUP_FILE = "ansible/inventory/group_vars/vault_servers.yml"
OPNSENSE_TEMPLATE = "opnsense/config.toml.j2"
SOURCE_REVISION = "0123456789abcdef0123456789abcdef01234567"
CONFIGS_REPOSITORY = "agoodkind/configs"
ASSET_PREFIX = "asset:"
MANIFEST_MEMBER = "manifest.json"
FILES_PREFIX = "files/"
PLAN_FILE = "projection.tfplan"
SYNTHETIC_SECRET = "synthetic-secret-marker"
OCTAL_BASE = 8
PERMISSION_BITS = 0o7777

type JsonObject = dict[str, JsonValue]
type Mutation = Callable[[JsonObject], None]


def as_object(value: JsonValue) -> JsonObject:
    assert isinstance(value, dict)
    return value


def as_list(value: JsonValue) -> list[JsonValue]:
    assert isinstance(value, list)
    return value


def as_text(value: JsonValue) -> str:
    assert isinstance(value, str)
    return value


def copy_repository(destination: Path) -> Path:
    for relative in COPIED_PATHS:
        shutil.copytree(
            REPOSITORY_ROOT / relative,
            destination / relative,
            ignore=shutil.ignore_patterns(*IGNORED_COPIES),
        )
    return destination


def run_tofu(repository: Path, *arguments: str) -> subprocess.CompletedProcess[str]:
    command = ["tofu", f"-chdir={repository / RENDER_ROOT}", *arguments]
    return subprocess.run(command, capture_output=True, text=True, check=False)


def plan_projection(repository: Path, workspace: str) -> subprocess.CompletedProcess[str]:
    initialized = run_tofu(repository, "init", "-backend=false", "-input=false")
    assert initialized.returncode == 0, initialized.stderr
    return run_tofu(
        repository,
        "plan",
        "-refresh=false",
        "-lock=false",
        "-input=false",
        "-no-color",
        f"-out={PLAN_FILE}",
        f"-var=workspace={workspace}",
    )


def project(repository: Path, workspace: str) -> JsonObject:
    planned = plan_projection(repository, workspace)
    assert planned.returncode == 0, planned.stderr
    shown = run_tofu(repository, "show", "-json", PLAN_FILE)
    assert shown.returncode == 0, shown.stderr
    plan: JsonValue = json.loads(shown.stdout)
    outputs = as_object(as_object(as_object(plan)["planned_values"])["outputs"])
    return as_object(as_object(outputs["projection"])["value"])


def asset_sources(projection: JsonObject) -> list[tuple[str, str]]:
    sources: list[tuple[str, str]] = []
    for entry in as_list(projection["files"]):
        source = as_text(as_object(entry)["source"])
        if source.startswith(ASSET_PREFIX):
            repository, path = source.removeprefix(ASSET_PREFIX).split(":", 1)
            sources.append((repository, path))
    return sources


def synthetic_asset(repository: str, path: str) -> bytes:
    return f"# synthetic {repository} {path}\n".encode()


def write_assets(directory: Path, projection: JsonObject) -> list[str]:
    roots: dict[str, Path] = {}
    for repository, path in asset_sources(projection):
        if repository == CONFIGS_REPOSITORY:
            continue
        root = directory / repository.replace("/", "-")
        roots[repository] = root
        asset = root / path
        asset.parent.mkdir(parents=True, exist_ok=True)
        asset.write_bytes(synthetic_asset(repository, path))
    arguments: list[str] = []
    for repository, root in sorted(roots.items()):
        arguments.extend(["--asset-root", f"{repository}={root}"])
    return arguments


def run_build(
    directory: Path, repository: Path, projection: JsonObject
) -> subprocess.CompletedProcess[str]:
    projection_file = directory / "projection.json"
    projection_file.write_text(json.dumps(projection), encoding="utf-8")
    command = [
        sys.executable,
        "-m",
        "hostartifact",
        "build",
        "--projection",
        str(projection_file),
        "--repository-root",
        str(repository),
        "--source-revision",
        SOURCE_REVISION,
        *write_assets(directory, projection),
        "--output-dir",
        str(directory / "output"),
    ]
    return subprocess.run(command, capture_output=True, text=True, check=False)


def expected_content(repository: Path, source: str) -> bytes | None:
    if not source.startswith(ASSET_PREFIX):
        return None
    asset_repository, path = source.removeprefix(ASSET_PREFIX).split(":", 1)
    if asset_repository == CONFIGS_REPOSITORY:
        return (repository / path).read_bytes()
    return synthetic_asset(asset_repository, path)


@pytest.fixture(scope="module")
def suburban_repository(tmp_path_factory: pytest.TempPathFactory) -> Path:
    return copy_repository(tmp_path_factory.mktemp("repository"))


@pytest.fixture(scope="module")
def suburban_projection(suburban_repository: Path) -> JsonObject:
    return project(suburban_repository, SUBURBAN)


def test_build_succeeds_while_another_target_lacks_a_group_file(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    (repository / VAULT_GROUP_FILE).unlink()
    projection = project(repository, SUBURBAN)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 0, built.stderr
    result = as_object(json.loads(built.stdout))
    archive_path = Path(as_text(result["archive"]))
    archive_bytes = archive_path.read_bytes()
    assert result["asset"] == "hostconfig_suburban_mwan.tar.gz"
    assert result["sha256"] == hashlib.sha256(archive_bytes).hexdigest()
    assert result["size"] == len(archive_bytes)
    with tarfile.open(archive_path, mode="r:gz") as archive:
        members = archive.getmembers()
        contents: dict[str, bytes] = {}
        member_modes: dict[str, int] = {}
        for member in members:
            assert member.isreg()
            assert (member.uid, member.gid, member.mtime) == (0, 0, 0)
            extracted = archive.extractfile(member)
            assert extracted is not None
            contents[member.name] = extracted.read()
            member_modes[member.name] = member.mode & PERMISSION_BITS
    assert members[0].name == MANIFEST_MEMBER
    manifest = as_object(json.loads(contents[MANIFEST_MEMBER]))
    assert manifest["schema_version"] == 1
    assert manifest["repository"] == CONFIGS_REPOSITORY
    assert manifest["source_revision"] == SOURCE_REVISION
    assert manifest["workspace"] == SUBURBAN
    assert manifest["node"] == projection["node"] == "hypervisor"
    assert manifest["environment"] == projection["environment"] == "testbed"
    assert manifest["directories"] == sorted(
        as_list(projection["directories"]),
        key=lambda item: as_text(as_object(item)["path"]),
    )

    catalog: dict[str, JsonObject] = {}
    for entry in as_list(projection["files"]):
        catalog[as_text(as_object(entry)["destination"])] = as_object(entry)
    manifest_files = [as_object(entry) for entry in as_list(manifest["files"])]
    assert {as_text(entry["path"]) for entry in manifest_files} == set(catalog)
    assert [member.name for member in members[1:]] == [
        as_text(entry["member"]) for entry in manifest_files
    ]
    for entry in manifest_files:
        path = as_text(entry["path"])
        content = contents[as_text(entry["member"])]
        cataloged = catalog[path]
        assert entry["member"] == FILES_PREFIX + path.lstrip("/")
        assert entry["size"] == len(content)
        assert entry["sha256"] == hashlib.sha256(content).hexdigest()
        assert (entry["mode"], entry["owner"], entry["group"]) == (
            cataloged["mode"],
            cataloged["owner"],
            cataloged["group"],
        )
        member_mode = member_modes[as_text(entry["member"])]
        assert member_mode == int(as_text(entry["mode"]), OCTAL_BASE)
        expected = expected_content(repository, as_text(cataloged["source"]))
        if expected is not None:
            assert content == expected

    mwan_config = tomllib.loads(contents["files/etc/mwan/config.toml"].decode())
    assert "smtp2go_api_key" not in mwan_config["email"]
    assert mwan_config["pve"] == {"node": "hypervisor"}
    opnsense_config = tomllib.loads(
        contents["files/etc/opnsensectl/config.toml"].decode()
    )
    assert "smtp2go_api_key" not in opnsense_config["email"]


def test_unknown_workspace_fails_with_known_selectors(suburban_repository: Path) -> None:
    planned = plan_projection(suburban_repository, "suburban/absent")

    assert planned.returncode != 0
    assert "suburban/mwan, vault/mwan" in planned.stderr


def remove_required_variable(projection: JsonObject) -> None:
    del as_object(projection["vars"])["mwan_email_from"]


def add_vault_variable(projection: JsonObject) -> None:
    as_object(projection["vars"])["vault_smtp2go_api_key"] = SYNTHETIC_SECRET


def conflict_node(projection: JsonObject) -> None:
    projection["node"] = "vault"


def use_underscore_selector(projection: JsonObject) -> None:
    projection["workspace"] = "suburban/mwan_extra"


def duplicate_destination(projection: JsonObject) -> None:
    files = as_list(projection["files"])
    files.append(copy.deepcopy(files[0]))


def point_at_missing_asset(projection: JsonObject) -> None:
    for entry in as_list(projection["files"]):
        file_entry = as_object(entry)
        if as_text(file_entry["destination"]).endswith("mwan-opnsense-drain.service"):
            file_entry["source"] = (
                "asset:agoodkind/configs:proxmox/services/absent.service"
            )


@pytest.mark.parametrize(
    ("mutation", "expected_text"),
    [
        pytest.param(remove_required_variable, "mwan_email_from", id="missing-variable"),
        pytest.param(add_vault_variable, "vault_smtp2go_api_key", id="vault-variable"),
        pytest.param(conflict_node, "mwan_proxmox_node", id="conflicting-node"),
        pytest.param(use_underscore_selector, "suburban/mwan_extra", id="underscore"),
        pytest.param(duplicate_destination, "/etc/mwan/config.toml", id="duplicate"),
        pytest.param(point_at_missing_asset, "absent.service", id="missing-asset"),
    ],
)
def test_build_rejects_invalid_projection(
    tmp_path: Path,
    suburban_repository: Path,
    suburban_projection: JsonObject,
    mutation: Mutation,
    expected_text: str,
) -> None:
    projection = copy.deepcopy(suburban_projection)
    mutation(projection)

    built = run_build(tmp_path, suburban_repository, projection)

    assert built.returncode != 0
    assert expected_text in built.stderr
    assert SYNTHETIC_SECRET not in built.stderr
    assert not (tmp_path / "output").exists()


def test_nested_selectors_keep_their_identity(
    tmp_path: Path, suburban_repository: Path, suburban_projection: JsonObject
) -> None:
    assets: dict[str, str] = {}
    for index, selector in enumerate(NESTED_SELECTORS):
        projection = copy.deepcopy(suburban_projection)
        projection["workspace"] = selector
        directory = tmp_path / str(index)
        directory.mkdir()

        built = run_build(directory, suburban_repository, projection)

        assert built.returncode == 0, built.stderr
        result = as_object(json.loads(built.stdout))
        with tarfile.open(as_text(result["archive"]), mode="r:gz") as archive:
            extracted = archive.extractfile(MANIFEST_MEMBER)
            assert extracted is not None
            manifest = as_object(json.loads(extracted.read()))
        assert manifest["workspace"] == selector
        assets[selector] = as_text(result["asset"])
    assert assets == {
        "suburban/simulators-astound": "hostconfig_suburban_simulators-astound.tar.gz",
        "suburban/simulators/astound": "hostconfig_suburban_simulators_astound.tar.gz",
        "suburban-simulators/astound": "hostconfig_suburban-simulators_astound.tar.gz",
    }


def test_build_rejects_rendered_secret_key(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    template = repository / OPNSENSE_TEMPLATE
    template.write_text(
        template.read_text(encoding="utf-8") + f'\ntoken_secret = "{SYNTHETIC_SECRET}"\n',
        encoding="utf-8",
    )
    projection = project(repository, SUBURBAN)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode != 0
    assert "token_secret" in built.stderr
    assert not (tmp_path / "output").exists()
