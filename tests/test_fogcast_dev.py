from __future__ import annotations

import hashlib
import io
import json
import os
import signal
import stat
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from scripts import fogcast_dev


RUN_ID = "0123456789abcdef0123456789abcdef"
SESSION = "abcdef0123456789abcdef0123456789"
RECOVERED_SESSION = "fedcba9876543210fedcba9876543210"
SOURCE_COMMIT = "0123456789abcdef0123456789abcdef01234567"
ARTIFACT = b"fixture-rbf"
ARTIFACT_SHA256 = hashlib.sha256(ARTIFACT).hexdigest()
REPORT_SHA256 = "fedcba9876543210" * 4
MAIN_SHA256 = "a" * 64
TOOL_SHA256 = "b" * 64
BOOT_ID = "00000000-0000-0000-0000-000000000001"
JOURNAL_SHA256 = "c" * 64
PROFILE_SHA256 = "d" * 64
CAPABILITIES = [
    "cast_unavailable",
    "input_unavailable",
    "controller_routes_unavailable",
    "presentation_unavailable",
    "audio_unavailable",
]
OWNER_KEYS = [
    "schema",
    "state",
    "phase",
    "boot_id",
    "run_id",
    "generation_high_water",
    "active_session",
    "active_generation",
    "active_mode",
    "candidate_session",
    "candidate_generation",
    "candidate_mode",
    "quiescing_owner",
    "candidate_owner",
    "active_owner",
    "active_leases",
    "requested_resources",
    "first_failure",
]
NORMAL_LEASES = [
    "command_fifo",
    "core_input_saves",
    "core_protocol",
    "fpga_bridges",
    "fpga_generation",
    "fpga_programming",
    "main_process_set",
    "native_video_audio",
]


def _json_bytes(fields: list[tuple[str, object]]) -> bytes:
    return (json.dumps(dict(fields), separators=(",", ":"), ensure_ascii=True) + "\n").encode()


def write_bundle(root: Path, run_id: str = RUN_ID, lane: str = "oss") -> Path:
    bundle = root / "build" / "dev-bundle" / lane / "020_linux_mailbox"
    bundle.mkdir(parents=True)
    os.chmod(root / "build", 0o700)
    os.chmod(root / "build" / "dev-bundle", 0o700)
    os.chmod(root / "build" / "dev-bundle" / lane, 0o700)
    os.chmod(bundle, 0o700)
    manifest = _json_bytes(
        [
            ("schema", 1),
            ("run_id", run_id),
            ("experiment", "020_linux_mailbox"),
            ("board", "misterpi"),
            ("build_lane", lane),
            ("artifact_filename", "top.rbf"),
            ("artifact_size", len(ARTIFACT)),
            ("artifact_sha256", ARTIFACT_SHA256),
            ("source_commit", SOURCE_COMMIT),
        ]
    )
    evidence = _json_bytes(
        [
            ("schema", 2),
            ("experiment", "020_linux_mailbox"),
            ("board", "misterpi"),
            ("build_lane", lane),
            ("source_commit", SOURCE_COMMIT),
            ("artifact_sha256", ARTIFACT_SHA256),
            ("synthesis_report_sha256", REPORT_SHA256),
            ("clock_inputs", 1),
            ("external_input_ports", 0),
            ("external_output_ports", 0),
            ("bidirectional_ports", 0),
            ("hps_general_purpose_interfaces", 1),
            ("pll_blocks", 0),
            ("dsp_blocks", 0),
            ("block_memory_bits", 0),
            ("lutram_bits", 0),
            ("sdram_interfaces", 0),
        ]
    )
    members = {"manifest.json": manifest, "resource_evidence.json": evidence, "top.rbf": ARTIFACT}
    checksums = b"".join(
        f"{hashlib.sha256(members[name]).hexdigest()}  {name}\n".encode()
        for name in ("manifest.json", "resource_evidence.json", "top.rbf")
    )
    members["bundle.sha256"] = checksums
    for name, payload in members.items():
        path = bundle / name
        path.write_bytes(payload)
        os.chmod(path, 0o600)
    return bundle


def valid_result(
    run_id: str = RUN_ID,
    lane: str = "oss",
    *,
    session: str = SESSION,
    generation: int = 42,
    recovery_request: str = "pending",
    source_commit: str = SOURCE_COMMIT,
    artifact_sha256: str = ARTIFACT_SHA256,
) -> bytes:
    payload = b"OSS FPGA OK\n"
    fields = [
        ("schema", 1),
        ("run_id", run_id),
        ("generation", generation),
        ("session", session),
        ("mode", "updating"),
        ("experiment", "020_linux_mailbox"),
        ("build_lane", lane),
        ("artifact_sha256", artifact_sha256),
        ("source_commit", source_commit),
        ("phase", "done_observed"),
        ("primary_code", "ok"),
        ("primary_detail", ""),
        ("payload_hex", payload.hex()),
        ("payload_length", len(payload)),
        ("payload_sha256", hashlib.sha256(payload).hexdigest()),
        ("terminal_word", "d3130c00"),
        ("recovery_request", recovery_request),
        ("elapsed_ms", 125),
    ]
    return _json_bytes(fields)


def valid_ready(
    *,
    session: str = SESSION,
    generation: int = 42,
) -> bytes:
    return _json_bytes(
        [
            ("schema", 3),
            ("boot_id", BOOT_ID),
            ("journal_sha256", JOURNAL_SHA256),
            ("owner_session", session),
            ("owner_generation", generation),
            ("profile_sha256", PROFILE_SHA256),
            ("capabilities", CAPABILITIES),
            ("supervisor_pid", 100),
            ("supervisor_start_time", 1000),
            ("main_pid", 101),
            ("main_start_time", 1001),
            ("agent_pid", 102),
            ("agent_start_time", 1002),
        ]
    )


def valid_owner(
    *,
    boot_id: str = BOOT_ID,
    session: str = SESSION,
    generation: int = 42,
) -> bytes:
    return _json_bytes(
        [
            ("schema", 1),
            ("state", "normal_main"),
            ("phase", ""),
            ("boot_id", boot_id),
            ("run_id", ""),
            ("generation_high_water", generation),
            ("active_session", session),
            ("active_generation", generation),
            ("active_mode", "fpga_native"),
            ("candidate_session", ""),
            ("candidate_generation", 0),
            ("candidate_mode", "none"),
            ("quiescing_owner", "none"),
            ("candidate_owner", "none"),
            ("active_owner", "compat_main"),
            ("active_leases", NORMAL_LEASES),
            ("requested_resources", []),
            ("first_failure", ""),
        ]
    )


