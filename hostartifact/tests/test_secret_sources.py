from __future__ import annotations

import json
import subprocess
import sys
import tarfile
from pathlib import Path

import pytest
lazy from pydantic import JsonValue

WORKSPACE = "lab/cert"
REPOSITORY = "agoodkind/configs"
SOURCE_REVISION = "0123456789abcdef0123456789abcdef01234567"
VARIABLE_NAME = "vault_demo"
VARIABLES_PATH = f"opentofu/{WORKSPACE}/variables.tf"
PROVIDER_PATH = f"opentofu/{WORKSPACE}/providers.tf"
SETTINGS_PATH = f"opentofu/{WORKSPACE}/configsctl.yml"
VARIABLES_DESTINATION = "/opt/example/workspace/variables.tf"
PROVIDER_DESTINATION = "/opt/example/workspace/providers.tf"
SETTINGS_DESTINATION = "/opt/example/configsctl.yml"
DECLARATION = f'variable "{VARIABLE_NAME}" {{ sensitive = true }}\n'
REFERENCE = f"locals {{ credential = var.{VARIABLE_NAME} }}\n"
SETTINGS = '{"tofu":{"env_key_prefix":"vault_tofu_env_"}}\n'


def file_entry(destination: str, source: str) -> dict[str, JsonValue]:
    return {
        "destination": destination,
        "source": f"asset:{REPOSITORY}:{source}",
        "mode": "0644",
        "owner": "root",
        "group": "root",
    }


def write_source(repository: Path, relative_path: str, content: str) -> None:
    path = repository / relative_path
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def source_fixture(tmp_path: Path) -> tuple[Path, dict[str, JsonValue]]:
    repository = tmp_path / "repository"
    write_source(repository, VARIABLES_PATH, DECLARATION)
    write_source(repository, PROVIDER_PATH, REFERENCE)
    write_source(repository, SETTINGS_PATH, SETTINGS)
    projection: dict[str, JsonValue] = {
        "workspace": WORKSPACE,
        "node": "lab",
        "environment": "testbed",
        "validation": {
            "identity_variables": {
                "node": "artifact_node",
                "environment": "artifact_env",
            },
            "variable_overrides": {},
            "forbidden_toml_keys": ["password"],
        },
        "vars": {"artifact_node": "lab", "artifact_env": "testbed"},
        "service_mapping": {},
        "directories": [],
        "files": [
            file_entry(VARIABLES_DESTINATION, VARIABLES_PATH),
            file_entry(PROVIDER_DESTINATION, PROVIDER_PATH),
            file_entry(SETTINGS_DESTINATION, SETTINGS_PATH),
        ],
        "programs": [],
    }
    return repository, projection


def build(
    tmp_path: Path, repository: Path, projection: dict[str, JsonValue]
) -> subprocess.CompletedProcess[str]:
    projection_path = tmp_path / "projection.json"
    projection_path.write_text(json.dumps(projection), encoding="utf-8")
    command = [
        sys.executable,
        "-m",
        "hostartifact",
        "build",
        "--projection",
        str(projection_path),
        "--repository-root",
        str(repository),
        "--source-revision",
        SOURCE_REVISION,
        "--output-dir",
        str(tmp_path / "output"),
    ]
    return subprocess.run(command, capture_output=True, text=True, check=False)


def test_build_keeps_sensitive_hcl_source_and_settings_bytes(tmp_path: Path) -> None:
    repository, projection = source_fixture(tmp_path)

    result = build(tmp_path, repository, projection)

    assert result.returncode == 0, result.stderr
    output: JsonValue = json.loads(result.stdout)
    assert isinstance(output, dict)
    archive_path = output["archive"]
    assert isinstance(archive_path, str)
    with tarfile.open(archive_path, mode="r:gz") as archive:
        for destination, source in (
            (VARIABLES_DESTINATION, VARIABLES_PATH),
            (PROVIDER_DESTINATION, PROVIDER_PATH),
            (SETTINGS_DESTINATION, SETTINGS_PATH),
        ):
            member = archive.extractfile(f"files{destination}")
            assert member is not None
            assert member.read() == (repository / source).read_bytes()


