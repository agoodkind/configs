#!/usr/bin/env python3
"""Read a TOML document on standard input and print it as JSON on standard output.

A spec renders a real Jinja template to a TOML file and then asserts on the
keys a Go program loads from that file. Reading the rendered document with a
TOML parser rather than with a regular expression is what makes the spec's
reading the program's reading: a key under the wrong table, a string where an
integer belongs, or a broken array fails here rather than passing a text match.
"""

from __future__ import annotations

import json
import sys
import tomllib

type JsonValue = str | int | float | bool | None | list[JsonValue] | dict[str, JsonValue]


def main() -> int:
    document: dict[str, JsonValue] = tomllib.loads(sys.stdin.read())
    json.dump(document, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
