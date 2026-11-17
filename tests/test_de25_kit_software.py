import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from scripts.de25_kit_software import runtime_digest, verify_runtime


HEAD = "a" * 40
RUNTIME = "b" * 64


class De25KitSoftwareTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.bundle = Path(self.temp.name) / "bundle.tar"

    def tearDown(self):
        self.temp.cleanup()

    def make_bundle(self, *, board="de25-nano", head=HEAD, digest=RUNTIME):
        manifest = {"board": board, "architecture": "aarch64",
                    "provenance": {"fogcast": head, "runtime": head},
                    "files": [{"path": "bin/mister-runtime", "sha256": digest}]}
        payload = json.dumps(manifest).encode()
        with tarfile.open(self.bundle, "w") as archive:
            member = tarfile.TarInfo("manifest.json")
            member.size = len(payload)
            archive.addfile(member, io.BytesIO(payload))

    def test_reads_exact_de25_runtime_digest(self):
        self.make_bundle()
        self.assertEqual(runtime_digest(self.bundle, HEAD), RUNTIME)

    def test_rejects_wrong_revision_or_board(self):
        self.make_bundle(head="c" * 40)
        with self.assertRaisesRegex(ValueError, "exact requested head"):
            runtime_digest(self.bundle, HEAD)
        self.make_bundle(board="de10-nano")
        with self.assertRaisesRegex(ValueError, "not for DE25"):
            runtime_digest(self.bundle, HEAD)

    def test_running_runtime_must_match_manifest(self):
        verify_runtime(RUNTIME, RUNTIME)
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            verify_runtime(RUNTIME, "c" * 64)

    def test_powerboat_runner_builds_installs_verifies_and_restores_bundle(self):
        root = Path(__file__).resolve().parents[1]
        runner = (root / "scripts/powerboat/hil-de25-core.sh").read_text()
        promoter = (root / "scripts/powerboat/de25-baseline-auto.sh").read_text()
        sealer = (root / "scripts/powerboat/de25-seal-auto.sh").read_text()
        self.assertIn("scripts/kit_software.py", runner)
        self.assertIn("--head \"$HEAD\"", runner)
        self.assertIn("--action install --bundle \"$SW_BUNDLE\"", runner)
        self.assertIn("--action activate --candidate \"$SW_BUNDLE_ID\"", runner)
        self.assertIn("verify-runtime --expected \"$SW_RUNTIME_SHA\"", runner)
        self.assertIn("--action activate --candidate \"$BASE_SW\"", runner)
        self.assertIn("software-status-candidate.json", runner)
        self.assertIn("software-status-baseline-restore.json", runner)
        self.assertGreaterEqual(runner.count(".pending == null"), 2)
        self.assertIn('"$Q/hil-de25-core.sh" D25', promoter)
        self.assertIn('de25-promotion-${HEAD:0:8}.heartbeat', promoter)
        self.assertIn('git -C "$R" fetch -q origin "$HEAD"', sealer)


if __name__ == "__main__":
    unittest.main()
