from __future__ import annotations

import copy
import hashlib
import io
import json
import os
import shutil
import subprocess
import sys
import tarfile
import tomllib
from collections.abc import Callable
from pathlib import Path
from typing import Literal

import pytest
from pydantic import BaseModel, JsonValue, ValidationError

from hostartifact.manifest import (
    Manifest,
    ManifestProgram,
    manifest_bytes,
    schema_version_for,
)

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
RENDER_ROOT = "opentofu/render/host-artifact"
PRODUCER_PATHS = (RENDER_ROOT, "opentofu/modules/host-artifact-targets")
SUBURBAN = "suburban/mwan"
VAULT = "vault/mwan"
DESCRIPTOR_NAME = "host-artifact-target.json"
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
VALIDATION_FILE = "mwan/config/artifact-validation.json"
TARGET_VALIDATION_FILE = "mwan/config/suburban-validation.json"
UNNORMALIZED_VALIDATION_FILE = "mwan/config/./artifact-validation.json"
VALIDATION_POLICY: JsonValue = {
    "identity_variables": {
        "node": "mwan_proxmox_node",
        "environment": "mwan_environment",
    },
    "variable_overrides": {"mwan_host_config_embeds_secrets": False},
    "forbidden_toml_keys": ["smtp2go_api_key", "token_secret", "token_id"],
}
CALLER_NODE_VARIABLE = "host_node"
CALLER_ENVIRONMENT_VARIABLE = "release_environment"
CALLER_BLOCKED_KEY = "product_credential"
CALLER_TEMPLATE = "product/config.toml.j2"
CALLER_DESTINATION = "/etc/product/config.toml"
CALLER_POLICY: JsonValue = {
    "identity_variables": {
        "node": CALLER_NODE_VARIABLE,
        "environment": CALLER_ENVIRONMENT_VARIABLE,
    },
    "variable_overrides": {"greeting": "caller override", "include_private": False},
    "forbidden_toml_keys": [CALLER_BLOCKED_KEY],
}
CALLER_TEMPLATE_TEXT = (
    'node = "{{ host_node }}"\n'
    'environment = "{{ release_environment }}"\n'
    'message = "{{ greeting }}"\n'
    "include_private = {{ include_private | to_json }}\n"
    'token_id = "public-label"\n'
)
FIXTURE_DESCRIPTOR: JsonValue = {
    "node": "hypervisor",
    "environment": "testbed",
    "validation_file": VALIDATION_FILE,
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
SUBURBAN_GROUP_FILE = "ansible/inventory/group_vars/suburban_servers.yml"
OOB_DROP_IN = "proxmox/services/mwan-ifmgr-oob-dhcpv4.conf"
SELECT_SCRIPT = REPOSITORY_ROOT / "hostartifact/scripts/select-targets.sh"
MAIN_BRANCH = "main"
ORIGIN_MAIN = "origin/main"
SHALLOW_FILE = "notes.txt"
ZERO_COMMIT = "0" * 40
MAIN_REF = "refs/heads/main"
FEATURE_BRANCH = "feature"
FEATURE_REF = "refs/heads/feature"
VAULT_EMAIL_LINE = 'mwan_email_from: "mwan@example.invalid"\n'
SERVICE_MAPPING_FILE = "ansible/inventory/group_vars/all/service_mapping.yml"
SHARED_GROUP_FILE = "ansible/inventory/group_vars/all/vars.yml"
WATCHDOG_TEMPLATE = "proxmox/services/mwan-watchdog.service.j2"
PROGRAM_REPOSITORY = "agoodkind/mwan"
PROGRAM_ASSET = "mwan_linux_amd64.tar.gz"
SHA256_LENGTH = 64
PROGRAM_NAME = "mwan"
PROGRAM_PATH = "/usr/local/bin/mwan"
OTHER_PROGRAM_NAME = "mwan-agent"
OTHER_PROGRAM_PATH = "/usr/local/bin/mwan-agent"
PROGRAM_RELEASE_TAG = "202610291257-ff-c09e62f"
PROGRAM_SOURCE_REVISION = "c09e62f7e0d61526c91183734bf28c6934accded"
PROGRAM_SIGNER_REVISION = "4e9b31f5b944594bb74e316aefa49093b5abd865"
PROGRAM_BUNDLE_TEXT = '{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json"}\n'
PROGRAM_BUNDLES_FILE = "program-bundles.json"
SUBJECT_CHECK = REPOSITORY_ROOT / "hostartifact/scripts/subject-check.jq"
SUBJECT_DIGEST = "a" * 64
OTHER_SUBJECT_DIGEST = "c" * 64
UNRELATED_ASSET = "unrelated.tar.gz"
NOT_UTF8_BYTES = b"\xff\xfe\x00"
PROGRAM_EXECUTABLE = b"synthetic program executable\n"
PROGRAM_README = b"synthetic program readme\n"
SCHEMA_ONE_MANIFEST_KEYS = [
    "directories",
    "environment",
    "files",
    "node",
    "repository",
    "schema_version",
    "source_revision",
    "workspace",
]
SCHEMA_ONE_ARCHIVE_SHA256 = (
    "6bb99ed9508831c78e8f975f48456a1a38fd583b74fd2304f8d192614670f8c6"
)
FIXTURE_FILES = {
    VALIDATION_FILE: json.dumps(VALIDATION_POLICY),
    SERVICE_MAPPING_FILE: (
        "service_mapping:\n"
        "  hypervisor:\n"
        "    vmid: 100\n"
        "  mwan:\n"
        "    vmid: 113\n"
        '    address: "3d06:bad:b01::113"\n'
        "  testbed_mwan:\n"
        "    vmid: 213\n"
        '    address: "3d06:bad:b01:210::213"\n'
    ),
    SHARED_GROUP_FILE: (
        'mwan_email_recipient: "alerts@example.invalid"\n'
        "mwan_host_config_embeds_secrets: true\n"
        'mwan_watchdog_description: "MWAN watchdog fixture"\n'
        "mwan_watchdog_enabled: true\n"
        'mwan_bgp_enabled: "no"\n'
        'mwan_ra_solicit: "yes"\n'
        'mwan_config_hostname: "mwan-{{ mwan_vmid }}"\n'
        "mwan_ping_targets:\n"
        '  - "2606:4700:4700::1111"\n'
        '  - "2001:4860:4860::8888"\n'
        "mwan_interfaces:\n"
        '  - name: "vmbr0"\n'
        "    accept_ra: 2\n"
        '    autoconf: "yes"\n'
        '  - name: "vmbr1"\n'
        "    accept_ra: 0\n"
        "    autoconf: false\n"
    ),
    SUBURBAN_GROUP_FILE: (
        'mwan_proxmox_node: "hypervisor"\n'
        'mwan_environment: "testbed"\n'
        'mwan_email_from: "mwan-testbed@example.invalid"\n'
        'mwan_is_primary: "no"\n'
        'mwan_vmid: "{{ service_mapping.testbed_mwan.vmid }}"\n'
        'mwan_agent_addr: "[{{ service_mapping.testbed_mwan.address }}]:50052"\n'
    ),
    VAULT_GROUP_FILE: (
        'mwan_proxmox_node: "vault"\n'
        'mwan_environment: "prod"\n'
        f"{VAULT_EMAIL_LINE}"
        'mwan_is_primary: "yes"\n'
        'mwan_vmid: "{{ service_mapping.mwan.vmid }}"\n'
        'mwan_agent_addr: "[{{ service_mapping.mwan.address }}]:50052"\n'
    ),
    HOST_TEMPLATE: (
        "{# fixture #}\n"
        "vmid = {{ mwan_vmid }}\n"
        'hostname = "{{ mwan_config_hostname }}"\n'
        'agent_addr = "{{ mwan_agent_addr }}"\n'
        "role = \"{{ mwan_is_primary | bool | ternary('primary', 'standby') }}\"\n"
        "watchdog_mode = \"{{ mwan_watchdog_enabled | bool | ternary('active', "
        "'passive') }}\"\n"
        "ping_targets = {{ mwan_ping_targets | to_json }}\n"
        "\n"
        "[email]\n"
        "{% if mwan_host_config_embeds_secrets %}\n"
        'smtp2go_api_key = "{{ vault_smtp2go_api_key }}"\n'
        "{% endif %}\n"
        'alert_email = "{{ mwan_email_recipient }}"\n'
        'from = "{{ mwan_email_from }}"\n'
        "\n"
        "[pve]\n"
        'node = "{{ mwan_proxmox_node }}"\n'
        "\n"
        "{% if mwan_watchdog_enabled %}\n"
        "[watchdog]\n"
        'service = "mwan-watchdog"\n'
        "\n"
        "{% endif %}\n"
        "{% if mwan_bgp_enabled | bool %}\n"
        "[bgp]\n"
        "enabled = true\n"
        "\n"
        "{% endif %}\n"
        '{% include "_ifmgr_common.toml.j2" %}\n'
    ),
    INCLUDE_TEMPLATE: (
        "[ifmgr]\n"
        'role = "oob"\n'
        "ra_solicit = {{ mwan_ra_solicit | bool | to_json }}\n"
        "{% for interface in mwan_interfaces %}\n"
        "\n"
        "[[ifmgr.interface]]\n"
        'name = "{{ interface.name }}"\n'
        "accept_ra = {{ interface.accept_ra }}\n"
        "autoconf = {{ interface.autoconf | bool | to_json }}\n"
        "{% endfor %}\n"
    ),
    OPNSENSE_TEMPLATE: (
        "[email]\n"
        "{% if mwan_host_config_embeds_secrets %}\n"
        'smtp2go_api_key = "{{ vault_smtp2go_api_key }}"\n'
        "{% endif %}\n"
        'from = "{{ mwan_email_from }}"\n'
    ),
    WATCHDOG_TEMPLATE: (
        "[Unit]\n"
        "Description={{ mwan_watchdog_description }}\n"
        "\n"
        "[Service]\n"
        "ExecStart=/usr/local/bin/mwan watchdog\n"
    ),
    OOB_DROP_IN: "[Service]\nEnvironment=MWAN_OOB=1\n",
    SUBURBAN_DROP_IN: "[Service]\nEnvironment=MWAN_HOST_IPV6=1\n",
}
EXPECTED_SUBURBAN_MWAN_CONFIG: JsonValue = {
    "vmid": 213,
    "hostname": "mwan-213",
    "agent_addr": "[3d06:bad:b01:210::213]:50052",
    "role": "standby",
    "watchdog_mode": "active",
    "ping_targets": ["2606:4700:4700::1111", "2001:4860:4860::8888"],
    "email": {
        "alert_email": "alerts@example.invalid",
        "from": "mwan-testbed@example.invalid",
    },
    "pve": {"node": "hypervisor"},
    "watchdog": {"service": "mwan-watchdog"},
    "ifmgr": {
        "role": "oob",
        "ra_solicit": True,
        "interface": [
            {"name": "vmbr0", "accept_ra": 2, "autoconf": True},
            {"name": "vmbr1", "accept_ra": 0, "autoconf": False},
        ],
    },
}
EXPECTED_SUBURBAN_MWAN_TEXT = (
    "vmid = 213\n"
    'hostname = "mwan-213"\n'
    'agent_addr = "[3d06:bad:b01:210::213]:50052"\n'
    'role = "standby"\n'
    'watchdog_mode = "active"\n'
    'ping_targets = ["2606:4700:4700::1111", "2001:4860:4860::8888"]\n'
    "\n"
    "[email]\n"
    'alert_email = "alerts@example.invalid"\n'
    'from = "mwan-testbed@example.invalid"\n'
    "\n"
    "[pve]\n"
    'node = "hypervisor"\n'
    "\n"
    "[watchdog]\n"
    'service = "mwan-watchdog"\n'
    "\n"
    "[ifmgr]\n"
    'role = "oob"\n'
    "ra_solicit = true\n"
    "\n"
    "[[ifmgr.interface]]\n"
    'name = "vmbr0"\n'
    "accept_ra = 2\n"
    "autoconf = true\n"
    "\n"
    "[[ifmgr.interface]]\n"
    'name = "vmbr1"\n'
    "accept_ra = 0\n"
    "autoconf = false\n"
)
OPNSENSE_CONFIG = "/etc/opnsensectl/config.toml"
OOB_DROP_IN_DESTINATION = "/etc/systemd/system/mwan-ifmgr.service.d/oob-dhcpv4.conf"
UNNORMALIZED_TEMPLATE_TEXT = (
    "source path './opnsense/config.toml.j2' must equal its normalized form "
    "'opnsense/config.toml.j2'\n"
)
UNNORMALIZED_ASSET_TEXT = (
    "source path 'proxmox/services/./mwan-ifmgr-oob-dhcpv4.conf' must equal its "
    "normalized form 'proxmox/services/mwan-ifmgr-oob-dhcpv4.conf'\n"
)
UNNORMALIZED_GROUP_FILE = "ansible/inventory/group_vars//suburban_servers.yml"
UNNORMALIZED_GROUP_TEXT = (
    "each group variable path in the descriptor must equal its normalized form: "
    f"{UNNORMALIZED_GROUP_FILE}"
)
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


def fixture_file(destination: str, source: str, mode: str) -> JsonObject:
    return {
        "destination": destination,
        "source": source,
        "mode": mode,
        "owner": "root",
        "group": "root",
    }


def fixture_descriptor(
    node: str, environment: str, host_group_file: str, extra_files: list[JsonValue]
) -> JsonObject:
    files: list[JsonValue] = [
        fixture_file(MWAN_CONFIG, f"template:{HOST_TEMPLATE}", "0600"),
        fixture_file(OPNSENSE_CONFIG, f"template:{OPNSENSE_TEMPLATE}", "0600"),
        fixture_file(
            "/etc/systemd/system/mwan-ifmgr.service",
            "asset:agoodkind/mwan:gateway/internal/installspec/mwan-ifmgr.service",
            "0644",
        ),
        fixture_file(
            OOB_DROP_IN_DESTINATION, f"asset:{CONFIGS_REPOSITORY}:{OOB_DROP_IN}", "0644"
        ),
        fixture_file(
            DRAIN_UNIT,
            "asset:agoodkind/opnsensectl:cmd/opnsensectl/mwan-opnsense-drain.service",
            "0644",
        ),
        *extra_files,
    ]
    directories: list[JsonValue] = [
        {"path": "/etc/mwan", "mode": "0755", "owner": "root", "group": "root"},
        {"path": "/etc/opnsensectl", "mode": "0755", "owner": "root", "group": "root"},
    ]
    group_vars: list[JsonValue] = [
        SERVICE_MAPPING_FILE,
        SHARED_GROUP_FILE,
        host_group_file,
    ]
    return {
        "node": node,
        "environment": environment,
        "validation_file": VALIDATION_FILE,
        "group_vars": group_vars,
        "directories": directories,
        "files": files,
        "secret_fields": [],
    }


def fixture_descriptors() -> dict[str, JsonObject]:
    suburban_files: list[JsonValue] = [
        fixture_file(WATCHDOG_UNIT, f"template:{WATCHDOG_TEMPLATE}", "0644"),
        fixture_file(
            "/etc/systemd/system/mwan-ifmgr.service.d/suburban-host-ipv6-policy.conf",
            f"asset:{CONFIGS_REPOSITORY}:{SUBURBAN_DROP_IN}",
            "0644",
        ),
    ]
    vault_files: list[JsonValue] = [
        fixture_file(
            "/etc/systemd/system/mwan-watchdog.service",
            f"template:{WATCHDOG_TEMPLATE}",
            "0644",
        )
    ]
    return {
        SUBURBAN: fixture_descriptor(
            "hypervisor", "testbed", SUBURBAN_GROUP_FILE, suburban_files
        ),
        VAULT: fixture_descriptor("vault", "prod", VAULT_GROUP_FILE, vault_files),
    }


def fixture_repository(destination: Path) -> Path:
    for relative in PRODUCER_PATHS:
        shutil.copytree(
            REPOSITORY_ROOT / relative,
            destination / relative,
            ignore=shutil.ignore_patterns(*IGNORED_COPIES),
        )
    for relative, text in FIXTURE_FILES.items():
        (destination / relative).parent.mkdir(parents=True, exist_ok=True)
        (destination / relative).write_text(text, encoding="utf-8")
    for workspace, descriptor in fixture_descriptors().items():
        path = descriptor_file(destination, workspace)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(descriptor, indent=2) + "\n", encoding="utf-8")
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


