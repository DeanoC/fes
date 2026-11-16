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

    def run_evidence(self, name, verdict="PASS", passed=True, head=HEAD, kit="D25", serial="TRWU8J8G",
                     result_name="result.json", core_id=None, package_id=None):
        path = self.root / name
        (path / "out").mkdir(parents=True)
        (path / "verdict").write_text(verdict)
        if core_id is None:
            core_id = "fes.menu" if name.startswith("menu") else "fes.zx81"
        if package_id is None:
            package_id = "a" * 64 if core_id == "fes.menu" else "b" * 64
        (path / "out" / result_name).write_text(json.dumps({
            "head": head, "kit": kit, "core_id": core_id, "package_id": package_id,
        }))
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

    def test_rejects_same_package_for_both_smokes(self):
        menu = self.run_evidence("menu")
        zx81 = self.run_evidence("zx81", package_id="a" * 64)
        with self.assertRaisesRegex(ValueError, "distinct packages"):
            promoted_record(self.baseline, HEAD, menu, zx81)

    def test_rejects_package_identity_for_wrong_core(self):
        menu = self.run_evidence("menu", core_id="fes.zx81")
        zx81 = self.run_evidence("zx81")
        with self.assertRaisesRegex(ValueError, "expected core"):
            promoted_record(self.baseline, HEAD, menu, zx81)

    def test_rejects_missing_or_invalid_package_identity(self):
        menu = self.run_evidence("menu", package_id="not-a-package-id")
        zx81 = self.run_evidence("zx81")
        with self.assertRaisesRegex(ValueError, "valid package identity"):
            promoted_record(self.baseline, HEAD, menu, zx81)

    def test_accepts_menu_smoke_result_filename(self):
        menu = self.run_evidence("menu", result_name="menu-result.json")
        zx81 = self.run_evidence("zx81")
        self.assertEqual(promoted_record(self.baseline, HEAD, menu, zx81)["schema"], 1)

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
