import contextlib
import hashlib
import importlib.util
import json
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest import mock
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "package_acceptance.py"
_MODULE_SPEC = importlib.util.spec_from_file_location("package_acceptance", SCRIPT)
assert _MODULE_SPEC and _MODULE_SPEC.loader
package_acceptance = importlib.util.module_from_spec(_MODULE_SPEC)
sys.modules[_MODULE_SPEC.name] = package_acceptance
_MODULE_SPEC.loader.exec_module(package_acceptance)

CORE_ID = "fes.sg1000"
PACKAGE_ID = "a" * 64
OLD_PACKAGE_ID = "b" * 64
STALE_PACKAGE_ID = "c" * 64
WRONG_PACKAGE_ID = "d" * 64
# Transport-only fixture: this raw text is intentionally not a valid .fcore;
# package manifest admission is covered by the pinned FogCast Go tests.
ARCHIVE = b"sealed format-2 package fixture\n"
ARCHIVE_SHA256 = hashlib.sha256(ARCHIVE).hexdigest()
HOST_REVISION = "host-revision-1"
AGENT_REVISION = "agent-revision-1"
RUNTIME_REVISION = "runtime-revision-1"
SERVER_ID = "server-instance-1"
TARGET_ID = "target-1"
FLIGHT_ID = "launch-flight-1"
GAME_ID = "sg1000-game"
NEW_GAME_ID = "sg1000-new-game"


def _entry(game_id=GAME_ID, package_id=OLD_PACKAGE_ID):
    return {"game_id": game_id, "title": "SG1000", "core_id": CORE_ID, "package_id": package_id}


def _package(package_id=PACKAGE_ID, core_id=CORE_ID):
    return {"package_id": package_id, "descriptor": {"core": {"id": core_id}}}


def _active_identity(
    package_id=PACKAGE_ID,
    flight_id=FLIGHT_ID,
    game_id=GAME_ID,
    generation=7,
    target_id=TARGET_ID,
    execution="fpga_development",
):
    return {
        "id": SERVER_ID,
        "target_id": target_id,
        "flight_id": flight_id,
        "game_id": game_id,
        "package_id": package_id,
        "generation": generation,
        "execution": execution,
    }


def _state():
    return {
        "health_calls": 0,
        "health_target_id": TARGET_ID,
        "session_target_id": TARGET_ID,
        "compatibility_target_id": TARGET_ID,
        "launch_target_id": TARGET_ID,
        "stop_response_target_id": TARGET_ID,
        "stop_confirmation_target_id": TARGET_ID,
        "session_state": "idle",
        "server_id": SERVER_ID,
        "flight_id": FLIGHT_ID,
        "entries": {GAME_ID: _entry()},
        "packages": [],
        "uploaded": False,
        "upload_bodies": [],
        "compatibility_calls": 0,
        "compatibility_paths": [],
        "compatibility_bodies": [],
        "selection_puts": [],
        "create_bodies": [],
        "launch_bodies": [],
        "stops": 0,
        "active_reads": 0,
        "session_reads_after_launch": 0,
        "confirmation_failures": 0,
        "cleanup_probe_seen": False,
        "active_identity": _active_identity(),
        "stop_status": 200,
        "omit_stop_response_id": False,
        "omit_stop_confirmation_id": False,
        "launch_execution": "fpga_development",
        "runtime_revision": RUNTIME_REVISION,
    }