def commit_all(repository: Path, message: str) -> str:
    run_git(repository, "add", "--all")
    run_git(repository, "commit", "--quiet", "--message", message)
    return run_git(repository, "rev-parse", "HEAD").strip()


def commit_repository(repository: Path) -> str:
    run_git(repository, "init", "--quiet", f"--initial-branch={MAIN_BRANCH}")
    return commit_all(repository, "base")


def run_select(
    repository: Path,
    ref: str,
    before: str,
    head: str,
    work: Path,
    main_ref: str = MAIN_BRANCH,
) -> subprocess.CompletedProcess[str]:
    search_path = f"{Path(sys.executable).parent}{os.pathsep}{os.environ['PATH']}"
    command = [
        str(SELECT_SCRIPT),
        str(repository),
        ref,
        main_ref,
        before,
        head,
        str(work),
        SUBURBAN,
        VAULT,
    ]
    return subprocess.run(
        command,
        capture_output=True,
        text=True,
        check=False,
        env={**os.environ, "PATH": search_path},
    )


def replace_in_file(path: Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    assert old in text
    path.write_text(text.replace(old, new), encoding="utf-8")


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
    directory: Path,
    repository: Path,
    projection: JsonObject,
    program_results: tuple[Path, ...] = (),
) -> subprocess.CompletedProcess[str]:
    projection_file = directory / "projection.json"
    projection_file.write_text(json.dumps(projection), encoding="utf-8")
    result_arguments: list[str] = []
    for result_file in program_results:
        result_arguments.extend(["--program-result", str(result_file)])
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
        *result_arguments,
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
    return fixture_repository(tmp_path_factory.mktemp("repository"))


