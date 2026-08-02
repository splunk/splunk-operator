from __future__ import annotations

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("render_shc_final_manifest.py")
TEMPLATE = Path(__file__).with_name("shc-final-qualification-cluster.yaml.in")
RUNTIME_IMAGE = "registry.example.test/splunk:qualification@sha256:" + "a" * 64


class RenderSHCFinalManifestTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def run_renderer(
        self, output: Path, *, runtime_image: str = RUNTIME_IMAGE
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--template",
                str(TEMPLATE),
                "--output",
                str(output),
                "--namespace",
                "shc-final-test",
                "--runtime-image",
                runtime_image,
                "--s3-bucket",
                "qualification-bucket-123",
                "--s3-prefix",
                "campaign/final",
            ],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_manifest_is_reproducible_and_fully_resolved(self) -> None:
        first = self.root / "first.yaml"
        second = self.root / "second.yaml"
        first_result = self.run_renderer(first)
        second_result = self.run_renderer(second)

        self.assertEqual(first_result.returncode, 0, first_result.stderr)
        self.assertEqual(second_result.returncode, 0, second_result.stderr)
        self.assertEqual(first.read_bytes(), second.read_bytes())
        rendered = first.read_text(encoding="utf-8")
        self.assertNotRegex(rendered, r"__[A-Z0-9_]+__")
        self.assertEqual(rendered.count(f"image: {RUNTIME_IMAGE}"), 4)
        self.assertEqual(rendered.count("namespace: shc-final-test"), 4)
        self.assertIn("name: shc-final-test", rendered)
        self.assertEqual(
            rendered.count("path: qualification-bucket-123/campaign/final/"), 2
        )
        self.assertIn("podUpdateStrategy: RollingUpdate", rendered)
        indexer_document = rendered.split("kind: IndexerCluster", maxsplit=1)[1].split(
            "\n---", maxsplit=1
        )[0]
        self.assertNotIn(
            "readinessProbe:",
            indexer_document,
            "IndexerCluster must inherit the lifecycle serving-readiness profile",
        )

    def test_mutable_image_is_rejected_without_output(self) -> None:
        output = self.root / "invalid.yaml"
        result = self.run_renderer(output, runtime_image="registry.example.test/splunk:latest")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("immutable @sha256 digest", result.stderr)
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