@pytest.mark.parametrize(
    ("case", "expected"),
    [
        ("secret-default", "must be declared once with sensitive = true"),
        ("nonsensitive", "must be declared once with sensitive = true"),
        ("duplicate-sensitive", "must be declared once with sensitive = true"),
        ("undeclared", "outside a sensitive variable declaration or reference"),
        ("literal", "outside a sensitive variable declaration or reference"),
        ("sibling-module", "outside a sensitive variable declaration or reference"),
        ("settings-extra", "contains vault references: vault_demo"),
        ("settings-encoded-extra", "contains vault references: vault_demo"),
        ("settings-encoded-invalid-shape", "contains vault references: vault_demo"),
        ("settings-invalid-prefix", "contains vault references: vault_demo"),
        ("service", "contains vault references: vault_demo"),
        ("toml", "contains vault references: vault_demo"),
        ("projection", "must not include vault values: vault_demo"),
    ],
)
def test_build_rejects_secret_source_misuse(
    tmp_path: Path, case: str, expected: str
) -> None:
    repository, projection = source_fixture(tmp_path)
    if case == "secret-default":
        write_source(
            repository, VARIABLES_PATH, DECLARATION.replace(" }", ' default = "secret" }')
        )
    elif case == "nonsensitive":
        write_source(repository, VARIABLES_PATH, DECLARATION.replace("true", "false"))
    elif case == "duplicate-sensitive":
        write_source(
            repository,
            VARIABLES_PATH,
            DECLARATION.replace(" }", " sensitive = false }"),
        )
    elif case == "undeclared":
        write_source(
            repository, PROVIDER_PATH, REFERENCE.replace(VARIABLE_NAME, "vault_missing")
        )
    elif case == "literal":
        write_source(
            repository, PROVIDER_PATH, f'locals {{ credential = "{VARIABLE_NAME}" }}\n'
        )
    elif case == "sibling-module":
        child_path = f"opentofu/{WORKSPACE}/child/main.tf"
        write_source(repository, child_path, REFERENCE)
        files = projection["files"]
        assert isinstance(files, list)
        files.append(file_entry("/opt/example/workspace/child/main.tf", child_path))
    elif case == "settings-extra":
        write_source(
            repository,
            SETTINGS_PATH,
            '{"tofu":{"env_key_prefix":"vault_tofu_env_","other":"vault_demo"}}\n',
        )
    elif case == "settings-encoded-extra":
        write_source(
            repository,
            SETTINGS_PATH,
            '{"tofu":{"env_key_prefix":"vault_tofu_env_","other":"\\u0076ault_demo"}}\n',
        )
    elif case == "settings-invalid-prefix":
        write_source(
            repository, SETTINGS_PATH, '{"tofu":{"env_key_prefix":"vault_demo"}}\n'
        )
    elif case == "settings-encoded-invalid-shape":
        write_source(repository, SETTINGS_PATH, '{"tofu":{"other":"\\u0076ault_demo"}}\n')
    elif case == "service":
        service_path = f"opentofu/{WORKSPACE}/test.service"
        write_source(
            repository, service_path, f"[Service]\nEnvironment=X={VARIABLE_NAME}\n"
        )
        files = projection["files"]
        assert isinstance(files, list)
        files.append(file_entry("/etc/systemd/system/test.service", service_path))
    elif case == "toml":
        toml_path = f"opentofu/{WORKSPACE}/test.toml"
        write_source(repository, toml_path, f'name = "{VARIABLE_NAME}"\n')
        files = projection["files"]
        assert isinstance(files, list)
        files.append(file_entry("/etc/test.toml", toml_path))
    elif case == "projection":
        variables = projection["vars"]
        assert isinstance(variables, dict)
        variables[VARIABLE_NAME] = "synthetic secret"
    else:
        raise AssertionError(case)

    result = build(tmp_path, repository, projection)

    assert result.returncode == 1
    assert expected in result.stderr
    assert "Traceback" not in result.stderr
    assert not (tmp_path / "output").exists()