@pytest.fixture(scope="module")
def suburban_projection(suburban_repository: Path) -> JsonObject:
    return project(suburban_repository, SUBURBAN)


def test_build_succeeds_while_another_target_is_broken(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
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
    assert mwan_config == EXPECTED_SUBURBAN_MWAN_CONFIG
    assert contents["files/etc/mwan/config.toml"].decode() == EXPECTED_SUBURBAN_MWAN_TEXT
    opnsense_config = tomllib.loads(
        contents["files/etc/opnsensectl/config.toml"].decode()
    )
    assert "smtp2go_api_key" not in opnsense_config["email"]
    assert opnsense_config == {"email": {"from": "mwan-testbed@example.invalid"}}


def test_target_without_programs_keeps_schema_one_bytes(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = project(repository, SUBURBAN)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 0, built.stderr
    result = as_object(json.loads(built.stdout))
    with tarfile.open(as_text(result["archive"]), mode="r:gz") as archive:
        extracted = archive.extractfile(MANIFEST_MEMBER)
        assert extracted is not None
        manifest = as_object(json.loads(extracted.read()))
    assert projection["programs"] == []
    assert sorted(os.listdir(tmp_path / "output")) == [target_asset(SUBURBAN)]
    assert result["sha256"] == SCHEMA_ONE_ARCHIVE_SHA256
    assert manifest["schema_version"] == 1
    assert sorted(manifest) == SCHEMA_ONE_MANIFEST_KEYS


def test_manifest_with_programs_serializes_as_schema_two() -> None:
    program = ManifestProgram(
        name=PROGRAM_NAME,
        repository=PROGRAM_REPOSITORY,
        release_tag="v1.0.0",
        asset=PROGRAM_ASSET,
        sha256="a" * SHA256_LENGTH,
        source_revision=SOURCE_REVISION,
        signer_revision=SOURCE_REVISION,
        member=PROGRAM_NAME,
        member_sha256="b" * SHA256_LENGTH,
        path=PROGRAM_PATH,
        mode="0755",
        owner="root",
        group="root",
    )
    manifest = Manifest(
        schema_version=schema_version_for([program]),
        repository=CONFIGS_REPOSITORY,
        source_revision=SOURCE_REVISION,
        workspace=SUBURBAN,
        node="hypervisor",
        environment="testbed",
        directories=[],
        files=[],
        programs=[program],
    )

    document = as_object(json.loads(manifest_bytes(manifest)))

    assert document["schema_version"] == 2
    assert document["programs"] == [program.model_dump(mode="json")]
    assert schema_version_for([]) == 1


def add_descriptor_program(repository: Path) -> None:
    path = descriptor_file(repository, SUBURBAN)
    descriptor = as_object(json.loads(path.read_text(encoding="utf-8")))
    descriptor["programs"] = [program_entry(PROGRAM_NAME, PROGRAM_PATH)]
    path.write_text(json.dumps(descriptor), encoding="utf-8")


def test_projection_includes_descriptor_programs(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    add_descriptor_program(repository)

    projection = project(repository, SUBURBAN)

    assert projection["programs"] == [program_entry(PROGRAM_NAME, PROGRAM_PATH)]


class FixtureMember(BaseModel):
    name: str
    content: bytes = b""
    kind: Literal["file", "symlink", "hardlink"] = "file"
    link_target: str = ""


class ProgramBuild(BaseModel):
    repository: Path
    projection: JsonObject
    results: tuple[Path, ...]


def write_program_archive(path: Path, members: list[FixtureMember]) -> str:
    with tarfile.open(path, mode="w:gz") as archive:
        for member in members:
            info = tarfile.TarInfo(member.name)
            info.mode = 0o755
            if member.kind == "file":
                info.size = len(member.content)
                archive.addfile(info, io.BytesIO(member.content))
            elif member.kind == "symlink":
                info.type = tarfile.SYMTYPE
                info.linkname = member.link_target
                archive.addfile(info)
            else:
                info.type = tarfile.LNKTYPE
                info.linkname = member.link_target
                archive.addfile(info)
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_program_result(
    directory: Path, name: str, archive_sha256: str, asset: str = PROGRAM_ASSET
) -> Path:
    bundle = directory / f"{name}-bundle.jsonl"
    bundle.write_text(PROGRAM_BUNDLE_TEXT, encoding="utf-8")
    result = {
        "name": name,
        "release_tag": PROGRAM_RELEASE_TAG,
        "asset": asset,
        "sha256": archive_sha256,
        "source_revision": PROGRAM_SOURCE_REVISION,
        "signer_revision": PROGRAM_SIGNER_REVISION,
        "archive": str(directory / f"{name}.tar.gz"),
        "bundle": str(bundle),
    }
    result_file = directory / f"{name}-result.json"
    result_file.write_text(json.dumps(result), encoding="utf-8")
    return result_file


def program_path(name: str) -> str:
    return f"/usr/local/bin/{name}"


def prepare_program_build(
    directory: Path,
    suburban_projection: JsonObject,
    archives: dict[str, list[FixtureMember]],
) -> ProgramBuild:
    repository = fixture_repository(directory / "repository")
    projection = copy.deepcopy(suburban_projection)
    projection["programs"] = [
        program_entry(name, program_path(name)) for name in archives
    ]
    results: list[Path] = []
    for name, members in archives.items():
        digest = write_program_archive(directory / f"{name}.tar.gz", members)
        results.append(write_program_result(directory, name, digest))
    return ProgramBuild(
        repository=repository, projection=projection, results=tuple(results)
    )


def read_manifest(archive_path: str) -> JsonObject:
    with tarfile.open(archive_path, mode="r:gz") as archive:
        extracted = archive.extractfile(MANIFEST_MEMBER)
        assert extracted is not None
        return as_object(json.loads(extracted.read()))


def test_build_records_the_selected_program_member_beside_a_readme(
    tmp_path: Path, suburban_projection: JsonObject
) -> None:
    other_name = "opnsensectl"
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {
            PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)],
            other_name: [
                FixtureMember(name="README.md", content=PROGRAM_README),
                FixtureMember(name=other_name, content=PROGRAM_EXECUTABLE + b"other\n"),
            ],
        },
    )

    built = run_build(
        tmp_path, prepared.repository, prepared.projection, prepared.results
    )

    assert built.returncode == 0, built.stderr
    result = as_object(json.loads(built.stdout))
    manifest = read_manifest(as_text(result["archive"]))
    assert manifest["schema_version"] == 2
    programs = [as_object(item) for item in as_list(manifest["programs"])]
    assert [item["name"] for item in programs] == [PROGRAM_NAME, other_name]
    other = programs[1]
    assert other["member"] == other_name
    assert (
        other["member_sha256"]
        == hashlib.sha256(PROGRAM_EXECUTABLE + b"other\n").hexdigest()
    )
    assert other["member_sha256"] != hashlib.sha256(PROGRAM_README).hexdigest()
    assert (
        other["sha256"]
        == hashlib.sha256((tmp_path / f"{other_name}.tar.gz").read_bytes()).hexdigest()
    )
    assert other["path"] == program_path(other_name)
    assert (other["mode"], other["owner"], other["group"]) == ("0755", "root", "root")
    assert other["repository"] == PROGRAM_REPOSITORY
    assert other["asset"] == PROGRAM_ASSET
    assert other["release_tag"] == PROGRAM_RELEASE_TAG
    assert other["source_revision"] == PROGRAM_SOURCE_REVISION
    assert other["signer_revision"] == PROGRAM_SIGNER_REVISION
    output = tmp_path / "output"
    bundle_sha256 = hashlib.sha256(PROGRAM_BUNDLE_TEXT.encode()).hexdigest()
    bundle_assets = {
        name: f"hostconfig_program-{name}.sigstore.jsonl"
        for name in (PROGRAM_NAME, other_name)
    }
    assert sorted(os.listdir(output)) == sorted(
        [target_asset(SUBURBAN), PROGRAM_BUNDLES_FILE, *bundle_assets.values()]
    )
    index = json.loads((output / PROGRAM_BUNDLES_FILE).read_text(encoding="utf-8"))
    assert index == {
        name: {"asset": asset, "sha256": bundle_sha256}
        for name, asset in bundle_assets.items()
    }
    for asset in bundle_assets.values():
        assert (output / asset).read_text(encoding="utf-8") == PROGRAM_BUNDLE_TEXT


