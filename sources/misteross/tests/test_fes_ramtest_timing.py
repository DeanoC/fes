"""The opt-in timing route must not overwrite the RAM-test package build."""
import json
import tomllib
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_ramtest as ramtest
from scripts import diagnose_fes_ramtest_timing as timing
from scripts.search_placer_qor import Candidate, _score_report
from tests.producer_fixture import FakeInvocation, init_source


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

    def test_run_requires_live_hip_even_with_complete_clock_rows(self):
        live_hip = "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
        fallback = "Warning: falling back to the CPU reference backend\nInfo: backend cpu-reference ready\n"
        for log_text, accepted in ((live_hip, True), (fallback, False),
                                   (live_hip + fallback, False), ("", False)):
            with self.subTest(log=log_text), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary) / "sources/misteross"
                for relative in timing.PINNED_INPUTS:
                    path = root / relative
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text("fixture\n")
                init_source(root)
                tools = {name: timing.board.AuthenticatedTool(Path("/tools") / name, name)
                         for name in ("yosys", "nextpnr-mistral")}

                def route(**kwargs):
                    run_dir = kwargs["output"] / "s2-w10-c2"
                    run_dir.mkdir()
                    log = run_dir / "route.log"
                    log.write_text(log_text + "Info: Program finished normally.\n")
                    report = {"fmax": {
                        "display.pixel_clk": {"achieved": 81, "constraint": 74.25},
                        "ram_clock.clocks[0]": {"achieved": 109, "constraint": 130},
                        "ram_clock.clocks[1]": {"achieved": 465, "constraint": 130},
                    }}
                    (run_dir / "timing.json").write_text(json.dumps(report))
                    (run_dir / "core.rbf").write_bytes(b"diagnostic fixture")
                    passing, worst, total, fmax = _score_report(report, timing.REQUIRED_CLOCKS)
                    return [Candidate(2, 10, 2, passing, worst, total, fmax, str(log), str(run_dir))]

                with (patch.object(timing, "ROOT", root),
                      patch.object(Path, "cwd", return_value=root),
                      patch.object(timing.board, "_authenticate_tools", return_value=tools),
                      patch.object(timing.board, "_run_tool"),
                      patch.object(timing, "FunctionalInvocation", FakeInvocation),
                      patch.object(timing, "search", side_effect=route)):
                    if accepted:
                        document = json.loads(timing.run().read_text())
                        self.assertTrue(document["valid_clock_set"])
                        self.assertFalse(document["winner"]["passing"])
                    else:
                        with self.assertRaisesRegex(timing.board.BuildError, "device backend"):
                            timing.run()

    def test_required_clocks_reject_missing_capture_domain(self):
        report = {"fmax": {
            "display.pixel_clk": {"achieved": 77, "constraint": 74.25007},
            "ram_clock.clocks[0]": {"achieved": 117, "constraint": 130.0052},
        }}
        with self.assertRaisesRegex(ValueError, "ram_clock.clocks\\[1\\]"):
            _score_report(report, timing.REQUIRED_CLOCKS)


if __name__ == "__main__":
    unittest.main()
