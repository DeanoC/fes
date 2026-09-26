import importlib.util
import contextlib
import io
import json
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "target_acceptance.py"
_MODULE_SPEC = importlib.util.spec_from_file_location("target_acceptance", SCRIPT)
assert _MODULE_SPEC and _MODULE_SPEC.loader
target_acceptance = importlib.util.module_from_spec(_MODULE_SPEC)
sys.modules[_MODULE_SPEC.name] = target_acceptance
_MODULE_SPEC.loader.exec_module(target_acceptance)


class _AcceptanceHandler(BaseHTTPRequestHandler):
    server_version = "fes-acceptance-test"

    def log_message(self, *_args):
        return

    def _write(self, value, status=200):
        body = json.dumps(value).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        state = self.server.state
        if self.path == "/api/v1/health":
            self._write({
                "ready": True,
                "host": {"revision": "test-revision"},
                "target": {
                    "reachable": True,
                    "ready": True,
                    "artifacts": {"agent_revision": "test-revision"},
                },
            })
        elif self.path == "/v1/health":
            self._write({"ready": True})
        elif self.path == "/api/v1/core-packages":
            self._write({"packages": state["packages"]})
        elif self.path == "/api/v1/library/core-entries":
            self._write({"entries": state["entries"]})
        elif self.path == "/api/v1/session":
            if state.get("session_failures", 0):
                state["session_failures"] -= 1
                self._write({"error": "transition"}, status=503)
                return
            if state["active"]:
                self._write({
                    "state": "active",
                    "game_id": state["game_id"],
                    "core_package": {"package_id": state["package_id"], "generation": 1},
                    "input": {
                        "state": "attached" if state["attached"] else "detached",
                        "ready": state["attached"],
                        "metrics": {"frames_sent": state["frames"]},
                    },
                })
            else:
                self._write({"state": "idle"})
        elif self.path == "/api/v1/session/input":
            self._write({
                "state": "attached" if state["attached"] else "detached",
                "ready": state["attached"],
                "metrics": {"frames_sent": state["frames"]},
            })
        else:
            self._write({"error": self.path}, status=404)

    def do_POST(self):
        state = self.server.state
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        if self.path == "/api/v1/session/launch":
            if "launch_body" in state:
                self._write(state["launch_body"], status=state.get("launch_status", 200))
                return
            value = json.loads(body)
            game_id = value["game_id"]
            package_id = next(
                entry["package_id"]
                for entry in state["entries"]
                if entry["game_id"] == game_id
            )
            state.update(active=True, attached=True, game_id=game_id, package_id=package_id)
            self._write({
                "state": "active",
                "game_id": game_id,
                "core_package": {"package_id": package_id, "generation": 1},
            })
        elif self.path == "/api/v1/session/input/attach":
            self.server.attach_bodies.append(body)
            state["attached"] = True
            self._write({"state": "active"})
        elif self.path == "/api/v1/session/input/event":
            self.server.events.append(json.loads(body))
            state["frames"] += 1
            self._write({"state": "active"})
        elif self.path == "/api/v1/session/input/detach":
            state["attached"] = False
            self._write({"state": "active"})
        elif self.path == "/api/v1/session/stop":
            self.server.stops += 1
            if state.get("stop_status"):
                self._write(state["stop_body"], status=state["stop_status"])
            else:
                state.update(active=False, attached=False)
                state["session_failures"] = 1
                self._write({"state": "idle"})
        else:
            self._write({"error": self.path}, status=404)


