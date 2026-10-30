from __future__ import annotations

import ast
import sys
from pathlib import Path

BANNED_NAME = "object"
SOURCE_SUFFIX = ".py"
FAILURE_EXIT_CODE = 1


def node_annotation(node: ast.AST) -> ast.expr | None:
    if isinstance(node, ast.arg | ast.AnnAssign):
        return node.annotation
    if isinstance(node, ast.FunctionDef | ast.AsyncFunctionDef):
        return node.returns
    return None


def annotation_nodes(tree: ast.Module) -> list[ast.expr]:
    annotations: list[ast.expr] = []
    for node in ast.walk(tree):
        annotation = node_annotation(node)
        if annotation is not None:
            annotations.append(annotation)
    return annotations


def banned_lines(annotation: ast.expr) -> list[int]:
    lines: list[int] = []
    for node in ast.walk(annotation):
        if isinstance(node, ast.Name) and node.id == BANNED_NAME:
            lines.append(node.lineno)
    return lines


def findings_in(path: Path) -> list[str]:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    findings: list[str] = []
    for annotation in annotation_nodes(tree):
        for line in banned_lines(annotation):
            findings.append(f"{path}:{line}: bare object in a type annotation")
    return findings


def source_files(roots: list[str]) -> list[Path]:
    files: list[Path] = []
    for root in roots:
        root_path = Path(root)
        if root_path.is_file():
            files.append(root_path)
        else:
            files.extend(sorted(root_path.rglob(f"*{SOURCE_SUFFIX}")))
    return files


def main(arguments: list[str]) -> int:
    findings: list[str] = []
    for path in source_files(arguments):
        findings.extend(findings_in(path))
    for finding in findings:
        print(finding, file=sys.stderr)
    if findings:
        return FAILURE_EXIT_CODE
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
