import json
from pathlib import Path
import tempfile
import unittest

from scripts.de25_promotion import IDENTITY, promoted_record


HEAD = "a" * 40


class De25PromotionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.baseline = {"schema": 1, **IDENTITY, "boot_id": "boot", "image_sha256": "image",
                         "kit_sw": "bundle", "baseline_state_sha256": "native", "host_revision": "host"}

    def tearDown(self):
        self.temp.cleanup()

    def run_evidence(self, name, verdict="PASS", passed=True, head=HEAD, kit="D25", serial="TRWU8J8G"):
        path = self.root / name
        (path / "out").mkdir(parents=True)
        (path / "verdict").write_text(verdict)
        (path / "out/result.json").write_text(json.dumps({"head": head, "kit": kit}))
        (path / "out/restore-verification.json").write_text(json.dumps({"passed": passed, "serial": serial}))
        return path

    def test_promotion_updates_only_independent_d25_provenance(self):
        menu, zx81 = self.run_evidence("menu"), self.run_evidence("zx81")
        result = promoted_record(self.baseline, HEAD, menu, zx81, "fixed-time")
        for key, value in self.baseline.items():
            self.assertEqual(result[key], value)
        self.assertEqual(result["verified_at_utc"], "fixed-time")
        self.assertIn(HEAD, result["source"])
        self.assertEqual(result["verified_restore_evidence"], f"{menu};{zx81}")

    def test_rejects_nonpass_restore(self):
        menu = self.run_evidence("menu")
        zx81 = self.run_evidence("zx81", passed=False)
        with self.assertRaisesRegex(ValueError, "restore verification"):
            promoted_record(self.baseline, HEAD, menu, zx81)

    def test_rejects_reusing_one_run_for_both_smokes(self):
        menu = self.run_evidence("menu")
        with self.assertRaisesRegex(ValueError, "distinct runs"):
            promoted_record(self.baseline, HEAD, menu, menu)

    def test_rejects_wrong_head_or_kit(self):
        menu = self.run_evidence("menu", head="b" * 40)
        zx81 = self.run_evidence("zx81")
        with self.assertRaisesRegex(ValueError, "exact main head"):
            promoted_record(self.baseline, HEAD, menu, zx81)
        menu = self.run_evidence("menu-wrong-kit", kit="A")
        with self.assertRaisesRegex(ValueError, "exact main head"):
            promoted_record(self.baseline, HEAD, menu, zx81)

    def test_rejects_non_j8g_baseline(self):
        menu, zx81 = self.run_evidence("menu"), self.run_evidence("zx81")
        self.baseline["serial"] = "TRWU8CBL"
        with self.assertRaisesRegex(ValueError, "identity"):
            promoted_record(self.baseline, HEAD, menu, zx81)


if __name__ == "__main__":
    unittest.main()