@pytest.mark.parametrize(
    ("members", "expected_text"),
    [
        pytest.param(
            [FixtureMember(name="other", content=PROGRAM_EXECUTABLE)],
            f"does not contain the member {PROGRAM_NAME}",
            id="missing-member",
        ),
        pytest.param(
            [
                FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE),
                FixtureMember(name=PROGRAM_NAME, content=PROGRAM_README),
            ],
            f"contains the member {PROGRAM_NAME} more than once",
            id="duplicated-member",
        ),
        pytest.param(
            [FixtureMember(name=PROGRAM_NAME, kind="symlink", link_target="/bin/true")],
            "must be a regular file and must not be a link",
            id="symbolic-link-member",
        ),
        pytest.param(
            [
                FixtureMember(name="other", content=PROGRAM_EXECUTABLE),
                FixtureMember(name=PROGRAM_NAME, kind="hardlink", link_target="other"),
            ],
            "must be a regular file and must not be a link",
            id="hard-link-member",
        ),
    ],
)
def test_build_rejects_a_program_archive_without_one_regular_member(
    tmp_path: Path,
    suburban_projection: JsonObject,
    members: list[FixtureMember],
    expected_text: str,
) -> None:
    prepared = prepare_program_build(
        tmp_path, suburban_projection, {PROGRAM_NAME: members}
    )

    built = run_build(
        tmp_path, prepared.repository, prepared.projection, prepared.results
    )

    assert built.returncode == 1, built.stderr
    assert expected_text in built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def test_build_rejects_an_archive_that_differs_from_its_result(
    tmp_path: Path, suburban_projection: JsonObject
) -> None:
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)]},
    )
    write_program_result(tmp_path, PROGRAM_NAME, "0" * SHA256_LENGTH)

    built = run_build(
        tmp_path, prepared.repository, prepared.projection, prepared.results
    )

    assert built.returncode == 1, built.stderr
    assert f"but the result records {'0' * SHA256_LENGTH}" in built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def test_build_rejects_a_result_for_another_asset(
    tmp_path: Path, suburban_projection: JsonObject
) -> None:
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)]},
    )
    digest = hashlib.sha256(
        (tmp_path / f"{PROGRAM_NAME}.tar.gz").read_bytes()
    ).hexdigest()
    write_program_result(tmp_path, PROGRAM_NAME, digest, asset="other_linux_amd64.tar.gz")

    built = run_build(
        tmp_path, prepared.repository, prepared.projection, prepared.results
    )

    assert built.returncode == 1, built.stderr
    assert "the result records asset other_linux_amd64.tar.gz" in built.stderr
    assert not (tmp_path / "output").exists()


