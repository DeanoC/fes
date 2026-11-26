"""The Linux-free build requires a deliberate selection, independent of BOARD."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("embedded_experiment", ROOT / "platform/embedded/experiment.py")
experiment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(experiment)


class EmbeddedOptInTests(unittest.TestCase):
    def test_only_explicit_one_enables_builds(self):
        for env in ({}, {"BOARD": "de25-nano"}, {experiment.VARIABLE: ""},
                    {experiment.VARIABLE: "0"}, {experiment.VARIABLE: "true"}):
            with self.subTest(env=env), self.assertRaisesRegex(ValueError, experiment.VARIABLE):
                experiment.require_experiment(env)
        experiment.require_experiment({experiment.VARIABLE: "1"})

    def run_direct(self, board, value, options=()):
        env = dict(os.environ, BOARD="de25-nano")
        env.pop(experiment.VARIABLE, None)
        if value is not None:
            env[experiment.VARIABLE] = value
        (ROOT / "out").mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / "out", prefix="embedded-opt-in-") as name:
            root = Path(name)
            marker, compiler, output = root / "invoked", root / "compiler", root / "output"
            compiler.write_text(
                f"#!{sys.executable}\nfrom pathlib import Path\n"
                f"Path({str(marker)!r}).touch()\nraise SystemExit(99)\n"
            )
            compiler.chmod(0o700)
            result = subprocess.run(
                [sys.executable, str(ROOT / "platform/embedded/tamago/run.py"),
                 "probe", "--board", board, "--compiler", str(compiler),
                 "--output", str(output),
                 *(["--qemu", str(compiler)] if board == "qemu" else []), *options],
                env=env, capture_output=True, text=True, timeout=10,
            )
            self.assertFalse(marker.exists(), result.stdout + result.stderr)
            self.assertFalse(output.exists())
            return result

    def test_direct_qemu_and_nano_reject_without_explicit_opt_in_before_io(self):
        for board in ("qemu", "de25-nano"):
            for value in (None, "", "0", "true"):
                with self.subTest(board=board, value=value):
                    result = self.run_direct(board, value)
                    self.assertEqual(result.returncode, 2)
                    self.assertIn(experiment.VARIABLE + "=1", result.stderr)

    def test_explicit_opt_in_reaches_each_boards_own_validation(self):
        for board, options, message in (
            ("qemu", ("--arm64-features",), "--arm64-features requires --board de25-nano"),
            ("qemu", ("--sd-bus-width", "4"), "SD transport options require --board de25-nano"),
            ("qemu", ("--sd-multi-block",), "SD transport options require --board de25-nano"),
            ("qemu", ("--sd-cmd23",), "SD transport options require --board de25-nano"),
            ("qemu", ("--sd-adma2",), "SD transport options require --board de25-nano"),
            ("de25-nano", (), "--board de25-nano requires --profile from the firmware handoff"),
        ):
            with self.subTest(board=board):
                result = self.run_direct(board, "1", options)
                self.assertEqual(result.returncode, 2)
                self.assertIn(message, result.stderr)
                self.assertNotIn(experiment.VARIABLE, result.stderr)
