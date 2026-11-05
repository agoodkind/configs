from __future__ import annotations

import posixpath
from pathlib import Path
from typing import cast

import hcl2
from hcl2.rules.abstract import LarkElement, LarkRule, LarkToken
from hcl2.rules.base import AttributeRule, BlockRule, StartRule
from hcl2.rules.directives import TemplateForRule, TemplateForStartRule
from hcl2.rules.for_expressions import ForIntroRule, ForObjectExprRule, ForTupleExprRule
from hcl2.rules.indexing import GetAttrExprTermRule
from hcl2.rules.literal_rules import IdentifierRule, LiteralValueRule
from hcl2.rules.strings import HeredocTemplateRule, StringPartRule, StringRule
from hcl2.rules.whitespace import NewLineOrCommentRule
from hcl2.utils import SerializationOptions, process_escape_sequences
from hcl2.walk import walk
from lark.exceptions import LarkError

from hostartifact.projection import REPOSITORY, BuildError, FileEntry
from hostartifact.secret_patterns import TEMPLATE_MARKERS, VAULT_REFERENCE_PATTERN

HCL_SUFFIX = ".tf"
CONFIGS_ASSET_PREFIX = f"asset:{REPOSITORY}:"
VARIABLE_BLOCK = "variable"
SENSITIVE_ATTRIBUTE = "sensitive"
DEFAULT_ATTRIBUTE = "default"
VARIABLE_ROOT = "var"
STRING_CHARS_TOKEN = "STRING_CHARS"
HEREDOC_EXPRESSION_MARKER = "__EXPRESSION__"
HEREDOC_WRAPPER = Path(__file__).with_name("heredoc-expression.hcl")

type ModuleKey = tuple[str, str]


def raw_hcl_asset(entry: FileEntry, workspace: str) -> bool:
    workspace_source_prefix = f"{CONFIGS_ASSET_PREFIX}opentofu/{workspace}/"
    return (
        entry.destination.endswith(HCL_SUFFIX)
        and entry.source.startswith(workspace_source_prefix)
        and entry.source.endswith(HCL_SUFFIX)
    )


def reject_vault_text(destination: str, text: str) -> None:
    references = sorted(set(VAULT_REFERENCE_PATTERN.findall(text)))
    if references:
        raise BuildError(
            f"{destination} contains vault reference {', '.join(references)} outside "
            "a sensitive variable declaration or reference"
        )


def static_string(rule: StringRule) -> str | None:
    parts = cast("list[StringPartRule]", rule.string_parts)
    if len(parts) != 1:
        return None
    content = cast("LarkElement", parts[0].content)
    if not isinstance(content, LarkToken) or content.lark_name() != STRING_CHARS_TOKEN:
        return None
    return process_escape_sequences(str(content.value))


def block_kind(block: BlockRule) -> str | None:
    labels = block.labels
    if not labels or not isinstance(labels[0], IdentifierRule):
        return None
    return str(labels[0].serialize())


def attribute_name(attribute: AttributeRule) -> str:
    return str(attribute.identifier.serialize())


def sensitive_declaration(destination: str, block: BlockRule) -> str | None:
    if block_kind(block) != VARIABLE_BLOCK:
        return None
    labels = block.labels
    names: list[str] = []
    for label in labels[1:]:
        if isinstance(label, StringRule):
            decoded = static_string(label)
            if decoded is None:
                decoded = str(label.serialize())
            names.append(decoded)
        else:
            names.append(str(label.serialize()))
    if not any(VAULT_REFERENCE_PATTERN.search(name) for name in names):
        return None
    if len(labels) != 2 or not isinstance(labels[1], StringRule):
        raise BuildError(
            f"{destination}: a sensitive variable requires exactly one variable label"
        )
    name = static_string(labels[1])
    if name is None or VAULT_REFERENCE_PATTERN.fullmatch(name) is None:
        raise BuildError(
            f"{destination}: a sensitive variable requires one literal vault_ label"
        )
    attributes: list[AttributeRule] = []
    for child in cast("list[LarkElement | None]", block.body.children):
        if isinstance(child, AttributeRule):
            attributes.append(child)
    has_default = any(attribute_name(item) == DEFAULT_ATTRIBUTE for item in attributes)
    sensitive = [
        item for item in attributes if attribute_name(item) == SENSITIVE_ATTRIBUTE
    ]
    is_sensitive = (
        len(sensitive) == 1
        and isinstance(sensitive[0].expression.expression, LiteralValueRule)
        and sensitive[0].expression.expression.serialize() is True
    )
    if has_default or not is_sensitive:
        raise BuildError(
            f"{destination}: variable {name} must be declared once with "
            "sensitive = true and without a default"
        )
    return name


