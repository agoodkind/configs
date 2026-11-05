from __future__ import annotations

import json
import re
import tomllib
from collections.abc import Mapping
from datetime import date, datetime, time

from pydantic import TypeAdapter, ValidationError
lazy from pydantic import JsonValue

from hostartifact.hcl_secrets import check_hcl_sources, raw_hcl_asset
from hostartifact.projection import BuildError, FileEntry
from hostartifact.secret_patterns import (
    TEMPLATE_MARKERS,
    VAULT_PREFIX,
    VAULT_REFERENCE_PATTERN,
)

TOML_SUFFIX = ".toml"
CONFIGSCTL_SETTINGS_SUFFIX = "/configsctl.yml"
VAULT_ENV_PREFIX_PATTERN = re.compile(r"^vault_[A-Za-z][A-Za-z0-9_]*_$")
SETTINGS_PREFIX_PATH = ("tofu", "env_key_prefix")

type TomlValue = (
    str | int | float | bool | datetime | date | time | list[TomlValue] | TomlTable
)
type TomlTable = dict[str, TomlValue]


def check_variable_names(variables: Mapping[str, JsonValue]) -> None:
    vault_names = sorted(name for name in variables if name.startswith(VAULT_PREFIX))
    if vault_names:
        raise BuildError(
            "the projection variables must not include vault values: "
            f"{', '.join(vault_names)}"
        )


def forbidden_keys(table: TomlTable, blocked_keys: frozenset[str]) -> list[str]:
    found: list[str] = []
    for key, value in table.items():
        if key in blocked_keys:
            found.append(key)
        found.extend(nested_forbidden_keys(value, blocked_keys))
    return found


def nested_forbidden_keys(value: TomlValue, blocked_keys: frozenset[str]) -> list[str]:
    if isinstance(value, dict):
        return forbidden_keys(value, blocked_keys)
    found: list[str] = []
    if isinstance(value, list):
        for item in value:
            found.extend(nested_forbidden_keys(item, blocked_keys))
    return found


def json_vault_references(value: JsonValue, path: tuple[str, ...]) -> set[str]:
    found: set[str] = set()
    if isinstance(value, dict):
        for key, child in value.items():
            found.update(VAULT_REFERENCE_PATTERN.findall(key))
            found.update(json_vault_references(child, (*path, key)))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            found.update(json_vault_references(child, (*path, str(index))))
    elif isinstance(value, str):
        if path == SETTINGS_PREFIX_PATH and VAULT_ENV_PREFIX_PATTERN.fullmatch(value):
            return found
        found.update(VAULT_REFERENCE_PATTERN.findall(value))
    return found


def reject_encoded_vault_references(destination: str, document: JsonValue) -> None:
    decoded_json = json.dumps(document, ensure_ascii=False)
    references = sorted(set(VAULT_REFERENCE_PATTERN.findall(decoded_json)))
    if references:
        raise BuildError(
            f"{destination} contains vault references: {', '.join(references)}"
        )


def reject_duplicate_json_pairs(
    pairs: list[tuple[str, JsonValue]],
) -> dict[str, JsonValue]:
    document: dict[str, JsonValue] = {}
    for key, value in pairs:
        if key in document:
            raise BuildError(f"duplicate JSON key {key!r} in controller settings")
        document[key] = value
    return document


def settings_vault_references(destination: str, text: str) -> set[str] | None:
    if not destination.endswith(CONFIGSCTL_SETTINGS_SUFFIX):
        return None
    try:
        parsed = json.loads(text, object_pairs_hook=reject_duplicate_json_pairs)
        document = TypeAdapter(JsonValue).validate_python(parsed)
    except json.JSONDecodeError, ValidationError:
        return None
    if not isinstance(document, dict):
        reject_encoded_vault_references(destination, document)
        return None
    tofu = document.get("tofu")
    if not isinstance(tofu, dict):
        reject_encoded_vault_references(destination, document)
        return None
    prefix = tofu.get("env_key_prefix")
    if not isinstance(prefix, str):
        reject_encoded_vault_references(destination, document)
        return None
    if VAULT_ENV_PREFIX_PATTERN.fullmatch(prefix) is None:
        reject_encoded_vault_references(destination, document)
        return None
    return json_vault_references(document, ())


def check_rendered_file(
    destination: str, content: bytes, blocked_keys: frozenset[str]
) -> None:
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as error:
        raise BuildError(f"{destination} does not contain UTF-8 text: {error}") from error
    markers = [marker for marker in TEMPLATE_MARKERS if marker in text]
    if markers:
        raise BuildError(
            f"{destination} contains unresolved template markers: {' '.join(markers)}"
        )
    settings_references = settings_vault_references(destination, text)
    if settings_references is None:
        references = set(VAULT_REFERENCE_PATTERN.findall(text))
    else:
        references = settings_references
    if references:
        raise BuildError(
            f"{destination} contains vault references: {', '.join(sorted(references))}"
        )
    if not destination.endswith(TOML_SUFFIX):
        return
    try:
        document: TomlTable = tomllib.loads(text)
    except tomllib.TOMLDecodeError as error:
        raise BuildError(f"{destination} is not valid TOML: {error}") from error
    found = sorted(set(forbidden_keys(document, blocked_keys)))
    if found:
        raise BuildError(f"{destination} contains secret keys: {', '.join(found)}")


def check_artifact_files(
    files: list[tuple[FileEntry, bytes]],
    blocked_keys: frozenset[str],
    workspace: str,
) -> None:
    checked_hcl = check_hcl_sources(files, workspace)
    for entry, content in files:
        if entry.destination in checked_hcl and raw_hcl_asset(entry, workspace):
            continue
        check_rendered_file(entry.destination, content, blocked_keys)
