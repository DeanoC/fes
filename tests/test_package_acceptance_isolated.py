import hashlib
import contextlib
import importlib.util
import io
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import textwrap
import time
import tomllib
import unittest
from types import SimpleNamespace
from unittest import mock
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "package_acceptance_isolated.py"
EXISTING_TESTS = ROOT / "tests" / "test_package_acceptance.py"
EXISTING_SPEC = importlib.util.spec_from_file_location("package_acceptance_tests", EXISTING_TESTS)
assert EXISTING_SPEC and EXISTING_SPEC.loader
package_acceptance_tests = importlib.util.module_from_spec(EXISTING_SPEC)
EXISTING_SPEC.loader.exec_module(package_acceptance_tests)
ISOLATED_SPEC = importlib.util.spec_from_file_location("package_acceptance_isolated", SCRIPT)
assert ISOLATED_SPEC and ISOLATED_SPEC.loader
package_acceptance_isolated = importlib.util.module_from_spec(ISOLATED_SPEC)
sys.modules[ISOLATED_SPEC.name] = package_acceptance_isolated
ISOLATED_SPEC.loader.exec_module(package_acceptance_isolated)
ARCHIVE = b"transport-only package fixture\n"
ARCHIVE_SHA256 = hashlib.sha256(ARCHIVE).hexdigest()
PACKAGE_ID = "a" * 64
CORE_ID = "fes.sg1000"
TARGET_ID = "target-1"
IMAGE = "sha256:" + "b" * 64
HOST_REVISION = "c" * 40
RUNTIME_REVISION = "d" * 40
HOST_BINARY = b"fake fogcast-api binary\n"
TEST_UID = os.getuid() or 1000
TEST_GID = os.getgid() or 1000


def _write_inputs(directory: str) -> dict[str, Path]:
    root = Path(directory)
    archive = root / "package.fcore"
    archive.write_bytes(ARCHIVE)
    binary = root / "fogcast-api"
    binary.write_bytes(HOST_BINARY)
    binary.chmod(0o755)
    launcher = root / "nonroot-python"
    launcher.write_text(
        textwrap.dedent(
            f"""
            #!/usr/bin/env python3
            import os
            import runpy
            import sys

            os.getuid = lambda: {TEST_UID}
            os.getgid = lambda: {TEST_GID}
            script, *arguments = sys.argv[1:]
            sys.argv = [script, *arguments]
            runpy.run_path(script, run_name="__main__")
            """
        ).lstrip(),
        encoding="utf-8",
    )
    launcher.chmod(0o755)
    config = root / "config.toml"
    config.write_text(
        """token = \"top-secret\"
selected_target = \"dev\"
libraries = [\"/live/library\"]
library_media = [\"/live/media\"]
watch_root = \"/live/watch\"

[[targets]]
name = \"dev\"
enabled = true
address = \"http://127.0.0.1:8182\"
agent = \"agent-secret\"
target_id = \"target-1\"
""",
        encoding="utf-8",
    )
    config.chmod(0o600)
    return {"archive": archive, "binary": binary, "config": config, "launcher": launcher}


def _command(
    paths: dict[str, Path],
    evidence: Path,
    *,
    execute: bool = True,
    allow_development: bool = False,
    runtime: Path | None = None,
    **overrides,
):
    values = {
        "expected_host_sha256": hashlib.sha256(HOST_BINARY).hexdigest(),
        "expected_package_id": PACKAGE_ID,
        "expected_core_id": CORE_ID,
        "expected_target_id": TARGET_ID,
        "expected_host_revision": HOST_REVISION,
        "expected_agent_revision": HOST_REVISION,
        "expected_runtime_revision": RUNTIME_REVISION,
    }
    values.update(overrides)
    command = [
        sys.executable,
        str(paths["launcher"]),
        str(SCRIPT),
        "--host-binary",
        str(paths["binary"]),
        "--expected-host-sha256",
        values["expected_host_sha256"],
        "--container-image",
        IMAGE,
        "--host-config",
        str(paths["config"]),
        "--evidence-dir",
        str(evidence),
        "--archive",
        str(paths["archive"]),
        "--expected-archive-sha256",
        ARCHIVE_SHA256,
        "--expected-package-id",
        values["expected_package_id"],
        "--expected-core-id",
        values["expected_core_id"],
        "--expected-target-id",
        values["expected_target_id"],
        "--expected-host-revision",
        values["expected_host_revision"],
        "--expected-agent-revision",
        values["expected_agent_revision"],
        "--expected-runtime-revision",
        values["expected_runtime_revision"],
        "--new-entry-title",
        "isolated SG1000",
        "--container-runtime",
        str(runtime or paths["binary"].parent / "unused-runtime"),
    ]
    if execute:
        command.append("--execute")
    if allow_development:
        command.append("--allow-development-host")
    return command


