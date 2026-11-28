from contextlib import redirect_stderr, redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPTS))
spec = importlib.util.spec_from_file_location("nightly_kit_worker", SCRIPTS / "powerboat/nightly-kit-worker.py")
worker = importlib.util.module_from_spec(spec)
_IMPORT_HOME = tempfile.TemporaryDirectory()
with patch.object(Path, "home", return_value=Path(_IMPORT_HOME.name)):
    spec.loader.exec_module(worker)
CANDIDATE_ARGUMENTS_SOURCE = Path(worker.candidate_arguments.__code__.co_filename).resolve()


class SimulatedClock:
    def __init__(self):
        self.now = 0.0
        self.sleeps = []
        self.on_sleep = None

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.sleeps.append(seconds)
        self.now += seconds
        if self.on_sleep:
            self.on_sleep(len(self.sleeps), seconds)


class NightlyKitWorkerTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        self.home = root / "home"
        self.home.mkdir()
        self.base = self.home / ".local/share/fes-qualification"
        self.repo = self.home / "fes"
        self.queue = self.home / "lander-hilq"
        self.clock = SimulatedClock()
        self.driver_calls = []
        self.driver_effect = self.pass_driver
        self.request = None
        self.binding = None
        self.config_count = 0

        environment = patch.dict(os.environ, {"HOME": str(self.home)}, clear=True)
        environment.start()
        self.addCleanup(environment.stop)
        for name, value in (("BASE", self.base), ("REPO", self.repo), ("QUEUE", self.queue)):
            p = patch.object(worker, name, value)
            p.start()
            self.addCleanup(p.stop)
        monotonic = patch.object(worker.time, "monotonic", self.clock.monotonic)
        sleep = patch.object(worker.time, "sleep", self.clock.sleep)
        monotonic.start()
        sleep.start()
        self.addCleanup(monotonic.stop)
        self.addCleanup(sleep.stop)
        self._patch(worker, "fetch", side_effect=self.fetch)
        self._patch(worker, "candidate_arguments", side_effect=self.candidate_arguments)
        self._patch(worker.subprocess, "run", side_effect=self.subprocess_run)
        self._patch(worker.subprocess, "check_output", side_effect=self.check_output)

    def _patch(self, obj, name, **kwargs):
        p = patch.object(obj, name, **kwargs)
        self.addCleanup(p.stop)
        return p.start()

    def configure(self, board="de10-nano", kits=("B", "A"), core="fes.pong"):
        self.config_count += 1
        head = "a" * 40
        self.request = {
            "main": head,
            "board": board,
            "core": core,
            "inputs": f"{self.config_count:064x}",
            "package_id": "c" * 64,
            "prepared_sha256": "d" * 64,
            "archive_sha256": "e" * 64,
            "hil": {"kits": list(kits), "check": None},
        }
        simulation = {
            "main": head,
            "run_url": "https://github.com/DeanoC/fes/actions/runs/1/attempts/1",
        }
        simulation_raw = json.dumps(simulation).encode()
        self.request["simulations_sha256"] = worker.sha(simulation_raw)
        self.binding = worker.sha(worker.canonical(self.request))
        folder = self.base / "runs" / self.binding
        build = folder / ("main-" + head[:8])
        build.mkdir(parents=True)
        prepared = {"sources": {"misteross": head}, "core_id": core}
        (build / "prepared.json").write_text(json.dumps(prepared))
        (build / "simulations.json").write_bytes(simulation_raw)
        platform = board.replace("-", "_")
        with tarfile.open(build / "core.fcore", "w") as archive:
            data = f'[target]\nplatform = "{platform}"\n[core]\nid = "{core}"\n'.encode()
            info = tarfile.TarInfo("manifest.toml")
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
        self.kit_status = self.home / "kit-status"
        self.kit_status.mkdir(exist_ok=True)
        self.ledger_paths = {}
        for name in kits:
            kit = "D25" if name == "TRWU8J8G" else name
            ledger = "de25-TRWU8J8G-STATUS.md" if kit == "D25" else f"kit{kit}-STATUS.md"
            path = self.kit_status / ledger
            path.write_text(f"## KIT {kit} CLAIMED by test\n")
            self.ledger_paths[kit] = path
        return folder, build

    def set_status(self, kit, heading):
        self.ledger_paths[kit].write_text(heading + "\n")

    def fetch(self, relative):
        self.assertEqual(relative, "requests/" + self.binding)
        return worker.canonical({"request": self.request})

    def candidate_arguments(self, prepared, provenance):
        provenance.update({key: self.request[key] for key in
                           ("package_id", "prepared_sha256", "archive_sha256")})

    def check_output(self, argv, **kwargs):
        if "--paginate" in argv:
            return b"completed"
        return json.dumps({
            "head_sha": self.request["main"],
            "head_branch": "main",
            "path": ".github/workflows/nightly.yml",
            "event": "schedule",
        }).encode()

    def subprocess_run(self, argv, **kwargs):
        if argv[0] == "git":
            return subprocess.CompletedProcess(argv, 0)
        self.assertIn("hil-", str(argv[0]))
        kit = argv[1]
        evidence = Path(argv[5])
        self.driver_calls.append(kit)
        returncode = self.driver_effect(kit, evidence, len(self.driver_calls))
        return subprocess.CompletedProcess(argv, returncode)

    def write_driver_result(self, evidence, verdict="PASS", returncode=0, identity=None):
        (evidence / "verdict").write_text(verdict + "\n")
        if verdict == "PASS":
            out = evidence / "out"
            out.mkdir()
            result = {
                "verdict": "PASS",
                "kit": identity or ("D25" if self.request["board"] == "de25-nano" else evidence.name.split("-")[0]),
                "head": self.request["main"],
                "package_id": self.request["package_id"],
            }
            filename = "menu-result.json" if result["kit"] == "D25" and self.request["core"] == "fes.menu" else "result.json"
            (out / filename).write_text(json.dumps(result))
        return returncode

    def pass_driver(self, kit, evidence, call_number):
        return self.write_driver_result(evidence, identity=kit)

    def invoke(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        try:
            with redirect_stdout(stdout), redirect_stderr(stderr):
                raw = worker.accept(self.binding)
            error = None
        except Exception as caught:
            raw, error = None, caught
        return raw, error, stdout.getvalue(), stderr.getvalue()

    def test_environment_defaults_overrides_zero_and_invalid_values(self):
        self.assertEqual(CANDIDATE_ARGUMENTS_SOURCE, (SCRIPTS / "core_dev_accept.py").resolve())
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(worker.kit_wait_settings(), (2700, 60))
        with patch.dict(os.environ, {
            "FES_NIGHTLY_KIT_WAIT_SECONDS": "125",
            "FES_NIGHTLY_KIT_POLL_SECONDS": "7",
        }, clear=True):
            self.assertEqual(worker.kit_wait_settings(), (125, 7))
        with patch.dict(os.environ, {"FES_NIGHTLY_KIT_WAIT_SECONDS": "0"}, clear=True):
            self.assertEqual(worker.kit_wait_settings(), (0, 60))
        for name, value in (
            ("FES_NIGHTLY_KIT_WAIT_SECONDS", "abc"),
            ("FES_NIGHTLY_KIT_WAIT_SECONDS", "-1"),
            ("FES_NIGHTLY_KIT_POLL_SECONDS", "1.5"),
            ("FES_NIGHTLY_KIT_POLL_SECONDS", "0"),
            ("FES_NIGHTLY_KIT_POLL_SECONDS", "-2"),
        ):
            with self.subTest(name=name, value=value), patch.dict(os.environ, {name: value}, clear=True):
                with self.assertRaisesRegex(ValueError, name):
                    worker.kit_wait_settings()

    def test_free_on_first_check_runs_once_without_sleep_and_logs_to_stderr(self):
        self.configure(kits=("B", "A"))
        self.set_status("B", "## KIT B FREE")
        raw, error, stdout, stderr = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(self.driver_calls, ["B"])
        self.assertEqual(self.clock.sleeps, [])
        self.assertEqual(stdout, "")
        self.assertEqual(json.loads(raw)["kit"], "B")
        self.assertIn("wait start: kits=B, A timeout=2700s interval=60s", stderr)
        self.assertIn("poll 1: kit B: '## KIT B FREE'", stderr)
        self.assertIn("kit B is FREE", stderr)

    def test_busy_kit_is_rechecked_until_free(self):
        self.configure(kits=("B",))
        os.environ["FES_NIGHTLY_KIT_WAIT_SECONDS"] = "5"
        os.environ["FES_NIGHTLY_KIT_POLL_SECONDS"] = "1"
        self.clock.on_sleep = lambda count, seconds: self.set_status("B", "## KIT B FREE") if count == 2 else None
        raw, error, _, stderr = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(json.loads(raw)["kit"], "B")
        self.assertEqual(self.clock.sleeps, [1, 1])
        self.assertEqual(self.driver_calls, ["B"])
        self.assertIn("poll 3: kit B: '## KIT B FREE'", stderr)

    def test_never_free_times_out_with_clipped_final_sleep_and_read_only_ledger(self):
        self.configure(kits=("B",))
        os.environ["FES_NIGHTLY_KIT_WAIT_SECONDS"] = "5"
        os.environ["FES_NIGHTLY_KIT_POLL_SECONDS"] = "2"
        path = self.ledger_paths["B"]
        original_bytes = path.read_bytes()
        original_open = Path.open
        ledger_modes = []

        def track_ledger_open(target, mode="r", *args, **kwargs):
            if target == path:
                ledger_modes.append(mode)
            return original_open(target, mode, *args, **kwargs)

        with patch.object(Path, "open", new=track_ledger_open):
            raw, error, _, stderr = self.invoke()
        self.assertIsNone(raw)
        self.assertIsInstance(error, ValueError)
        self.assertEqual(str(error), "designated kits unavailable")
        self.assertEqual(self.clock.sleeps, [2, 2, 1])
        self.assertLessEqual(sum(self.clock.sleeps), 5)
        self.assertEqual(ledger_modes, ["r"] * 4)
        self.assertEqual(path.read_bytes(), original_bytes)
        self.assertEqual(self.driver_calls, [])
        self.assertIn("wait timed out after 5s", stderr)

    def test_zero_wait_makes_one_pass_without_sleep(self):
        self.configure(kits=("B", "A"))
        os.environ["FES_NIGHTLY_KIT_WAIT_SECONDS"] = "0"
        raw, error, _, stderr = self.invoke()
        self.assertIsNone(raw)
        self.assertIsInstance(error, ValueError)
        self.assertEqual(str(error), "designated kits unavailable")
        self.assertEqual(self.clock.sleeps, [])
        self.assertIn("poll 1: kit A", stderr)
        self.assertNotIn("poll 2:", stderr)

    def test_busy_b_then_free_a_uses_a_and_both_free_uses_b_first(self):
        self.configure(kits=("B", "A"))
        self.set_status("A", "## KIT A FREE")
        raw, error, _, _ = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(json.loads(raw)["kit"], "A")
        self.assertEqual(self.driver_calls, ["A"])

        self.driver_calls.clear()
        self.configure(kits=("B", "A"))
        self.set_status("B", "## KIT B FREE")
        self.set_status("A", "## KIT A FREE")
        raw, error, _, _ = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(json.loads(raw)["kit"], "B")
        self.assertEqual(self.driver_calls, ["B"])

    def test_notstarted_keeps_waiting_and_driver_time_uses_same_deadline(self):
        self.configure(kits=("B",))
        self.set_status("B", "## KIT B FREE")
        os.environ["FES_NIGHTLY_KIT_WAIT_SECONDS"] = "6"
        os.environ["FES_NIGHTLY_KIT_POLL_SECONDS"] = "5"

        def notstarted_then_pass(kit, evidence, call_number):
            if call_number == 1:
                self.write_driver_result(evidence, verdict="NOTSTARTED")
                self.clock.now += 4
                return 0
            return self.write_driver_result(evidence, identity=kit)

        self.driver_effect = notstarted_then_pass
        raw, error, _, stderr = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(json.loads(raw)["kit"], "B")
        self.assertEqual(self.driver_calls, ["B", "B"])
        self.assertEqual(self.clock.sleeps, [2])
        self.assertIn("poll 2: kit B: '## KIT B FREE'", stderr)

    def test_real_driver_failure_is_not_retried(self):
        for verdict, returncode in (("FAIL", 0), ("PASS", 1)):
            with self.subTest(verdict=verdict, returncode=returncode):
                self.clock.now = 0
                self.clock.sleeps.clear()
                self.driver_calls.clear()
                self.configure(kits=("B",))
                self.set_status("B", "## KIT B FREE")
                self.driver_effect = lambda kit, evidence, call, v=verdict, rc=returncode: self.write_driver_result(
                    evidence, verdict=v, returncode=rc, identity=kit)
                raw, error, _, _ = self.invoke()
                self.assertIsNone(raw)
                self.assertIsInstance(error, ValueError)
                self.assertIn("kit B failed, see", str(error))
                self.assertEqual(self.driver_calls, ["B"])
                self.assertEqual(self.clock.sleeps, [])

    def test_d25_reads_serial_bound_ledger_and_accepts_d25_heading(self):
        self.configure(board="de25-nano", kits=("TRWU8J8G",), core="fes.menu")
        path = self.ledger_paths["D25"]
        self.assertEqual(path.name, "de25-TRWU8J8G-STATUS.md")
        self.set_status("D25", "## KIT D25 FREE")
        raw, error, _, stderr = self.invoke()
        self.assertIsNone(error)
        self.assertEqual(json.loads(raw)["kit"], "TRWU8J8G")
        self.assertEqual(self.driver_calls, ["D25"])
        self.assertIn("poll 1: kit D25: '## KIT D25 FREE'", stderr)


if __name__ == "__main__":
    unittest.main()