@pytest.mark.parametrize(
    ("projection_names", "result_names", "expected_text"),
    [
        pytest.param(
            [PROGRAM_NAME],
            [],
            f"programs [{PROGRAM_NAME}] do not have results, and results [] do not match",
            id="missing-result",
        ),
        pytest.param(
            [PROGRAM_NAME],
            [PROGRAM_NAME, OTHER_PROGRAM_NAME],
            f"programs [] do not have results, and results [{OTHER_PROGRAM_NAME}] do not "
            "match any program",
            id="result-without-program",
        ),
        pytest.param(
            [PROGRAM_NAME],
            [OTHER_PROGRAM_NAME],
            f"programs [{PROGRAM_NAME}] do not have results, and results "
            f"[{OTHER_PROGRAM_NAME}] do not match any program",
            id="renamed-result",
        ),
        pytest.param(
            [],
            [PROGRAM_NAME],
            f"programs [] do not have results, and results [{PROGRAM_NAME}] do not match "
            "any program",
            id="result-for-target-without-programs",
        ),
    ],
)
def test_build_rejects_program_results_that_differ_from_the_projection(
    tmp_path: Path,
    suburban_projection: JsonObject,
    projection_names: list[str],
    result_names: list[str],
    expected_text: str,
) -> None:
    archives: dict[str, list[FixtureMember]] = {}
    for name in sorted({*projection_names, *result_names}):
        archives[name] = [FixtureMember(name=name, content=PROGRAM_EXECUTABLE)]
    prepared = prepare_program_build(tmp_path, suburban_projection, archives)
    projection = copy.deepcopy(prepared.projection)
    projection["programs"] = [
        program_entry(name, program_path(name)) for name in projection_names
    ]
    results = tuple(tmp_path / f"{name}-result.json" for name in result_names)

    built = run_build(tmp_path, prepared.repository, projection, results)

    assert built.returncode == 1, built.stderr
    assert expected_text in built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def test_build_rejects_two_results_for_one_program(
    tmp_path: Path, suburban_projection: JsonObject
) -> None:
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)]},
    )

    built = run_build(
        tmp_path,
        prepared.repository,
        prepared.projection,
        (*prepared.results, *prepared.results),
    )

    assert built.returncode == 1, built.stderr
    assert (
        f"The --program-result files specify program {PROGRAM_NAME} more than once"
        in built.stderr
    )
    assert not (tmp_path / "output").exists()


def test_build_rejects_a_program_result_that_is_not_utf8(
    tmp_path: Path, suburban_projection: JsonObject
) -> None:
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)]},
    )
    invalid_result = tmp_path / "invalid-result.json"
    invalid_result.write_bytes(NOT_UTF8_BYTES)

    built = run_build(
        tmp_path, prepared.repository, prepared.projection, (invalid_result,)
    )

    assert built.returncode == 1, built.stderr
    assert "The build cannot read or validate program result" in built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def subject_statement(subjects: list[tuple[str, str]]) -> JsonObject:
    listed: list[JsonValue] = []
    for name, digest in subjects:
        listed.append({"name": name, "digest": {"sha256": digest}})
    return {"verificationResult": {"statement": {"subject": listed}}}


def run_subject_check(statements: list[JsonObject]) -> subprocess.CompletedProcess[str]:
    command = [
        "jq",
        "-e",
        "--arg",
        "asset",
        PROGRAM_ASSET,
        "--arg",
        "sha256",
        SUBJECT_DIGEST,
        "-f",
        str(SUBJECT_CHECK),
    ]
    return subprocess.run(
        command, input=json.dumps(statements), capture_output=True, text=True, check=False
    )


MATCHING_SUBJECT = (PROGRAM_ASSET, SUBJECT_DIGEST)
WRONG_DIGEST_SUBJECT = (PROGRAM_ASSET, OTHER_SUBJECT_DIGEST)
UNRELATED_SUBJECT = (UNRELATED_ASSET, OTHER_SUBJECT_DIGEST)
OTHER_ASSET_SUBJECT = (UNRELATED_ASSET, SUBJECT_DIGEST)
STATEMENT_WITHOUT_SUBJECT: JsonObject = {"verificationResult": {"statement": {}}}


@pytest.mark.parametrize(
    ("statements", "accepted"),
    [
        pytest.param([subject_statement([MATCHING_SUBJECT])], True, id="exact-subject"),
        pytest.param(
            [subject_statement([UNRELATED_SUBJECT, MATCHING_SUBJECT])],
            True,
            id="matching-subject-beside-an-unrelated-subject",
        ),
        pytest.param(
            [
                subject_statement([MATCHING_SUBJECT]),
                subject_statement([UNRELATED_SUBJECT, MATCHING_SUBJECT]),
            ],
            True,
            id="every-statement-matches",
        ),
        pytest.param(
            [subject_statement([WRONG_DIGEST_SUBJECT])],
            False,
            id="right-asset-with-another-digest",
        ),
        pytest.param(
            [subject_statement([WRONG_DIGEST_SUBJECT, OTHER_ASSET_SUBJECT])],
            False,
            id="asset-and-digest-split-across-two-subjects",
        ),
        pytest.param(
            [
                subject_statement([MATCHING_SUBJECT]),
                subject_statement([WRONG_DIGEST_SUBJECT]),
            ],
            False,
            id="second-statement-with-the-wrong-digest",
        ),
        pytest.param([], False, id="no-statements"),
        pytest.param([STATEMENT_WITHOUT_SUBJECT], False, id="statement-without-subject"),
    ],
)
def test_subject_check_requires_the_exact_asset_and_digest_in_every_statement(
    statements: list[JsonObject], accepted: bool
) -> None:
    checked = run_subject_check(statements)

    assert (checked.returncode == 0) is accepted, checked.stderr


