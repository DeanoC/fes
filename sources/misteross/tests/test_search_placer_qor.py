from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from scripts.search_placer_qor import (
    Candidate,
    SearchError,
    _run_nextpnr,
    _score_report,
    evaluate_pairs,
    extend_seed_sweep,
    plan_staged,
    promote_candidate,
    route_after_synth,
    search,
)


def _candidate(seed: int, weight: int, sys_mhz: float) -> Candidate:
    constraint = 52.0
    pixel = 100.0
    return Candidate(
        seed=seed,
        weight=weight,
        critexp=5,
        passing=sys_mhz >= constraint and pixel >= 74.25,
        worst_ratio=min(sys_mhz / constraint, pixel / 74.25),
        sum_ratio=sys_mhz / constraint + pixel / 74.25,
        fmax={"clk_sys": (sys_mhz, constraint), "pixel_clk": (pixel, 74.25)},
        log="",
        run_dir="",
    )


class SearchPlacerQorTests(unittest.TestCase):
    def test_evaluate_pairs_uses_multiple_workers(self) -> None:
        import threading
        import time

        current = 0
        peak = 0
        lock = threading.Lock()

        def run(seed: int, weight: int) -> Candidate:
            nonlocal current, peak
            with lock:
                current += 1
                peak = max(peak, current)
            time.sleep(0.05)
            with lock:
                current -= 1
            return _candidate(seed, weight, 57.0)

        evaluate_pairs([(1, 10), (2, 10), (3, 10), (4, 10)], run, workers=2)
        self.assertGreaterEqual(peak, 2)

    def test_first_pass_stays_sequential_with_two_gpu_devices(self) -> None:
        import threading
        import time

        current = 0
        peak = 0
        lock = threading.Lock()

        def run(seed: int, weight: int) -> Candidate:
            nonlocal current, peak
            with lock:
                current += 1
                peak = max(peak, current)
            time.sleep(0.03)
            with lock:
                current -= 1
            return _candidate(seed, weight, 51.0 if seed != 2 else 56.0)

        ranked = search(
            nextpnr=Path("nextpnr"),
            fixture=Path("synth.json"),
            output=Path("/tmp"),
            device="5CSEBA6U23I7",
            qsf=Path("x.qsf"),
            sdc=None,
            freq=None,
            seeds=(4, 1, 2),
            weights=(10,),
            critexp=5,
            budget=8,
            mode="first-pass",
            extra=(),
            timeout=1,
            gpu_devices=(0, 1),
            run_one=run,
        )
        self.assertEqual(peak, 1)
        self.assertEqual(ranked[0].seed, 2)

    def test_staged_plan_probes_every_weight_on_diverse_seeds(self) -> None:
        seeds = (4, 1, 2, 3, 5)
        weights = (10, 300, 1000)
        planned = plan_staged(seeds, weights, budget=20)
        self.assertEqual(
            planned,
            [
                (4, 10),
                (2, 10),
                (5, 10),
                (4, 300),
                (2, 300),
                (5, 300),
                (4, 1000),
                (2, 1000),
                (5, 1000),
            ],
        )

    def test_seed_sweep_appends_remaining_seeds_at_winning_weight(self) -> None:
        planned = plan_staged((4, 1, 2, 3), (10, 300), budget=20)
        extended = extend_seed_sweep(planned, (4, 1, 2, 3), 300, budget=20)
        self.assertIn((1, 300), extended)
        self.assertIn((3, 300), extended)
        self.assertEqual(extended.count((4, 300)), 1)

    def test_staged_search_locks_the_weight_that_closes_then_sweeps_seeds(self) -> None:
        table = {
            (4, 10): 51.67,
            (1, 10): 56.45,
            (12, 10): 50.0,
            (4, 300): 57.45,
            (1, 300): 54.82,
            (12, 300): 53.0,
            (4, 1000): 56.62,
            (1, 1000): 54.0,
            (12, 1000): 52.5,
            (2, 300): 55.0,
        }

        def run(seed: int, weight: int) -> Candidate:
            return _candidate(seed, weight, table.get((seed, weight), 40.0))

        ranked = search(
            nextpnr=Path("nextpnr"),
            fixture=Path("synth.json"),
            output=Path("/tmp"),
            device="5CSEBA6U23I7",
            qsf=Path("x.qsf"),
            sdc=None,
            freq="74.25",
            seeds=(4, 1, 2, 12),
            weights=(10, 300, 1000),
            critexp=5,
            budget=24,
            mode="staged",
            extra=(),
            timeout=1,
            run_one=run,
        )
        winner = ranked[0]
        self.assertEqual((winner.seed, winner.weight), (4, 300))
        self.assertTrue(winner.passing)
        evaluated = {(item.seed, item.weight) for item in ranked}
        self.assertIn((2, 300), evaluated)

    def test_first_pass_stops_at_the_first_closing_candidate(self) -> None:
        calls: list[tuple[int, int]] = []

        def run(seed: int, weight: int) -> Candidate:
            calls.append((seed, weight))
            return _candidate(seed, weight, 51.0 if (seed, weight) != (1, 10) else 56.0)

        ranked = search(
            nextpnr=Path("nextpnr"),
            fixture=Path("synth.json"),
            output=Path("/tmp"),
            device="5CSEBA6U23I7",
            qsf=Path("x.qsf"),
            sdc=None,
            freq=None,
            seeds=(4, 1, 2),
            weights=(10, 300),
            critexp=5,
            budget=24,
            mode="first-pass",
            extra=(),
            timeout=1,
            run_one=run,
        )
        self.assertEqual(calls, [(4, 10), (1, 10)])
        self.assertEqual((ranked[0].seed, ranked[0].weight), (1, 10))

    def test_paired_first_pass_reaches_second_weight_before_later_seeds(self) -> None:
        calls: list[tuple[int, int]] = []

        def run(seed: int, weight: int) -> Candidate:
            calls.append((seed, weight))
            return _candidate(seed, weight, 53.0 if (seed, weight) == (12, 300) else 49.0)

        ranked = search(
            nextpnr=Path("nextpnr"), fixture=Path("synth.json"), output=Path("/tmp"),
            device="5CSEBA6U23I7", qsf=Path("x.qsf"), sdc=None, freq="74.25",
            seeds=(10, 5, 12, 2), weights=(1000, 300), critexp=5,
            budget=8, mode="first-pass-paired", extra=(), timeout=1, run_one=run,
        )
        self.assertEqual(calls, [
            (10, 1000), (10, 300), (5, 1000), (5, 300),
            (12, 1000), (12, 300),
        ])
        self.assertEqual((ranked[0].seed, ranked[0].weight), (12, 300))

    def test_paired_first_pass_keeps_remaining_weights_as_fallback(self) -> None:
        calls: list[tuple[int, int]] = []

        def run(seed: int, weight: int) -> Candidate:
            calls.append((seed, weight))
            return _candidate(seed, weight, 49.0)

        search(
            nextpnr=Path("nextpnr"), fixture=Path("synth.json"), output=Path("/tmp"),
            device="5CSEBA6U23I7", qsf=Path("x.qsf"), sdc=None, freq=None,
            seeds=(10, 5), weights=(1000, 300, 2000, 100), critexp=5,
            budget=8, mode="first-pass-paired", extra=(), timeout=1, run_one=run,
        )
        self.assertEqual(calls, [
            (10, 1000), (10, 300), (5, 1000), (5, 300),
            (10, 2000), (5, 2000), (10, 100), (5, 100),
        ])

    def test_ranking_json_shape_from_winner(self) -> None:
        winner = _candidate(4, 300, 57.45)
        payload = {
            "mode": "staged",
            "winner": {
                "seed": winner.seed,
                "weight": winner.weight,
                "passing": winner.passing,
            },
        }
        encoded = json.dumps(payload)
        self.assertIn('"seed": 4', encoded)
        self.assertIn('"weight": 300', encoded)

    def test_promote_copies_winner_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            src = Path(directory) / "s4-w300-c5"
            dest = Path(directory) / "out"
            src.mkdir()
            dest.mkdir()
            for name in ("timing.json", "routed.json", "core.rbf", "route.log"):
                (src / name).write_text(name)
            winner = _candidate(4, 300, 57.45)
            winner = Candidate(**{**winner.__dict__, "run_dir": str(src)})
            promote_candidate(winner, dest)
            self.assertEqual((dest / "nextpnr.log").read_text(), "route.log")
            self.assertEqual((dest / "core.rbf").read_text(), "core.rbf")

    def test_route_after_synth_promotes_the_best_passing_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            dest = Path(directory)

            def run(seed: int, weight: int) -> Candidate:
                run_dir = dest / "qor-search" / f"s{seed}-w{weight}-c5"
                run_dir.mkdir(parents=True, exist_ok=True)
                for name in ("timing.json", "routed.json", "core.rbf", "route.log"):
                    (run_dir / name).write_text(f"{seed}-{weight}-{name}")
                sys_mhz = 57.45 if (seed, weight) == (4, 300) else 51.0
                base = _candidate(seed, weight, sys_mhz)
                return Candidate(**{**base.__dict__, "run_dir": str(run_dir)})

            winner = route_after_synth(
                nextpnr=Path("nextpnr"),
                fixture=dest / "synth.json",
                dest=dest,
                device="5CSEBA6U23I7",
                qsf=Path("x.qsf"),
                sdc=None,
                freq="74.25",
                seeds=(4, 1),
                weights=(10, 300),
                critexp=5,
                budget=8,
                mode="staged",
                extra=(),
                run_one=run,
            )
            self.assertEqual((winner.seed, winner.weight), (4, 300))
            self.assertEqual((dest / "core.rbf").read_text(), "4-300-core.rbf")
            ranking = json.loads((dest / "qor-ranking.json").read_text())
            self.assertEqual(ranking["winner"]["seed"], 4)
            self.assertEqual(ranking["winner"]["weight"], 300)

    def test_score_report_uses_recipe_clocks_not_the_50mhz_input(self) -> None:
        report = {
            "fmax": {
                "clk_sys": {"achieved": 57.45, "constraint": 52.0},
                "pixel_clk": {"achieved": 100.0, "constraint": 74.25},
                "FPGA_CLK1_50": {"achieved": 40.0, "constraint": 50.0},
            }
        }
        passing_all, _, _, _ = _score_report(report)
        self.assertFalse(passing_all)
        passing_recipe, worst, _, _ = _score_report(
            report, required=(("clk_sys", 52.0), (None, 74.25))
        )
        self.assertTrue(passing_recipe)
        self.assertGreater(worst, 1.0)

    def test_finished_nextpnr_exit_1_is_still_scored(self) -> None:
        fake = r"""#!/usr/bin/env python3
import json, pathlib, sys
report = write = rbf = None
args = sys.argv[1:]
for i, arg in enumerate(args):
    if arg == "--report":
        report = pathlib.Path(args[i + 1])
    elif arg == "--write":
        write = pathlib.Path(args[i + 1])
    elif arg == "--rbf":
        rbf = pathlib.Path(args[i + 1])
report.write_text(json.dumps({"fmax": {
    "clk_sys": {"achieved": 57.45, "constraint": 52.0},
    "pixel_clk": {"achieved": 100.0, "constraint": 74.25},
}}))
write.write_text("{}")
rbf.write_bytes(b"rbf")
print("Info: Program finished normally.")
raise SystemExit(1)
"""
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "nextpnr-mistral"
            binary.write_text(fake)
            binary.chmod(0o755)
            output = Path(directory) / "out"
            output.mkdir()
            (Path(directory) / "synth.json").write_text("{}")
            (Path(directory) / "x.qsf").write_text("")
            candidate = _run_nextpnr(
                binary,
                Path(directory) / "synth.json",
                output,
                device="5CSEBA6U23I7",
                qsf=Path(directory) / "x.qsf",
                sdc=None,
                freq="74.25",
                seed=4,
                weight=300,
                critexp=5,
                extra=(),
                timeout=10,
                required=(("clk_sys", 52.0), (None, 74.25)),
            )
            self.assertTrue(candidate.passing)
            self.assertGreater(candidate.worst_ratio, 1.0)

    def test_failed_arc_with_normal_footer_is_not_a_passing_route(self) -> None:
        fake = r"""#!/usr/bin/env python3
import json, pathlib, sys
args = sys.argv[1:]
def path(flag):
    return pathlib.Path(args[args.index(flag) + 1])
path("--report").write_text(json.dumps({"fmax": {
    "clk_sys": {"achieved": 54.0, "constraint": 52.002},
    "pixel_clk": {"achieved": 100.0, "constraint": 74.25},
}}))
path("--write").write_text("{}")
path("--rbf").write_bytes(b"rbf")
print("ERROR: Failed to route arc 6.0 of net 'cpu.NMI_n', from WIRE to GOUT.")
print("Info: Program finished normally.")
"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "nextpnr-mistral"
            binary.write_text(fake)
            binary.chmod(0o755)
            output = root / "out"
            output.mkdir()
            (root / "synth.json").write_text("{}")
            (root / "x.qsf").write_text("")
            candidate = _run_nextpnr(
                binary, root / "synth.json", output,
                device="5CSEBA6U23I7", qsf=root / "x.qsf", sdc=None,
                freq="74.25", seed=5, weight=1000, critexp=5,
                extra=(), timeout=10,
                required=(("clk_sys", 52.0), (None, 74.25)),
            )
            self.assertFalse(candidate.passing)
            self.assertEqual(candidate.worst_ratio, 0.0)

    def test_crashed_nextpnr_is_scored_zero(self) -> None:
        fake = "#!/usr/bin/env python3\nraise SystemExit(1)\n"
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "nextpnr-mistral"
            binary.write_text(fake)
            binary.chmod(0o755)
            output = Path(directory) / "out"
            output.mkdir()
            (Path(directory) / "synth.json").write_text("{}")
            (Path(directory) / "x.qsf").write_text("")
            candidate = _run_nextpnr(
                binary,
                Path(directory) / "synth.json",
                output,
                device="5CSEBA6U23I7",
                qsf=Path(directory) / "x.qsf",
                sdc=None,
                freq=None,
                seed=4,
                weight=10,
                critexp=5,
                extra=(),
                timeout=10,
            )
            self.assertFalse(candidate.passing)
            self.assertEqual(candidate.worst_ratio, 0.0)

    def test_route_after_synth_errors_when_nothing_closes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            dest = Path(directory)

            def run(seed: int, weight: int) -> Candidate:
                run_dir = dest / "qor-search" / f"s{seed}-w{weight}-c5"
                run_dir.mkdir(parents=True, exist_ok=True)
                for name in ("timing.json", "routed.json", "core.rbf", "route.log"):
                    (run_dir / name).write_text("x")
                base = _candidate(seed, weight, 40.0)
                return Candidate(**{**base.__dict__, "run_dir": str(run_dir)})

            with self.assertRaises(SearchError):
                route_after_synth(
                    nextpnr=Path("nextpnr"),
                    fixture=dest / "synth.json",
                    dest=dest,
                    device="5CSEBA6U23I7",
                    qsf=Path("x.qsf"),
                    sdc=None,
                    freq=None,
                    seeds=(4,),
                    weights=(10,),
                    critexp=5,
                    budget=1,
                    mode="first-pass",
                    extra=(),
                    run_one=run,
                )


if __name__ == "__main__":
    unittest.main()