def declared_variables(destination: str, root: StartRule) -> set[str]:
    names: set[str] = set()
    for child in cast("list[LarkElement | None]", root.body.children):
        if not isinstance(child, BlockRule):
            continue
        name = sensitive_declaration(destination, child)
        if name is None:
            continue
        if name in names:
            raise BuildError(f"{destination}: variable {name} is declared more than once")
        names.add(name)
    return names


def iterator_names(
    destination: str, first: IdentifierRule, second: IdentifierRule | None
) -> frozenset[str]:
    names = {str(first.serialize())}
    if second is not None:
        names.add(str(second.serialize()))
    for name in names:
        reject_vault_text(destination, name)
    return frozenset(names)


def inspect_for_intro(
    intro: ForIntroRule,
    declarations: set[str],
    bindings: frozenset[str],
    destination: str,
) -> frozenset[str]:
    inspect_element(intro.iterable, declarations, bindings, destination)
    names = iterator_names(destination, intro.first_iterator, intro.second_iterator)
    return bindings | names


def inspect_template_for(
    rule: TemplateForRule,
    declarations: set[str],
    bindings: frozenset[str],
    destination: str,
) -> None:
    children = cast("list[LarkElement | None]", rule.children)
    start = children[0]
    if not isinstance(start, TemplateForStartRule):
        raise BuildError(f"{destination} has an invalid HCL template for expression")
    inspect_element(start.collection, declarations, bindings, destination)
    names = iterator_names(destination, start.iterator, start.key_iterator)
    for child in children[1:-1]:
        if child is not None:
            inspect_element(child, declarations, bindings | names, destination)


def inspect_heredoc(
    rule: HeredocTemplateRule,
    declarations: set[str],
    bindings: frozenset[str],
    destination: str,
) -> None:
    quoted = rule.serialize(SerializationOptions(preserve_heredocs=False))
    if not isinstance(quoted, str):
        raise BuildError(f"{destination} has an invalid HCL heredoc")
    wrapper = HEREDOC_WRAPPER.read_text(encoding="utf-8")
    try:
        root = hcl2.parses(wrapper.replace(HEREDOC_EXPRESSION_MARKER, quoted))
    except LarkError as error:
        raise BuildError(f"{destination} has an invalid HCL heredoc: {error}") from error
    for child in cast("list[LarkElement | None]", root.body.children):
        if isinstance(child, AttributeRule):
            inspect_element(child.expression, declarations, bindings, destination)
            return
    raise BuildError(f"{destination} has an invalid HCL heredoc")


def inspect_attribute_access(
    rule: GetAttrExprTermRule,
    declarations: set[str],
    bindings: frozenset[str],
    destination: str,
) -> None:
    attribute = str(rule.get_attr.identifier.serialize())
    if VAULT_REFERENCE_PATTERN.search(attribute):
        base = rule.expr_term.expression
        root_is_free_var = (
            isinstance(base, IdentifierRule)
            and str(base.serialize()) == VARIABLE_ROOT
            and VARIABLE_ROOT not in bindings
        )
        if not root_is_free_var or attribute not in declarations:
            reject_vault_text(destination, attribute)
    inspect_element(rule.expr_term, declarations, bindings, destination)