def _write_fake_runtime(directory: str) -> Path:
    runtime = Path(directory) / "fake-container-runtime.py"
    runtime.write_text(
        textwrap.dedent(
            f"""
            #!/usr/bin/env python3
            import json
            import os
            import sys
            import time
            from pathlib import Path

            IMAGE = {IMAGE!r}
            log_path = Path(os.environ["FAKE_RUNTIME_LOG"])
            state_path = Path(os.environ["FAKE_RUNTIME_STATE"])
            mode = os.environ.get("FAKE_RUNTIME_MODE", "normal")

            def load():
                if not state_path.exists():
                    return {{}}
                return json.loads(state_path.read_text(encoding="utf-8"))

            def save(value):
                state_path.write_text(json.dumps(value, sort_keys=True), encoding="utf-8")

            def record(kind, args, **extra):
                value = {{"kind": kind, "args": args, **extra}}
                with log_path.open("a", encoding="utf-8") as handle:
                    handle.write(json.dumps(value, sort_keys=True) + "\\n")

            args = sys.argv[1:]
            record("call", args)
            state = load()
            if args[:2] == ["image", "inspect"]:
                user = os.environ.get("FAKE_IMAGE_USER", "builder")
                uid = os.environ.get("FAKE_IMAGE_UID", {str(TEST_UID)!r})
                gid = os.environ.get("FAKE_IMAGE_GID", {str(TEST_GID)!r})
                value = {{"Id": os.environ.get("FAKE_IMAGE_ID", IMAGE), "Config": {{
                    "User": user,
                    "Env": ["PATH=/usr/bin"],
                    "Labels": {{"org.fes.media.uid": uid, "org.fes.media.gid": gid}},
                }}}}
                print(json.dumps(value))
                raise SystemExit(0)

            if args and args[0] == "run":
                name = args[args.index("--name") + 1]
                mounts = [value for value in args if value.startswith("type=bind,")]
                home_mount = next(value for value in mounts if "dst=/home/builder" in value)
                home = Path(home_mount.split("src=", 1)[1].split(",", 1)[0])
                config = home / ".config" / "fogcast" / "config.toml"
                config_text = config.read_text(encoding="utf-8")
                record("run", args, name=name, config_has_live_root="/live/" in config_text,
                       config_private=(config.stat().st_mode & 0o077) == 0,
                       config_has_top_secret="top-secret" in config_text,
                       config_has_agent_secret="agent-secret" in config_text,
                       home=str(home))
                if os.environ.get("FAKE_REPLACE_CONFIG") and not state.get("replaced_config"):
                    with config.open("r+", encoding="utf-8") as handle:
                        handle.seek(0)
                        handle.write("replacement")
                        handle.truncate()
                    state["replaced_config"] = True
                if mode == "start-failure":
                    print("docker: invalid field rw must be a key=value pair; agent-secret", file=sys.stderr)
                    raise SystemExit(125)
                if mode == "exited":
                    state[name] = {{"Running": False, "Status": "exited", "ExitCode": 1,
                                    "log": ""}}
                elif mode == "missing-log":
                    state[name] = {{"Running": True, "Status": "running", "ExitCode": 0,
                                    "log": ""}}
                elif mode == "stale-log":
                    state[name] = {{"Running": True, "Status": "running", "ExitCode": 0,
                                    "log": "FogCast API listening on http://127.0.0.1:8787\\n"}}
                else:
                    port = os.environ["FAKE_API_PORT"]
                    state[name] = {{"Running": True, "Status": "running", "ExitCode": 0,
                                    "log": f"FogCast API listening on http://127.0.0.1:{{port}}\\n"}}
                save(state)
                print("fake-container-id")
                raise SystemExit(0)

            if args and args[0] == "logs":
                name = args[-1]
                print(state[name].get("log", ""), end="")
                raise SystemExit(0)

            if args and args[0] == "inspect":
                name = args[-1]
                print(json.dumps({{"State": state[name]}}))
                raise SystemExit(0)

            if args and args[0] == "stop":
                name = args[-1]
                sleep_for = float(os.environ.get("FAKE_STOP_SLEEP", "0"))
                if sleep_for:
                    time.sleep(sleep_for)
                state[name]["Running"] = False
                state[name]["Status"] = "exited"
                state[name]["ExitCode"] = int(os.environ.get("FAKE_STOP_EXIT", "0"))
                save(state)
                record("stop", args, name=name, exit_code=state[name]["ExitCode"])
                raise SystemExit(state[name]["ExitCode"])

            if args and args[0] == "rm":
                name = args[-1]
                record("rm", args, name=name)
                failures_remaining = state.get("rm_failures_remaining")
                if failures_remaining is None:
                    failures_remaining = int(os.environ.get("FAKE_RM_FAILURES", "0"))
                if failures_remaining:
                    state["rm_failures_remaining"] = failures_remaining - 1
                    save(state)
                    raise SystemExit(1)
                state.pop("rm_failures_remaining", None)
                if int(os.environ.get("FAKE_RM_EXIT", "0")):
                    save(state)
                    raise SystemExit(int(os.environ["FAKE_RM_EXIT"]))
                state.pop(name, None)
                save(state)
                raise SystemExit(0)

            record("unexpected", args)
            raise SystemExit(99)
            """
        ).lstrip(),
        encoding="utf-8",
    )
    runtime.chmod(0o755)
    return runtime


def _isolated_command(
    paths: dict[str, Path],
    evidence: Path,
    runtime: Path,
    *,
    container_timeout: str = "3",
    shutdown_timeout: str = "1",
    runner_timeout: str = "120",
    **overrides,
):
    values = {
        "expected_host_revision": "diag-host-revision",
        "expected_agent_revision": "a" * 40,
        "expected_runtime_revision": "b" * 40,
    }
    values.update(overrides)
    command = _command(
        paths,
        evidence,
        runtime=runtime,
        allow_development=True,
        **values,
    )
    command.extend([
        "--container-timeout",
        container_timeout,
        "--shutdown-timeout",
        shutdown_timeout,
        "--runner-timeout",
        runner_timeout,
    ])
    return command


