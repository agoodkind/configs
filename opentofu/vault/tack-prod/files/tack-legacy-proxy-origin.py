#!/usr/bin/env python3
from __future__ import annotations

import os
import re
import stat
import sys
import tempfile
from pathlib import Path

OLD_ORIGIN = "h2c://[3d06:bad:b01::117]:8000"
NEW_ORIGIN = "h2c://[3d06:bad:b01:0:7ac::2]:8000"
ROUTES_PATH = Path("/etc/traefik/dynamic/routes.yml")
URL_PATTERN = re.compile(r'          - url: "([^"\r\n]+)"(?P<ending>\r?\n)?')
HIERARCHY = ("http", "services", "tack-service", "loadBalancer", "servers")
INDENT_WIDTH = 2


def save_routes(path: Path, updated: bytes) -> None:
    original_stat = path.stat()
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=".tack-routes-", suffix=".tmp", dir=path.parent
    )
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as temporary_file:
            os.fchown(temporary_file.fileno(), original_stat.st_uid, original_stat.st_gid)
            os.fchmod(temporary_file.fileno(), stat.S_IMODE(original_stat.st_mode))
            temporary_file.write(updated)
            temporary_file.flush()
            os.fsync(temporary_file.fileno())
        if temporary_path.read_bytes() != updated:
            raise ValueError("Could not verify the temporary Tack routes")
        os.replace(temporary_path, path)
    finally:
        temporary_path.unlink(missing_ok=True)


def find_block(
    lines: list[str], key: str, start: int, end: int, indent: int
) -> tuple[int, int]:
    expected = " " * indent + key + ":"
    key_pattern = re.compile(
        "^" + " " * indent + "(?:" + re.escape(key) + '|"' + re.escape(key)
        + '"|\'' + re.escape(key) + "')\\s*:"
    )
    matches: list[int] = []
    for index in range(start, end):
        if key_pattern.match(lines[index]):
            matches.append(index)
    if len(matches) != 1:
        raise ValueError(f"Expected exactly one {key} mapping at indentation {indent}")
    if lines[matches[0]].rstrip() != expected:
        raise ValueError(f"Unexpected mapping syntax for {key}")
    block_start = matches[0] + 1
    block_end = end
    for index in range(block_start, end):
        line = lines[index]
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        line_indent = len(line) - len(line.lstrip(" "))
        if line_indent <= indent:
            block_end = index
            break
    return block_start, block_end


def replace_origin(path: Path) -> None:
    original = path.read_bytes()
    lines = original.decode("utf-8").splitlines(keepends=True)
    start = 0
    end = len(lines)
    for depth, key in enumerate(HIERARCHY):
        start, end = find_block(lines, key, start, end, depth * INDENT_WIDTH)
    entries: list[int] = []
    for index in range(start, end):
        if lines[index].strip() and not lines[index].lstrip().startswith("#"):
            entries.append(index)
    if len(entries) != 1:
        raise ValueError("Expected exactly one server in http.services.tack-service")
    index = entries[0]
    match = URL_PATTERN.fullmatch(lines[index])
    if match is None:
        raise ValueError("Expected one quoted URL in http.services.tack-service")
    origin = match.group(1)
    if origin not in (OLD_ORIGIN, NEW_ORIGIN):
        raise ValueError("Unexpected URL in http.services.tack-service")
    if origin == NEW_ORIGIN:
        return
    lines[index] = lines[index].replace(OLD_ORIGIN, NEW_ORIGIN, 1)
    verified = URL_PATTERN.fullmatch(lines[index])
    if verified is None or verified.group(1) != NEW_ORIGIN:
        raise ValueError("Could not verify the replacement Tack URL")
    updated = "".join(lines).encode("utf-8")
    save_routes(path, updated)


def main() -> None:
    path = ROUTES_PATH
    if len(sys.argv) == 2:
        path = Path(sys.argv[1])
    elif len(sys.argv) != 1:
        raise ValueError("Usage: tack-legacy-proxy-origin.py [routes-path]")
    replace_origin(path)


if __name__ == "__main__":
    main()
