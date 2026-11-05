from __future__ import annotations

import posixpath
import re
from collections.abc import Callable
from typing import cast

import hcl2
from lark import Token, Tree
from lark.exceptions import LarkError

from hostartifact.projection import REPOSITORY, BuildError, FileEntry

parse_hcl_tree = cast("Callable[[str], Tree[Token]]", hcl2.parses_to_tree)

HCL_SUFFIX = ".tf"
VAULT_REFERENCE_PATTERN = re.compile(r"\bvault_[A-Za-z0-9_]+")
CONFIGS_ASSET_PREFIX = f"asset:{REPOSITORY}:"
TEMPLATE_MARKERS = ("{{", "{%", "{#")
VARIABLE_BLOCK = "variable"
SENSITIVE_ATTRIBUTE = "sensitive"
DEFAULT_ATTRIBUTE = "default"
VARIABLE_ROOT = "var"
TRUE_TOKEN = "TRUE"


def raw_hcl_asset(entry: FileEntry, workspace: str) -> bool:
    workspace_source_prefix = f"{CONFIGS_ASSET_PREFIX}opentofu/{workspace}/"
    return (
        entry.destination.endswith(HCL_SUFFIX)
        and entry.source.startswith(workspace_source_prefix)
        and entry.source.endswith(HCL_SUFFIX)
    )


def identifier(tree: Tree[Token]) -> Token | None:
    if tree.data != "identifier" or len(tree.children) != 1:
        return None
    child = tree.children[0]
    if isinstance(child, Token):
        return child
    return None


def string_label(tree: Tree[Token]) -> Token | None:
    if tree.data != "string":
        return None
    parts = list(tree.find_data("string_part"))
    if len(parts) != 1 or len(parts[0].children) != 1:
        return None
    child = parts[0].children[0]
    if isinstance(child, Token) and child.type == "STRING_CHARS":
        return child
    return None


def variable_declaration(block: Tree[Token]) -> tuple[Token, Tree[Token]] | None:
    if block.data != "block" or len(block.children) < 4:
        return None
    block_type = block.children[0]
    label = block.children[1]
    if not isinstance(block_type, Tree) or not isinstance(label, Tree):
        return None
    block_name = identifier(block_type)
    variable_name = string_label(label)
    if block_name != VARIABLE_BLOCK or variable_name is None:
        return None
    for child in block.children:
        if isinstance(child, Tree) and child.data == "body":
            return variable_name, child
    return None


def attribute_name(attribute: Tree[Token]) -> str | None:
    if attribute.data != "attribute" or not attribute.children:
        return None
    name = attribute.children[0]
    if not isinstance(name, Tree):
        return None
    token = identifier(name)
    if token is None:
        return None
    return str(token)


def literal_true(attribute: Tree[Token]) -> bool:
    if len(attribute.children) < 3:
        return False
    expression = attribute.children[-1]
    if not isinstance(expression, Tree) or expression.data != "expr_term":
        return False
    if len(expression.children) != 1:
        return False
    literal = expression.children[0]
    if not isinstance(literal, Tree) or literal.data != "literal_value":
        return False
    return (
        len(literal.children) == 1
        and isinstance(literal.children[0], Token)
        and (literal.children[0].type == TRUE_TOKEN)
    )


def token_span(token: Token) -> tuple[int, int]:
    if token.start_pos is None or token.end_pos is None:
        raise BuildError("the HCL parser did not report a source token position")
    return token.start_pos, token.end_pos


def sensitive_declarations(
    destination: str, tree: Tree[Token]
) -> dict[str, tuple[int, int]]:
    declarations: dict[str, tuple[int, int]] = {}
    top_level_blocks: list[Tree[Token]] = []
    for child in tree.children:
        if isinstance(child, Tree) and child.data == "body":
            for item in child.children:
                if isinstance(item, Tree) and item.data == "block":
                    top_level_blocks.append(item)
    for block in top_level_blocks:
        parsed = variable_declaration(block)
        if parsed is None:
            continue
        name, body = parsed
        if VAULT_REFERENCE_PATTERN.fullmatch(str(name)) is None:
            continue
        attributes = [
            child
            for child in body.children
            if isinstance(child, Tree) and child.data == "attribute"
        ]
        has_default = any(
            attribute_name(item) == DEFAULT_ATTRIBUTE for item in attributes
        )
        sensitive_attributes = [
            item for item in attributes if attribute_name(item) == SENSITIVE_ATTRIBUTE
        ]
        is_sensitive = len(sensitive_attributes) == 1 and literal_true(
            sensitive_attributes[0]
        )
        if has_default or not is_sensitive or str(name) in declarations:
            raise BuildError(
                f"{destination}: variable {name} must be declared once with "
                "sensitive = true and without a default"
            )
        declarations[str(name)] = token_span(name)
    return declarations


def variable_reference(node: Tree[Token]) -> Token | None:
    if node.data != "get_attr_expr_term" or len(node.children) != 2:
        return None
    root = node.children[0]
    attribute = node.children[1]
    if not isinstance(root, Tree) or not isinstance(attribute, Tree):
        return None
    if root.data != "expr_term" or len(root.children) != 1:
        return None
    root_identifier = root.children[0]
    if not isinstance(root_identifier, Tree):
        return None
    if identifier(root_identifier) != VARIABLE_ROOT:
        return None
    if attribute.data != "get_attr" or len(attribute.children) != 2:
        return None
    member = attribute.children[1]
    if not isinstance(member, Tree):
        return None
    return identifier(member)


def check_hcl_sources(files: list[tuple[FileEntry, bytes]], workspace: str) -> set[str]:
    parsed_files: list[tuple[str, str, str, Tree[Token]]] = []
    declarations_by_directory: dict[str, set[str]] = {}
    declaration_spans: dict[str, set[tuple[int, int]]] = {}
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
            tree = parse_hcl_tree(source)
        except LarkError as error:
            raise BuildError(f"{entry.destination} is not valid HCL: {error}") from error
        source_path = entry.source.removeprefix(CONFIGS_ASSET_PREFIX)
        module_directory = posixpath.dirname(source_path)
        found = sensitive_declarations(entry.destination, tree)
        declarations = declarations_by_directory.setdefault(module_directory, set())
        repeated = declarations.intersection(found)
        if repeated:
            raise BuildError(
                f"{entry.destination}: variable {', '.join(sorted(repeated))} is "
                "declared more than once"
            )
        declarations.update(found)
        declaration_spans[entry.destination] = set(found.values())
        parsed_files.append((entry.destination, source, module_directory, tree))

    for destination, source, module_directory, tree in parsed_files:
        allowed_spans = declaration_spans[destination]
        declarations = declarations_by_directory[module_directory]
        for node in tree.find_data("get_attr_expr_term"):
            token = variable_reference(node)
            if token is not None and str(token) in declarations:
                allowed_spans.add(token_span(token))
        for match in VAULT_REFERENCE_PATTERN.finditer(source):
            if match.span() not in allowed_spans:
                raise BuildError(
                    f"{destination} contains vault reference {match.group()} outside "
                    "a sensitive variable declaration or reference"
                )
    return {destination for destination, _, _, _ in parsed_files}