def valid_live(ready: bytes | None = None, owner: bytes | None = None) -> str:
    parsed = json.loads(ready or valid_ready())
    owner_raw = owner or valid_owner(
        boot_id=parsed["boot_id"],
        session=parsed["owner_session"],
        generation=parsed["owner_generation"],
    )
    return (
        "FOGCAST_MISTEROSS_LIVE_V1\n"
        f"BOOT|{(parsed['boot_id'] + chr(10)).encode().hex()}\n"
        f"JOURNAL|regular file|0|0|600|1|{parsed['journal_sha256']}\n"
        f"PROFILE|regular file|0|0|600|1|{parsed['profile_sha256']}\n"
        f"OWNER|regular file|0|0|600|1|{len(owner_raw)}|{hashlib.sha256(owner_raw).hexdigest()}\n"
        f"OWNER_RECORD|{owner_raw.hex()}\n"
        f"PROC|supervisor|{parsed['supervisor_pid']}|{parsed['supervisor_start_time']}\n"
        f"PROC|main|{parsed['main_pid']}|{parsed['main_start_time']}\n"
        f"PROC|agent|{parsed['agent_pid']}|{parsed['agent_start_time']}\n"
        "FIFO|fifo|0|0|600|1\n"
        "FPGA|6f7065726174696e670a\n"
        "MENU_FIRST|4d454e550a\n"
        "MENU_SECOND|4d454e550a\n"
    )


class FakeClock:
    def __init__(self):
        self.value = 0.0

    def __call__(self) -> float:
        return self.value

    def advance(self, seconds: float) -> None:
        self.value += seconds

    def sleep(self, seconds: float) -> None:
        self.advance(seconds)


class FakeProcess:
    def __init__(self, returncode: int = 255, *, poll_returncode: int | None = None):
        self.returncode = returncode
        self.poll_returncode = poll_returncode
        self.wait_calls = 0
        self.terminate_calls = 0
        self.kill_calls = 0
        self.args = None

    def wait(self, timeout=None):
        self.wait_calls += 1
        return self.returncode

    def poll(self):
        return self.poll_returncode

    def terminate(self):
        self.terminate_calls += 1

    def kill(self):
        self.kill_calls += 1


class FakeTransport:
    def __init__(self, result: bytes | None = None, *, run_returncode: int = 255):
        self.commands: list[tuple[str, list[str], str | None]] = []
        self.result = result or valid_result()
        self.ready = valid_ready(session=RECOVERED_SESSION, generation=43)
        self.owner = valid_owner(session=RECOVERED_SESSION, generation=43)
        self.run_returncode = run_returncode
        self.load_run_returncode = 255
        self.load_run_stdout = ""
        self.load_run_stderr = ""
        self.reboot_returncode = 255
        self.reboot_stdout = ""
        self.reboot_stderr = ""
        self.preflight_returncode = 0
        self.preflight_stdout = "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n"
        self.preflight_stderr = ""
        self.attestation_returncode = 0
        self.attestation_stdout: str | None = None
        self.cleanup_returncode = 0
        self.verify_returncode = 0
        self.mkdir_stdout = "DIR|directory|0|0|700|2\n"
        self.live_stdout: str | None = None
        self.inspect_outputs: list[str] = []
        self.inspect_timeouts: list[float | None] = []
        self.fault_kill_stdout = f"FOGCAST_FPGA_DEV_FAULT_KILLED run_id={RUN_ID}\n"
        self.fault_kill_stderr = ""
        self.fault_recovery_returncode = 0
        self.fault_recovery_stdout = "FOGCAST_MISTEROSS_FAULT_RECOVERY_V1\n"
        self.fault_recovery_stderr = ""
        self.child_stdout = b""
        self.child_stderr = b""
        self.processes: list[FakeProcess] = []
        self.remote_order: list[str] = []
        self.resolutions = 0
        self.uploaded: dict[str, bytes] = {}
        self.clock: FakeClock | None = None
        self.resolve_elapsed = 0.0
        self.elapsed: dict[str, float] = {}

    def _advance(self, label: str) -> None:
        if self.clock is not None:
            self.clock.advance(self.elapsed.get(label, 0.0))

    def _attestation(self) -> str:
        if self.attestation_stdout is not None:
            return self.attestation_stdout
        return (
            "FOGCAST_MISTEROSS_ATTEST_V1\n"
            "BOARD|misterpi\n"
            f"TOOL|/usr/bin/mister-fpga-dev|{TOOL_SHA256}|regular file|0|0|755|1\n"
            f"MAIN|/media/fat/MiSTer|{MAIN_SHA256}|regular file|0|0|755|1\n"
        )

    def run(self, argv, *, timeout=None, env=None, **_kwargs):
        command = argv[-1] if argv else ""
        self.commands.append(("run", list(argv), command))
        if "FOGCAST_MISTEROSS_RECONNECT_V1" in command:
            self.remote_order.append("reconnect")
            self._advance("reconnect")
            return SimpleNamespace(
                returncode=0,
                stdout="FOGCAST_MISTEROSS_RECONNECT_V1\n",
                stderr="",
            )
        if "FOGCAST_MISTEROSS_LIVE_V1" in command:
            self.remote_order.append("live")
            self._advance("live")
            return SimpleNamespace(
                returncode=0,
                stdout=self.live_stdout or valid_live(self.ready, self.owner),
                stderr="",
            )
        if "FOGCAST_MISTEROSS_FAULT_RECOVERY_V1" in command:
            self.remote_order.append("fault-recovery")
            self._advance("fault-recovery")
            return SimpleNamespace(
                returncode=self.fault_recovery_returncode,
                stdout=self.fault_recovery_stdout,
                stderr=self.fault_recovery_stderr,
            )
        if "FOGCAST_MISTEROSS_READINESS_V3" in command:
            self.remote_order.append("ready")
            self._advance("ready")
            ready_hash = hashlib.sha256(self.ready).hexdigest()
            out = self._attestation() + (
                "FOGCAST_MISTEROSS_READY_V3\n"
                f"READY|regular file|0|0|600|1|{len(self.ready)}|{ready_hash}\n"
                f"RECORD|{self.ready.hex()}\n"
            )
            return SimpleNamespace(returncode=0, stdout=out, stderr="")
        if "FOGCAST_MISTEROSS_ATTEST_V1" in command:
            self.remote_order.append("attest")
            return SimpleNamespace(
                returncode=self.attestation_returncode,
                stdout=self._attestation(),
                stderr="",
            )
        if "FOGCAST_MISTEROSS_MKDIR_V1" in command:
            self.remote_order.append("mkdir")
            return SimpleNamespace(returncode=0, stdout=self.mkdir_stdout, stderr="")
        if "FOGCAST_MISTEROSS_VERIFY_V1" in command:
            self.remote_order.append("verify")
            lines = ["FOGCAST_MISTEROSS_STAGE_V1", "DIR|directory|0|0|700|2"]
            for name in ("manifest.json", "resource_evidence.json", "top.rbf", "bundle.sha256"):
                payload = self.uploaded[name]
                digest = hashlib.sha256(payload).hexdigest()
                lines.append(f"FILE|{name}|regular file|0|0|600|1|{len(payload)}|{digest}")
            return SimpleNamespace(
                returncode=self.verify_returncode,
                stdout="\n".join(lines) + "\n",
                stderr="verify failed" if self.verify_returncode else "",
            )
        if "mister-fpga-dev preflight" in command:
            self.remote_order.append("preflight")
            self._advance("preflight")
            if (
                "run" in self.remote_order
                and "result-delete" not in self.remote_order
                and self.preflight_returncode == 0
                and self.preflight_stdout == "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n"
                and self.preflight_stderr == ""
            ):
                return SimpleNamespace(
                    returncode=2,
                    stdout="",
                    stderr="FOGCAST_FPGA_DEV_PREFLIGHT code=result_conflict\n",
                )
            return SimpleNamespace(
                returncode=self.preflight_returncode,
                stdout=self.preflight_stdout,
                stderr=self.preflight_stderr,
            )
        if "mister-fpga-dev fault-arm" in command:
            self.remote_order.append("arm")
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        if "mister-fpga-dev inspect" in command:
            self.remote_order.append("inspect")
            self.inspect_timeouts.append(timeout)
            self._advance("inspect")
            out = self.inspect_outputs.pop(0) if self.inspect_outputs else (
                "FOGCAST_FPGA_DEV_INSPECT run_id="
                f"{RUN_ID} session={SESSION} generation=42 phase=load_attempted "
                f"pid=123 start_time=456 executable_sha256={TOOL_SHA256}\n"
            )
            return SimpleNamespace(returncode=0, stdout=out, stderr="")
        if "mister-fpga-dev fault-kill" in command:
            self.remote_order.append("fault-kill")
            return SimpleNamespace(
                returncode=0,
                stdout=self.fault_kill_stdout,
                stderr=self.fault_kill_stderr,
            )
        if "mister-fpga-dev recovery-reboot" in command:
            self.remote_order.append("reboot")
            return SimpleNamespace(
                returncode=self.reboot_returncode,
                stdout=self.reboot_stdout,
                stderr=self.reboot_stderr,
            )
        if "FOGCAST_MISTEROSS_RESULT_V1" in command:
            self.remote_order.append("result-probe")
            self._advance("result-probe")
            digest = hashlib.sha256(self.result).hexdigest()
            return SimpleNamespace(
                returncode=0,
                stdout=f"RESULT|regular file|0|0|600|1|{len(self.result)}|{digest}\n",
                stderr="",
            )
        if "FOGCAST_MISTEROSS_RESULT_DELETE_V1" in command:
            self.remote_order.append("result-delete")
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        if "mister-fpga-dev run" in command:
            self.remote_order.append("run")
            return SimpleNamespace(
                returncode=self.load_run_returncode,
                stdout=self.load_run_stdout,
                stderr=self.load_run_stderr,
            )
        if "FOGCAST_MISTEROSS_CLEANUP_V1" in command:
            self.remote_order.append("cleanup")
            return SimpleNamespace(returncode=self.cleanup_returncode, stdout="", stderr="cleanup failed" if self.cleanup_returncode else "")
        return SimpleNamespace(returncode=0, stdout="", stderr="")

    def popen(self, argv, *, stdout, stderr, stdin=None, env=None, **_kwargs):
        process = FakeProcess(self.run_returncode)
        process.args = list(argv)
        stdout.write(self.child_stdout)
        stdout.flush()
        stderr.write(self.child_stderr)
        stderr.flush()
        self.processes.append(process)
        return process

    def scp(self, argv, *, timeout=None, env=None, **_kwargs):
        self.commands.append(("scp", list(argv), None))
        for value in argv:
            path = Path(value)
            if path.name in {"manifest.json", "resource_evidence.json", "top.rbf", "bundle.sha256"} and path.is_file():
                self.uploaded[path.name] = path.read_bytes()
        # Result retrieval uses the remote source followed by a local path.
        if len(argv) >= 2 and "result-" in argv[-2]:
            Path(argv[-1]).write_bytes(self.result)
            os.chmod(argv[-1], 0o600)
        return SimpleNamespace(returncode=0, stdout="", stderr="")

    def resolve(self, host):
        self.resolutions += 1
        if self.clock is not None:
            self.clock.advance(self.resolve_elapsed)
        return host


