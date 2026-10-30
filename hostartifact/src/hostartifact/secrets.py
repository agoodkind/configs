from __future__ import annotations

import tomllib
from collections.abc import Mapping
from datetime import date, datetime, time

lazy from pydantic import JsonValue

from hostartifact.projection import BuildError

VAULT_PREFIX = "vault_"
EMBEDS_SECRETS_VARIABLE = "mwan_host_config_embeds_secrets"
FORBIDDEN_KEYS = frozenset({"smtp2go_api_key", "token_secret", "token_id"})
TOML_SUFFIX = ".toml"

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


def forbidden_keys(table: TomlTable) -> list[str]:
    found: list[str] = []
    for key, value in table.items():
        if key in FORBIDDEN_KEYS:
            found.append(key)
        found.extend(nested_forbidden_keys(value))
    return found


def nested_forbidden_keys(value: TomlValue) -> list[str]:
    if isinstance(value, dict):
        return forbidden_keys(value)
    found: list[str] = []
    if isinstance(value, list):
        for item in value:
            found.extend(nested_forbidden_keys(item))
    return found


def check_rendered_file(destination: str, content: bytes) -> None:
    if not destination.endswith(TOML_SUFFIX):
        return
    try:
        document: TomlTable = tomllib.loads(content.decode("utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as error:
        raise BuildError(f"{destination} is not valid TOML: {error}") from error
    found = sorted(set(forbidden_keys(document)))
    if found:
        raise BuildError(f"{destination} contains secret keys: {', '.join(found)}")