@contextlib.contextmanager
def _full_identity_fixture(state):
    old_values = (
        package_acceptance_tests.HOST_REVISION,
        package_acceptance_tests.AGENT_REVISION,
        package_acceptance_tests.RUNTIME_REVISION,
    )
    package_acceptance_tests.HOST_REVISION = "diag-host-revision"
    package_acceptance_tests.AGENT_REVISION = "a" * 40
    package_acceptance_tests.RUNTIME_REVISION = "b" * 40
    state["runtime_revision"] = package_acceptance_tests.RUNTIME_REVISION
    try:
        with package_acceptance_tests.fixture(state) as server:
            yield server
    finally:
        (
            package_acceptance_tests.HOST_REVISION,
            package_acceptance_tests.AGENT_REVISION,
            package_acceptance_tests.RUNTIME_REVISION,
        ) = old_values


def _run_isolated(command, runtime_log, runtime_state, *, api_port=None, **env_overrides):
    environment = os.environ.copy()
    environment.update(
        {
            "FAKE_RUNTIME_LOG": str(runtime_log),
            "FAKE_RUNTIME_STATE": str(runtime_state),
        }
    )
    if api_port is not None:
        environment["FAKE_API_PORT"] = str(api_port)
    environment.update({key: str(value) for key, value in env_overrides.items()})
    return subprocess.run(
        command,
        cwd=ROOT,
        text=True,
        capture_output=True,
        env=environment,
    )