def set_recovered_ready(
    fake: FakeTransport,
    *,
    session: str = RECOVERED_SESSION,
    generation: int = 43,
) -> None:
    fake.ready = valid_ready(session=session, generation=generation)
    fake.owner = valid_owner(session=session, generation=generation)


def config(root: Path, fake: FakeTransport, *, dry_run: bool = False) -> fogcast_dev.DevConfig:
    return fogcast_dev.DevConfig(
        host="fixture.example",
        user="root",
        expected_board="misterpi",
        expected_main_sha256=MAIN_SHA256,
        expected_tool_sha256=TOOL_SHA256,
        ssh="ssh-fixture",
        scp="scp-fixture",
        dry_run=dry_run,
        repo_root=root,
        trace_root=root / "traces",
        run_command=fake.run,
        popen_factory=fake.popen,
        scp_command=fake.scp,
        resolve_host=fake.resolve,
        clock=fake.clock or (lambda: 0.0),
        sleep=fake.clock.sleep if fake.clock else (lambda _seconds: None),
    )


class FogCastBundleTests(unittest.TestCase):
    def test_bundle_validation_binds_run_lane_hashes_and_private_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            selected = fogcast_dev.load_bundle(bundle, RUN_ID, "oss")
            self.assertEqual(selected.run_id, RUN_ID)
            self.assertEqual(selected.artifact_sha256, ARTIFACT_SHA256)
            self.assertEqual(selected.source_commit, SOURCE_COMMIT)
            self.assertEqual(stat.S_IMODE(bundle.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE((bundle / "manifest.json").stat().st_mode), 0o600)

    def test_bundle_rejects_path_injection_and_run_collision(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.load_bundle(bundle, "../" + RUN_ID, "oss")
            fake = FakeTransport()
            transport = fogcast_dev.Transport(config(root, fake))
            (root / "traces").mkdir(mode=0o700)
            (root / "traces" / RUN_ID).mkdir(mode=0o700)
            with self.assertRaisesRegex(fogcast_dev.TransportError, "collision"):
                transport.load(bundle)

    def test_bundle_rejects_noncanonical_tampered_or_unsafe_members(self):
        mutations = {
            "directory mode": lambda bundle: os.chmod(bundle, 0o755),
            "file mode": lambda bundle: os.chmod(bundle / "manifest.json", 0o644),
            "artifact tamper": lambda bundle: (bundle / "top.rbf").write_bytes(b"changed"),
            "extra member": lambda bundle: (bundle / "unexpected").write_bytes(b"x"),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                mutate(bundle)
                with self.assertRaises(fogcast_dev.TransportError):
                    fogcast_dev.load_bundle(bundle, RUN_ID, "oss")

    def test_bundle_rejects_symlinked_ancestor_without_following_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            link = root / "bundle-link"
            link.symlink_to(bundle, target_is_directory=True)
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.load_bundle(link, RUN_ID, "oss")


class FogCastCommandTests(unittest.TestCase):
    def test_remote_run_command_is_exact_direct_exec_and_argv_only(self):
        command = fogcast_dev.remote_run_command(RUN_ID)
        self.assertEqual(
            command,
            "exec mister-fpga-dev run --manifest "
            f"/tmp/misteross-fpgadev-{RUN_ID}/manifest.json --artifact "
            f"/tmp/misteross-fpgadev-{RUN_ID}/top.rbf",
        )
        self.assertNotIn(";", command)
        self.assertNotIn("&", command)
        argv = fogcast_dev.ssh_argv("ssh", "root", "fixture.example", command)
        self.assertNotIn("root@fixture.example", command)
        self.assertEqual(argv[-1], command)
        self.assertIn("-q", argv)
        self.assertIn("-T", argv)

    def test_preflight_stages_exact_bundle_and_cleans_without_reconnect_or_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            transport = fogcast_dev.Transport(config(root, fake))
            report = transport.preflight(bundle)
            self.assertTrue(report.ok)
            self.assertEqual(fake.remote_order.count("preflight"), 1)
            self.assertNotIn("ready", fake.remote_order)
            self.assertNotIn("result", fake.remote_order)
            self.assertEqual(fake.remote_order[-1], "cleanup")
            preflight = [command for kind, _argv, command in fake.commands if kind == "run" and command and "preflight" in command]
            self.assertEqual(len(preflight), 1)
            self.assertEqual(
                preflight[0],
                "exec mister-fpga-dev preflight --manifest "
                f"/tmp/misteross-fpgadev-{RUN_ID}/manifest.json --artifact "
                f"/tmp/misteross-fpgadev-{RUN_ID}/top.rbf",
            )

    def test_preflight_rejects_every_nonexact_framing_and_still_cleans(self):
        malformed = (
            "",
            "FOGCAST_FPGA_DEV_PREFLIGHT code=ok",
            "FOGCAST_FPGA_DEV_PREFLIGHT code=OK\n",
            "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\nextra\n",
        )
        for stdout in malformed:
            with self.subTest(stdout=stdout), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport()
                fake.preflight_stdout = stdout
                transport = fogcast_dev.Transport(config(root, fake))
                with self.assertRaises(fogcast_dev.TransportError):
                    transport.preflight(bundle)
                self.assertEqual(fake.remote_order[-1], "cleanup")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.preflight_stderr = "warning\n"
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.Transport(config(root, fake)).preflight(bundle)
            self.assertEqual(fake.remote_order[-1], "cleanup")

    def test_recovery_preflight_accepts_only_success_or_exact_retained_result_conflict(self):
        accepted = (
            SimpleNamespace(
                returncode=0,
                stdout="FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n",
                stderr="",
            ),
            SimpleNamespace(
                returncode=2,
                stdout="",
                stderr="FOGCAST_FPGA_DEV_PREFLIGHT code=result_conflict\n",
            ),
        )
        for completed in accepted:
            with self.subTest(completed=completed):
                fogcast_dev._parse_recovery_preflight(completed)
        rejected = (
            SimpleNamespace(
                returncode=0,
                stdout="",
                stderr="FOGCAST_FPGA_DEV_PREFLIGHT code=result_conflict\n",
            ),
            SimpleNamespace(
                returncode=2,
                stdout="FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n",
                stderr="",
            ),
            SimpleNamespace(
                returncode=2,
                stdout="",
                stderr="FOGCAST_FPGA_DEV_PREFLIGHT code=result_conflict",
            ),
            SimpleNamespace(
                returncode=2,
                stdout="",
                stderr="FOGCAST_FPGA_DEV_PREFLIGHT code=ownership_conflict\n",
            ),
        )
        for completed in rejected:
            with self.subTest(completed=completed), self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev._parse_recovery_preflight(completed)

    def test_preflight_aggregates_primary_and_cleanup_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.preflight_stdout = "bad\n"
            fake.cleanup_returncode = 1
            with self.assertRaisesRegex(fogcast_dev.TransportError, "preflight.*cleanup|cleanup.*preflight"):
                fogcast_dev.Transport(config(root, fake)).preflight(bundle)

    def test_preflight_cleans_a_stage_when_post_mkdir_verification_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.verify_returncode = 1
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.Transport(config(root, fake)).preflight(bundle)
            self.assertIn("mkdir", fake.remote_order)
            self.assertEqual(fake.remote_order[-1], "cleanup")

    def test_preflight_cleans_and_aggregates_after_malformed_successful_mkdir(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.mkdir_stdout = "DIR|directory|0|0|755|2\n"
            fake.cleanup_returncode = 1
            with self.assertRaises(fogcast_dev.TransportError) as caught:
                fogcast_dev.Transport(config(root, fake)).preflight(bundle)
            self.assertIn("stage directory metadata", str(caught.exception))
            self.assertIn("stage cleanup failed", str(caught.exception))
            self.assertEqual(fake.remote_order[-1], "cleanup")

    def test_load_accepts_provisional_disconnect_only_after_bound_result_and_readiness(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            transport = fogcast_dev.Transport(config(root, fake))
            report = transport.load(bundle)
            self.assertTrue(report.ok)
            self.assertEqual(report.result.run_id, RUN_ID)
            self.assertEqual(report.result.session, SESSION)
            self.assertEqual(report.result.generation, 42)
            self.assertEqual(fake.remote_order.count("run"), 1)
            self.assertIn("ready", fake.remote_order)
            self.assertIn("result-probe", fake.remote_order)
            self.assertEqual(fake.remote_order.count("preflight"), 2)
            first_preflight = fake.remote_order.index("preflight")
            final_preflight = len(fake.remote_order) - 1 - fake.remote_order[::-1].index("preflight")
            self.assertLess(first_preflight, fake.remote_order.index("ready"))
            self.assertLess(fake.remote_order.index("ready"), fake.remote_order.index("live"))
            self.assertLess(fake.remote_order.index("live"), fake.remote_order.index("result-probe"))
            self.assertLess(fake.remote_order.index("result-probe"), fake.remote_order.index("result-delete"))
            self.assertLess(fake.remote_order.index("result-delete"), final_preflight)
            self.assertLess(fake.remote_order.index("result-delete"), fake.remote_order.index("cleanup"))
            self.assertEqual(fake.resolutions, 1)
            trace = report.trace_dir
            self.assertEqual(stat.S_IMODE(trace.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE((trace / "result.json").stat().st_mode), 0o600)

    def test_load_accepts_exact_nonempty_success_framing_bound_to_persisted_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.load_run_stdout = (
                "FPGA> OSS FPGA OK\n"
                f"FOGCAST_FPGA_DEV_RESULT run_id={RUN_ID} primary=ok\n"
            )
            report = fogcast_dev.Transport(config(root, fake)).load(bundle)
            self.assertTrue(report.ok)

    def test_load_rejects_nonexact_or_contradictory_nonempty_run_stdout(self):
        cases = {
            "wrong run": (
                "FPGA> OSS FPGA OK\n"
                f"FOGCAST_FPGA_DEV_RESULT run_id={'1' * 32} primary=ok\n"
            ),
            "extra line": (
                "FPGA> OSS FPGA OK\n"
                f"FOGCAST_FPGA_DEV_RESULT run_id={RUN_ID} primary=ok\nextra\n"
            ),
            "contradictory primary": (
                f"FOGCAST_FPGA_DEV_RESULT run_id={RUN_ID} primary=protocol_violation\n"
            ),
            "success without payload line": (
                f"FOGCAST_FPGA_DEV_RESULT run_id={RUN_ID} primary=ok\n"
            ),
        }
        for label, stdout in cases.items():
            with self.subTest(label=label), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport()
                fake.load_run_stdout = stdout
                with self.assertRaises(fogcast_dev.TransportError):
                    fogcast_dev.Transport(config(root, fake)).load(bundle)

    def test_load_result_tuple_precedes_fresh_higher_recovered_normal_tuple(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport(result=valid_result(session=SESSION, generation=42))
            set_recovered_ready(fake)
            caught = None
            try:
                report = fogcast_dev.Transport(config(root, fake)).load(bundle)
            except fogcast_dev.TransportError as exc:
                caught = exc
                report = None
            self.assertIsNone(caught)
            self.assertIsNotNone(report)
            self.assertEqual((report.result.session, report.result.generation), (SESSION, 42))
            self.assertEqual(
                (report.ready.session, report.ready.generation),
                (RECOVERED_SESSION, 43),
            )

    def test_load_rejects_reused_session_or_nonincreasing_recovery_generation(self):
        cases = (
            (SESSION, 43),
            (RECOVERED_SESSION, 42),
            (RECOVERED_SESSION, 41),
        )
        for session, generation in cases:
            with self.subTest(session=session, generation=generation), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport(result=valid_result(session=SESSION, generation=42))
                set_recovered_ready(fake, session=session, generation=generation)
                with self.assertRaisesRegex(
                    fogcast_dev.TransportError,
                    "fresh recovery session|strictly higher recovery generation",
                ):
                    fogcast_dev.Transport(config(root, fake)).load(bundle)

    def test_load_rejects_recovery_failed_and_preserves_cleanup_error(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport(result=valid_result(recovery_request="failed"))
            fake.cleanup_returncode = 1
            transport = fogcast_dev.Transport(config(root, fake))
            with self.assertRaises(fogcast_dev.TransportError) as caught:
                transport.load(bundle)
            self.assertIn("recovery_request=failed", str(caught.exception))
            self.assertIn("stage cleanup failed", str(caught.exception))

    def test_load_requires_a_closed_disconnect_even_with_a_valid_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.load_run_returncode = 0
            with self.assertRaisesRegex(fogcast_dev.TransportError, "disconnect"):
                fogcast_dev.Transport(config(root, fake)).load(bundle)
            self.assertIn("ready", fake.remote_order)

    def test_load_classifies_only_the_exact_result_unavailable_line(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.load_run_returncode = 2
            fake.load_run_stderr = "FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n"
            fake.result = b""

            def missing_result(argv, **kwargs):
                if len(argv) < 2 or "result-" not in argv[-2]:
                    return fake.scp(argv, **kwargs)
                fake.commands.append(("scp", list(argv), None))
                return SimpleNamespace(returncode=1, stdout="", stderr="missing")

            cfg = config(root, fake)
            cfg = fogcast_dev.DevConfig(**{**cfg.__dict__, "scp_command": missing_result})
            with self.assertRaisesRegex(fogcast_dev.TransportError, "result_unavailable"):
                fogcast_dev.Transport(cfg).load(bundle)
            self.assertIn("ready", fake.remote_order)

    def test_result_parser_rejects_duplicate_missing_and_binding_mismatches(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            selected = fogcast_dev.load_bundle(write_bundle(root), RUN_ID, "oss")
            base = valid_result()
            mutations = {
                "duplicate": base.replace(b'"run_id":"', b'"run_id":"' + RUN_ID.encode() + b'","run_id":"', 1),
                "missing": base.replace(b',"elapsed_ms":125', b'', 1),
                "run": valid_result(run_id="1" * 32),
                "lane": valid_result(lane="oracle"),
                "source": valid_result(source_commit="1" * 40),
                "artifact": valid_result(artifact_sha256="1" * 64),
                "mode": valid_result().replace(b'"mode":"updating"', b'"mode":"normal"', 1),
            }
            for label, raw in mutations.items():
                with self.subTest(label=label), self.assertRaises(fogcast_dev.TransportError):
                    fogcast_dev.parse_result(raw, selected)

    def test_result_parser_rejects_nonprinting_failure_detail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            selected = fogcast_dev.load_bundle(write_bundle(root), RUN_ID, "oss")
            fields = json.loads(valid_result())
            fields.update(
                phase="message_partial",
                primary_code="protocol_violation",
                primary_detail="bad\x1bdetail",
                payload_hex="",
                payload_length=0,
                payload_sha256=hashlib.sha256(b"").hexdigest(),
                terminal_word="00000000",
            )
            raw = _json_bytes(list(fields.items()))
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.parse_result(raw, selected)

    def test_attestation_rejects_wrong_target_and_unsafe_executable(self):
        base = FakeTransport()._attestation()
        mutations = (
            base.replace("BOARD|misterpi", "BOARD|de10nano"),
            base.replace("/usr/bin/mister-fpga-dev", "/tmp/mister-fpga-dev"),
            base.replace("regular file|0|0|755|1", "symbolic link|0|0|777|1", 1),
            base.replace(TOOL_SHA256, "f" * 64),
            base.replace(MAIN_SHA256, "f" * 64),
        )
        for output in mutations:
            with self.subTest(output=output), self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.parse_attestation(
                    output,
                    expected_board="misterpi",
                    expected_main_sha256=MAIN_SHA256,
                    expected_tool_sha256=TOOL_SHA256,
                )

    def test_attestation_binds_the_production_main_path(self):
        output = FakeTransport()._attestation()
        parsed = fogcast_dev.parse_attestation(
            output,
            expected_board="misterpi",
            expected_main_sha256=MAIN_SHA256,
            expected_tool_sha256=TOOL_SHA256,
        )
        self.assertEqual(parsed.main_sha256, MAIN_SHA256)
        with self.assertRaises(fogcast_dev.TransportError):
            fogcast_dev.parse_attestation(
                output.replace("/media/fat/MiSTer", "/usr/bin/Main_MiSTer"),
                expected_board="misterpi",
                expected_main_sha256=MAIN_SHA256,
                expected_tool_sha256=TOOL_SHA256,
            )


class FogCastFaultTests(unittest.TestCase):
    def test_fault_recovery_uses_fresh_attested_readiness_and_absence_not_bundle_preflight(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            set_recovered_ready(fake)
            caught = None
            try:
                report = fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            except fogcast_dev.TransportError as exc:
                caught = exc
                report = None
            self.assertIsNone(caught)
            self.assertIsNotNone(report)
            self.assertEqual(
                (report.inspection.session, report.inspection.generation),
                (SESSION, 42),
            )
            self.assertEqual(
                (report.ready.session, report.ready.generation),
                (RECOVERED_SESSION, 43),
            )
            reconnect = fake.remote_order.index("reconnect")
            post_reconnect = fake.remote_order[reconnect + 1 :]
            self.assertIn("attest", post_reconnect)
            self.assertIn("ready", post_reconnect)
            self.assertIn("live", post_reconnect)
            self.assertIn("fault-recovery", post_reconnect)
            self.assertNotIn("preflight", post_reconnect)
            cleanup_command = next(
                command
                for kind, _argv, command in fake.commands
                if kind == "run" and command and "FOGCAST_MISTEROSS_FAULT_RECOVERY_V1" in command
            )
            socket_digest = hashlib.sha256(RUN_ID.encode()).hexdigest()
            for path in (
                f"/tmp/misteross-fpgadev-{RUN_ID}",
                f"/var/lib/fogcast/fpga-dev/results/result-{RUN_ID}.json",
                f"/run/fogcast/fpga-dev-{RUN_ID}.diagnostic",
                f"/run/fogcast/fpga-dev-{RUN_ID}.armed",
                f"/run/fogcast/f-{socket_digest}.sock",
            ):
                self.assertIn(path, cleanup_command)

    def test_fault_recovery_rejects_result_presence_and_never_accepts_result_conflict(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            set_recovered_ready(fake)
            fake.fault_recovery_returncode = 1
            fake.fault_recovery_stdout = ""
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            self.assertIn("fault-recovery", fake.remote_order)
            reconnect = fake.remote_order.index("reconnect")
            self.assertNotIn("preflight", fake.remote_order[reconnect + 1 :])

    def test_fault_recovery_rejects_reused_or_nonincreasing_inspected_tuple(self):
        cases = (
            (SESSION, 43),
            (RECOVERED_SESSION, 42),
            (RECOVERED_SESSION, 41),
        )
        for session, generation in cases:
            with self.subTest(session=session, generation=generation), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport()
                set_recovered_ready(fake, session=session, generation=generation)
                with self.assertRaisesRegex(
                    fogcast_dev.TransportError,
                    "fresh recovery session|strictly higher recovery generation",
                ):
                    fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)

    def test_fault_inspect_rejects_valid_identity_returned_after_absolute_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.clock = FakeClock()
            fake.elapsed["inspect"] = fogcast_dev.FAULT_INSPECT_TIMEOUT + 1.0
            caught = None
            try:
                fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            except fogcast_dev.TransportError as exc:
                caught = exc
            self.assertIsNotNone(caught)
            self.assertNotIn("fault-kill", fake.remote_order)
            self.assertEqual(fake.inspect_timeouts, [fogcast_dev.FAULT_INSPECT_TIMEOUT])

    def test_fault_inspect_immediate_failures_poll_until_absolute_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.clock = FakeClock()
            fake.inspect_outputs = ["malformed\n"] * 1000
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            self.assertGreaterEqual(fake.clock.value, fogcast_dev.FAULT_INSPECT_TIMEOUT)
            self.assertGreater(len(fake.inspect_timeouts), 64)
            self.assertAlmostEqual(fake.inspect_timeouts[0], fogcast_dev.FAULT_INSPECT_TIMEOUT)
            self.assertLessEqual(fake.inspect_timeouts[-1], 0.051)

    def test_fault_inject_uses_one_owned_run_child_and_separate_inspect_kill_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            transport = fogcast_dev.Transport(config(root, fake))
            report = transport.fault_inject(bundle)
            self.assertTrue(report.ok)
            self.assertEqual(len(fake.processes), 1)
            self.assertEqual(fake.processes[0].wait_calls, 1)
            self.assertIn("inspect", fake.remote_order)
            self.assertIn("fault-kill", fake.remote_order)
            self.assertIn("reboot", fake.remote_order)
            self.assertIn("ready", fake.remote_order)
            self.assertNotIn("cleanup", fake.remote_order)
            self.assertEqual((report.trace_dir / "inspect.log").read_text(), report.inspect_output)
            self.assertEqual((report.trace_dir / "fault-kill.log").read_text(), report.fault_kill_output)
            for name in (
                "run.stdout",
                "run.stderr",
                "inspect.log",
                "fault-kill.log",
                "reboot.json",
                "reboot.stdout",
                "reboot.stderr",
                "fence-recovery.json",
                "ready.json",
            ):
                self.assertEqual(stat.S_IMODE((report.trace_dir / name).stat().st_mode), 0o600)
            run_command = fake.processes[0].args[-1]
            self.assertTrue(run_command.startswith("exec mister-fpga-dev run "))
            self.assertNotIn("&", run_command)

    def test_fault_inject_rejects_nonclosed_disconnect_and_nonempty_child_output(self):
        for returncode, child_output in ((1, b""), (255, b"unexpected\n")):
            with self.subTest(returncode=returncode, child_output=child_output), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport(run_returncode=returncode)
                fake.child_stdout = child_output
                transport = fogcast_dev.Transport(config(root, fake))
                with self.assertRaises(fogcast_dev.TransportError):
                    transport.fault_inject(bundle)
                trace = root / "traces" / RUN_ID
                self.assertEqual((trace / "inspect.log").read_text(), (
                    "FOGCAST_FPGA_DEV_INSPECT run_id="
                    f"{RUN_ID} session={SESSION} generation=42 phase=load_attempted "
                    f"pid=123 start_time=456 executable_sha256={TOOL_SHA256}\n"
                ))
                self.assertEqual(
                    (trace / "fault-kill.log").read_text(),
                    f"FOGCAST_FPGA_DEV_FAULT_KILLED run_id={RUN_ID}\n",
                )

    def test_fault_rejects_every_signal_disconnect_code(self):
        for returncode in (
            -signal.SIGHUP,
            -signal.SIGINT,
            -signal.SIGKILL,
            -signal.SIGTERM,
        ):
            with self.subTest(returncode=returncode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport(run_returncode=returncode)
                with self.assertRaises(fogcast_dev.TransportError):
                    fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
                self.assertNotIn("reboot", fake.remote_order)

    def test_fault_rejects_any_nonempty_child_stdout_or_stderr(self):
        cases = (
            ("printable stdout", b"diagnostic\n", b""),
            (
                "FOGCAST stdout",
                b"FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n",
                b"",
            ),
            ("printable stderr", b"", b"diagnostic\n"),
            (
                "FOGCAST stderr",
                b"",
                b"FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n",
            ),
        )
        for label, child_stdout, child_stderr in cases:
            with self.subTest(label=label), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport()
                fake.child_stdout = child_stdout
                fake.child_stderr = child_stderr
                with self.assertRaises(fogcast_dev.TransportError):
                    fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
                self.assertNotIn("reboot", fake.remote_order)

    def test_fault_accepts_only_exact_255_with_empty_child_streams(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport(run_returncode=255)
            report = fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            self.assertEqual(report.run_returncode, 255)
            self.assertIn("reboot", fake.remote_order)
            self.assertIn("ready", fake.remote_order)

    def test_fault_mismatch_terminates_and_reaps_owned_child_but_preserves_stage(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            wrong = (
                "FOGCAST_FPGA_DEV_INSPECT run_id="
                f"{RUN_ID} session={SESSION} generation=42 phase=hello_observed "
                f"pid=123 start_time=456 executable_sha256={TOOL_SHA256}\n"
            )
            fake.clock = FakeClock()
            fake.inspect_outputs = [wrong] * 1000
            with self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.Transport(config(root, fake)).fault_inject(bundle)
            self.assertEqual(len(fake.processes), 1)
            self.assertEqual(fake.processes[0].terminate_calls, 1)
            self.assertGreaterEqual(fake.processes[0].wait_calls, 1)
            self.assertNotIn("cleanup", fake.remote_order)

    def test_inspect_and_fault_kill_parsers_are_exact(self):
        line = (
            "FOGCAST_FPGA_DEV_INSPECT run_id="
            f"{RUN_ID} session={SESSION} generation=42 phase=load_attempted "
            f"pid=123 start_time=456 executable_sha256={TOOL_SHA256}\n"
        )
        parsed = fogcast_dev.parse_inspection(line, RUN_ID, TOOL_SHA256)
        self.assertEqual(parsed.session, SESSION)
        for bad in (line.rstrip("\n"), line + "extra\n", line.replace("load_attempted", "hello_observed")):
            with self.subTest(bad=bad), self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.parse_inspection(bad, RUN_ID, TOOL_SHA256)
        fogcast_dev.parse_fault_kill(
            f"FOGCAST_FPGA_DEV_FAULT_KILLED run_id={RUN_ID}\n", RUN_ID
        )
        with self.assertRaises(fogcast_dev.TransportError):
            fogcast_dev.parse_fault_kill(
                f"FOGCAST_FPGA_DEV_FAULT_KILLED run_id={RUN_ID}", RUN_ID
            )


class FogCastRecoveryTests(unittest.TestCase):
    def test_live_readiness_script_uses_deployed_protected_core_name_path(self):
        ready = fogcast_dev.parse_ready_record(valid_ready())
        script = fogcast_dev._live_readiness_script(ready)
        self.assertIn("core=/tmp/CORENAME\n", script)
        self.assertNotIn("/media/fat/CORENAME", script)
        self.assertEqual(
            script.count("busybox od -An -tx1 \"$core\" | busybox tr -d ' \\n'"),
            2,
        )
        self.assertIn("busybox sleep 0.01", script)

    def test_live_readiness_binds_every_record_tuple_and_current_runtime_gate(self):
        ready = fogcast_dev.parse_ready_record(valid_ready())
        self.assertEqual(
            (
                ready.supervisor_pid,
                ready.supervisor_start_time,
                ready.main_pid,
                ready.main_start_time,
                ready.agent_pid,
                ready.agent_start_time,
            ),
            (100, 1000, 101, 1001, 102, 1002),
        )
        fogcast_dev.parse_live_readiness(valid_live(), ready)

        other_boot = "00000000-0000-0000-0000-000000000002"
        mutations = {
            "stale supervisor tuple": valid_live().replace(
                "PROC|supervisor|100|1000", "PROC|supervisor|100|999"
            ),
            "boot": valid_live().replace(
                (BOOT_ID + "\n").encode().hex(), (other_boot + "\n").encode().hex()
            ),
            "journal": valid_live().replace(JOURNAL_SHA256, "e" * 64),
            "profile": valid_live().replace(PROFILE_SHA256, "e" * 64),
            "owner": valid_live(owner=valid_owner(session="1" * 32)),
            "fifo": valid_live().replace("FIFO|fifo|0|0|600|1", "FIFO|regular file|0|0|600|1"),
            "fpga": valid_live().replace("FPGA|6f7065726174696e670a", "FPGA|6572726f720a"),
            "menu between reads": valid_live().replace("MENU_SECOND|4d454e550a", "MENU_SECOND|47414d450a"),
        }
        for label, output in mutations.items():
            with self.subTest(label=label), self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.parse_live_readiness(output, ready)

    def test_load_runs_post_reconnect_preflight_and_independent_live_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            report = fogcast_dev.Transport(config(root, fake)).load(bundle)
            self.assertTrue(report.ok)
            for operation in ("reconnect", "result-delete", "preflight", "ready", "live"):
                self.assertIn(operation, fake.remote_order)
            self.assertEqual(fake.remote_order.count("preflight"), 2)
            first_preflight = fake.remote_order.index("preflight")
            final_preflight = len(fake.remote_order) - 1 - fake.remote_order[::-1].index("preflight")
            self.assertLess(fake.remote_order.index("reconnect"), first_preflight)
            self.assertLess(first_preflight, fake.remote_order.index("ready"))
            self.assertLess(fake.remote_order.index("ready"), fake.remote_order.index("live"))
            self.assertLess(fake.remote_order.index("live"), fake.remote_order.index("result-probe"))
            self.assertLess(fake.remote_order.index("result-delete"), final_preflight)

    def test_reconnect_deadline_is_absolute_from_the_disconnect_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.clock = FakeClock()
            fake.resolve_elapsed = 119.0
            fake.elapsed.update(reconnect=2.0, ready=2.0)
            with self.assertRaisesRegex(fogcast_dev.TransportError, "reconnect.*120"):
                fogcast_dev.Transport(config(root, fake)).load(bundle)
            self.assertNotIn("result-delete", fake.remote_order)

    def test_readiness_uses_one_cumulative_thirty_second_deadline_after_reconnect(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.clock = FakeClock()
            fake.elapsed.update(preflight=20.0, ready=11.0)
            with self.assertRaisesRegex(fogcast_dev.TransportError, "readiness.*30"):
                fogcast_dev.Transport(config(root, fake)).load(bundle)
            self.assertNotIn("live", fake.remote_order)

    def test_missing_result_still_runs_exact_readiness_within_separate_bound(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.result = b""

            def missing_result_scp(argv, **kwargs):
                if len(argv) < 2 or "result-" not in argv[-2]:
                    return fake.scp(argv, **kwargs)
                fake.commands.append(("scp", list(argv), None))
                return SimpleNamespace(returncode=1, stdout="", stderr="missing")

            cfg = config(root, fake)
            cfg = fogcast_dev.DevConfig(**{**cfg.__dict__, "scp_command": missing_result_scp})
            transport = fogcast_dev.Transport(cfg)
            with self.assertRaisesRegex(fogcast_dev.TransportError, "result_unavailable"):
                transport.load(bundle)
            for operation in ("preflight", "ready", "live", "result-probe"):
                self.assertIn(operation, fake.remote_order)
            self.assertLess(fake.remote_order.index("preflight"), fake.remote_order.index("ready"))
            self.assertLess(fake.remote_order.index("ready"), fake.remote_order.index("live"))
            self.assertLess(fake.remote_order.index("live"), fake.remote_order.index("result-probe"))
            self.assertLessEqual(transport.last_readiness_budget, fogcast_dev.READINESS_TIMEOUT)

    def test_slow_result_probe_cannot_consume_readiness_before_it_executes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            fake.clock = FakeClock()
            fake.elapsed["result-probe"] = fogcast_dev.READINESS_TIMEOUT + 1.0
            with self.assertRaisesRegex(fogcast_dev.TransportError, "30-second bound"):
                fogcast_dev.Transport(config(root, fake)).load(bundle)
            for operation in ("preflight", "ready", "live", "result-probe"):
                self.assertIn(operation, fake.remote_order)
            self.assertLess(fake.remote_order.index("preflight"), fake.remote_order.index("ready"))
            self.assertLess(fake.remote_order.index("ready"), fake.remote_order.index("live"))
            self.assertLess(fake.remote_order.index("live"), fake.remote_order.index("result-probe"))
            self.assertNotIn("result-delete", fake.remote_order)

    def test_invalid_dry_run_value_is_rejected(self):
        with self.assertRaises(fogcast_dev.TransportError):
            fogcast_dev.parse_dry_run("yes")

    def test_dry_run_parser_accepts_only_exact_closed_values(self):
        for value, expected in (("0", False), ("false", False), ("1", True), ("true", True)):
            with self.subTest(value=value):
                self.assertIs(fogcast_dev.parse_dry_run(value), expected)
        for value in ("", "TRUE", "False", "yes", "on", " 1"):
            with self.subTest(value=value), self.assertRaises(fogcast_dev.TransportError):
                fogcast_dev.parse_dry_run(value)

    def test_dry_run_plans_every_action_without_network_or_trace_mutation(self):
        for action in ("load", "preflight", "fault_inject"):
            with self.subTest(action=action), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                bundle = write_bundle(root)
                fake = FakeTransport()
                transport = fogcast_dev.Transport(config(root, fake, dry_run=True))
                report = getattr(transport, action)(bundle)
                self.assertTrue(report.ok)
                self.assertTrue(report.argv)
                self.assertEqual(fake.commands, [])
                self.assertEqual(fake.processes, [])
                self.assertEqual(fake.resolutions, 0)
                self.assertFalse((root / "traces").exists())

    def test_dry_run_orders_static_recovery_before_dynamic_live_validation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = write_bundle(root)
            fake = FakeTransport()
            report = fogcast_dev.Transport(config(root, fake, dry_run=True)).load(bundle)
            commands = [command[-1] for command in report.argv]

            def positions(marker: str) -> list[int]:
                return [index for index, command in enumerate(commands) if marker in command]

            preflights = positions("mister-fpga-dev preflight")
            self.assertEqual(len(preflights), 2)
            self.assertLess(positions("mister-fpga-dev run")[0], positions("FOGCAST_MISTEROSS_RECONNECT_V1")[0])
            self.assertLess(positions("FOGCAST_MISTEROSS_RECONNECT_V1")[0], preflights[0])
            self.assertLess(preflights[0], positions("FOGCAST_MISTEROSS_READINESS_V3")[0])
            self.assertLess(positions("FOGCAST_MISTEROSS_READINESS_V3")[0], positions("FOGCAST_MISTEROSS_RESULT_V1")[0])
            self.assertLess(positions("FOGCAST_MISTEROSS_RESULT_DELETE_V1")[0], preflights[1])

    def test_blank_optional_client_environment_uses_pinned_openssh_defaults(self):
        with tempfile.TemporaryDirectory() as directory:
            values = {
                "FOGCAST_DEV_HOST": "fixture.example",
                "FOGCAST_DEV_USER": "root",
                "FOGCAST_DEV_EXPECTED_BOARD": "misterpi",
                "FOGCAST_DEV_EXPECTED_MAIN_SHA256": MAIN_SHA256,
                "FOGCAST_DEV_TOOL_SHA256": TOOL_SHA256,
                "FOGCAST_DEV_SSH": "",
                "FOGCAST_DEV_SCP": "",
                "FOGCAST_DEV_DRY_RUN": "1",
            }
            with mock.patch.dict(os.environ, values, clear=True):
                cfg = fogcast_dev.config_from_environment(Path(directory))
            self.assertEqual(Path(cfg.ssh).name, "ssh")
            self.assertEqual(Path(cfg.scp).name, "scp")

    def test_cli_run_id_is_rebound_to_the_selected_bundle_before_dry_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_bundle(root, run_id=RUN_ID)
            values = {
                "FOGCAST_DEV_HOST": "fixture.example",
                "FOGCAST_DEV_USER": "root",
                "FOGCAST_DEV_EXPECTED_BOARD": "misterpi",
                "FOGCAST_DEV_EXPECTED_MAIN_SHA256": MAIN_SHA256,
                "FOGCAST_DEV_TOOL_SHA256": TOOL_SHA256,
                "FOGCAST_DEV_DRY_RUN": "1",
            }
            stdout, stderr = io.StringIO(), io.StringIO()
            with (
                mock.patch.object(fogcast_dev, "REPO_ROOT", root),
                mock.patch.dict(os.environ, values, clear=True),
                mock.patch("sys.stdout", stdout),
                mock.patch("sys.stderr", stderr),
            ):
                status = fogcast_dev.main(
                    [
                        "load",
                        "--experiment",
                        "020_linux_mailbox",
                        "--build",
                        "oss",
                        "--run-id",
                        "1" * 32,
                    ]
                )
            self.assertEqual(status, 2)
            self.assertIn("run_id", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
