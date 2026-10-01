from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
from typing import TypeAlias

NANOSECONDS_PER_MICROSECOND = 1000

JsonValue: TypeAlias = (
    str | int | float | bool | None | list["JsonValue"] | dict[str, "JsonValue"]
)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("interface")
    arguments = parser.parse_args()
    result: subprocess.CompletedProcess[str] = subprocess.run(
        ["networkctl", "status", arguments.interface, "--json=short"],
        capture_output=True,
        text=True,
        check=False,
    )
    sys.stderr.write(result.stderr)
    if result.returncode != 0:
        sys.exit(result.returncode)
    document: JsonValue = json.loads(result.stdout)
    if not isinstance(document, dict):
        raise ValueError("networkctl status must return a JSON object")
    boot_time_usec = (
        time.clock_gettime_ns(time.CLOCK_BOOTTIME) // NANOSECONDS_PER_MICROSECOND
    )
    sample: dict[str, JsonValue] = {
        "networkd": document,
        "boot_time_usec": boot_time_usec,
    }
    print(json.dumps(sample))


if __name__ == "__main__":
    main()
