from __future__ import annotations

import re
import tomllib
from collections.abc import Mapping
from datetime import date, datetime, time

lazy from pydantic import JsonValue

from hostartifact.projection import BuildError

VAULT_PREFIX = "vault_"
TOML_SUFFIX = ".toml"
TEMPLATE_MARKERS = ("{{", "{%", "{#")
VAULT_REFERENCE_PATTERN = re.compile(r"\bvault_[A-Za-z0-9_]+")

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
    references = sorted(set(VAULT_REFERENCE_PATTERN.findall(text)))
    if references:
        raise BuildError(
            f"{destination} contains vault references: {', '.join(references)}"
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
