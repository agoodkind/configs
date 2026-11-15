from __future__ import annotations

import importlib.util
import stat
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("tack-search-caller.py")
SPECIFICATION = importlib.util.spec_from_file_location("search_caller", SCRIPT)
assert SPECIFICATION is not None
assert SPECIFICATION.loader is not None
MODULE = importlib.util.module_from_spec(SPECIFICATION)
SPECIFICATION.loader.exec_module(MODULE)
CALLER = "3d06:bad:b01:210::228"
HEADER = (
    'http:\n  middlewares:\n    tack-search-callers:\n'
    '      ipAllowList:\n        sourceRange:\n'
)
ENTRIES = '          - "3d06:bad:b01:210::217/128"\n'
SUFFIX = '  services:\n    health:\n      header: "preserve synthetic value"\n'


class SearchCallerTest(unittest.TestCase):
    def test_append_preserves_bytes_metadata_and_idempotency(self) -> None:
        for ending in ("\n", "\r\n"):
            with self.subTest(ending=repr(ending)), tempfile.TemporaryDirectory() as folder:
                path = Path(folder) / "traefik.yml"
                original = (HEADER + ENTRIES + SUFFIX).replace("\n", ending).encode()
                path.write_bytes(original)
                path.chmod(0o600)
                original_stat = path.stat()
                self.assertTrue(MODULE.add_caller(path, CALLER))
                insertion = f'          - "{CALLER}/128"{ending}'.encode()
                expected = (HEADER + ENTRIES).replace("\n", ending).encode()
                expected += insertion + SUFFIX.replace("\n", ending).encode()
                self.assertEqual(path.read_bytes(), expected)
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
                self.assertEqual(path.stat().st_uid, original_stat.st_uid)
                self.assertEqual(path.stat().st_gid, original_stat.st_gid)
                unchanged_stat = path.stat()
                self.assertFalse(MODULE.add_caller(path, CALLER))
                self.assertEqual(path.stat().st_ino, unchanged_stat.st_ino)
                self.assertEqual(path.read_bytes(), expected)

    def test_rejects_ambiguous_or_unexpected_structure_without_writing(self) -> None:
        for configuration in (
            HEADER + ENTRIES + ENTRIES + SUFFIX,
            HEADER.replace("ipAllowList:", "ipAllowList: {}") + ENTRIES + SUFFIX,
            HEADER + '          - "invalid"\n' + SUFFIX,
            HEADER + ENTRIES + '    tack-search-callers: {}\n' + SUFFIX,
            HEADER.replace("http:\n", "http:\n other:\n") + ENTRIES + SUFFIX,
        ):
            with self.subTest(configuration=configuration), tempfile.TemporaryDirectory() as folder:
                path = Path(folder) / "traefik.yml"
                path.write_text(configuration)
                with self.assertRaises(ValueError):
                    MODULE.add_caller(path, CALLER)
                self.assertEqual(path.read_text(), configuration)


if __name__ == "__main__":
    unittest.main()