def inspect_element(
    element: LarkElement,
    declarations: set[str],
    bindings: frozenset[str],
    destination: str,
) -> None:
    if isinstance(element, NewLineOrCommentRule):
        return
    if isinstance(element, ForTupleExprRule):
        inner_bindings = inspect_for_intro(
            element.for_intro, declarations, bindings, destination
        )
        inspect_element(element.value_expr, declarations, inner_bindings, destination)
        if element.condition is not None:
            inspect_element(
                element.condition.condition_expr,
                declarations,
                inner_bindings,
                destination,
            )
        return
    if isinstance(element, ForObjectExprRule):
        inner_bindings = inspect_for_intro(
            element.for_intro, declarations, bindings, destination
        )
        inspect_element(element.key_expr, declarations, inner_bindings, destination)
        inspect_element(element.value_expr, declarations, inner_bindings, destination)
        if element.condition is not None:
            inspect_element(
                element.condition.condition_expr,
                declarations,
                inner_bindings,
                destination,
            )
        return
    if isinstance(element, TemplateForRule):
        inspect_template_for(element, declarations, bindings, destination)
        return
    if isinstance(element, GetAttrExprTermRule):
        inspect_attribute_access(element, declarations, bindings, destination)
        return
    if isinstance(element, HeredocTemplateRule):
        inspect_heredoc(element, declarations, bindings, destination)
        return
    if isinstance(element, BlockRule) and block_kind(element) == VARIABLE_BLOCK:
        inspect_element(element.body, declarations, bindings, destination)
        return
    if isinstance(element, StringPartRule):
        content = cast("LarkElement", element.content)
        if isinstance(content, LarkToken):
            value = str(content.value)
            if content.lark_name() == STRING_CHARS_TOKEN:
                value = process_escape_sequences(value)
            reject_vault_text(destination, value)
        else:
            inspect_element(content, declarations, bindings, destination)
        return
    if isinstance(element, LarkToken):
        reject_vault_text(destination, str(element.value))
        return
    if isinstance(element, LarkRule):
        for child in cast("list[LarkElement | None]", element.children):
            if child is not None:
                inspect_element(child, declarations, bindings, destination)


def check_hcl_sources(files: list[tuple[FileEntry, bytes]], workspace: str) -> set[str]:
    parsed_files: list[tuple[str, ModuleKey, StartRule]] = []
    declarations_by_module: dict[ModuleKey, set[str]] = {}
    for entry, content in files:
        if not raw_hcl_asset(entry, workspace):
            continue
        try:
            source = content.decode("utf-8")
        except UnicodeDecodeError as error:
            raise BuildError(
                f"{entry.destination} does not contain UTF-8 text: {error}"
            ) from error
        markers = [marker for marker in TEMPLATE_MARKERS if marker in source]
        if markers:
            raise BuildError(
                f"{entry.destination} contains unresolved template markers: "
                f"{' '.join(markers)}"
            )
        try:
            root = hcl2.parses(source)
        except LarkError as error:
            raise BuildError(f"{entry.destination} is not valid HCL: {error}") from error
        source_path = entry.source.removeprefix(CONFIGS_ASSET_PREFIX)
        module = (posixpath.dirname(source_path), posixpath.dirname(entry.destination))
        names = declarations_by_module.setdefault(module, set())
        found = declared_variables(entry.destination, root)
        repeated = names.intersection(found)
        if repeated:
            raise BuildError(
                f"{entry.destination}: variable {', '.join(sorted(repeated))} is "
                "declared more than once"
            )
        names.update(found)
        parsed_files.append((entry.destination, module, root))

    for destination, module, root in parsed_files:
        for element in walk(root):
            if isinstance(element, NewLineOrCommentRule):
                reject_vault_text(destination, str(element.serialize()))
        inspect_element(root, declarations_by_module[module], frozenset(), destination)
    return {destination for destination, _, _ in parsed_files}
