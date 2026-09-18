import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from input_diagnostic import load_diagnostic, run_diagnostic, validate_timeout


def document():
    return {"format": 1, "interface": {"id": "fes.gamepad", "major": 1, "minor": 0},
            "events": [{"device": 1, "kind": 1, "action": action, "code": 100, "value": 0}
                       for action in (1, 0)]}


class LoaderTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "input.json"

    def load(self, value=None, raw=None):
        raw = json.dumps(value if value is not None else document()).encode() if raw is None else raw
        self.path.write_bytes(raw)
        return load_diagnostic(self.path, hashlib.sha256(raw).hexdigest())

    def test_exact_snapshot_and_balanced_axis_keyboard_and_gamepad(self):
        result = self.load()
        self.assertEqual(result.raw, self.path.read_bytes())
        self.assertEqual(result.sha256, hashlib.sha256(result.raw).hexdigest())
        self.assertEqual(len(result.events), 2)
        value = document()
        value["interface"]["id"] = "fes.keyboard"
        self.load(value)  # gamepad-origin events may reach the keyboard mapper
        for event in value["events"]:
            event.update(device=0, kind=0, code=256)
        self.load(value)
        for event, position in zip(value["events"], (32767, 0)):
            event.update(device=1, kind=2, action=2, code=200, value=position)
        self.load(value)

    def test_closed_schema_strict_numbers_and_supported_codes(self):
        mutations = [
            lambda v: v.update(format=True),
            lambda v: v.update(delay=1),
            lambda v: v["interface"].update(major=True),
            lambda v: v["interface"].update(major=0),
            lambda v: v["interface"].update(id="fes.video"),
            lambda v: v["events"][0].update(action=True),
            lambda v: v["events"][0].update(value=float("nan")),
            lambda v: v["events"][0].update(value=float("inf")),
            lambda v: v["events"][0].update(code=113),
            lambda v: v["events"][0].update(device=0, kind=0, code=1),
            lambda v: v["events"][0].update(device=0, kind=0, code=296),
            lambda v: v["events"][0].update(value=1),
            lambda v: v["events"].pop(),
            lambda v: v["events"].reverse(),
            lambda v: v["events"].insert(1, dict(v["events"][0])),
            lambda v: v.update(events=[]),
            lambda v: v.update(events=v["events"] * 33),
        ]
        for mutation in mutations:
            value = document()
            mutation(value)
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.load(value)
        with self.assertRaises(ValueError):
            self.load(raw=b'{"format":1,"format":1}')

    def test_axis_bounds_and_neutral_end(self):
        for position in (32768, -32769, 1):
            value = document()
            value["events"] = [{"device": 1, "kind": 2, "action": 2, "code": 201, "value": position}]
            with self.assertRaises(ValueError):
                self.load(value)

    def test_file_bounds_symlink_fifo_and_digest(self):
        self.load()
        with self.assertRaises(ValueError):
            load_diagnostic(self.path, "0" * 64)
        for raw in (b"", b" " * 65537):
            with self.assertRaises(ValueError):
                self.load(raw=raw)
        self.path.unlink()
        os.mkfifo(self.path)
        start = time.monotonic()
        with self.assertRaises(ValueError):
            load_diagnostic(self.path, "0" * 64)
        self.assertLess(time.monotonic() - start, 1)
        self.path.unlink()
        self.path.symlink_to(Path(self.tmp.name) / "absent")
        with self.assertRaises(ValueError):
            load_diagnostic(self.path, "0" * 64)

    def test_timeout_finite_bounded(self):
        for value in (True, 0, -1, 61, float("nan"), float("inf")):
            with self.assertRaises(ValueError):
                validate_timeout(value)
        validate_timeout(10)
        validate_timeout(60)


class FakeRunner:
    def __init__(self, diagnostic):
        self.args = SimpleNamespace(poll_attempts=4, poll_interval=0)
        self.api = SimpleNamespace(timeout=30, post_json=self.post)
        self.value = {"owner": "owned", "core_package": {"active_interfaces": [diagnostic.interface]},
                      "input": {"session_id": "input-1", "state": "attached", "ready": True,
                                "metrics": {"frames_sent": 10, "state_resyncs": 0}}}
        self.posts = []
        self.timeouts = []
        self.progress = 3
        self.change = lambda: None

    def session(self):
        self.timeouts.append(self.api.timeout)
        return copy.deepcopy(self.value)

    def session_identity(self, value, _context):
        return value["owner"]

    def post(self, path, body):
        self.posts.append((path, body))
        self.timeouts.append(self.api.timeout)
        self.value["input"]["metrics"]["frames_sent"] += self.progress
        self.change()
        return {"ok": True}


