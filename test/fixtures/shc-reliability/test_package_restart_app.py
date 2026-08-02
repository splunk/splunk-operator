from __future__ import annotations

import hashlib
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("package_restart_app.py")


class PackageRestartAppTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.source = self.root / "qualification_app"
        (self.source / "default").mkdir(parents=True)
        (self.source / "default" / "app.conf").write_text(
            "[install]\nstate_change_requires_restart = true\n\n"
            "[launcher]\nversion = 1.0.0\n",
            encoding="utf-8",
        )
        (self.source / "default" / "inputs.conf").write_text(
            "[splunktcp://9997]\ndisabled = false\n",
            encoding="utf-8",
        )
        self.original_app_conf = (self.source / "default" / "app.conf").read_bytes()

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def run_packager(self, output: Path, version: str = "2.3.4") -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--source-dir",
                str(self.source),
                "--version",
                version,
                "--output",
                str(output),
            ],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_archive_is_reproducible_and_normalized(self) -> None:
        first = self.root / "first.tgz"
        second = self.root / "second.tgz"
        first_result = self.run_packager(first)
        second_result = self.run_packager(second)

        self.assertEqual(first_result.returncode, 0, first_result.stderr)
        self.assertEqual(second_result.returncode, 0, second_result.stderr)
        self.assertEqual(first.read_bytes(), second.read_bytes())
        self.assertIn(hashlib.sha256(first.read_bytes()).hexdigest(), first_result.stdout)
        self.assertEqual((self.source / "default" / "app.conf").read_bytes(), self.original_app_conf)

        with tarfile.open(first, mode="r:gz") as archive:
            names = archive.getnames()
            self.assertEqual(names, sorted(names))
            app_conf = archive.extractfile("qualification_app/default/app.conf")
            self.assertIsNotNone(app_conf)
            self.assertIn(b"version = 2.3.4", app_conf.read())
            for member in archive.getmembers():
                self.assertEqual(member.mtime, 0)
                self.assertEqual(member.uid, 0)
                self.assertEqual(member.gid, 0)
                self.assertEqual(member.uname, "")
                self.assertEqual(member.gname, "")
                self.assertEqual(member.mode, 0o755 if member.isdir() else 0o644)

    def test_invalid_version_is_rejected_without_output(self) -> None:
        output = self.root / "invalid.tgz"
        result = self.run_packager(output, version="candidate")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("numeric major.minor.patch", result.stderr)
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