class _HTTPErrorHandler(BaseHTTPRequestHandler):
    server_version = "fes-acceptance-error-test"

    def log_message(self, *_args):
        return

    def do_GET(self):
        body = json.dumps(self.server.error_body).encode("utf-8")
        self.send_response(self.server.error_status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class TargetAcceptanceTests(unittest.TestCase):
    def test_inventory_explicit_entry_and_singleton_fallback(self):
        expected = {core: str(index) * 64 for index, core in enumerate(target_acceptance.CORE_ORDER, 1)}
        entries = [dict(core_id=core, package_id=package, game_id=core + "-one")
                   for core, package in expected.items()]
        packages = [dict(package_id=package, descriptor={"core": {"id": core}})
                    for core, package in expected.items()]
        runner = target_acceptance.Runner(target_acceptance.parser().parse_args([]))
        runner.host = SimpleNamespace(get=lambda path: (
            {"entries": entries} if path.endswith("core-entries") else {"packages": packages}))
        self.assertEqual(runner.inventory(expected)["fes.coleco"], "fes.coleco-one")
        entries.append(dict(entries[-1], game_id="coleco-two"))
        with self.assertRaisesRegex(target_acceptance.AcceptanceError, "specify --entry"):
            runner.inventory(expected)
        runner.entries = target_acceptance.parse_entries(["fes.coleco=coleco-two"])
        self.assertEqual(runner.inventory(expected)["fes.coleco"], "coleco-two")
        entries.append(dict(core_id="fes.coleco", package_id="f" * 64, game_id="wrong-package"))
        for entry in (
            "missing", "fes.pong-one", "wrong-package",
        ):
            with self.subTest(entry=entry):
                runner.entries = {"fes.coleco": entry}
                with self.assertRaisesRegex(target_acceptance.AcceptanceError, "must match exactly one"):
                    runner.inventory(expected)

    def test_entry_arguments_reject_invalid_and_duplicate_core(self):
        for values in (["fes.sms=game"], ["fes.pong="], ["fes.pong= "], ["fes.pong"],
                       ["fes.pong=one", "fes.pong=two"]):
            with self.subTest(values=values):
                with self.assertRaises(target_acceptance.AcceptanceError):
                    target_acceptance.parse_entries(values)
        args = target_acceptance.parser().parse_args(
            ["--entry", "fes.pong=one", "--entry", "fes.coleco=two"])
        self.assertEqual(target_acceptance.parse_entries(args.entry),
                         {"fes.pong": "one", "fes.coleco": "two"})

    def test_api_http_error_includes_bounded_structured_detail(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), _HTTPErrorHandler)
        server.error_status = 503
        server.error_body = {
            "error": {
                "code": "TARGET_UNAVAILABLE",
                "phase": "session",
                "message": "target status is unavailable",
            },
            "private_token": "must-not-leak",
        }
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            api = target_acceptance.Api(f"http://127.0.0.1:{server.server_port}", 1)
            with self.assertRaises(target_acceptance.AcceptanceError) as raised:
                api.get("/api/v1/session")
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

        message = str(raised.exception)
        self.assertIn("GET /api/v1/session", message)
        self.assertIn("HTTP 503", message)
        self.assertIn("TARGET_UNAVAILABLE", message)
        self.assertIn("phase=session", message)
        self.assertIn("target status is unavailable", message)
        self.assertNotIn("must-not-leak", message)

    def test_run_core_does_not_record_success_when_stop_fails(self):
        package_id = "a" * 64
        state = {
            "active": False,
            "attached": False,
            "frames": 0,
            "session_failures": 0,
            "stop_status": 500,
            "stop_body": {
                "error": {
                    "code": "STOP_FAILED",
                    "phase": "stop",
                    "message": "target did not become idle",
                },
            },
        }
        state["entries"] = [
            {
                "game_id": "fes.pong-game",
                "core_id": "fes.pong",
                "package_id": package_id,
            }
        ]
        server = ThreadingHTTPServer(("127.0.0.1", 0), _AcceptanceHandler)
        server.state = state
        server.events = []
        server.attach_bodies = []
        server.stops = 0
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            origin = f"http://127.0.0.1:{server.server_port}"
            runner = target_acceptance.Runner(SimpleNamespace(entry=[],
                host_api=origin,
                target_api=origin,
                timeout=1,
                selection_dir=Path("."),
                poll_attempts=1,
                poll_interval=0,
                media=[],
                capture_dir=None,
                video_device="/dev/null",
                capture_size="1x1",
                capture_frames=1,
            ))
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                with self.assertRaises(target_acceptance.AcceptanceError) as raised:
                    runner.run_core(
                        target_acceptance.CORE_SPECS[0], package_id, "fes.pong-game"
                    )
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

        message = str(raised.exception)
        self.assertEqual(server.stops, 1)
        self.assertEqual(runner.records, [])
        self.assertEqual(output.getvalue(), "")
        self.assertIn("fes.pong", message)
        self.assertIn(f"package={package_id}", message)
        self.assertIn("game=fes.pong-game", message)
        self.assertIn("HTTP 500", message)
        self.assertIn("STOP_FAILED", message)
        self.assertIn("phase=stop", message)
        self.assertIn("target did not become idle", message)

    def test_main_reports_primary_and_cleanup_failure_and_nonzero(self):
        ids = {
            "fes.pong": "a" * 64,
            "fes.zx81": "b" * 64,
            "fes.coleco": "c" * 64,
        }
        state = {
            "active": False,
            "attached": False,
            "frames": 0,
            "session_failures": 0,
            "stop_status": 500,
            "stop_body": {
                "error": {
                    "code": "STOP_FAILED",
                    "phase": "stop",
                    "message": "target did not become idle",
                },
            },
            "launch_body": {"state": "failed"},
            "packages": [
                {"package_id": package_id, "descriptor": {"core": {"id": core}}}
                for core, package_id in ids.items()
            ],
            "entries": [
                {"game_id": f"{core}-game", "core_id": core, "package_id": package_id}
                for core, package_id in ids.items()
            ],
        }
        server = ThreadingHTTPServer(("127.0.0.1", 0), _AcceptanceHandler)
        server.state = state
        server.events = []
        server.attach_bodies = []
        server.stops = 0
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                selection_dir = Path(directory)
                for core, package_id in ids.items():
                    filename = core.replace(".", "-") + ".package-selection.toml"
                    (selection_dir / filename).write_text(
                        f'package_id = "{package_id}"\n', encoding="utf-8"
                    )
                origin = f"http://127.0.0.1:{server.server_port}"
                stdout = io.StringIO()
                stderr = io.StringIO()
                with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                    result = target_acceptance.main([
                        "--host-api",
                        origin,
                        "--target-api",
                        origin,
                        "--selection-dir",
                        str(selection_dir),
                        "--poll-attempts",
                        "1",
                        "--poll-interval",
                        "0",
                        "--no-capture",
                    ])
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

        self.assertEqual(result, 1)
        self.assertEqual(server.stops, 1)
        self.assertNotIn("passed fes.pong", stdout.getvalue())
        self.assertNotIn("target acceptance passed", stdout.getvalue())
        self.assertIn("fes.pong", stderr.getvalue())
        self.assertIn("launch did not become active", stderr.getvalue())
        self.assertIn("cleanup also failed", stderr.getvalue())
        self.assertIn("package=" + ids["fes.pong"], stderr.getvalue())
        self.assertIn("game=fes.pong-game", stderr.getvalue())
        self.assertIn("HTTP 500", stderr.getvalue())
        self.assertIn("STOP_FAILED", stderr.getvalue())
        self.assertIn("phase=stop", stderr.getvalue())
        self.assertIn("target did not become idle", stderr.getvalue())

    def test_capture_command_discards_buffered_frames(self):
        capture_command = getattr(target_acceptance, "capture_command", None)
        self.assertTrue(callable(capture_command), "capture command helper is missing")
        if not callable(capture_command):
            return
        command = capture_command(
            "/dev/video0", "1280x720", 5, Path("frame.jpg")
        )
        self.assertIn(r"select=eq(n\,4)", command)
        self.assertEqual(command[command.index("-frames:v") + 1], "1")

    def test_capture_defaults_to_yuyv_and_supports_explicit_mjpeg(self):
        command = target_acceptance.capture_command(
            "/dev/video0", "1280x720", 5, Path("frame.jpg")
        )
        self.assertEqual(command[command.index("-input_format") + 1], "yuyv422")
        command = target_acceptance.capture_command(
            "/dev/video0", "1280x720", 5, Path("frame.jpg"), "mjpeg"
        )
        self.assertEqual(command[command.index("-input_format") + 1], "mjpeg")
        self.assertEqual(target_acceptance.parser().parse_args([]).capture_input_format, "yuyv422")

    def test_capture_waits_for_hdmi_and_records_capture_parameters(self):
        with tempfile.TemporaryDirectory() as temporary:
            args = target_acceptance.parser().parse_args(["--capture-dir", temporary])
            self.assertEqual(args.capture_frames, 120)
            self.assertEqual(args.capture_settle_seconds, 3)
            runner = target_acceptance.Runner(args)
            def record_frame(command, **kwargs):
                Path(command[-1]).write_bytes(b"captured frame")
                return SimpleNamespace()
            with patch.object(target_acceptance.time, "sleep") as sleep, patch.object(
                    target_acceptance.subprocess, "run", side_effect=record_frame) as run:
                receipt = runner.capture("fes.pong")
            sleep.assert_called_once_with(3)
            self.assertEqual(receipt["input_format"], "yuyv422")
            self.assertEqual(receipt["frames"], 120)
            self.assertEqual(receipt["settle_seconds"], 3)
            self.assertIn(r"select=eq(n\,119)", run.call_args.args[0])

    def test_runs_exact_three_package_launch_input_stop_lanes(self):
        ids = {
            "fes.pong": "a" * 64,
            "fes.zx81": "b" * 64,
            "fes.coleco": "c" * 64,
        }
        state = {"active": False, "attached": False, "frames": 0, "session_failures": 0}
        state["packages"] = [
            {
                "package_id": package_id,
                "descriptor": {"core": {"id": core}},
            }
            for core, package_id in ids.items()
        ]
        state["entries"] = [
            {
                "game_id": f"{core}-game",
                "core_id": core,
                "package_id": package_id,
            }
            for core, package_id in ids.items()
        ]
        server = ThreadingHTTPServer(("127.0.0.1", 0), _AcceptanceHandler)
        server.state = state
        server.events = []
        server.attach_bodies = []
        server.stops = 0
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                selection_dir = Path(directory)
                for core, package_id in ids.items():
                    filename = core.replace(".", "-") + ".package-selection.toml"
                    (selection_dir / filename).write_text(
                        f'package_id = "{package_id}"\n', encoding="utf-8"
                    )
                origin = f"http://127.0.0.1:{server.server_port}"
                result = subprocess.run(
                    [
                        sys.executable,
                        str(SCRIPT),
                        "--host-api",
                        origin,
                        "--target-api",
                        origin,
                        "--selection-dir",
                        str(selection_dir),
                        "--no-capture",
                    ],
                    cwd=ROOT,
                    text=True,
                    capture_output=True,
                )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("target acceptance passed", result.stdout)
            self.assertEqual(server.stops, 3)
            self.assertEqual(len(server.events), 12)
            self.assertEqual(server.attach_bodies, [])
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
