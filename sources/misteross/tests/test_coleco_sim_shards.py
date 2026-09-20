"""Check simulation shard selection through Make's real dependency graph."""
from collections import Counter
import os
import shlex
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
CASES = ("graphics", "stream", "interactive", "controllers", "vdp-io", "sprites")


def dry_run(*targets, cache=""):
    env = dict(os.environ, FES_TOOLCHAIN_CACHE_ROOT=cache, CACHE_ROOT="")
    return subprocess.run(
        ["make", "--no-print-directory", "--dry-run", "VERILATOR=true", *targets],
        cwd=ROOT, env=env, capture_output=True, text=True, check=False,
    )


def commands(result):
    return Counter(result.stdout.replace("\\\n", "").splitlines())


class ColecoSimulationShardsTest(unittest.TestCase):
    def test_aggregate_keeps_every_shard_and_builds_each_lane_once(self):
        targets = ["sim-fes-coleco-unit", "sim-fes-coleco-unit-oss"]
        targets += [f"sim-fes-coleco-board-{case}{lane}"
                    for lane in ("", "-oss") for case in CASES]
        aggregate = dry_run("sim-fes-coleco")
        shards = dry_run(*targets)
        self.assertEqual(aggregate.returncode, 0, aggregate.stderr)
        self.assertEqual(shards.returncode, 0, shards.stderr)
        self.assertEqual(commands(aggregate), commands(shards))
        lines = list(commands(aggregate).elements())
        self.assertEqual(sum('"' + str(ROOT) + '/cores/fes-coleco/sim/board_tb.cpp"' in line
                             for line in lines), 2)
        self.assertEqual(sum(line.startswith("build/sim/fes-coleco-board")
                             for line in lines), 12)

    def test_oss_aggregate_keeps_only_oss_shards(self):
        aggregate = dry_run("sim-fes-coleco-oss")
        shards = dry_run("sim-fes-coleco-unit-oss",
                         *(f"sim-fes-coleco-board-{case}-oss" for case in CASES))
        self.assertEqual(aggregate.returncode, 0, aggregate.stderr)
        self.assertEqual(shards.returncode, 0, shards.stderr)
        self.assertEqual(commands(aggregate), commands(shards))
        self.assertNotIn("build/sim/fes-coleco-board/Vtop", aggregate.stdout)

    def test_each_board_shard_builds_one_lane_and_generates_its_media(self):
        for case in CASES:
            for lane in ("", "-oss"):
                target = f"sim-fes-coleco-board-{case}{lane}"
                with self.subTest(target=target):
                    result = dry_run(target)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    lines = list(commands(result).elements())
                    builds = [line for line in lines if "--top-module top" in line]
                    self.assertEqual(len(builds), 1)
                    self.assertEqual("-DFES_COLECO_OSS=1" in builds[0], bool(lane))
                    runs = [line for line in lines
                            if line.startswith("build/sim/fes-coleco-board")]
                    self.assertEqual(len(runs), 1)
                    generated = set()
                    for line in lines:
                        args = shlex.split(line)
                        if "--output" in args:
                            generated.add(args[args.index("--output") + 1])
                    media = {arg for arg in shlex.split(runs[0]) if arg.endswith(".rom")}
                    self.assertTrue(media)
                    self.assertLessEqual(media, generated)

    def test_direct_shards_reject_shared_cache_before_diagnostics(self):
        for target in ("sim-fes-coleco-unit", "sim-fes-coleco-unit-oss",
                       *(f"sim-fes-coleco-board-{case}{lane}"
                         for case in CASES for lane in ("", "-oss"))):
            with self.subTest(target=target):
                result = dry_run(target, cache="/unused/shared-cache")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("shared toolchain cache is unsupported", result.stderr)
                self.assertEqual(result.stdout, "")

    def test_unknown_case_fails_instead_of_running_an_aggregate(self):
        result = dry_run("sim-fes-coleco-board-unknown")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("No rule to make target", result.stderr)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
