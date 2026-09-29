"""The opt-in timing route must not overwrite the RAM-test package build."""
import tomllib
import tempfile
import unittest
from pathlib import Path

from scripts import build_fes_ramtest as ramtest
from scripts import diagnose_fes_ramtest_timing as timing
from scripts.search_placer_qor import _score_report


class RamtestTimingDiagnosticTest(unittest.TestCase):
    def test_compiler_lock_matches_diagnostic_and_stays_separate(self):
        lock = tomllib.loads((timing.ROOT / timing.LOCK).read_text())
        self.assertEqual(lock["tool"]["yosys"]["commit"], timing.TOOL_COMMITS["yosys"])
        self.assertEqual(lock["tool"]["nextpnr"]["commit"], timing.TOOL_COMMITS["nextpnr"])
        self.assertNotEqual(timing.LOCK, ramtest.TOOLCHAIN_LOCK)
        self.assertNotEqual(timing.OUTPUT, ramtest.OUTPUT_130)

    def test_synthesis_writes_diagnostic_fixture_without_changing_normal_path(self):
        tools = {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")}
        build_id = "1" * 32
        normal = ramtest.build_commands(timing.ROOT, build_id, tools, memory_mhz=130)
        diagnostic = ramtest.build_commands(timing.ROOT, build_id, tools,
                                            memory_mhz=130, output_relative=timing.OUTPUT)
        self.assertIn("build/fes-ramtest-130/synth.json", normal[0][2])
        self.assertIn("build/fes-ramtest-timing-130/synth.json", diagnostic[0][2])
        self.assertIn("RAM_130_ONLY=1", diagnostic[0][2])
        self.assertIn("build/fes-ramtest-130/synth.json", normal[1])
        self.assertIn("build/fes-ramtest-timing-130/synth.json", diagnostic[1])

    def test_search_output_rejects_symlink_and_clears_stale_routes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            external = root / "external"
            external.mkdir()
            routes = root / "qor-search"
            routes.symlink_to(external)
            with self.assertRaisesRegex(ValueError, "non-symlink"):
                timing.fresh_directory(routes)
            self.assertTrue(external.is_dir())
            routes.unlink()
            routes.mkdir()
            (routes / "old-route.json").write_text("stale")
            timing.fresh_directory(routes)
            self.assertEqual(list(routes.iterdir()), [])

    def test_required_clocks_reject_missing_capture_domain(self):
        report = {"fmax": {
            "display.pixel_clk": {"achieved": 77, "constraint": 74.25007},
            "ram_clock.clocks[0]": {"achieved": 117, "constraint": 130.0052},
        }}
        with self.assertRaisesRegex(ValueError, "ram_clock.clocks\\[1\\]"):
            _score_report(report, timing.REQUIRED_CLOCKS)


if __name__ == "__main__":
    unittest.main()