@pytest.mark.parametrize(
    ("override", "expected_text"),
    [
        pytest.param(
            {"asset": "release/mwan.tar.gz"},
            "asset 'release/mwan.tar.gz'",
            id="asset-directory",
        ),
        pytest.param(
            {"asset": ".hidden.tar.gz"}, "asset '.hidden.tar.gz'", id="asset-hidden"
        ),
        pytest.param({"member": "../mwan"}, "member '../mwan'", id="member-parent"),
        pytest.param({"member": "/mwan"}, "member '/mwan'", id="member-absolute"),
        pytest.param({"member": "./mwan"}, "member './mwan'", id="member-unnormalized"),
        pytest.param({"member": ""}, "member ''", id="member-empty"),
    ],
)
def test_build_rejects_an_invalid_program_asset_or_member(
    tmp_path: Path,
    suburban_projection: JsonObject,
    override: JsonObject,
    expected_text: str,
) -> None:
    prepared = prepare_program_build(
        tmp_path,
        suburban_projection,
        {PROGRAM_NAME: [FixtureMember(name=PROGRAM_NAME, content=PROGRAM_EXECUTABLE)]},
    )
    projection = copy.deepcopy(prepared.projection)
    projection["programs"] = [{**program_entry(PROGRAM_NAME, PROGRAM_PATH), **override}]

    built = run_build(tmp_path, prepared.repository, projection, prepared.results)

    assert built.returncode == 1, built.stderr
    assert expected_text in built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def manifest_with(
    schema_version: Literal[1, 2], programs: list[ManifestProgram]
) -> Manifest:
    return Manifest(
        schema_version=schema_version,
        repository=CONFIGS_REPOSITORY,
        source_revision=SOURCE_REVISION,
        workspace=SUBURBAN,
        node="hypervisor",
        environment="testbed",
        directories=[],
        files=[],
        programs=programs,
    )


def test_manifest_rejects_a_schema_that_disagrees_with_its_programs() -> None:
    program = ManifestProgram(
        name=PROGRAM_NAME,
        repository=PROGRAM_REPOSITORY,
        release_tag=PROGRAM_RELEASE_TAG,
        asset=PROGRAM_ASSET,
        sha256="a" * SHA256_LENGTH,
        source_revision=PROGRAM_SOURCE_REVISION,
        signer_revision=PROGRAM_SIGNER_REVISION,
        member=PROGRAM_NAME,
        member_sha256="b" * SHA256_LENGTH,
        path=PROGRAM_PATH,
        mode="0755",
        owner="root",
        group="root",
    )

    with pytest.raises(ValidationError, match="schema 1 manifest must not list programs"):
        manifest_with(1, [program])
    with pytest.raises(ValidationError, match="schema 2 manifest must list at least one"):
        manifest_with(2, [])


