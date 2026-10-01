from __future__ import annotations

import argparse
import subprocess
import sys
import time

NANOSECONDS_PER_MICROSECOND = 1000


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
    boot_time_usec = (
        time.clock_gettime_ns(time.CLOCK_BOOTTIME) // NANOSECONDS_PER_MICROSECOND
    )
    print(f'{{"networkd":{result.stdout},"boot_time_usec":{boot_time_usec}}}')


if __name__ == "__main__":
    main()