class _PackageAcceptanceHandler(BaseHTTPRequestHandler):
    server_version = "fes-package-acceptance-test"

    def log_message(self, *_args):
        return

    @property
    def state(self):
        return self.server.state

    def _body(self):
        length = int(self.headers.get("Content-Length", "0"))
        return self.rfile.read(length)

    def _write(self, value, status=200):
        body = json.dumps(value, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _error(self, code, message, status):
        self._write({"error": {"code": code, "message": message}}, status=status)

    def _health(self):
        self.state["health_calls"] += 1
        runtime_revision = self.state["runtime_revision"]
        if self.state.get("flip_runtime_after_upload") and self.state["uploaded"]:
            runtime_revision = "runtime-revision-changed"
        if self.state.get("flip_revision_after_stop") and self.state["stops"]:
            runtime_revision = "runtime-revision-after-stop"
        connection = {}
        if self.state["health_target_id"] is not None:
            connection["target_id"] = self.state["health_target_id"]
        self._write({
            "ready": True,
            "host": {"revision": HOST_REVISION},
            "target": {
                "reachable": True,
                "ready": True,
                "connection": connection,
                "artifacts": {
                    "agent_revision": AGENT_REVISION,
                    "runtime_commit": runtime_revision,
                },
            },
        })

    def _session(self):
        if self.state["launch_bodies"]:
            self.state["session_reads_after_launch"] += 1
            if self.state.get("confirmation_failures", 0):
                self.state["confirmation_failures"] -= 1
                self._error("TARGET_UNAVAILABLE", "confirmation unavailable", 503)
                return
        if self.state["session_state"] == "idle":
            payload = {"id": self.state["server_id"], "state": "idle"}
            target_id = (
                self.state["stop_confirmation_target_id"]
                if self.state["stops"]
                else self.state["session_target_id"]
            )
            if target_id is not None:
                payload["target_id"] = target_id
            if self.state["stops"] and self.state["omit_stop_confirmation_id"]:
                payload.pop("id")
            payload.update(self.state.get("idle_fields", {}))
            self._write(payload)
            return

        identity = dict(self.state["active_identity"])
        if self.state.get("change_identity_before_stop") and self.state["active_reads"] >= 1:
            identity.update(self.state.get("changed_identity", {}))
        if self.state.get("confirmation_probe"):
            self.state["cleanup_probe_seen"] = True
            identity.update(self.state.get("changed_identity", {}))
        self.state["active_reads"] += 1
        response = {
            "id": identity["id"],
            "flight_id": identity["flight_id"],
            "state": "active",
            "game_id": identity["game_id"],
            "execution": identity["execution"],
            "core_package": {
                "package_id": identity["package_id"],
                "generation": identity["generation"],
            },
        }
        if identity.get("target_id") is not None:
            response["target_id"] = identity["target_id"]
        self._write(response)

    def do_GET(self):
        path = urlsplit(self.path).path
        if path == "/api/v1/health":
            self._health()
        elif path == "/api/v1/session":
            self._session()
        elif path == "/api/v1/core-packages":
            self._write({"packages": self.state["packages"]})
        elif path.startswith("/api/v1/core-media/"):
            media_id = path.rsplit("/", 1)[-1]
            media = self.state.get("media_store", {}).get(media_id)
            self.state.setdefault("media_reads", []).append(media_id)
            if media is None:
                self._error("ROM_NOT_FOUND", "retained media missing", 404)
            else:
                self._write({"media_id": media_id, "size": len(media)})
        elif path.startswith("/api/v1/library/core-entries/"):
            game_id = path.rsplit("/", 1)[-1]
            entry = self.state["entries"].get(game_id)
            if entry is None:
                self._error("ROM_NOT_FOUND", "entry not found", 404)
            else:
                self._write(entry)
        else:
            self._error("NOT_FOUND", path, 404)

    def do_POST(self):
        path = urlsplit(self.path).path
        if path == "/api/v1/core-media":
            body = self._body()
            self.state.setdefault("media_uploads", []).append(body)
            self.state.setdefault("media_store", {})[hashlib.sha256(body).hexdigest()] = body
            if self.state.get("media_disconnect"):
                self.close_connection = True
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            if self.state.get("media_redirect"):
                self.send_response(302)
                self.send_header("Location", "/api/v1/library/core-entries/" + GAME_ID)
                self.end_headers()
                return
            self._write(self.state.get("media_response", {"media_id": hashlib.sha256(body).hexdigest(), "size": len(body)}), status=201)
        elif path == "/api/v1/core-packages":
            body = self._body()
            self.state["upload_bodies"].append(body)
            self.state["uploaded"] = True
            if self.state.get("upload_disconnect"):
                self.close_connection = True
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            if self.state.get("upload_status"):
                self._error(
                    self.state.get("upload_error_code", "INTERNAL"),
                    "no space left on device",
                    self.state["upload_status"],
                )
                return
            response = self.state.get("import_response", _package())
            self.state["packages"].append(response)
            self._write(response, status=self.state.get("upload_response_status", 201))
        elif path == f"/api/v1/core-packages/{PACKAGE_ID}/compatibility":
            self.state["compatibility_calls"] += 1
            self.state["compatibility_paths"].append(path)
            self.state["compatibility_bodies"].append(self._body())
            if self.state.get("stale_on_compatibility"):
                self.state["entries"][GAME_ID] = _entry(package_id=STALE_PACKAGE_ID)
            response = self.state.get("compatibility_response")
            if response is None:
                response = {
                    "package_id": PACKAGE_ID,
                    "descriptor": {"core": {"id": CORE_ID}},
                    "compatible": True,
                    "compatibility_error": None,
                    "target": "dev",
                    "state": "compatible",
                }
                if self.state["compatibility_target_id"] is not None:
                    response["target_id"] = self.state["compatibility_target_id"]
            self._write(response)
        elif path == "/api/v1/library/core-entries":
            body = json.loads(self._body())
            self.state["create_bodies"].append(body)
            entry = _entry(NEW_GAME_ID, body["package_id"])
            entry["title"] = body["title"]
            for field in ("media_id", "media_role"):
                if field in body:
                    entry[field] = body[field]
            self.state["entries"][NEW_GAME_ID] = entry
            self._write(entry, status=201)
        elif path == "/api/v1/session/launch":
            body = json.loads(self._body())
            self.state["launch_bodies"].append(body)
            if self.state.get("launch_mode") == "ambiguous":
                self.state["session_state"] = "active"
                self._error("TARGET_UNAVAILABLE", "launch reply was lost", 503)
                return
            if self.state.get("launch_mode") == "malformed":
                self._write({"state": "active"})
                return
            entry = self.state["entries"][body["game_id"]]
            if self.state.get("media_drift_on_launch"):
                entry["media_id"] = WRONG_PACKAGE_ID
            identity = _active_identity(
                package_id=entry["package_id"],
                game_id=body["game_id"],
                target_id=self.state["launch_target_id"],
                execution=self.state["launch_execution"],
            )
            self.state["active_identity"] = identity
            self.state["session_state"] = "active"
            self.state["active_reads"] = 0
            response = {
                "id": identity["id"],
                "flight_id": identity["flight_id"],
                "state": "active",
                "game_id": identity["game_id"],
                "execution": identity["execution"],
                "core_package": {
                    "package_id": identity["package_id"],
                    "generation": identity["generation"],
                },
            }
            if identity.get("target_id") is not None:
                response["target_id"] = identity["target_id"]
            self._write(response)
        elif path == "/api/v1/session/stop":
            self.state["stops"] += 1
            if self.state.get("forget_media_after_stop"):
                self.state["media_store"] = {}
            if self.state.get("stop_status", 200) != 200:
                self._error("TARGET_UNAVAILABLE", "stop did not become idle", self.state["stop_status"])
                return
            self.state["session_state"] = "idle"
            response = {"id": self.state["server_id"], "state": "idle"}
            if self.state["stop_response_target_id"] is not None:
                response["target_id"] = self.state["stop_response_target_id"]
            if self.state["omit_stop_response_id"]:
                response.pop("id")
            self._write(response)
        else:
            self._error("NOT_FOUND", path, 404)

    def do_PUT(self):
        path = urlsplit(self.path).path
        if path.endswith("/media"):
            body = json.loads(self._body())
            self.state.setdefault("media_puts", []).append(body)
            entry = self.state["entries"].get(path.split("/")[-2])
            if self.state.get("media_cas_conflict") or entry is None or entry["package_id"] != body["expected_package_id"] or entry.get("media_id", "") != body["expected_media_id"]:
                self._error("STALE_REVISION", "media changed", 409)
                return
            entry.update(media_id=body["media_id"], media_role=body["media_role"])
            self._write(entry)
            return
        if not path.startswith("/api/v1/library/core-entries/"):
            self._error("NOT_FOUND", path, 404)
            return
        game_id = path.rsplit("/", 1)[-1]
        body = json.loads(self._body())
        self.state["selection_puts"].append(body)
        if self.state.get("cas_response") is not None:
            self._write(self.state["cas_response"], status=self.state.get("cas_response_status", 200))
            return
        if self.state.get("cas_status"):
            self._error("STALE_REVISION", "selection changed", self.state["cas_status"])
            return
        entry = self.state["entries"].get(game_id)
        if entry is None or body["expected_package_id"] != entry["package_id"]:
            self._error("STALE_REVISION", "selection changed", 409)
            return
        entry = dict(entry)
        entry["package_id"] = body["package_id"]
        self.state["entries"][game_id] = entry
        self._write(entry)


@contextlib.contextmanager
def fixture(state):
    server = ThreadingHTTPServer(("127.0.0.1", 0), _PackageAcceptanceHandler)
    server.state = state
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def _write_archive(directory):
    archive = Path(directory) / "sg1000.fcore"
    archive.write_bytes(ARCHIVE)
    return archive


def _command(server, archive, receipt, *extra):
    return [
        sys.executable,
        str(SCRIPT),
        "--host-api",
        f"http://127.0.0.1:{server.server_port}",
        "--archive",
        str(archive),
        "--expected-archive-sha256",
        ARCHIVE_SHA256,
        "--expected-package-id",
        PACKAGE_ID,
        "--expected-core-id",
        CORE_ID,
        "--expected-target-id",
        TARGET_ID,
        "--expected-host-revision",
        HOST_REVISION,
        "--expected-agent-revision",
        AGENT_REVISION,
        "--expected-runtime-revision",
        RUNTIME_REVISION,
        "--receipt",
        str(receipt),
        "--poll-attempts",
        "3",
        "--poll-interval",
        "0",
        "--timeout",
        "1",
        "--execute",
        *extra,
    ]


def _run_command(server, directory, *extra):
    archive = _write_archive(directory)
    receipt = Path(directory) / "receipt.json"
    result = subprocess.run(
        _command(
            server,
            archive,
            receipt,
            "--game-id",
            GAME_ID,
            "--expected-selected-package",
            OLD_PACKAGE_ID,
            *extra,
        ),
        cwd=ROOT,
        text=True,
        capture_output=True,
    )
    return result, receipt


class LibraryMediaAcceptanceTests(unittest.TestCase):
    MEDIA = bytes(range(256)) * 128
    DIGEST = hashlib.sha256(MEDIA).hexdigest()

    def run_media(self, state, directory, server, *, existing=False, extra=()):
        media = Path(directory) / "rom.bin"
        media.write_bytes(self.MEDIA)
        archive = _write_archive(directory)
        receipt = Path(directory) / "receipt.json"
        selection = ["--game-id", GAME_ID, "--expected-selected-package", OLD_PACKAGE_ID,
                     "--expected-selected-media", "none"] if existing else ["--new-entry-title", "32 KiB title"]
        result = subprocess.run(_command(server, archive, receipt, *selection,
                                "--library-media", str(media), "--expected-media-sha256", self.DIGEST, *extra),
                                text=True, capture_output=True)
        return result, receipt

    def test_new_and_existing_entry_import_32k_and_bind_receipt(self):
        for existing in (False, True):
            with self.subTest(existing=existing), fixture(_state()) as server, tempfile.TemporaryDirectory() as directory:
                state = server.state
                result, receipt = self.run_media(state, directory, server, existing=existing)
                self.assertEqual(result.returncode, 0, result.stderr)
                value = json.loads(receipt.read_text())
                self.assertEqual(value["library_media"], {"media_id": self.DIGEST, "sha256": self.DIGEST, "size": 32768, "role": "blob"})
                self.assertEqual(value["selection"]["media_id"], self.DIGEST)
                self.assertEqual(state["media_uploads"], [self.MEDIA])
                self.assertEqual(state["stops"], 1)
                self.assertEqual(state["launch_bodies"], [{"game_id": GAME_ID if existing else NEW_GAME_ID}])
                if existing:
                    self.assertEqual(state["media_puts"], [{"expected_package_id": PACKAGE_ID,
                        "expected_media_id": "", "media_id": self.DIGEST, "media_role": "blob"}])

    def test_digest_and_existing_media_expectation_fail_before_mutation(self):
        for extra in (("--expected-media-sha256", WRONG_PACKAGE_ID), ("--expected-selected-media", WRONG_PACKAGE_ID)):
            with self.subTest(extra=extra), fixture(_state()) as server, tempfile.TemporaryDirectory() as directory:
                result, receipt = self.run_media(server.state, directory, server, existing=True, extra=extra)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(receipt.exists())
                self.assertEqual(server.state["upload_bodies"], [])

    def test_existing_bound_media_is_preserved_until_explicit_media_cas(self):
        state = _state()
        state["entries"][GAME_ID].update(media_id=OLD_PACKAGE_ID, media_role="blob")
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            result, receipt = self.run_media(state, directory, server, existing=True,
                extra=("--expected-selected-media", OLD_PACKAGE_ID))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(receipt.exists())
            self.assertEqual(state["media_puts"][0]["expected_media_id"], OLD_PACKAGE_ID)

    def test_optional_media_arguments_require_pair_and_explicit_existing_expectation(self):
        with fixture(_state()) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            base = _command(server, archive, receipt, "--game-id", GAME_ID,
                            "--expected-selected-package", OLD_PACKAGE_ID)
            for flags in (["--library-media", "missing.bin"],
                          ["--expected-media-sha256", self.DIGEST],
                          ["--library-media", "missing.bin", "--expected-media-sha256", self.DIGEST],
                          ["--expected-selected-media", "none"]):
                with self.subTest(flags=flags):
                    args = package_acceptance.parser().parse_args(base[2:] + flags)
                    with self.assertRaises(package_acceptance.AcceptanceError):
                        package_acceptance.validate_args(args)
            self.assertEqual(server.state["health_calls"], 0)

    def test_media_file_bounds_and_symlink_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "media"
            for size in (0, package_acceptance.MAX_MEDIA_BYTES + 1):
                with path.open("wb") as handle:
                    handle.truncate(size)
                with self.assertRaises(package_acceptance.AcceptanceError):
                    package_acceptance._read_archive(path, package_acceptance.MAX_MEDIA_BYTES)
            link = Path(directory) / "link"
            link.symlink_to(path)
            with self.assertRaises(package_acceptance.AcceptanceError):
                package_acceptance._read_archive(link, package_acceptance.MAX_MEDIA_BYTES)

    def test_bad_import_or_ambiguous_response_never_selects_or_replays(self):
        cases = [{"media_response": {"media_id": WRONG_PACKAGE_ID, "size": 32768}},
                 {"media_response": {"media_id": self.DIGEST, "size": 16384}},
                 {"media_disconnect": True}, {"media_redirect": True}]
        for changes in cases:
            state = _state()
            state.update(changes)
            with self.subTest(changes=changes), fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                result, receipt = self.run_media(state, directory, server)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(receipt.exists())
                self.assertEqual(state["media_uploads"], [self.MEDIA])
                self.assertEqual(state["create_bodies"], [])
                self.assertEqual(state["launch_bodies"], [])

    def test_media_cas_conflict_does_not_launch_or_rollback(self):
        state = _state()
        state["media_cas_conflict"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            result, receipt = self.run_media(state, directory, server, existing=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(receipt.exists())
            self.assertEqual(len(state["selection_puts"]), 1)
            self.assertEqual(len(state["media_puts"]), 1)
            self.assertEqual(state["launch_bodies"], [])
            self.assertEqual(state["entries"][GAME_ID]["package_id"], PACKAGE_ID)

    def test_media_drift_after_launch_cleans_owned_session_without_receipt(self):
        state = _state()
        state["media_drift_on_launch"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            result, receipt = self.run_media(state, directory, server)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(receipt.exists())
            self.assertEqual(state["stops"], 1)


class PackageAcceptanceTests(unittest.TestCase):
    def test_accepts_new_core_without_image_selection_files_and_receipts_owned_flight(self):
        state = _state()
        state["entries"] = {}
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(server, archive, receipt, "--new-entry-title", "SG1000"),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("package acceptance passed", result.stdout)
            value = json.loads(receipt.read_text(encoding="utf-8"))
            self.assertTrue(value["success"])
            self.assertEqual(value["archive_sha256"], ARCHIVE_SHA256)
            self.assertEqual(value["package_id"], PACKAGE_ID)
            self.assertEqual(value["core_id"], CORE_ID)
            self.assertEqual(value["target_id"], TARGET_ID)
            self.assertEqual(value["revisions"], {
                "host": HOST_REVISION,
                "agent": AGENT_REVISION,
                "runtime": RUNTIME_REVISION,
            })
            self.assertEqual(value["diagnostics"], [])
            self.assertEqual(value["session"]["flight_id"], FLIGHT_ID)
            self.assertEqual(value["session"]["generation"], 7)
            self.assertEqual(state["upload_bodies"], [ARCHIVE])
            self.assertEqual(state["compatibility_calls"], 1)
            self.assertEqual(
                state["compatibility_paths"],
                [f"/api/v1/core-packages/{PACKAGE_ID}/compatibility"],
            )
            self.assertEqual(state["compatibility_bodies"], [b""])
            self.assertGreater(state["health_calls"], 0)
            self.assertEqual(state["create_bodies"], [{"title": "SG1000", "package_id": PACKAGE_ID}])
            self.assertEqual(state["launch_bodies"], [{"game_id": NEW_GAME_ID}])
            self.assertEqual(state["stops"], 1)
            self.assertEqual(state["session_state"], "idle")

    def test_expected_target_id_is_required(self):
        state = _state()
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            command = _command(
                server,
                archive,
                receipt,
                "--game-id",
                GAME_ID,
                "--expected-selected-package",
                OLD_PACKAGE_ID,
            )
            target_index = command.index("--expected-target-id")
            del command[target_index:target_index + 2]
            result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)

        self.assertEqual(result.returncode, 2)
        self.assertIn("expected-target-id", result.stderr)
        self.assertFalse(receipt.exists())
        self.assertEqual(state["health_calls"], 0)

    def test_health_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["health_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertEqual(result.returncode, 1)
                    self.assertIn("target_id", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["health_calls"], 1)
                    self.assertEqual(state["upload_bodies"], [])
                    self.assertEqual(state["selection_puts"], [])
                    self.assertEqual(state["launch_bodies"], [])
                    self.assertEqual(state["stops"], 0)

    def test_preflight_session_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["session_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("target", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["upload_bodies"], [])
                    self.assertEqual(state["selection_puts"], [])
                    self.assertEqual(state["launch_bodies"], [])
                    self.assertEqual(state["stops"], 0)

    def test_compatibility_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["compatibility_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("target", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["upload_bodies"], [ARCHIVE])
                    self.assertEqual(state["compatibility_calls"], 1)
                    self.assertEqual(state["selection_puts"], [])
                    self.assertEqual(state["launch_bodies"], [])
                    self.assertEqual(state["stops"], 0)

    def test_launch_acknowledgement_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["launch_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("target", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["launch_bodies"], [{"game_id": GAME_ID}])
                    self.assertEqual(state["stops"], 0)
                    self.assertEqual(state["session_state"], "active")

    def test_pre_stop_target_id_change_refuses_stop(self):
        state = _state()
        state["change_identity_before_stop"] = True
        state["changed_identity"] = {"target_id": "target-2"}
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            result, receipt = _run_command(server, directory)

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("target", result.stderr.lower())
            self.assertFalse(receipt.exists())
            self.assertEqual(state["stops"], 0)
            self.assertEqual(state["session_state"], "active")

    def test_stop_response_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["stop_response_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("target", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["stops"], 1)

    def test_stop_confirmation_target_id_is_required_and_exact(self):
        for target_id in (None, "target-2"):
            with self.subTest(target_id=target_id):
                state = _state()
                state["stop_confirmation_target_id"] = target_id
                with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
                    result, receipt = _run_command(server, directory)

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("target", result.stderr.lower())
                    self.assertFalse(receipt.exists())
                    self.assertEqual(state["stops"], 1)

    def test_updates_existing_selection_with_compare_and_swap(self):
        state = _state()
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(state["selection_puts"], [{
                "package_id": PACKAGE_ID,
                "expected_package_id": OLD_PACKAGE_ID,
            }])
            self.assertEqual(state["entries"][GAME_ID]["package_id"], PACKAGE_ID)
            self.assertEqual(state["stops"], 1)

    def test_refuses_active_session_before_any_mutation(self):
        state = _state()
        state["session_state"] = "active"
        state["active_identity"] = _active_identity(
            package_id=OLD_PACKAGE_ID, flight_id="foreign-flight", game_id="foreign-game"
        )
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("idle", result.stderr.lower())
        self.assertEqual(state["upload_bodies"], [])
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], OLD_PACKAGE_ID)

    def test_archive_hash_mismatch_is_rejected_before_upload(self):
        state = _state()
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            command = _command(
                server,
                archive,
                Path(directory) / "receipt.json",
                "--expected-archive-sha256",
                "e" * 64,
                "--game-id",
                GAME_ID,
                "--expected-selected-package",
                OLD_PACKAGE_ID,
            )
            result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("archive", result.stderr.lower())
        self.assertEqual(state["upload_bodies"], [])
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["stops"], 0)

    def test_existing_receipt_is_rejected_before_any_mutation(self):
        state = _state()
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            receipt.write_text('{"success":true}\n', encoding="utf-8")
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("receipt", result.stderr.lower())
            self.assertEqual(json.loads(receipt.read_text(encoding="utf-8"))["success"], True)
            self.assertEqual(state["upload_bodies"], [])
            self.assertEqual(state["selection_puts"], [])
            self.assertEqual(state["launch_bodies"], [])
            self.assertEqual(state["stops"], 0)

    def test_upload_enospc_preserves_selection_and_does_not_launch(self):
        state = _state()
        # FogCast maps the service's ENOSPC/internal import failure to HTTP 500.
        state["upload_status"] = 500
        state["upload_error_code"] = "INTERNAL"
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("HTTP 500", result.stderr)
        self.assertIn("INTERNAL", result.stderr)
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], OLD_PACKAGE_ID)

    def test_interrupted_upload_does_not_select_or_launch(self):
        state = _state()
        state["upload_disconnect"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("core-packages", result.stderr)
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)

    def test_incompatible_package_is_not_selected(self):
        state = _state()
        state["compatibility_response"] = {
            "package_id": PACKAGE_ID,
            "descriptor": {"core": {"id": CORE_ID}},
            "compatible": False,
            "compatibility_error": {"code": "INCOMPATIBLE_DATA", "message": "ABI rejected"},
            "state": "incompatible",
            "target_id": TARGET_ID,
        }
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compatible", result.stderr.lower())
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], OLD_PACKAGE_ID)

    def test_malformed_compatibility_response_is_not_selected(self):
        state = _state()
        state["compatibility_response"] = {
            "package_id": PACKAGE_ID,
            "descriptor": {"core": {"id": CORE_ID}},
            "compatible": True,
            "compatibility_error": None,
            "target_id": TARGET_ID,
        }
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compatible", result.stderr.lower())
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)


    def test_changed_import_identity_is_rejected_before_selection(self):
        state = _state()
        state["import_response"] = _package(WRONG_PACKAGE_ID)
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("package identity", result.stderr.lower())
        self.assertEqual(state["compatibility_calls"], 0)
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)

    def test_revision_change_aborts_before_selection(self):
        state = _state()
        state["flip_runtime_after_upload"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("revision", result.stderr.lower())
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], OLD_PACKAGE_ID)

    def test_stale_selection_is_rejected_before_cas_and_launch(self):
        state = _state()
        state["stale_on_compatibility"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("selection", result.stderr.lower())
        self.assertEqual(state["selection_puts"], [])
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], STALE_PACKAGE_ID)

    def test_cas_conflict_is_not_followed_by_launch(self):
        state = _state()
        state["cas_status"] = 409
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("STALE_REVISION", result.stderr)
        self.assertEqual(len(state["selection_puts"]), 1)
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)
        self.assertEqual(state["entries"][GAME_ID]["package_id"], OLD_PACKAGE_ID)

    def test_malformed_cas_response_is_not_followed_by_launch(self):
        state = _state()
        state["cas_response"] = {"game_id": GAME_ID, "package_id": PACKAGE_ID}
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("selection", result.stderr.lower())
        self.assertEqual(state["launch_bodies"], [])
        self.assertEqual(state["stops"], 0)

    def test_ambiguous_launch_is_never_blindly_stopped(self):
        state = _state()
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            state["launch_mode"] = "ambiguous"
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("launch", result.stderr.lower())
        self.assertEqual(state["stops"], 0)
        self.assertGreaterEqual(state["session_reads_after_launch"], 1)

    def test_session_identity_change_refuses_stop(self):
        state = _state()
        state["change_identity_before_stop"] = True
        state["changed_identity"] = {
            "id": SERVER_ID,
            "flight_id": "foreign-flight",
            "package_id": PACKAGE_ID,
            "generation": 8,
            "game_id": GAME_ID,
        }
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("identity", result.stderr.lower())
        self.assertEqual(state["stops"], 0)

    def test_stop_failure_does_not_emit_success_receipt(self):
        state = _state()
        state["stop_status"] = 500
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stop", result.stderr.lower())
        self.assertEqual(state["stops"], 1)
        self.assertFalse(receipt.exists())

    def test_post_stop_revision_change_prevents_success_receipt(self):
        state = _state()
        state["flip_revision_after_stop"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("revision", result.stderr.lower())
        self.assertEqual(state["stops"], 1)
        self.assertFalse(receipt.exists())

    def test_stop_requires_response_session_id(self):
        state = _state()
        state["omit_stop_response_id"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("session ID", result.stderr)
        self.assertEqual(state["stops"], 1)
        self.assertFalse(receipt.exists())

    def test_stop_requires_confirmed_session_id(self):
        state = _state()
        state["omit_stop_confirmation_id"] = True
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("session ID", result.stderr)
        self.assertEqual(state["stops"], 1)
        self.assertFalse(receipt.exists())

    def test_rejects_non_development_launch_execution(self):
        state = _state()
        state["launch_execution"] = "fpga_native"
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            receipt = Path(directory) / "receipt.json"
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    receipt,
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("execution", result.stderr.lower())
        self.assertEqual(state["stops"], 0)
        self.assertFalse(receipt.exists())

    def test_confirmation_failure_cleans_up_when_owned_flight_is_reconfirmed(self):
        state = _state()
        state["confirmation_failures"] = 3
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("confirmed", result.stderr.lower())
        self.assertEqual(state["stops"], 1)
        self.assertEqual(state["session_state"], "idle")

    def test_confirmation_failure_refuses_cleanup_after_flight_changes(self):
        state = _state()
        state["confirmation_failures"] = 3
        state["confirmation_probe"] = True
        state["changed_identity"] = {
            "id": SERVER_ID,
            "flight_id": "foreign-flight",
            "package_id": PACKAGE_ID,
            "generation": 8,
            "game_id": GAME_ID,
        }
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            result = subprocess.run(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("cleanup", result.stderr.lower())
        self.assertTrue(state["cleanup_probe_seen"])
        self.assertEqual(state["stops"], 0)

    def test_keyboard_interrupt_during_confirmation_cleans_up_same_flight(self):
        state = _state()
        state["confirmation_failures"] = 1
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            args = package_acceptance.parser().parse_args(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                )[2:]
            )
            package_acceptance.validate_args(args)
            runner = package_acceptance.Runner(args)
            with mock.patch.object(package_acceptance.time, "sleep", side_effect=KeyboardInterrupt):
                with self.assertRaises(KeyboardInterrupt):
                    runner.run()

        self.assertEqual(state["stops"], 1)
        self.assertEqual(state["session_state"], "idle")

    def test_keyboard_interrupt_during_confirmation_refuses_foreign_flight_stop(self):
        state = _state()
        state["confirmation_failures"] = 1
        state["confirmation_probe"] = True
        state["changed_identity"] = {
            "id": SERVER_ID,
            "flight_id": "foreign-flight",
            "package_id": PACKAGE_ID,
            "generation": 8,
            "game_id": GAME_ID,
        }
        with fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            archive = _write_archive(directory)
            args = package_acceptance.parser().parse_args(
                _command(
                    server,
                    archive,
                    Path(directory) / "receipt.json",
                    "--game-id",
                    GAME_ID,
                    "--expected-selected-package",
                    OLD_PACKAGE_ID,
                )[2:]
            )
            package_acceptance.validate_args(args)
            runner = package_acceptance.Runner(args)
            with mock.patch.object(package_acceptance.time, "sleep", side_effect=KeyboardInterrupt):
                with self.assertRaises(KeyboardInterrupt):
                    runner.run()

        self.assertTrue(state["cleanup_probe_seen"])
        self.assertEqual(state["stops"], 0)


if __name__ == "__main__":
    unittest.main()
