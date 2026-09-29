"""The opt-in timing route must not overwrite the RAM-test package build."""
from contextlib import contextmanager
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

    @contextmanager
    def diagnostic_fixture(self, stages):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "sources/misteross"
            for relative in timing.PINNED_INPUTS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("fixture\n")
            init_source(root)
            tools = {name: timing.board.AuthenticatedTool(Path("/tools") / name, name)
                     for name in ("yosys", "nextpnr-mistral")}
            calls = []

            def route(**kwargs):
                stage = stages[len(calls)]
                calls.append(kwargs)
                run_dir = kwargs["output"] / "s2-w10-c2"
                run_dir.mkdir()
                log = run_dir / "route.log"
                log_text = stage.get("backend", "Info: backend hip:AMD Radeon RX 7900 XTX ready\n")
                if stage.get("decline") or stage.get("failed"):
                    log_text += ("Local remap: 0 qualified candidates; no candidate applied.\n"
                                 "ERROR: Requested local-remap candidate was not qualified; routing was not started.\n"
                                 if stage.get("decline") else "ERROR: unrelated route failure\n")
                    log.write_text(log_text)
                    return [Candidate(2, 10, 2, False, 0, 0, {}, str(log), str(run_dir))]
                if stage.get("signoff", True):
                    log_text += "Info: Running signoff timing analysis...\n"
                log_text += stage.get("hold", "") + "Info: Program finished normally.\n"
                log.write_text(log_text)
                report = {"fmax": {
                    "display.pixel_clk": {"achieved": stage.get("pixel", 81), "constraint": 74.25},
                    "ram_clock.clocks[0]": {"achieved": stage.get("memory", 109), "constraint": 130},
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
                yield root, calls

    def test_run_requires_live_hip_even_with_complete_clock_rows(self):
        live_hip = "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
        fallback = "Warning: falling back to the CPU reference backend\nInfo: backend cpu-reference ready\n"
        for backend in (fallback, live_hip + fallback, ""):
            for stage in (0, 1):
                with self.subTest(backend=backend, stage=stage):
                    stages = [{}, {"memory": 112}]
                    stages[stage]["backend"] = backend
                    with self.diagnostic_fixture(stages):
                        with self.assertRaisesRegex(timing.board.BuildError, "device backend"):
                            timing.run()

    def test_remap_uses_same_fixture_and_selects_final_gain(self):
        with self.diagnostic_fixture([{}, {"memory": 112, "pixel": 83}]) as (root, calls):
            document = json.loads(timing.run().read_text())
            self.assertEqual(len(calls), 2)
            for key in ("fixture", "device", "qsf", "sdc", "freq", "seeds", "weights",
                        "critexp", "gpu_devices", "env"):
                self.assertEqual(calls[0][key], calls[1][key], key)
            self.assertNotEqual(calls[0]["output"], calls[1]["output"])
            self.assertEqual(calls[1]["extra"][-6:], (
                "--remap-critical", str(calls[0]["output"] / "s2-w10-c2/timing.json"),
                "--remap-candidate", "0", "--remap-groups", "8"))
            self.assertEqual(document["remap_status"], "selected")
            self.assertEqual(document["winner"]["fmax"]["ram_clock.clocks[0]"]["achieved"], 112)
            self.assertTrue(document["valid_clock_set"])
            self.assertFalse(document["winner"]["passing"])
            self.assertEqual(len(document["ranking"]), 2)
            inputs = json.loads((root / timing.OUTPUT / "run-inputs.json").read_text())
            self.assertEqual(inputs["remap"], {"candidate": 0, "groups": 8})

    def test_remap_regression_or_hold_violation_keeps_baseline(self):
        for trial in ({"memory": 108}, {"memory": 112, "pixel": 80},
                      {"memory": 112, "hold": "Warning: Hold/min time violation at test\n"}):
            with self.subTest(trial=trial), self.diagnostic_fixture([{}, trial]):
                document = json.loads(timing.run().read_text())
                self.assertEqual(document["remap_status"], "rejected")
                self.assertEqual(document["winner"]["fmax"]["ram_clock.clocks[0]"]["achieved"], 109)
                self.assertEqual(len(document["ranking"]), 2)

    def test_no_candidate_is_reported_but_other_failures_are_errors(self):
        with self.diagnostic_fixture([{}, {"decline": True}]):
            document = json.loads(timing.run().read_text())
            self.assertEqual(document["remap_status"], "no_candidate")
            self.assertEqual(document["winner"]["fmax"]["ram_clock.clocks[0]"]["achieved"], 109)
        for trial in ({"failed": True}, {"memory": 112, "signoff": False}):
            with self.subTest(trial=trial), self.diagnostic_fixture([{}, trial]):
                with self.assertRaises(timing.board.BuildError):
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