class IsolatedInputDiagnosticTests(unittest.TestCase):
    def diagnostic(self):
        interface = {"id": "fes.keyboard", "major": 1, "minor": 0}
        events = [{"device": 0, "kind": 0, "action": action, "code": 256, "value": 0}
                  for action in (1, 0)]
        raw = json.dumps({"format": 1, "interface": interface, "events": events}).encode()
        return SimpleNamespace(raw=raw, sha256=hashlib.sha256(raw).hexdigest(),
                               interface=interface, events=events)

    def arguments(self, directory):
        paths = _write_inputs(directory)
        evidence = Path(directory) / "evidence"
        source = Path(directory) / "diagnostic.json"
        diagnostic = self.diagnostic()
        source.write_bytes(diagnostic.raw)
        command = _command(paths, evidence)[3:] + [
            "--input-events", str(source), "--expected-input-sha256", diagnostic.sha256,
            "--input-timeout", "7.5"]
        args = package_acceptance_isolated.parser().parse_args(command)
        return args, paths, evidence, source, diagnostic

    def child_receipt(self, diagnostic):
        return {"success": True, "package_id": PACKAGE_ID, "core_id": CORE_ID,
                "target_id": TARGET_ID, "selection": {"game_id": "same-entry"},
                "mode": "lifecycle-input-diagnostic", "diagnostics": [{
                    "sha256": diagnostic.sha256, "interface": diagnostic.interface,
                    "events_requested": 2, "events_acknowledged": 2,
                    "frames_before": 10, "frames_after": 12, "frame_delta": 2,
                    "input_session_id": "input-1"}]}

    def test_preflight_loader_failure_precedes_all_docker_and_api_operations(self):
        with tempfile.TemporaryDirectory() as directory:
            args, _, evidence, _, _ = self.arguments(directory)
            loader = mock.Mock(side_effect=ValueError("invalid fixture or digest"))
            with mock.patch.dict(sys.modules, {"input_diagnostic": SimpleNamespace(load_diagnostic=loader)}), \
                    mock.patch.object(package_acceptance_isolated, "DockerRuntime") as docker, \
                    mock.patch.object(package_acceptance_isolated, "_get_json") as api:
                with self.assertRaisesRegex(package_acceptance_isolated.AcceptanceError, "invalid fixture"):
                    package_acceptance_isolated.validate_args(args)
                docker.assert_not_called()
                api.assert_not_called()
            loader.assert_called_once_with(args.input_events, args.expected_input_sha256)
            self.assertFalse(evidence.exists())

    def test_pairing_digest_and_timeout_rejected_before_loader_or_docker(self):
        cases = [
            {"input_events": None}, {"expected_input_sha256": None},
            {"expected_input_sha256": "wrong"},
            *({"input_timeout": value} for value in (0, -1, 60.1, float("inf"), float("nan"))),
        ]
        for change in cases:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                args, _, evidence, _, _ = self.arguments(directory)
                for name, value in change.items():
                    setattr(args, name, value)
                loader = mock.Mock()
                with mock.patch.dict(sys.modules, {"input_diagnostic": SimpleNamespace(load_diagnostic=loader)}), \
                        mock.patch.object(package_acceptance_isolated, "DockerRuntime") as docker:
                    with self.assertRaises(package_acceptance_isolated.AcceptanceError):
                        package_acceptance_isolated.validate_args(args)
                    loader.assert_not_called()
                    docker.assert_not_called()
                self.assertFalse(evidence.exists())

    def test_explicit_empty_input_flags_do_not_silently_disable_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            command = _command(paths, evidence)[3:] + [
                "--input-events", "", "--expected-input-sha256", ""]
            with mock.patch.object(package_acceptance_isolated, "DockerRuntime") as docker, \
                    mock.patch.object(package_acceptance_isolated, "_get_json") as api, \
                    contextlib.redirect_stderr(io.StringIO()) as errors:
                self.assertEqual(package_acceptance_isolated.main(command), 1)
                docker.assert_not_called()
                api.assert_not_called()
            self.assertIn("input events path", errors.getvalue())
            self.assertFalse(evidence.exists())

    def test_child_receipt_requires_bound_identity_complete_ack_and_consistent_counters(self):
        mutations = [
            lambda value: value.pop("diagnostics"),
            lambda value: value.update(diagnostics=[]),
            lambda value: value.update(mode="lifecycle-only"),
            lambda value: value["diagnostics"].append(dict(value["diagnostics"][0])),
        ]
        for field, bad_values in {
            "sha256": ["e" * 64, None],
            "interface": [{"id": "fes.gamepad", "major": 1, "minor": 0},
                          {"id": "fes.keyboard", "major": True, "minor": 0}, None],
            "events_requested": [1, 0, True, "2"],
            "events_acknowledged": [1, 3, False],
            "frames_before": [-1, True, 13],
            "frames_after": [-1, "12", 9],
            "frame_delta": [0, -1, 1, True],
            "input_session_id": [None, "", "bad\nID"],
        }.items():
            for value in bad_values:
                mutations.append(lambda receipt, field=field, value=value:
                                 receipt["diagnostics"][0].update({field: value}))
        with tempfile.TemporaryDirectory() as directory:
            args, _, _, _, diagnostic = self.arguments(directory)
            args.input_diagnostic = diagnostic
            receipt = Path(directory) / "child.json"
            receipt.write_text(json.dumps(self.child_receipt(diagnostic)))
            self.assertEqual(package_acceptance_isolated._read_runner_receipt(receipt, args), "same-entry")
            for mutation in mutations:
                value = self.child_receipt(diagnostic)
                mutation(value)
                receipt.write_text(json.dumps(value))
                with self.subTest(value=value), self.assertRaises(package_acceptance_isolated.AcceptanceError):
                    package_acceptance_isolated._read_runner_receipt(receipt, args)

    def run_cycles(self, directory, bad_cycle=None):
        args, paths, evidence, source, diagnostic = self.arguments(directory)
        target = package_acceptance_isolated.validate_args(args)
        self.assertEqual(args.input_diagnostic.raw, diagnostic.raw)
        calls = []
        snapshot_identity = []

        def start(acceptance, cycle):
            package_acceptance_isolated._cycle_directory(evidence, cycle)
            record = package_acceptance_isolated.ContainerRecord(
                name=cycle, cycle=cycle, start_attempted=True, started=True)
            acceptance.containers.append(record)
            return record, "http://127.0.0.1:1234"

        def child(command, **kwargs):
            calls.append(command)
            frozen = Path(command[command.index("--input-events") + 1])
            self.assertEqual(frozen, evidence / "input-events.json")
            self.assertEqual(frozen.read_bytes(), diagnostic.raw)
            self.assertEqual(frozen.stat().st_mode & 0o777, 0o400)
            snapshot_identity.append((frozen.stat().st_ino, frozen.stat().st_mtime_ns))
            self.assertEqual(command[command.index("--expected-input-sha256") + 1], diagnostic.sha256)
            self.assertEqual(command[command.index("--input-timeout") + 1], "7.5")
            source.write_bytes(b"changed original after preflight")
            value = self.child_receipt(diagnostic)
            if len(calls) == bad_cycle:
                value.pop("diagnostics")
            Path(command[command.index("--receipt") + 1]).write_text(json.dumps(value))
            return subprocess.CompletedProcess(command, 0, "", "")

        with mock.patch.object(package_acceptance_isolated, "DockerRuntime") as docker, \
                mock.patch.object(package_acceptance_isolated, "validate_image_metadata"), \
                mock.patch.object(package_acceptance_isolated.IsolatedAcceptance, "_start", start), \
                mock.patch.object(package_acceptance_isolated.subprocess, "run", side_effect=child):
            runtime = docker.return_value
            runtime.stop.return_value = {"exit_code": 0, "status": "exited"}
            if bad_cycle:
                with self.assertRaisesRegex(package_acceptance_isolated.AcceptanceError, "input diagnostic"):
                    package_acceptance_isolated.execute_isolated(args, target)
                self.assertFalse((evidence / "receipt.json").exists())
                failure = json.loads((evidence / "failure.json").read_text())
                self.assertFalse(failure["success"])
                self.assertEqual(len(calls), bad_cycle)
                self.assertEqual(failure["cleanup_errors"], [])
                self.assertEqual(len(failure["cycles"]), bad_cycle - 1)
                self.assertEqual(len(failure["containers"]), bad_cycle)
                for container in failure["containers"]:
                    self.assertTrue(container["removed"])
                    self.assertEqual(container["remove_attempts"], 1)
                    self.assertEqual(container["exit_code"], 0)
                    self.assertEqual(container["status"], "exited")
            else:
                package_acceptance_isolated.execute_isolated(args, target)
                self.assertEqual(len(calls), 2)
                self.assertEqual(snapshot_identity[0], snapshot_identity[1])
                self.assertIn("--new-entry-title", calls[0])
                self.assertIn("--game-id", calls[1])
                receipt = json.loads((evidence / "receipt.json").read_text())
                self.assertEqual(receipt["mode"], "isolated-lifecycle-input-diagnostic")
                self.assertEqual(receipt["input_diagnostic"]["sha256"], diagnostic.sha256)
                self.assertIn("not hardware consumption", receipt["diagnostic_scope"])
                for cycle in receipt["cycles"]:
                    self.assertEqual(cycle["diagnostics"], self.child_receipt(diagnostic)["diagnostics"])
            # Exercise the real _stop/_remove and cleanup bookkeeping. A failed
            # receipt still retires its started container once; previously
            # completed cycles must not be stopped or removed again.
            expected_cleanup = []
            for index in range(1, (bad_cycle or 2) + 1):
                expected_cleanup.extend([
                    mock.call.stop(f"cycle-{index}", args.shutdown_timeout),
                    mock.call.remove(f"cycle-{index}", force=False),
                ])
            self.assertEqual([call for call in runtime.mock_calls
                              if call[0] in ("stop", "remove")], expected_cleanup)
        self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())

    def test_real_invalid_snapshot_rejected_by_main_before_docker(self):
        for contents, correct_hash in ((b"not JSON", True), (b"{}", False)):
            with self.subTest(contents=contents), tempfile.TemporaryDirectory() as directory:
                args, paths, evidence, source, diagnostic = self.arguments(directory)
                source.write_bytes(contents)
                expected = hashlib.sha256(contents).hexdigest() if correct_hash else diagnostic.sha256
                command = _command(paths, evidence)[3:] + [
                    "--input-events", str(source), "--expected-input-sha256", expected]
                with mock.patch.object(package_acceptance_isolated, "DockerRuntime") as docker, \
                        mock.patch.object(package_acceptance_isolated, "_get_json") as api, \
                        contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(package_acceptance_isolated.main(command), 1)
                    docker.assert_not_called()
                    api.assert_not_called()
                self.assertFalse(evidence.exists())

    def test_both_cycles_forward_one_snapshot_even_when_original_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            self.run_cycles(directory)

    def test_each_cycle_requires_diagnostic_receipt(self):
        for cycle in (1, 2):
            with self.subTest(cycle=cycle), tempfile.TemporaryDirectory() as directory:
                self.run_cycles(directory, bad_cycle=cycle)

    def test_default_mode_receipt_and_runner_have_no_diagnostic_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            args, _, _, _, _ = self.arguments(directory)
            args.input_events = args.expected_input_sha256 = None
            package_acceptance_isolated.validate_args(args)
            self.assertIsNone(args.input_diagnostic)
            receipt = package_acceptance_isolated._receipt_base(args, True)
            self.assertEqual(receipt["mode"], "isolated-lifecycle")
            self.assertNotIn("input_diagnostic", receipt)
            command = package_acceptance_isolated.runner_command(args, "http://127.0.0.1:1234", Path("receipt"), None)
            self.assertNotIn("--input-events", command)
            self.assertNotIn("--input-timeout", command)


