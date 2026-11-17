import json
from pathlib import Path
import tempfile
import unittest

from scripts.de25_promotion import IDENTITY, promoted_record


HEAD = "25b4d3628d283a4c2ce7a17992965ecd7718a2aa"
MENU_PACKAGE_ID = "d379d05e526d019345a6ef5b47cf4beee23b81689ab77938e00b9e1812462c1f"
ZX81_PACKAGE_ID = "5a9a608d716e8c6f6aa8065a605799797e91b70fa7f58fbf4e87f7449d13509f"


class De25PromotionRunnerResultTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.baseline = {
            "schema": 1,
            **IDENTITY,
            "boot_id": "boot",
            "image_sha256": "image",
            "kit_sw": "bundle",
            "baseline_state_sha256": "native",
            "host_revision": "host",
        }

    def tearDown(self):
        self.temp.cleanup()

    def run_evidence(self, name, result, result_name="result.json"):
        run = self.root / name
        output = run / "out"
        output.mkdir(parents=True)
        (run / "verdict").write_text("PASS\n")
        (output / result_name).write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
        (output / "restore-verification.json").write_text(
            json.dumps({"passed": True, "serial": "TRWU8J8G"})
        )
        return run

    def test_accepts_runner_result_shapes_from_menu_and_zx81_smokes(self):
        # These retain the real runner's top-level fields and checks while
        # omitting captured frame metadata, descriptor details, and other bulk.
        menu = self.run_evidence("menu", {
            "checks": {
                "exact_menu_active": {"detail": "active package id", "ok": True},
                "exact_package_core_load": {"detail": "active core package", "ok": True},
                "loaded_exact_package_id": {"detail": MENU_PACKAGE_ID, "ok": True},
                "stop_returns_idle": {"detail": '{"state":"idle"}', "ok": True},
            },
            "core_id": "fes.menu",
            "head": HEAD,
            "kit": "D25",
            "package": f"/build/package-out/{MENU_PACKAGE_ID}.fcore",
            "package_id": MENU_PACKAGE_ID,
            "verdict": "PASS",
        }, result_name="menu-result.json")
        zx81 = self.run_evidence("zx81", {
            "kit": "D25",
            "target": "de25",
            "head": HEAD,
            "package": f"/build/package-out/{ZX81_PACKAGE_ID}.fcore",
            "checks": {
                "host_ready": {"ok": True, "detail": "ready"},
                "core_install": {"ok": True, "detail": ZX81_PACKAGE_ID},
                "idle_after_stop": {"ok": True, "detail": "idle"},
            },
            "package_id": ZX81_PACKAGE_ID,
            "descriptor": {"format": 2, "core": {"id": "fes.zx81"}},
            "game_id": "fpga-hilq-core-baseline-zx81",
            "core_id": "fes.zx81",
            "verdict": "PASS",
        })
        result = promoted_record(self.baseline, HEAD, menu, zx81, "fixed-time")
        self.assertEqual(result["verified_at_utc"], "fixed-time")

    def test_rejects_runner_result_missing_core_id(self):
        menu = self.run_evidence("menu", {
            "checks": {"stop_returns_idle": {"detail": "idle", "ok": True}},
            "head": HEAD,
            "kit": "D25",
            "package": f"/build/package-out/{MENU_PACKAGE_ID}.fcore",
            "package_id": MENU_PACKAGE_ID,
            "verdict": "PASS",
        }, result_name="menu-result.json")
        zx81 = self.run_evidence("zx81", {
            "head": HEAD,
            "kit": "D25",
            "core_id": "fes.zx81",
            "package_id": ZX81_PACKAGE_ID,
            "verdict": "PASS",
        })
        with self.assertRaisesRegex(ValueError, "expected core"):
            promoted_record(self.baseline, HEAD, menu, zx81)


if __name__ == "__main__":
    unittest.main()
