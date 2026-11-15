#!/usr/bin/env python3
from __future__ import annotations

import ipaddress
import os
import re
import stat
import subprocess
import sys
import tempfile
from pathlib import Path

ROUTES_PATH = Path("/etc/tack-search-proxy/traefik.yml")
CALLER_PATTERN = re.compile(r'          - "([^"\r\n]+)"(?P<ending>\r?\n)')
HIERARCHY = ("http", "middlewares", "tack-search-callers", "ipAllowList", "sourceRange")
INDENT_WIDTH = 2
RESTART_TIMEOUT_SECONDS = 45


def save_routes(path: Path, updated: bytes) -> None:
    original_stat = path.stat()
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=".tack-search-", suffix=".tmp", dir=path.parent
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
            raise ValueError("Could not verify the temporary search configuration")
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


def add_caller(path: Path, caller: str) -> bool:
    address = ipaddress.IPv6Address(caller)
    caller_range = f"{address}/128"
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
    if not entries:
        raise ValueError("Expected existing search caller entries")
    existing: list[str] = []
    for index in entries:
        match = CALLER_PATTERN.fullmatch(lines[index])
        if match is None:
            raise ValueError("Expected a quoted search caller CIDR")
        existing.append(str(ipaddress.IPv6Network(match.group(1))))
    if len(existing) != len(set(existing)):
        raise ValueError("Duplicate search caller entries")
    if caller_range in existing:
        return False
    ending = "\n"
    if lines[entries[-1]].endswith("\r\n"):
        ending = "\r\n"
    lines.insert(entries[-1] + 1, f'          - "{caller_range}"{ending}')
    updated = "".join(lines).encode("utf-8")
    save_routes(path, updated)
    return True


def main() -> None:
    if len(sys.argv) != 2:
        raise ValueError("Usage: tack-search-caller.py <node-ipv6-address>")
    if add_caller(ROUTES_PATH, sys.argv[1]):
        subprocess.run(
            ["/usr/bin/systemctl", "restart", "tack-search-proxy.service"],
            check=True,
            timeout=RESTART_TIMEOUT_SECONDS,
        )


if __name__ == "__main__":
    main()