class IsolatedPackageAcceptanceOfflineTests(unittest.TestCase):
    def test_media_receipt_binding_is_required(self):
        args = SimpleNamespace(library_media="rom.bin", expected_media_sha256="e" * 64,
            library_media_bytes=32768, expected_host_sha256="f" * 64, container_image=IMAGE,
            expected_package_id=PACKAGE_ID, expected_core_id=CORE_ID, expected_target_id=TARGET_ID,
            expected_host_revision=HOST_REVISION, expected_agent_revision=HOST_REVISION,
            expected_runtime_revision=RUNTIME_REVISION, allow_development_host=False)
        expected = {"media_id": "e" * 64, "sha256": "e" * 64, "size": 32768, "role": "blob"}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "receipt.json"
            value = {"success": True, "package_id": PACKAGE_ID, "core_id": CORE_ID,
                     "target_id": TARGET_ID, "library_media": expected, "media_admission": "imported",
                     "selection": {"game_id": "sms-title", "package_id": PACKAGE_ID,
                                   "media_id": "e" * 64, "media_role": "blob"}}
            path.write_text(json.dumps(value))
            self.assertEqual(package_acceptance_isolated._read_runner_receipt(path, args), "sms-title")
            for media in (None, {**expected, "media_id": "f" * 64},
                          {**expected, "size": 16384}, {**expected, "role": "other"}):
                with self.subTest(media=media):
                    path.write_text(json.dumps({**value, "library_media": media}))
                    with self.assertRaises(package_acceptance_isolated.AcceptanceError):
                        package_acceptance_isolated._read_runner_receipt(path, args)
            value["selection"]["media_id"] = "f" * 64
            path.write_text(json.dumps(value))
            with self.assertRaises(package_acceptance_isolated.AcceptanceError):
                package_acceptance_isolated._read_runner_receipt(path, args)

    def test_rejects_root_caller_for_supported_image(self):
        metadata = {
            "Id": IMAGE,
            "Config": {
                "User": "builder",
                "Labels": {
                    "org.fes.media.uid": "0",
                    "org.fes.media.gid": str(os.getgid()),
                },
            },
        }
        with mock.patch.object(package_acceptance_isolated.os, "getuid", return_value=0):
            with self.assertRaisesRegex(package_acceptance_isolated.AcceptanceError, "non-root"):
                package_acceptance_isolated.validate_image_metadata(IMAGE, metadata)

    def test_requires_execute_before_creating_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(paths, evidence, execute=False),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertEqual(result.returncode, 2, result.stderr)
            self.assertIn("execute", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_rejects_host_binary_digest_before_container_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(paths, evidence, expected_host_sha256="e" * 64),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("host binary", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_rejects_nonmatching_strict_host_and_agent_revisions_offline(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(paths, evidence, expected_agent_revision="f" * 40),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("host and agent", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_rejects_config_target_id_mismatch_before_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(paths, evidence, expected_target_id="target-2"),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("target", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_development_mode_still_requires_full_agent_and_runtime_commits(self):
        for field in ("expected_agent_revision", "expected_runtime_revision"):
            with self.subTest(field=field):
                with tempfile.TemporaryDirectory() as directory:
                    paths = _write_inputs(directory)
                    evidence = Path(directory) / "evidence"
                    result = subprocess.run(
                        _command(
                            paths,
                            evidence,
                            allow_development=True,
                            expected_host_revision="diag-host-revision",
                            **{field: "not-a-commit"},
                        ),
                        cwd=ROOT,
                        text=True,
                        capture_output=True,
                    )

                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(field.removeprefix("expected_").replace("_", " "), result.stderr.lower())
                    self.assertFalse(evidence.exists())

    def test_development_mode_rejects_unequal_full_host_and_agent_commits(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(
                    paths,
                    evidence,
                    allow_development=True,
                    expected_host_revision="a" * 40,
                    expected_agent_revision="b" * 40,
                ),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("host and agent", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_rejects_target_url_userinfo_before_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            paths["config"].write_text(
                paths["config"].read_text(encoding="utf-8").replace(
                    'address = "http://127.0.0.1:8182"',
                    'address = "http://user:secret@127.0.0.1:8182"',
                ),
                encoding="utf-8",
            )
            paths["config"].chmod(0o600)
            evidence = Path(directory) / "evidence"
            result = subprocess.run(
                _command(paths, evidence),
                cwd=ROOT,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("userinfo", result.stderr.lower())
            self.assertNotIn("secret", result.stderr.lower())
            self.assertFalse(evidence.exists())


class IsolatedPackageAcceptanceContainerTests(unittest.TestCase):
    def test_media_is_bound_across_restart_and_final_receipt(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        media_bytes = bytes(range(256)) * 128
        digest = hashlib.sha256(media_bytes).hexdigest()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            media = Path(directory) / "sms.bin"
            media.write_bytes(media_bytes)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            command = _isolated_command(paths, evidence, runtime)
            command.extend(["--library-media", str(media), "--expected-media-sha256", digest])
            result = _run_isolated(command, Path(directory) / "runtime.log",
                                   Path(directory) / "runtime-state.json", api_port=server.server_port)
            self.assertEqual(result.returncode, 0, result.stderr)
            expected = {"media_id": digest, "sha256": digest, "size": 32768, "role": "blob"}
            receipt = json.loads((evidence / "receipt.json").read_text())
            self.assertEqual(receipt["library_media"], expected)
            self.assertEqual(receipt["cycles"][0]["game_id"], receipt["cycles"][1]["game_id"])
            self.assertEqual(state["media_uploads"], [media_bytes])
            self.assertEqual(state.get("media_puts", []), [])
            self.assertEqual(state["media_reads"], [digest])
            self.assertEqual(state["stops"], 2)
            self.assertEqual(len(state["launch_bodies"]), 2)
            for cycle in ("cycle-1", "cycle-2"):
                value = json.loads((evidence / cycle / "receipt.json").read_text())
                self.assertEqual(value["library_media"], expected)
                self.assertEqual(value["selection"]["media_id"], digest)
                self.assertEqual(value["media_admission"], "imported" if cycle == "cycle-1" else "retained")
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())

    def test_missing_retained_media_fails_second_cycle_without_repair(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        state["forget_media_after_stop"] = True
        media_bytes = bytes(range(256)) * 128
        digest = hashlib.sha256(media_bytes).hexdigest()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            media = Path(directory) / "sms.bin"
            media.write_bytes(media_bytes)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            command = _isolated_command(paths, evidence, runtime)
            command.extend(["--library-media", str(media), "--expected-media-sha256", digest])
            result = _run_isolated(command, Path(directory) / "runtime.log",
                                   Path(directory) / "runtime-state.json", api_port=server.server_port)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertFalse((evidence / "cycle-2/receipt.json").exists())
            self.assertTrue((evidence / "failure.json").exists())
            self.assertEqual(state["media_uploads"], [media_bytes])
            self.assertEqual(state["media_reads"], [digest])
            self.assertEqual(state.get("media_puts", []), [])
            self.assertEqual(len(state["launch_bodies"]), 1)
            self.assertEqual(state["stops"], 1)
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())

    def test_same_inode_changed_config_is_retained(self):
        target = package_acceptance_isolated.PrivateTarget(
            name="dev",
            address="http://127.0.0.1:8182",
            agent="agent-secret",
            target_id=TARGET_ID,
        )
        with tempfile.TemporaryDirectory() as directory:
            evidence = Path(directory) / "evidence"
            evidence.mkdir()
            home = package_acceptance_isolated.prepare_private_home(evidence, target)
            original = home.config.read_text(encoding="utf-8")
            changed = original.replace('agent = "agent-secret"', 'agent = "other-secret"')
            self.assertEqual(len(changed), len(original))
            with home.config.open("r+", encoding="utf-8") as handle:
                handle.seek(0)
                handle.write(changed)
                handle.truncate()
            self.assertEqual(
                (home.config.stat().st_dev, home.config.stat().st_ino),
                home.config_identity,
            )

            with self.assertRaisesRegex(package_acceptance_isolated.AcceptanceError, "changed"):
                package_acceptance_isolated.remove_private_config(home)
            self.assertEqual(home.config.read_text(encoding="utf-8"), changed)

    def test_setup_failure_after_private_config_creation_removes_credential(self):
        state = package_acceptance_tests._state()
        with tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            runtime_state = Path(directory) / "runtime-state.json"
            command = _isolated_command(paths, evidence, runtime)
            args = package_acceptance_isolated.parser().parse_args(command[3:])
            target = package_acceptance_isolated.validate_args(args)
            original_prepare = package_acceptance_isolated.prepare_private_home

            def prepare_then_fail(path, selected_target):
                original_prepare(path, selected_target)
                raise package_acceptance_isolated.AcceptanceError("injected setup interruption")

            with mock.patch.dict(
                os.environ,
                {
                    "FAKE_RUNTIME_LOG": str(runtime_log),
                    "FAKE_RUNTIME_STATE": str(runtime_state),
                },
            ), mock.patch.object(
                package_acceptance_isolated,
                "prepare_private_home",
                side_effect=prepare_then_fail,
            ), mock.patch.object(
                package_acceptance_isolated.os,
                "getuid",
                return_value=TEST_UID,
            ), mock.patch.object(
                package_acceptance_isolated.os,
                "getgid",
                return_value=TEST_GID,
            ):
                with self.assertRaises(package_acceptance_isolated.AcceptanceError):
                    package_acceptance_isolated.execute_isolated(args, target)

            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            self.assertTrue((evidence / "failure.json").exists())

    def test_runs_two_fresh_containers_with_one_private_home_and_persists_entry(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            runtime_state = Path(directory) / "runtime-state.json"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                runtime_log,
                runtime_state,
                api_port=server.server_port,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("isolated package acceptance passed", result.stdout)
            receipt = json.loads((evidence / "receipt.json").read_text(encoding="utf-8"))
            self.assertTrue(receipt["success"])
            self.assertEqual(receipt["cycles"][0]["game_id"], receipt["cycles"][1]["game_id"])
            self.assertEqual(receipt["cycles"][0]["container"]["exit_code"], 0)
            self.assertEqual(receipt["cycles"][1]["container"]["exit_code"], 0)
            records = [
                json.loads(line)
                for line in runtime_log.read_text(encoding="utf-8").splitlines()
            ]
            runs = [record for record in records if record["kind"] == "run"]
            self.assertEqual(len(runs), 2)
            self.assertNotEqual(runs[0]["name"], runs[1]["name"])
            self.assertEqual(runs[0]["home"], runs[1]["home"])
            for record in runs:
                self.assertFalse(record["config_has_live_root"])
                self.assertTrue(record["config_private"])
                self.assertFalse(record["config_has_top_secret"])
                self.assertTrue(record["config_has_agent_secret"])
                args = record["args"]
                self.assertIn("--pull=never", args)
                self.assertIn("--network=host", args)
                self.assertIn("--read-only", args)
                self.assertIn("--cap-drop=ALL", args)
                self.assertIn("--security-opt=no-new-privileges", args)
                self.assertIn("--headless", args)
                self.assertNotIn("--env", args)
                binary_mount = next(value for value in args if "dst=/opt/fes/fogcast-api" in value)
                self.assertEqual(
                    binary_mount.split("src=", 1)[1].split(",", 1)[0],
                    str(evidence / "host-binary"),
                )
                home_mount = next(value for value in args if "dst=/home/builder" in value)
                self.assertNotIn(",rw", home_mount)
                self.assertTrue(home_mount.endswith(",dst=/home/builder"))
            self.assertFalse((Path(runs[0]["home"]) / ".config/fogcast/config.toml").exists())
            snapshot = evidence / "host-binary"
            self.assertEqual(hashlib.sha256(snapshot.read_bytes()).hexdigest(), hashlib.sha256(HOST_BINARY).hexdigest())
            self.assertEqual(snapshot.stat().st_mode & 0o077, 0)
            self.assertNotIn("top-secret", result.stdout + result.stderr)
            self.assertNotIn("agent-secret", result.stdout + result.stderr)

    def test_refuses_unsupported_image_before_starting_a_container(self):
        state = package_acceptance_tests._state()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                Path(directory) / "runtime.log",
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_IMAGE_USER="root",
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("builder", result.stderr.lower())
            self.assertFalse(evidence.exists())

    def test_container_start_failure_preserves_sanitized_runtime_stderr(self):
        state = package_acceptance_tests._state()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                Path(directory) / "runtime.log",
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_RUNTIME_MODE="start-failure",
            )

            self.assertNotEqual(result.returncode, 0)
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertIn("invalid field rw", failure["error"])
            self.assertNotIn("agent-secret", failure["error"])
            self.assertNotIn("agent-secret", result.stdout + result.stderr)
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())

    def test_startup_without_current_listener_log_never_runs_acceptance(self):
        state = package_acceptance_tests._state()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime, container_timeout="0.2"),
                runtime_log,
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_RUNTIME_MODE="missing-log",
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(state["health_calls"], 0)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertFalse(failure["success"])

    def test_nonzero_container_shutdown_prevents_restart_and_success_receipt(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                runtime_log,
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_STOP_EXIT=1,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len(state["launch_bodies"]), 1)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertFalse(failure["success"])
            self.assertIn("shutdown", failure["error"].lower())

    def test_stop_timeout_cleans_credential_and_owned_container(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime, shutdown_timeout="0.01"),
                runtime_log,
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_STOP_SLEEP="6",
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertIn("timeout", failure["error"].lower())
            self.assertTrue(failure["containers"][0]["stop_error"])

    def test_container_remove_failure_is_reported_without_replaying_stop(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            runtime_state = Path(directory) / "runtime-state.json"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                runtime_log,
                runtime_state,
                api_port=server.server_port,
                FAKE_RM_EXIT=1,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertFalse((evidence / "container-home/.config/fogcast/config.toml").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertIn("cleanup", failure["error"].lower())
            self.assertTrue(any("residual" in error.lower() for error in failure["cleanup_errors"]))
            self.assertFalse(failure["containers"][0]["removed"])
            records = [json.loads(line) for line in runtime_log.read_text(encoding="utf-8").splitlines()]
            self.assertEqual([record["kind"] for record in records if record["kind"] == "stop"], ["stop"])
            removal_records = [record for record in records if record["kind"] == "rm"]
            self.assertEqual(len(removal_records), 2)
            self.assertIn("--force", removal_records[1]["args"])
            self.assertTrue(json.loads(runtime_state.read_text(encoding="utf-8")))

    def test_transient_container_remove_failure_uses_one_forced_retry(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            runtime_state = Path(directory) / "runtime-state.json"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                runtime_log,
                runtime_state,
                api_port=server.server_port,
                FAKE_RM_FAILURES=1,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((evidence / "receipt.json").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertTrue(failure["containers"][0]["removed"])
            records = [json.loads(line) for line in runtime_log.read_text(encoding="utf-8").splitlines()]
            removal_records = [record for record in records if record["kind"] == "rm"]
            self.assertEqual(len(removal_records), 2)
            self.assertNotIn("--force", removal_records[0]["args"])
            self.assertIn("--force", removal_records[1]["args"])
            self.assertEqual(json.loads(runtime_state.read_text(encoding="utf-8")), {})

    def test_replaced_private_config_is_retained_and_success_is_refused(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime),
                Path(directory) / "runtime.log",
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
                FAKE_REPLACE_CONFIG="1",
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((evidence / "receipt.json").exists())
            self.assertEqual(
                (evidence / "container-home/.config/fogcast/config.toml").read_text(encoding="utf-8"),
                "replacement",
            )
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertIn("changed", failure["error"].lower())

    def test_runner_timeout_stops_and_removes_owned_container(self):
        state = package_acceptance_tests._state()
        state["entries"] = {}
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            result = _run_isolated(
                _isolated_command(paths, evidence, runtime, runner_timeout="0.001"),
                runtime_log,
                Path(directory) / "runtime-state.json",
                api_port=server.server_port,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("runner", result.stderr.lower())
            self.assertFalse((evidence / "receipt.json").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertFalse(failure["success"])
            records = [json.loads(line) for line in runtime_log.read_text(encoding="utf-8").splitlines()]
            self.assertEqual([record["kind"] for record in records if record["kind"] in {"stop", "rm"}], ["stop", "rm"])

    def test_sigterm_during_readiness_cleans_owned_container(self):
        state = package_acceptance_tests._state()
        with _full_identity_fixture(state) as server, tempfile.TemporaryDirectory() as directory:
            paths = _write_inputs(directory)
            runtime = _write_fake_runtime(directory)
            evidence = Path(directory) / "evidence"
            runtime_log = Path(directory) / "runtime.log"
            runtime_state = Path(directory) / "runtime-state.json"
            environment = {
                "FAKE_RUNTIME_MODE": "missing-log",
            }
            command = _isolated_command(paths, evidence, runtime, container_timeout="5")
            process_environment = os.environ.copy()
            process_environment.update(
                {
                    "FAKE_RUNTIME_LOG": str(runtime_log),
                    "FAKE_RUNTIME_STATE": str(runtime_state),
                    "FAKE_API_PORT": str(server.server_port),
                    **environment,
                }
            )
            process = subprocess.Popen(
                command,
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=process_environment,
            )
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                if runtime_log.exists() and '"args": ["inspect"' in runtime_log.read_text(encoding="utf-8"):
                    break
                time.sleep(0.01)
            else:
                process.kill()
                process.wait(timeout=3)
                self.fail("fake container did not start")
            process.send_signal(signal.SIGTERM)
            stdout, stderr = process.communicate(timeout=5)

            self.assertNotEqual(process.returncode, 0)
            self.assertIn("sigterm", (stdout + stderr).lower())
            self.assertFalse((evidence / "receipt.json").exists())
            failure = json.loads((evidence / "failure.json").read_text(encoding="utf-8"))
            self.assertFalse(failure["success"])
            records = [json.loads(line) for line in runtime_log.read_text(encoding="utf-8").splitlines()]
            self.assertEqual([record["kind"] for record in records if record["kind"] in {"stop", "rm"}], ["stop", "rm"])



if __name__ == "__main__":
    unittest.main()