@pytest.mark.parametrize(
    "program",
    [
        pytest.param({"name": "Mwan"}, id="uppercase-name"),
        pytest.param({"name": "1mwan"}, id="leading-digit-name"),
        pytest.param({"repository": "mwan"}, id="repository-without-owner"),
        pytest.param({"path": "usr/local/bin/mwan"}, id="relative-path"),
        pytest.param({"mode": "4755"}, id="special-mode"),
        pytest.param({"owner": "Root"}, id="invalid-owner"),
    ],
)
def test_build_rejects_an_invalid_program_entry(
    tmp_path: Path, suburban_projection: JsonObject, program: JsonObject
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = copy.deepcopy(suburban_projection)
    projection["programs"] = [{**program_entry(PROGRAM_NAME, PROGRAM_PATH), **program}]

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert "Traceback" not in built.stderr
    assert not (tmp_path / "output").exists()


def test_affected_reads_a_descriptor_with_programs(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    add_descriptor_program(repository)

    selected = run_affected(tmp_path, repository, [SUBURBAN_GROUP_FILE])

    assert affected_output(selected) == [SUBURBAN]
    assert selected.stderr == ""


def test_affected_selects_a_target_whose_descriptor_lists_parent_directories(
    tmp_path: Path,
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    path = descriptor_file(repository, SUBURBAN)
    descriptor = as_object(json.loads(path.read_text(encoding="utf-8")))
    descriptor["parent_directories"] = [{"path": "/usr/local/bin"}]
    path.write_text(json.dumps(descriptor), encoding="utf-8")

    selected = run_affected(tmp_path, repository, [SUBURBAN_GROUP_FILE])

    assert affected_output(selected) == [SUBURBAN]
    assert selected.stderr == ""


def test_affected_ignores_an_unrelated_descriptor_that_is_not_utf8(
    tmp_path: Path,
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    descriptor_file(repository, VAULT).write_bytes(NOT_UTF8_BYTES)

    selected = run_affected(tmp_path, repository, [SUBURBAN_GROUP_FILE])

    assert selected.returncode == 0, selected.stderr
    assert json.loads(selected.stdout) == [SUBURBAN]
    assert VAULT in selected.stderr
    assert "Traceback" not in selected.stderr


def test_unknown_workspace_fails_with_known_selectors(suburban_repository: Path) -> None:
    planned = plan_projection(suburban_repository, "suburban/absent")

    assert planned.returncode != 0
    assert "suburban/mwan, vault/mwan" in planned.stderr


def caller_projection(repository: Path) -> JsonObject:
    descriptor_path = descriptor_file(repository, SUBURBAN)
    descriptor = as_object(json.loads(descriptor_path.read_text(encoding="utf-8")))
    descriptor["group_vars"] = [SERVICE_MAPPING_FILE, SUBURBAN_GROUP_FILE]
    descriptor["files"] = [
        fixture_file(CALLER_DESTINATION, f"template:{CALLER_TEMPLATE}", "0600")
    ]
    descriptor_path.write_text(json.dumps(descriptor), encoding="utf-8")
    (repository / VALIDATION_FILE).write_text(json.dumps(CALLER_POLICY), encoding="utf-8")
    (repository / SUBURBAN_GROUP_FILE).write_text(
        'host_node: "hypervisor"\n'
        'release_environment: "testbed"\n'
        'greeting: "original value"\n'
        "include_private: true\n",
        encoding="utf-8",
    )
    template = repository / CALLER_TEMPLATE
    template.parent.mkdir(parents=True)
    template.write_text(CALLER_TEMPLATE_TEXT, encoding="utf-8")
    return project(repository, SUBURBAN)


def test_build_consumes_caller_validation_without_mwan_defaults(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = caller_projection(repository)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 0, built.stderr
    result = as_object(json.loads(built.stdout))
    with tarfile.open(as_text(result["archive"]), mode="r:gz") as archive:
        extracted = archive.extractfile(FILES_PREFIX + CALLER_DESTINATION.lstrip("/"))
        assert extracted is not None
        document = tomllib.loads(extracted.read().decode())
        manifest_file = archive.extractfile(MANIFEST_MEMBER)
        assert manifest_file is not None
        manifest = as_object(json.loads(manifest_file.read()))
    assert document == {
        "node": "hypervisor",
        "environment": "testbed",
        "message": "caller override",
        "include_private": False,
        "token_id": "public-label",
    }
    assert "validation" not in manifest


@pytest.mark.parametrize(
    ("variable", "value", "expected"),
    [
        pytest.param(CALLER_NODE_VARIABLE, "other-node", CALLER_NODE_VARIABLE, id="node"),
        pytest.param(
            CALLER_ENVIRONMENT_VARIABLE,
            "other-environment",
            CALLER_ENVIRONMENT_VARIABLE,
            id="environment",
        ),
        pytest.param(
            "vault_product_credential",
            SYNTHETIC_SECRET,
            "must not include vault values: vault_product_credential",
            id="vault-override",
        ),
        pytest.param(
            "service_mapping",
            {},
            "must not define service_mapping",
            id="reserved-override",
        ),
    ],
)
def test_caller_validation_rejects_invalid_variables(
    tmp_path: Path, variable: str, value: JsonValue, expected: str
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = caller_projection(repository)
    validation = as_object(projection["validation"])
    as_object(validation["variable_overrides"])[variable] = value

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert expected in built.stderr
    assert SYNTHETIC_SECRET not in built.stderr
    assert not (tmp_path / "output").exists()


@pytest.mark.parametrize("table", ["", "[nested]\n", "[[nested]]\n"])
def test_caller_validation_rejects_custom_toml_keys(tmp_path: Path, table: str) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = caller_projection(repository)
    append_to_template(
        repository,
        CALLER_TEMPLATE,
        f'\n{table}{CALLER_BLOCKED_KEY} = "{SYNTHETIC_SECRET}"\n',
    )

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert f"contains secret keys: {CALLER_BLOCKED_KEY}" in built.stderr
    assert SYNTHETIC_SECRET not in built.stderr
    assert not (tmp_path / "output").exists()


@pytest.mark.parametrize(
    "missing",
    ["validation", "identity_variables", "variable_overrides", "forbidden_toml_keys"],
)
def test_build_requires_validation_inputs(tmp_path: Path, missing: str) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = caller_projection(repository)
    if missing == "validation":
        del projection[missing]
    else:
        del as_object(projection["validation"])[missing]

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert missing in built.stderr
    assert "Field required" in built.stderr
    assert not (tmp_path / "output").exists()


@pytest.mark.parametrize("identity", ["node", "environment"])
def test_build_rejects_empty_identity_variable_names(
    tmp_path: Path, identity: str
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    projection = caller_projection(repository)
    validation = as_object(projection["validation"])
    as_object(validation["identity_variables"])[identity] = ""

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert f"validation.identity_variables.{identity}" in built.stderr
    assert "at least 1 character" in built.stderr


@pytest.mark.parametrize("deleted", [False, True])
def test_validation_file_changes_select_only_dependent_targets(
    tmp_path: Path, deleted: bool
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    replace_in_file(
        descriptor_file(repository, SUBURBAN), VALIDATION_FILE, TARGET_VALIDATION_FILE
    )
    policy_file = repository / TARGET_VALIDATION_FILE
    shutil.copyfile(repository / VALIDATION_FILE, policy_file)
    base_revision = commit_repository(repository)
    if deleted:
        policy_file.unlink()
    else:
        policy_file.write_text(json.dumps(CALLER_POLICY), encoding="utf-8")
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert changed_file.read_bytes() == f"{TARGET_VALIDATION_FILE}\0".encode()
    assert affected_output(selected) == [SUBURBAN]
    if deleted:
        planned = plan_projection(repository, SUBURBAN)
        assert planned.returncode != 0
        assert TARGET_VALIDATION_FILE in planned.stderr


@pytest.mark.parametrize("broken", ["missing", "invalid"])
def test_build_ignores_another_targets_invalid_validation(
    tmp_path: Path, broken: str
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    replace_in_file(
        descriptor_file(repository, VAULT), VALIDATION_FILE, TARGET_VALIDATION_FILE
    )
    if broken == "invalid":
        (repository / TARGET_VALIDATION_FILE).write_text(
            INVALID_DESCRIPTOR, encoding="utf-8"
        )

    projection = project(repository, SUBURBAN)
    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 0, built.stderr


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


def program_entry(name: str, path: str) -> JsonObject:
    return {
        "name": name,
        "repository": PROGRAM_REPOSITORY,
        "asset": PROGRAM_ASSET,
        "member": name,
        "path": path,
        "mode": "0755",
        "owner": "root",
        "group": "root",
    }


def duplicate_program_name(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["programs"] = [
        program_entry(PROGRAM_NAME, PROGRAM_PATH),
        program_entry(PROGRAM_NAME, OTHER_PROGRAM_PATH),
    ]


def duplicate_program_path(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["programs"] = [
        program_entry(PROGRAM_NAME, PROGRAM_PATH),
        program_entry(OTHER_PROGRAM_NAME, PROGRAM_PATH),
    ]


def program_at_file_destination(projection: JsonObject, repository: Path) -> None:
    del repository
    projection["programs"] = [program_entry(PROGRAM_NAME, MWAN_CONFIG)]


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


def use_unnormalized_template_source(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, OPNSENSE_CONFIG)["source"] = f"template:./{OPNSENSE_TEMPLATE}"


def use_unnormalized_asset_source(projection: JsonObject, repository: Path) -> None:
    del repository
    file_entry(projection, OOB_DROP_IN_DESTINATION)["source"] = (
        f"asset:{CONFIGS_REPOSITORY}:proxmox/services/./mwan-ifmgr-oob-dhcpv4.conf"
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
            duplicate_program_name,
            f"hostartifact: the projection lists program name {PROGRAM_NAME} more than "
            "once\n",
            id="duplicate-program-name",
        ),
        pytest.param(
            duplicate_program_path,
            f"hostartifact: the projection lists program path {PROGRAM_PATH} more than "
            "once\n",
            id="duplicate-program-path",
        ),
        pytest.param(
            program_at_file_destination,
            f"hostartifact: the projection lists {MWAN_CONFIG} as both the path for "
            f"program {PROGRAM_NAME} and a file destination\n",
            id="program-at-file-destination",
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
            use_unnormalized_template_source,
            f"hostartifact: {UNNORMALIZED_TEMPLATE_TEXT}",
            id="unnormalized-template-source",
        ),
        pytest.param(
            use_unnormalized_asset_source,
            f"hostartifact: {UNNORMALIZED_ASSET_TEXT}",
            id="unnormalized-asset-source",
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
    repository = fixture_repository(tmp_path / "repository")
    projection = copy.deepcopy(suburban_projection)
    mutation(projection, repository)

    built = run_build(tmp_path, repository, projection)

    assert built.returncode == 1, built.stderr
    assert expected_text in built.stderr
    assert "Traceback" not in built.stderr
    assert SYNTHETIC_SECRET not in built.stderr
    assert not (tmp_path / "output").exists()


def test_nested_and_underscore_selectors_keep_their_identity(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
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
        pytest.param([VALIDATION_FILE], [SUBURBAN, VAULT], id="shared-validation"),
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
    repository = fixture_repository(tmp_path / "repository")
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
    repository = fixture_repository(tmp_path / "repository")
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
    repository = fixture_repository(tmp_path / "repository")
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
    repository = fixture_repository(tmp_path / "repository")
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
    repository = fixture_repository(tmp_path / "repository")
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
    repository = fixture_repository(tmp_path / "repository")
    descriptor_file(repository, VAULT).write_text(INVALID_DESCRIPTOR, encoding="utf-8")
    base_revision = commit_repository(repository)
    (repository / SUBURBAN_DROP_IN).unlink()
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert affected_output(selected) == [SUBURBAN]
    assert (
        f"hostartifact: {VAULT}: cannot read the target dependencies" in selected.stderr
    )


def test_unnormalized_group_file_fails_the_plan(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    replace_in_file(
        descriptor_file(repository, SUBURBAN),
        SUBURBAN_GROUP_FILE,
        UNNORMALIZED_GROUP_FILE,
    )

    planned = plan_projection(repository, SUBURBAN)

    assert planned.returncode != 0
    assert UNNORMALIZED_GROUP_TEXT in " ".join(planned.stderr.split())


@pytest.mark.parametrize(
    ("declared", "unnormalized", "changed", "expected"),
    [
        pytest.param(
            VALIDATION_FILE,
            UNNORMALIZED_VALIDATION_FILE,
            VALIDATION_FILE,
            [SUBURBAN, VAULT],
            id="validation-file",
        ),
        pytest.param(
            f"template:{OPNSENSE_TEMPLATE}",
            f"template:./{OPNSENSE_TEMPLATE}",
            OPNSENSE_TEMPLATE,
            [SUBURBAN, VAULT],
            id="template-source",
        ),
        pytest.param(
            f"asset:{CONFIGS_REPOSITORY}:{SUBURBAN_DROP_IN}",
            f"asset:{CONFIGS_REPOSITORY}:proxmox/services/./"
            "mwan-ifmgr-suburban-host-ipv6-policy.conf",
            SUBURBAN_DROP_IN,
            [SUBURBAN],
            id="configs-asset",
        ),
        pytest.param(
            SUBURBAN_GROUP_FILE,
            UNNORMALIZED_GROUP_FILE,
            SUBURBAN_GROUP_FILE,
            [SUBURBAN],
            id="group-file",
        ),
    ],
)
def test_affected_compares_normalized_dependency_paths(
    tmp_path: Path, declared: str, unnormalized: str, changed: str, expected: list[str]
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    replace_in_file(descriptor_file(repository, SUBURBAN), declared, unnormalized)
    base_revision = commit_repository(repository)
    path = repository / changed
    path.write_text(path.read_text(encoding="utf-8") + "\n", encoding="utf-8")
    changed_file = git_changed_list(tmp_path, repository, base_revision)

    selected = run_affected_command(repository, changed_file, base_revision=base_revision)

    assert changed_file.read_bytes() == f"{changed}\0".encode()
    assert affected_output(selected) == expected


def test_branch_selection_keeps_a_failed_target_after_a_documentation_push(
    tmp_path: Path,
) -> None:
    repository = fixture_repository(tmp_path / "repository")
    commit_repository(repository)
    run_git(repository, "checkout", "--quiet", "-b", FEATURE_BRANCH)
    replace_in_file(repository / VAULT_GROUP_FILE, VAULT_EMAIL_LINE, "")
    failed_push = commit_all(repository, "break vault input")
    (repository / "docs").mkdir()
    (repository / "docs/README.md").write_text("documentation\n", encoding="utf-8")
    documentation_push = commit_all(repository, "documentation only")

    selected = run_select(
        repository, FEATURE_REF, failed_push, documentation_push, tmp_path / "selection"
    )
    built = build_projection_of(tmp_path, repository, VAULT)

    assert affected_output(selected) == [VAULT]
    assert (
        "config-host.toml.j2: unresolved variable: 'mwan_email_from' is undefined"
        in built
    )


def test_main_selection_compares_with_the_preceding_main_commit(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    first = commit_repository(repository)
    path = repository / SUBURBAN_DROP_IN
    path.write_text(path.read_text(encoding="utf-8") + "\n", encoding="utf-8")
    second = commit_all(repository, "change suburban input")
    (repository / "docs").mkdir()
    (repository / "docs/README.md").write_text("documentation\n", encoding="utf-8")
    third = commit_all(repository, "documentation only")

    input_push = run_select(repository, MAIN_REF, first, second, tmp_path / "input")
    documentation_push = run_select(
        repository, MAIN_REF, second, third, tmp_path / "documentation"
    )
    first_push = run_select(repository, MAIN_REF, ZERO_COMMIT, second, tmp_path / "zero")

    assert affected_output(input_push) == [SUBURBAN]
    assert affected_output(documentation_push) == []
    assert affected_output(first_push) == [SUBURBAN]


def test_invalid_target_in_the_base_does_not_affect_another_build(tmp_path: Path) -> None:
    repository = fixture_repository(tmp_path / "repository")
    descriptor_file(repository, VAULT).write_text(INVALID_DESCRIPTOR, encoding="utf-8")
    commit_repository(repository)
    run_git(repository, "checkout", "--quiet", "-b", FEATURE_BRANCH)
    path = repository / SUBURBAN_DROP_IN
    path.write_text(path.read_text(encoding="utf-8") + "\n", encoding="utf-8")
    head = commit_all(repository, "change suburban input")

    selected = run_select(repository, FEATURE_REF, head, head, tmp_path / "selection")
    projection = project(repository, SUBURBAN)
    built = run_build(tmp_path, repository, projection)

    assert affected_output(selected) == [SUBURBAN]
    assert (
        f"hostartifact: {VAULT}: cannot read the target dependencies" in selected.stderr
    )
    assert built.returncode == 0, built.stderr


def test_selection_in_a_shallow_clone_reports_the_missing_merge_base(
    tmp_path: Path,
) -> None:
    origin = tmp_path / "origin"
    origin.mkdir()
    (origin / SHALLOW_FILE).write_text("base\n", encoding="utf-8")
    commit_repository(origin)
    run_git(origin, "checkout", "--quiet", "-b", FEATURE_BRANCH)
    (origin / SHALLOW_FILE).write_text("feature\n", encoding="utf-8")
    feature_head = commit_all(origin, "feature change")
    run_git(origin, "checkout", "--quiet", MAIN_BRANCH)
    (origin / SHALLOW_FILE).write_text("main\n", encoding="utf-8")
    commit_all(origin, "main change")
    clone = tmp_path / "clone"
    run_git(
        tmp_path,
        "clone",
        "--quiet",
        "--depth",
        "1",
        "--no-single-branch",
        origin.as_uri(),
        str(clone),
    )

    selected = run_select(
        clone,
        FEATURE_REF,
        feature_head,
        feature_head,
        tmp_path / "selection",
        main_ref=ORIGIN_MAIN,
    )

    assert run_git(clone, "rev-parse", "--is-shallow-repository").strip() == "true"
    assert selected.returncode == 1
    assert selected.stdout == ""
    assert (
        "::error::cannot compute the selection base: git merge-base "
        f"{ORIGIN_MAIN} {feature_head} failed with exit_code=1\n"
    ) in selected.stderr
