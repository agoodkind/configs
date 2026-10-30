from __future__ import annotations

import json
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import cast

from jinja2 import (
    Environment,
    FileSystemLoader,
    StrictUndefined,
    TemplateError,
    UndefinedError,
    meta,
    nodes,
)
from pydantic import JsonValue

from hostartifact.projection import BuildError

TRUE_WORDS = frozenset({"yes", "on", "true", "1"})
FALSE_WORDS = frozenset({"no", "off", "false", "0"})
JINJA_MARKERS = ("{{", "{%", "{#")
NEWLINE = "\n"

type ContextValue = JsonValue | StrictUndefined
type Context = dict[str, ContextValue]
type Filter = (
    Callable[[JsonValue], JsonValue]
    | Callable[[JsonValue, JsonValue, JsonValue], JsonValue]
)


def to_bool(value: JsonValue) -> bool:
    if isinstance(value, bool | int | str):
        word = str(value).lower()
        if word in TRUE_WORDS:
            return True
        if word in FALSE_WORDS:
            return False
    raise BuildError(f"the bool filter cannot convert {value!r}")


def ternary(value: JsonValue, true_value: JsonValue, false_value: JsonValue) -> JsonValue:
    if value:
        return true_value
    return false_value


def to_json(value: JsonValue) -> str:
    return json.dumps(value)


def new_environment(search_path: Path | None) -> Environment:
    loader = None
    if search_path is not None:
        loader = FileSystemLoader(str(search_path))
    environment = Environment(
        loader=loader,
        undefined=StrictUndefined,
        trim_blocks=True,
        autoescape=False,
    )
    # The jinja2 type hints infer Environment.filters from the built-in filter
    # functions. The cast lets this module add the bool, ternary, and to_json
    # filters that the templates use.
    filters = cast("dict[str, Filter]", environment.filters)
    filters["bool"] = to_bool
    filters["ternary"] = ternary
    filters["to_json"] = to_json
    return environment


def trailing_newlines(text: str) -> int:
    return len(text) - len(text.rstrip(NEWLINE))


def restore_trailing_newlines(source: str, rendered: str) -> str:
    missing = trailing_newlines(source) - trailing_newlines(rendered)
    if missing > 0:
        return rendered + NEWLINE * missing
    return rendered


def has_jinja(text: str) -> bool:
    return any(marker in text for marker in JINJA_MARKERS)


class VariableResolver:
    def __init__(self, raw: Mapping[str, JsonValue]) -> None:
        self._raw = raw
        self._environment = new_environment(None)
        self._resolved: dict[str, ContextValue] = {}
        self._in_progress: set[str] = set()

    def context(self, names: set[str]) -> Context:
        context: Context = {}
        for name in sorted(names):
            if name in self._raw:
                context[name] = self._resolve(name)
        return context

    def _resolve(self, name: str) -> ContextValue:
        if name in self._resolved:
            return self._resolved[name]
        if name in self._in_progress:
            raise BuildError(f"variable {name} refers to itself")
        self._in_progress.add(name)
        try:
            resolved: ContextValue = self._resolve_value(self._raw[name])
        except (BuildError, TemplateError) as error:
            resolved = StrictUndefined(hint=f"cannot resolve variable {name}: {error}")
        finally:
            self._in_progress.discard(name)
        self._resolved[name] = resolved
        return resolved

    def _resolve_value(self, value: JsonValue) -> JsonValue:
        if isinstance(value, str) and has_jinja(value):
            return self._render_expression(value)
        if isinstance(value, list):
            return [self._resolve_value(item) for item in value]
        if isinstance(value, dict):
            return {key: self._resolve_value(item) for key, item in value.items()}
        return value

    def _render_expression(self, text: str) -> str:
        names = meta.find_undeclared_variables(self._environment.parse(text))
        template = self._environment.from_string(text)
        return template.render(self.context(names))


def load_template_tree(
    environment: Environment, name: str
) -> list[tuple[Path, nodes.Template]]:
    loader = environment.loader
    if loader is None:
        raise BuildError(
            f"cannot load template {name} because the environment does not have a "
            "template loader"
        )
    loaded: list[tuple[Path, nodes.Template]] = []
    pending = [name]
    seen: set[str] = set()
    while pending:
        current = pending.pop()
        if current in seen:
            continue
        seen.add(current)
        source, filename, _ = loader.get_source(environment, current)
        tree: nodes.Template = environment.parse(source)
        if filename is None:
            raise BuildError(f"template {current} does not have a file path")
        loaded.append((Path(filename), tree))
        for included in meta.find_referenced_templates(tree):
            if included is None:
                raise BuildError(
                    f"template {current} includes a template by a computed name"
                )
            pending.append(included)
    return loaded


def template_variables(environment: Environment, name: str) -> set[str]:
    names: set[str] = set()
    for _, tree in load_template_tree(environment, name):
        names |= meta.find_undeclared_variables(tree)
    return names


def render_template(template_file: Path, resolver: VariableResolver) -> bytes:
    environment = new_environment(template_file.parent)
    try:
        names = template_variables(environment, template_file.name)
        template = environment.get_template(template_file.name)
        rendered = template.render(resolver.context(names))
    except UndefinedError as error:
        raise BuildError(f"{template_file}: unresolved variable: {error}") from error
    except TemplateError as error:
        raise BuildError(f"{template_file}: rendering failed: {error}") from error
    source = template_file.read_text(encoding="utf-8")
    return restore_trailing_newlines(source, rendered).encode("utf-8")
