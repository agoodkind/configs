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
FIXTURES = Path(__file__).parent / "fixtures" / "secret-sources"
VARIABLES_PATH = f"opentofu/{WORKSPACE}/variables.tf"
PROVIDER_PATH = f"opentofu/{WORKSPACE}/providers.tf"
SETTINGS_PATH = f"opentofu/{WORKSPACE}/configsctl.yml"
VARIABLES_DESTINATION = "/opt/example/workspace/variables.tf"
PROVIDER_DESTINATION = "/opt/example/workspace/providers.tf"
SETTINGS_DESTINATION = "/opt/example/configsctl.yml"

VARIABLE_CASES = {
    "secret-default": "variables-default.tf",
    "nonsensitive": "variables-nonsensitive.tf",
    "duplicate-sensitive": "variables-duplicate-sensitive.tf",
    "extra-label": "variables-extra-label.tf",
}
PROVIDER_CASES = {
    "undeclared": "providers-undeclared.tf",
    "literal": "providers-literal.tf",
    "encoded-literal": "providers-encoded-literal.tf",
    "for-shadow": "providers-for-shadow.tf",
}
SETTINGS_CASES = {
    "settings-extra": "settings-extra.json",
    "settings-encoded-extra": "settings-encoded-extra.json",
    "settings-encoded-invalid-shape": "settings-encoded-invalid-shape.json",
    "settings-invalid-prefix": "settings-invalid-prefix.json",
    "settings-duplicate": "settings-duplicate.json",
    "settings-duplicate-encoded": "settings-duplicate-encoded.json",
}


def file_entry(destination: str, source: str) -> dict[str, JsonValue]:
    return {
        "destination": destination,
        "source": f"asset:{REPOSITORY}:{source}",
        "mode": "0644",
        "owner": "root",
        "group": "root",
    }


def copy_fixture(repository: Path, relative_path: str, fixture_name: str) -> None:
    path = repository / relative_path
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes((FIXTURES / fixture_name).read_bytes())


def source_fixture(tmp_path: Path) -> tuple[Path, dict[str, JsonValue]]:
    repository = tmp_path / "repository"
    copy_fixture(repository, VARIABLES_PATH, "variables.tf")
    copy_fixture(repository, PROVIDER_PATH, "providers.tf")
    copy_fixture(repository, SETTINGS_PATH, "settings.json")
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


@pytest.mark.parametrize("provider_fixture", ["providers.tf", "providers-heredoc.tf"])
def test_build_keeps_sensitive_hcl_source_and_settings_bytes(
    tmp_path: Path, provider_fixture: str
) -> None:
    repository, projection = source_fixture(tmp_path)
    copy_fixture(repository, PROVIDER_PATH, provider_fixture)

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
        ("secret-default", "sensitive = true and without a default"),
        ("nonsensitive", "sensitive = true and without a default"),
        ("duplicate-sensitive", "sensitive = true and without a default"),
        ("extra-label", "exactly one variable label"),
        ("undeclared", "outside a sensitive variable declaration or reference"),
        ("literal", "outside a sensitive variable declaration or reference"),
        ("encoded-literal", "outside a sensitive variable declaration or reference"),
        ("for-shadow", "outside a sensitive variable declaration or reference"),
        ("sibling-module", "outside a sensitive variable declaration or reference"),
        ("child-destination", "outside a sensitive variable declaration or reference"),
        ("settings-extra", "contains vault references: vault_demo"),
        ("settings-encoded-extra", "contains vault references: vault_demo"),
        ("settings-encoded-invalid-shape", "contains vault references: vault_demo"),
        ("settings-invalid-prefix", "contains vault references: vault_demo"),
        ("settings-duplicate", "duplicate JSON key"),
        ("settings-duplicate-encoded", "duplicate JSON key"),
        ("service", "contains vault references: vault_demo"),
        ("toml", "contains vault references: vault_demo"),
        ("projection", "must not include vault values: vault_demo"),
    ],
)
def test_build_rejects_secret_source_misuse(
    tmp_path: Path, case: str, expected: str
) -> None:
    repository, projection = source_fixture(tmp_path)
    if case in VARIABLE_CASES:
        copy_fixture(repository, VARIABLES_PATH, VARIABLE_CASES[case])
    elif case in PROVIDER_CASES:
        copy_fixture(repository, PROVIDER_PATH, PROVIDER_CASES[case])
    elif case in SETTINGS_CASES:
        copy_fixture(repository, SETTINGS_PATH, SETTINGS_CASES[case])
    elif case == "sibling-module":
        child_path = f"opentofu/{WORKSPACE}/child/main.tf"
        copy_fixture(repository, child_path, "providers.tf")
        files = projection["files"]
        assert isinstance(files, list)
        files.append(file_entry("/opt/example/workspace/child/main.tf", child_path))
    elif case == "child-destination":
        files = projection["files"]
        assert isinstance(files, list)
        for item in files:
            assert isinstance(item, dict)
            if item["destination"] == PROVIDER_DESTINATION:
                item["destination"] = "/opt/example/workspace/child/providers.tf"
    elif case == "service":
        service_path = f"opentofu/{WORKSPACE}/test.service"
        copy_fixture(repository, service_path, "secret.service")
        files = projection["files"]
        assert isinstance(files, list)
        files.append(file_entry("/etc/systemd/system/test.service", service_path))
    elif case == "toml":
        toml_path = f"opentofu/{WORKSPACE}/test.toml"
        copy_fixture(repository, toml_path, "secret.toml")
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
