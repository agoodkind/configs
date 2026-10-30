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
    "opentofu/modules/host-artifact-targets",
    "ansible/inventory/group_vars",
    "mwan/config",
    "opnsense",
    "proxmox/services",
)
SUBURBAN = "suburban/mwan"
VAULT = "vault/mwan"
DESCRIPTOR_NAME = "host-artifact-target.json"
COPIED_FILES = (
    f"opentofu/{SUBURBAN}/{DESCRIPTOR_NAME}",
    f"opentofu/{VAULT}/{DESCRIPTOR_NAME}",
)
IGNORED_COPIES = (".terraform", "*.tfplan", "*.tfstate")
EXTRA_SELECTORS = (
    "suburban_mwan",
    "suburban/simulators/astound",
    "suburban/simulators/astound_static",
)
INVALID_DESCRIPTOR = "{ this descriptor does not parse"
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
MWAN_CONFIG = "/etc/mwan/config.toml"
DRAIN_UNIT = "/etc/systemd/system/mwan-opnsense-drain.service"
WATCHDOG_UNIT = "/etc/systemd/system/mwan-watchdog-testbed.service"
MISSING_ASSET_SOURCE = f"asset:{CONFIGS_REPOSITORY}:proxmox/services/absent.service"
OUTSIDE_LINK = "proxmox/services/outside-link.conf"
RAW_TEMPLATE_SOURCE = f"asset:{CONFIGS_REPOSITORY}:mwan/config/config-host.toml.j2"
UNRESOLVED_TEMPLATE_TEXT = "contains unresolved template markers: {{ {% {#\n"
VAULT_REFERENCE_FILE = "proxmox/services/vault-reference.conf"
VAULT_REFERENCE_TEXT = "contains vault references: vault_smtp2go_api_key\n"
DELETED_TEMPLATE = "deleted.toml.j2"
FIXTURE_DESCRIPTOR: JsonValue = {
    "node": "hypervisor",
    "environment": "testbed",
    "group_vars": [],
    "directories": [],
    "files": [
        {
            "destination": "/etc/example.toml",
            "source": f"template:{DELETED_TEMPLATE}",
            "mode": "0644",
            "owner": "root",
            "group": "root",
        }
    ],
    "secret_fields": [],
}
INCLUDE_TEMPLATE = "mwan/config/_ifmgr_common.toml.j2"
HOST_TEMPLATE = "mwan/config/config-host.toml.j2"
SUBURBAN_DROP_IN = "proxmox/services/mwan-ifmgr-suburban-host-ipv6-policy.conf"
RENAMED_VAULT_GROUP_FILE = "ansible/inventory/group_vars/vault_hosts.yml"
GIT_TEST_OPTIONS = (
    "-c",
    "user.name=hostartifact-test",
    "-c",
    "user.email=hostartifact-test@example.invalid",
    "-c",
    "commit.gpgsign=false",
    "-c",
    "core.hooksPath=/dev/null",
)

type JsonObject = dict[str, JsonValue]
type Mutation = Callable[[JsonObject, Path], None]


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
    for relative in COPIED_FILES:
        (destination / relative).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(REPOSITORY_ROOT / relative, destination / relative)
    return destination


def descriptor_file(repository: Path, workspace: str) -> Path:
    return repository / "opentofu" / workspace / DESCRIPTOR_NAME


def target_asset(workspace: str) -> str:
    identifier = hashlib.sha256(workspace.encode("utf-8")).hexdigest()
    return f"hostconfig_{identifier}.tar.gz"


def write_changed_list(directory: Path, changed: list[str]) -> Path:
    changed_file = directory / "changed.bin"
    changed_file.write_bytes(b"".join(f"{path}\0".encode() for path in changed))
    return changed_file