class ExecutionTests(unittest.TestCase):
    setUp = LoaderTests.setUp
    load = LoaderTests.load

    def test_progress_not_one_frame_per_event_and_timeout_restored(self):
        diagnostic = self.load()
        runner = FakeRunner(diagnostic)
        result = run_diagnostic(runner, "owned", diagnostic, 1)
        self.assertEqual(result["events_acknowledged"], 2)
        self.assertEqual(result["frame_delta"], 6)
        self.assertEqual(runner.api.timeout, 30)
        self.assertTrue(all(0 < t <= 1 for t in runner.timeouts))
        self.assertEqual(set(result), {"sha256", "interface", "events_requested", "events_acknowledged",
                                      "frames_before", "frames_after", "frame_delta", "input_session_id"})

    def test_initial_not_ready_capability_and_launcher_fail_without_events(self):
        diagnostic = self.load()
        for change in (
            lambda r: r.value["input"].update(state="reconnecting"),
            lambda r: r.value["input"].update(source="launcher"),
            lambda r: r.value["input"].update(ready=False),
            lambda r: r.value["core_package"].update(active_interfaces=[]),
        ):
            runner = FakeRunner(diagnostic)
            change(runner)
            with self.assertRaises(ValueError):
                run_diagnostic(runner, "owned", diagnostic, 1)
            self.assertEqual(runner.posts, [])

    def test_waits_for_initial_ready_without_sending_early_events(self):
        diagnostic = self.load()
        runner = FakeRunner(diagnostic)
        original = runner.session
        calls = []
        def session():
            calls.append(1)
            value = original()
            if len(calls) < 3:
                self.assertEqual(runner.posts, [])
                value["input"].update(state="starting", ready=False)
            return value
        runner.session = session
        result = run_diagnostic(runner, "owned", diagnostic, 1)
        self.assertEqual(result["events_acknowledged"], 2)
        self.assertEqual(result["frames_before"], 10)

    def test_waiting_checks_owner_and_deadline_without_events(self):
        diagnostic = self.load()
        for foreign in (False, True):
            runner = FakeRunner(diagnostic)
            runner.value["input"].update(state="starting", ready=False)
            if foreign:
                runner.value["owner"] = "foreign"
            runner.args.poll_interval = 20
            start = time.monotonic()
            with self.assertRaisesRegex(ValueError, "ownership changed" if foreign else "timed out"):
                run_diagnostic(runner, "owned", diagnostic, .05)
            self.assertLess(time.monotonic() - start, .5)
            self.assertEqual(runner.posts, [])
            self.assertEqual(runner.api.timeout, 30)

    def test_replacement_reconnect_and_counter_regression_abort_before_next_event(self):
        diagnostic = self.load()
        for change in (
            lambda r: r.value.update(owner="foreign"),
            lambda r: r.value["input"].update(session_id="other"),
            lambda r: r.value["input"].update(state="reconnecting"),
            lambda r: r.value["input"]["metrics"].update(state_resyncs=1),
            lambda r: r.value["input"]["metrics"].update(frames_sent=0),
        ):
            runner = FakeRunner(diagnostic)
            runner.change = lambda: change(runner)
            with self.assertRaises(ValueError):
                run_diagnostic(runner, "owned", diagnostic, 1)
            self.assertEqual(len(runner.posts), 1)
            self.assertEqual(runner.api.timeout, 30)

    def test_timeout_caps_sleep_and_restores_for_cleanup(self):
        diagnostic = self.load()
        runner = FakeRunner(diagnostic)
        runner.progress = 0
        runner.args.poll_interval = 20
        start = time.monotonic()
        with self.assertRaisesRegex(ValueError, "timed out"):
            run_diagnostic(runner, "owned", diagnostic, .05)
        self.assertLess(time.monotonic() - start, .5)
        self.assertEqual(runner.api.timeout, 30)


if __name__ == "__main__":
    unittest.main()
