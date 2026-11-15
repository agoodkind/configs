from __future__ import annotations

import resource
import signal
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent.parent / "files/tack-legacy-proxy-origin.py"
OLD_ORIGIN = "h2c://[3d06:bad:b01::117]:8000"
NEW_ORIGIN = "h2c://[3d06:bad:b01:0:7ac::2]:8000"
HEADER = "http:\n  services:\n    tack-service:\n      loadBalancer:\n        servers:\n"
SERVER = f'          - url: "{OLD_ORIGIN}"\n'
OTHER_SERVICE = (
    "    other-service:\n      loadBalancer:\n        servers:\n"
    f'          - url: "{OLD_ORIGIN}/other"\n'
)
WRITE_LIMIT = 32


def limit_file_writes() -> None:
    signal.signal(signal.SIGXFSZ, signal.SIG_IGN)
    resource.setrlimit(resource.RLIMIT_FSIZE, (WRITE_LIMIT, WRITE_LIMIT))


class LegacyProxyOriginTest(unittest.TestCase):
    def run_editor(self, path: Path) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(path)],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_replaces_only_tack_origin_and_accepts_second_run(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "routes.yml"
            original = HEADER + SERVER + OTHER_SERVICE
            expected = HEADER + SERVER.replace(OLD_ORIGIN, NEW_ORIGIN) + OTHER_SERVICE
            path.write_bytes(original.encode())
            path.chmod(0o640)
            original_stat = path.stat()
            result = self.run_editor(path)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(path.read_bytes(), expected.encode())
            updated_stat = path.stat()
            self.assertEqual(stat.S_IMODE(updated_stat.st_mode), 0o640)
            self.assertEqual(updated_stat.st_uid, original_stat.st_uid)
            self.assertEqual(updated_stat.st_gid, original_stat.st_gid)
            result = self.run_editor(path)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(path.read_bytes(), expected.encode())

    def test_rejects_unexpected_or_ambiguous_target_without_writing(self) -> None:
        invalid_inputs = [
            HEADER + SERVER.replace(OLD_ORIGIN, OLD_ORIGIN + "/other") + OTHER_SERVICE,
            HEADER + SERVER + SERVER + OTHER_SERVICE,
            HEADER + SERVER + "    tack-service:\n      loadBalancer: {}\n",
            OTHER_SERVICE.replace(OLD_ORIGIN + "/other", NEW_ORIGIN),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "routes.yml"
            for original in invalid_inputs:
                with self.subTest(original=original):
                    path.write_bytes(original.encode())
                    result = self.run_editor(path)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(path.read_bytes(), original.encode())

    def test_failed_temporary_write_preserves_original_and_removes_temporary(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "routes.yml"
            original = (HEADER + SERVER + OTHER_SERVICE).encode()
            path.write_bytes(original)
            result = subprocess.run(
                [sys.executable, str(SCRIPT), str(path)],
                capture_output=True,
                text=True,
                check=False,
                preexec_fn=limit_file_writes,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("File too large", result.stderr)
            self.assertEqual(path.read_bytes(), original)
            self.assertEqual(list(Path(directory).iterdir()), [path])


if __name__ == "__main__":
    unittest.main()