def run_affected_command(
    repository: Path,
    changed_file: Path,
    workspaces: tuple[str, ...] = (SUBURBAN, VAULT),
    base_revision: str | None = None,
) -> subprocess.CompletedProcess[str]:
    command = [
        sys.executable,
        "-m",
        "hostartifact",
        "affected",
        "--repository-root",
        str(repository),
        "--changed-files",
        str(changed_file),
    ]
    for workspace in workspaces:
        command.extend(["--workspace", workspace])
    if base_revision is not None:
        command.extend(["--base-revision", base_revision])
    return subprocess.run(command, capture_output=True, text=True, check=False)


def run_affected(
    directory: Path, repository: Path, changed: list[str]
) -> subprocess.CompletedProcess[str]:
    return run_affected_command(repository, write_changed_list(directory, changed))


def run_git(repository: Path, *arguments: str) -> str:
    command = ["git", *GIT_TEST_OPTIONS, "-C", str(repository), *arguments]
    completed = subprocess.run(command, capture_output=True, text=True, check=False)
    assert completed.returncode == 0, completed.stderr
    return completed.stdout


def commit_repository(repository: Path) -> str:
    run_git(repository, "init", "--quiet")
    run_git(repository, "add", "--all")
    run_git(repository, "commit", "--quiet", "--message", "base")
    return run_git(repository, "rev-parse", "HEAD").strip()


def git_changed_list(directory: Path, repository: Path, base_revision: str) -> Path:
    run_git(repository, "add", "--all")
    command = [
        "git",
        *GIT_TEST_OPTIONS,
        "-C",
        str(repository),
        "diff",
        "--cached",
        "-z",
        "--name-only",
        "--no-renames",
        base_revision,
    ]
    completed = subprocess.run(command, capture_output=True, check=False)
    assert completed.returncode == 0, completed.stderr
    changed_file = directory / "changed.bin"
    changed_file.write_bytes(completed.stdout)
    return changed_file


def affected_output(selected: subprocess.CompletedProcess[str]) -> JsonValue:
    assert selected.returncode == 0, selected.stderr
    output: JsonValue = json.loads(selected.stdout)
    return output


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


def test_build_succeeds_while_another_target_is_broken(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    (repository / VAULT_GROUP_FILE).unlink()
    descriptor_file(repository, VAULT).write_text(INVALID_DESCRIPTOR, encoding="utf-8")
    projection = project(repository, SUBURBAN)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 0, built.stderr
    result = as_object(json.loads(built.stdout))
    archive_path = Path(as_text(result["archive"]))
    archive_bytes = archive_path.read_bytes()
    assert result["asset"] == target_asset(SUBURBAN)
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


def file_entry(projection: JsonObject, destination: str) -> JsonObject:
    for entry in as_list(projection["files"]):
        candidate = as_object(entry)
        if candidate["destination"] == destination:
            return candidate
    raise AssertionError(destination)


def append_to_template(repository: Path, template: str, text: str) -> None:
    path = repository / template
    path.write_text(path.read_text(encoding="utf-8") + text, encoding="utf-8")


def remove_required_variable(projection: JsonObject, repository: Path) -> None:
    del repository
    del as_object(projection["vars"])["mwan_email_from"]


def add_vault_variable(projection: JsonObject, repository: Path) -> None:
    del repository
    as_object(projection["vars"])["vault_smtp2go_api_key"] = SYNTHETIC_SECRET


def conflict_node(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["node"] = "vault"


def conflict_environment(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["environment"] = "prod"


def use_unclean_selector(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["workspace"] = "suburban/../vault/mwan"


def use_excluded_selector(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["workspace"] = "modules/mwan"


def duplicate_destination(projection: JsonObject, repository: Path) -> None:
    del repository
    files = as_list(projection["files"])
    files.append(copy.deepcopy(file_entry(projection, MWAN_CONFIG)))


def duplicate_directory(projection: JsonObject, repository: Path) -> None:
    del repository
    directories = as_list(projection["directories"])
    directories.append(copy.deepcopy(directories[0]))


def use_special_mode(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, MWAN_CONFIG)["mode"] = "4755"


def point_at_missing_asset(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, DRAIN_UNIT)["source"] = MISSING_ASSET_SOURCE


def point_at_outside_link(projection: JsonObject, repository: Path) -> None:
    outside = repository.parent / "outside.conf"
    outside.write_text("[Service]\n", encoding="utf-8")
    (repository / OUTSIDE_LINK).symlink_to(outside)
    file_entry(projection, DRAIN_UNIT)["source"] = (
        f"asset:{CONFIGS_REPOSITORY}:{OUTSIDE_LINK}"
    )


def copy_template_into_service_file(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, WATCHDOG_UNIT)["source"] = RAW_TEMPLATE_SOURCE


def copy_template_into_config_file(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, MWAN_CONFIG)["source"] = RAW_TEMPLATE_SOURCE


def copy_vault_reference(projection: JsonObject, repository: Path) -> None:
    (repository / VAULT_REFERENCE_FILE).write_text(
        "[Service]\nEnvironment=SMTP2GO_API_KEY=vault_smtp2go_api_key\n", encoding="utf-8"
    )
    file_entry(projection, DRAIN_UNIT)["source"] = (
        f"asset:{CONFIGS_REPOSITORY}:{VAULT_REFERENCE_FILE}"
    )


def render_invalid_toml(projection: JsonObject, repository: Path) -> None:
    del projection
    append_to_template(repository, OPNSENSE_TEMPLATE, "\nthis line = = invalid\n")


def render_secret_key(projection: JsonObject, repository: Path) -> None:
    del projection
    append_to_template(
        repository, OPNSENSE_TEMPLATE, f'\ntoken_secret = "{SYNTHETIC_SECRET}"\n'
    )


@pytest.mark.parametrize(
    ("mutation", "expected_text"),
    [
        pytest.param(
            remove_required_variable,
            "config-host.toml.j2: unresolved variable: 'mwan_email_from' is undefined",
            id="missing-variable",
        ),
        pytest.param(
            add_vault_variable,
            "hostartifact: the projection variables must not include vault values: "
            "vault_smtp2go_api_key\n",
            id="vault-variable",
        ),
        pytest.param(
            conflict_node,
            "hostartifact: mwan_proxmox_node is 'hypervisor' in the group variables, "
            "but the projection sets 'vault'\n",
            id="conflicting-node",
        ),
        pytest.param(
            conflict_environment,
            "hostartifact: mwan_environment is 'testbed' in the group variables, "
            "but the projection sets 'prod'\n",
            id="conflicting-environment",
        ),
        pytest.param(
            use_unclean_selector,
            "hostartifact: workspace selector 'suburban/../vault/mwan' must be a clean "
            "relative path inside the workspace tree\n",
            id="unclean-selector",
        ),
        pytest.param(
            use_excluded_selector,
            "hostartifact: workspace selector 'modules/mwan' contains an excluded "
            "directory\n",
            id="excluded-selector",
        ),
        pytest.param(
            duplicate_destination,
            "hostartifact: the projection lists destination /etc/mwan/config.toml "
            "more than once\n",
            id="duplicate-destination",
        ),
        pytest.param(
            duplicate_directory,
            "hostartifact: the projection lists directory /etc/mwan more than once\n",
            id="duplicate-directory",
        ),
        pytest.param(
            use_special_mode,
            "hostartifact: /etc/mwan/config.toml: mode '4755' must be four octal "
            "digits that start with 0\n",
            id="special-mode",
        ),
        pytest.param(
            point_at_missing_asset,
            "hostartifact: proxmox/services/absent.service is not a regular file in "
            "the agoodkind/configs checkout at ",
            id="missing-asset",
        ),
        pytest.param(
            point_at_outside_link,
            f"hostartifact: {OUTSIDE_LINK} is not a regular file in the "
            "agoodkind/configs checkout at ",
            id="link-outside-checkout",
        ),
        pytest.param(
            copy_template_into_service_file,
            f"hostartifact: {WATCHDOG_UNIT} {UNRESOLVED_TEMPLATE_TEXT}",
            id="unresolved-service-file",
        ),
        pytest.param(
            copy_template_into_config_file,
            f"hostartifact: {MWAN_CONFIG} {UNRESOLVED_TEMPLATE_TEXT}",
            id="unresolved-config-file",
        ),
        pytest.param(
            copy_vault_reference,
            f"hostartifact: {DRAIN_UNIT} {VAULT_REFERENCE_TEXT}",
            id="vault-reference-service-file",
        ),
        pytest.param(
            render_invalid_toml,
            "hostartifact: /etc/opnsensectl/config.toml is not valid TOML: ",
            id="invalid-toml",
        ),
        pytest.param(
            render_secret_key,
            "hostartifact: /etc/opnsensectl/config.toml contains secret keys: "
            "token_secret\n",
            id="rendered-secret-key",
        ),
    ],
)
def test_build_rejects_each_invalid_input(
    tmp_path: Path,
    suburban_projection: JsonObject,
    mutation: Mutation,
    expected_text: str,
) -> None:
    repository = copy_repository(tmp_path / "repository")
    projection = copy.deepcopy(suburban_projection)
    mutation(projection, repository)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert expected_text in built.stderr
    assert "Traceback" not in built.stderr
    assert SYNTHETIC_SECRET not in built.stderr
    assert not (tmp_path / "output").exists()


def test_nested_and_underscore_selectors_keep_their_identity(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    source_descriptor = descriptor_file(repository, SUBURBAN)
    for selector in EXTRA_SELECTORS:
        target_descriptor = descriptor_file(repository, selector)
        target_descriptor.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source_descriptor, target_descriptor)

    assets: set[str] = set()
    for index, selector in enumerate((SUBURBAN, *EXTRA_SELECTORS)):
        directory = tmp_path / f"build-{index}"
        directory.mkdir()
        projection = project(repository, selector)

        built = run_build(directory, repository, projection)

        assert built.returncode == 0, built.stderr
        result = as_object(json.loads(built.stdout))
        with tarfile.open(as_text(result["archive"]), mode="r:gz") as archive:
            extracted = archive.extractfile(MANIFEST_MEMBER)
            assert extracted is not None
            manifest = as_object(json.loads(extracted.read()))
        assert projection["workspace"] == selector
        assert manifest["workspace"] == selector
        assert result["asset"] == target_asset(selector)
        assets.add(as_text(result["asset"]))
    assert len(assets) == len(EXTRA_SELECTORS) + 1


@pytest.mark.parametrize(
    ("changed", "expected"),
    [
        pytest.param([VAULT_GROUP_FILE], [VAULT], id="host-group-file"),
        pytest.param(
            ["mwan/config/_ifmgr_common.toml.j2"], [SUBURBAN, VAULT], id="include"
        ),
        pytest.param(
            ["proxmox/services/mwan-ifmgr-suburban-host-ipv6-policy.conf"],
            [SUBURBAN],
            id="configs-asset",
        ),
        pytest.param(
            [f"opentofu/{SUBURBAN}/host-artifact.auto.tfvars.json"], [SUBURBAN], id="pin"
        ),
        pytest.param(
            ["hostartifact/src/hostartifact/pack.py"], [SUBURBAN, VAULT], id="helper"
        ),
        pytest.param(
            ["ansible/inventory/group_vars/all/service_mapping.yml"],
            [SUBURBAN, VAULT],
            id="service-mapping",
        ),
        pytest.param(
            ["ansible/inventory/group_vars/proxy_servers.yml"], [], id="unread-group-file"
        ),
        pytest.param(["docs/README.md"], [], id="unrelated"),
    ],
)
def test_affected_selects_targets_by_dependency(
    tmp_path: Path, suburban_repository: Path, changed: list[str], expected: list[str]
) -> None:
    selected = run_affected(tmp_path, suburban_repository, changed)

    assert selected.returncode == 0, selected.stderr
    assert json.loads(selected.stdout) == expected


def test_affected_ignores_a_broken_unrelated_target(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    descriptor_file(repository, VAULT).write_text(INVALID_DESCRIPTOR, encoding="utf-8")

    selected = run_affected(
        tmp_path, repository, ["ansible/inventory/group_vars/suburban_servers.yml"]
    )

    assert selected.returncode == 0, selected.stderr
    assert json.loads(selected.stdout) == [SUBURBAN]
    assert VAULT in selected.stderr


def test_affected_selects_the_saved_deleted_template_case(tmp_path: Path) -> None:
    repository = tmp_path / "repository"
    descriptor = descriptor_file(repository, SUBURBAN)
    descriptor.parent.mkdir(parents=True)
    descriptor.write_text(json.dumps(FIXTURE_DESCRIPTOR), encoding="utf-8")

    selected = run_affected_command(
        repository,
        write_changed_list(tmp_path, [DELETED_TEMPLATE]),
        workspaces=(SUBURBAN,),
    )

    assert affected_output(selected) == [SUBURBAN]


def build_projection_of(directory: Path, repository: Path, workspace: str) -> str:
    projection = project(repository, workspace)
    built = run_build(directory, repository, projection)
    assert built.returncode == 1, built.stdout
    return built.stderr


def test_deleted_template_selects_targets_and_fails_their_build(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    base_revision = commit_repository(repository)
    (repository / OPNSENSE_TEMPLATE).unlink()
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert changed_file.read_bytes() == f"{OPNSENSE_TEMPLATE}\0".encode()
    assert affected_output(selected) == [SUBURBAN, VAULT]
    assert (
        f"hostartifact: {OPNSENSE_TEMPLATE} is not a regular file in the "
        "agoodkind/configs checkout at "
    ) in build_projection_of(tmp_path, repository, SUBURBAN)


def test_deleted_include_selects_targets_and_fails_their_build(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    base_revision = commit_repository(repository)
    (repository / INCLUDE_TEMPLATE).unlink()
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert affected_output(selected) == [SUBURBAN, VAULT]
    assert (
        f"{HOST_TEMPLATE}: rendering failed: '_ifmgr_common.toml.j2' not found in "
        "search path" in build_projection_of(tmp_path, repository, SUBURBAN)
    )


def test_include_of_a_deleted_template_comes_from_the_base_revision(
    tmp_path: Path,
) -> None:
    repository = copy_repository(tmp_path / "repository")
    base_revision = commit_repository(repository)
    (repository / HOST_TEMPLATE).unlink()
    changed_file = write_changed_list(tmp_path, [INCLUDE_TEMPLATE])

    with_base = run_affected_command(
        repository, changed_file, base_revision=base_revision
    )
    without_base = run_affected_command(repository, changed_file)

    assert affected_output(with_base) == [SUBURBAN, VAULT]
    assert affected_output(without_base) == []


def test_renamed_input_selects_only_its_target(tmp_path: Path) -> None:
    repository = copy_repository(tmp_path / "repository")
    base_revision = commit_repository(repository)
    run_git(repository, "mv", VAULT_GROUP_FILE, RENAMED_VAULT_GROUP_FILE)
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)
    planned = plan_projection(repository, VAULT)

    assert affected_output(selected) == [VAULT]
    assert planned.returncode != 0
    assert "vault_servers.yml" in planned.stderr


def test_deletion_selects_its_target_while_another_target_is_broken(
    tmp_path: Path,
) -> None:
    repository = copy_repository(tmp_path / "repository")
    descriptor_file(repository, VAULT).write_text(INVALID_DESCRIPTOR, encoding="utf-8")
    base_revision = commit_repository(repository)
    (repository / SUBURBAN_DROP_IN).unlink()
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert affected_output(selected) == [SUBURBAN]
    assert (
        f"hostartifact: {VAULT}: cannot read the target dependencies" in selected.stderr
    )
