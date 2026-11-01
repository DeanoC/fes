import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from scripts import fes_build_common as board
from scripts import compiler_read_audit as audit
from scripts.functional_execution import AuditedRoots, source_roots_for_producer


ROOT = Path(__file__).resolve().parents[1]


def narrowed_source_events(route, directory):
    """Run an audited route inside the menu package's narrowed source closure.

    Return the route result and the source events the audit recorded. fes#881:
    a lease dispatcher that imported the producer modules listed the source root
    and failed with "source list outside audited closure: .".
    """
    record = Path(directory) / "source-events.jsonl"
    with patch.dict(os.environ, {"FES_SOURCE_READ_RECORD": str(record)}):
        for name in ("FES_SOURCE_CLOSURE_AUDIT", "FES_SOURCE_CLOSURE_BROAD",
                     "FES_SOURCE_CLOSURE_RECORD_ONLY"):
            os.environ.pop(name, None)
        roots = source_roots_for_producer("build_fes_menu_package", (), ROOT)
        assert isinstance(roots, AuditedRoots)
        with audit.python_source_guard(ROOT, roots):
            result = route()
    events = [json.loads(line) for line in record.read_text(encoding="utf-8").splitlines()]
    return result, {(event["event"], event["path"]) for event in events}


class PerInvocationGpuLeaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.directory = Path(self.temporary.name)
        self.wrapper = self.directory / "fake-gpu-run"
        self.wrapper.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, sys, time\n"
            "args = sys.argv[1:]\n"
            "command = args[args.index('--') + 1:]\n"
            "time.sleep(float(os.environ.get('FAKE_LEASE_DELAY', '0')))\n"
            "with open(os.environ['LEASE_CAPTURE'], 'w', encoding='utf-8') as stream:\n"
            "    json.dump({'gpu_ids': os.environ.get('GPU_LEASE_IDS', ''), 'argv': args}, stream)\n"
            "env = os.environ.copy()\n"
            "env.update({'FES_GPU_IDS': '7', 'GPU_RUN_DEVICE': '0',\n"
            "            'GPU_RUN_EXPECTED_DEVICE': '/dev/nvidia7',\n"
            "            'CUDA_VISIBLE_DEVICES': '7', 'NEXTPNR_REQUIRE_GPU': '1'})\n"
            "os.execvpe(command[0], command, env)\n",
            encoding="utf-8",
        )
        self.wrapper.chmod(self.wrapper.stat().st_mode | stat.S_IXUSR)
        self.nextpnr = self.directory / "nextpnr-mistral"
        self.nextpnr.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, sys, time\n"
            "if os.environ.get('FAKE_BACKEND_READY'):\n"
            "    print('Info: backend cuda:fixture ready', flush=True)\n"
            "if os.environ.get('FAKE_CPU_FALLBACK'):\n"
            "    print('Warning: falling back to the CPU reference backend', flush=True)\n"
            "if os.environ.get('FAKE_CHILD_PID_FILE'):\n"
            "    import subprocess\n"
            "    child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(30)'])\n"
            "    with open(os.environ['FAKE_CHILD_PID_FILE'], 'w', encoding='utf-8') as stream:\n"
            "        stream.write(str(child.pid))\n"
            "with open(os.environ['CAPTURE'], 'w', encoding='utf-8') as stream:\n"
            "    json.dump({'argv': sys.argv[1:], 'env': dict(os.environ)}, stream)\n"
            "time.sleep(float(os.environ.get('FAKE_ROUTE_DELAY', '0')))\n",
            encoding="utf-8",
        )
        self.nextpnr.chmod(self.nextpnr.stat().st_mode | stat.S_IXUSR)
        self.capture = self.directory / "nextpnr.json"
        self.real_lease = self.directory / "real-leases"

    def tearDown(self):
        self.temporary.cleanup()

    def environment(self, **extra):
        return {
            "PATH": os.environ["PATH"],
            "CAPTURE": str(self.capture),
            "LEASE_CAPTURE": str(self.directory / "lease.txt"),
            "GPU_LEASE_VENDOR": "nvidia",
            "GPU_LEASE_MAX_PER_OWNER": "2",
            "FES_GPU_LEASE_OWNER": "lander",
            **extra,
        }

    def run_gpu_route(self, *, timeout=None, cwd=ROOT, **environment):
        with patch.object(board, "_gpu_run_path", return_value=self.wrapper):
            return board._run_command(
                (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                cwd=cwd,
                env=self.environment(**environment),
                timeout=timeout,
                stdout=board.subprocess.PIPE,
                stderr=board.subprocess.STDOUT,
                gpu_label="unit-test-route",
            )

    def test_gpu_device_and_visibility_come_from_the_granted_lease(self):
        result = self.run_gpu_route()
        self.assertEqual(result.returncode, 0, result.stdout)
        lease = json.loads((self.directory / "lease.txt").read_text(encoding="utf-8"))
        # --gpu-device does not narrow the lease. This run sets no box inventory.
        self.assertEqual(lease["gpu_ids"], "")
        # This nextpnr has no GPU_LEASE_FD client, so it keeps the whole-route lease.
        self.assertNotIn("--phase-lease", lease["argv"])
        self.assertIn("--gpus", lease["argv"])
        self.assertEqual(lease["argv"][lease["argv"].index("--gpus") + 1], "1")
        self.assertEqual(lease["argv"][lease["argv"].index("--owner") + 1], "lander")
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        device = captured["argv"].index("--gpu-device")
        self.assertEqual(captured["argv"][device + 1], "0")
        self.assertIn(b"FES-GPU-GRANT 7", result.stdout)
        self.assertEqual(captured["env"]["FES_GPU_IDS"], "7")
        self.assertEqual(captured["env"]["CUDA_VISIBLE_DEVICES"], "7")
        self.assertEqual(captured["env"]["NEXTPNR_REQUIRE_GPU"], "1")
        self.assertNotIn("GPU_LEASE_MAX_PER_OWNER", captured["env"])
        self.assertNotIn("FES_GPU_LEASE_OWNER", captured["env"])

    def test_dispatch_does_not_depend_on_working_directory(self):
        result = self.run_gpu_route(cwd=self.directory)
        self.assertEqual(result.returncode, 0, result.stdout)
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertEqual(captured["env"]["CUDA_VISIBLE_DEVICES"], "7")

    @unittest.skipUnless(audit.TRACER.is_file(), "strace required for audited routes")
    def test_audited_route_reads_only_the_dispatcher_from_a_narrowed_closure(self):
        result, events = narrowed_source_events(lambda: board._run_command(
            (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
            cwd=ROOT,
            env=self.environment(GPU_LEASE_DIR=str(self.real_lease), GPU_LEASE_IDS="7",
                                 FAKE_BACKEND_READY="1"),
            timeout=10,
            audit_source_root=ROOT,
            stdout=board.subprocess.PIPE,
            stderr=board.subprocess.STDOUT,
            gpu_label="narrowed-closure-route",
        ), self.directory)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(events, {("read", "scripts/nextpnr_lease.py")})
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertEqual(captured["argv"][captured["argv"].index("--gpu-device") + 1], "0")

    @unittest.skipUnless(audit.TRACER.is_file(), "strace required for audited timeout cancellation")
    def test_audited_timeout_covers_lease_wait_and_cancels_route_children(self):
        child_pid_file = self.directory / "route-child.pid"
        with patch.object(board, "_gpu_run_path", return_value=self.wrapper):
            with self.assertRaises(subprocess.TimeoutExpired):
                board._run_command(
                    (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                    cwd=ROOT,
                    env=self.environment(FAKE_LEASE_DELAY="0.5"),
                    timeout=0.2,
                    audit_source_root=ROOT,
                    stdout=board.subprocess.PIPE,
                    stderr=board.subprocess.STDOUT,
                )
            self.assertFalse(self.capture.exists(), "nextpnr started after the total deadline")

            with self.assertRaises(subprocess.TimeoutExpired):
                board._run_command(
                    (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                    cwd=ROOT,
                    env=self.environment(
                        FAKE_LEASE_DELAY="0.15",
                        FAKE_ROUTE_DELAY="30.0",
                        FAKE_CHILD_PID_FILE=str(child_pid_file),
                    ),
                    timeout=0.5,
                    audit_source_root=ROOT,
                    stdout=board.subprocess.PIPE,
                    stderr=board.subprocess.STDOUT,
                )
        self.assertTrue(child_pid_file.is_file(), "route did not start before the total deadline")
        child_pid = int(child_pid_file.read_text(encoding="utf-8"))
        with self.assertRaises(ProcessLookupError):
            os.kill(child_pid, 0)

    def test_real_gpu_run_wrapper_handles_short_routes_and_rejects_fallback(self):
        def run_real_route(**extra):
            return board._run_command(
                (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                cwd=ROOT,
                env=self.environment(
                    GPU_LEASE_DIR=str(self.real_lease),
                    GPU_LEASE_IDS="7",
                    FAKE_BACKEND_READY="1",
                    **extra,
                ),
                timeout=3.0,
                stdout=board.subprocess.PIPE,
                stderr=board.subprocess.STDOUT,
                gpu_label="real-wrapper-test",
            )

        passed = run_real_route()
        self.assertEqual(passed.returncode, 0, passed.stdout)
        self.assertIn(b"backend cuda:fixture ready", passed.stdout)
        lease = json.loads((self.real_lease / "gpu-7.lock").read_text(encoding="utf-8"))
        self.assertEqual(lease["owner"], "lander")
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        device = captured["argv"].index("--gpu-device")
        self.assertEqual(captured["argv"][device + 1], "0")

        fallback = run_real_route(FAKE_CPU_FALLBACK="1")
        self.assertEqual(fallback.returncode, 70, fallback.stdout)
        self.assertIn(b"CPU reference backend", fallback.stdout)

    def test_route_lease_uses_a_free_gpu_instead_of_the_argv_device(self):
        """A --gpu-device argument must not pin the lease while another GPU is free."""
        gpu_run = board._gpu_run_path()
        gpu_status = gpu_run.with_name("gpu-status")
        lease_env = self.environment(
            GPU_LEASE_DIR=str(self.real_lease),
            GPU_LEASE_IDS="7,8",
        )
        release_holder = self.directory / "release-holder"
        holder_code = (
            "import pathlib,sys,time; release=pathlib.Path(sys.argv[1]); "
            "\nwhile not release.exists(): time.sleep(0.01)"
        )
        holder = subprocess.Popen(
            [str(gpu_run), "--no-wait", "--owner", "blocker", "--label", "gpu-7-holder",
             "--", sys.executable, "-c", holder_code, str(release_holder)],
            env=lease_env,
        )
        route_result = {}

        def status():
            result = subprocess.run(
                [str(gpu_status), "--json"], env=lease_env, check=True,
                capture_output=True, text=True,
            )
            return json.loads(result.stdout)

        def wait_for(predicate, message):
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                current = status()
                if predicate(current):
                    return current
                time.sleep(0.02)
            self.fail(message)

        def run_route():
            try:
                route_result["result"] = board._run_command(
                    (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                    cwd=ROOT,
                    env=self.environment(
                        GPU_LEASE_DIR=str(self.real_lease),
                        GPU_LEASE_IDS="7,8",
                        FAKE_BACKEND_READY="1",
                    ),
                    timeout=8,
                    stdout=board.subprocess.PIPE,
                    stderr=board.subprocess.STDOUT,
                    gpu_label="per-invocation-free-gpu",
                )
            except BaseException as error:
                route_result["error"] = error

        route_thread = None
        try:
            # The lock is taken before its owner metadata is written, so
            # wait for both (#1271).
            held = wait_for(
                lambda payload: payload["gpus"][0]["state"] == "held"
                and payload["gpus"][0]["owner"] == "blocker",
                "the GPU 7 blocker did not acquire its lease",
            )
            self.assertEqual(held["gpus"][0]["id"], "7")
            self.assertEqual(held["gpus"][0]["owner"], "blocker")
            self.assertEqual(held["gpus"][1]["state"], "free")

            route_thread = threading.Thread(target=run_route)
            route_thread.start()
            route_thread.join(timeout=9)
            self.assertFalse(route_thread.is_alive(), "the route waited on the argv GPU")
            self.assertIsNone(holder.poll(), "the GPU 7 holder exited before the route finished")
            still = status()
            self.assertEqual(still["gpus"][0]["state"], "held")
            self.assertEqual(still["gpus"][0]["owner"], "blocker")
            self.assertFalse(any(
                item["label"] == "per-invocation-free-gpu" for item in still["queue"]
            ))
            self.assertNotIn("error", route_result)
            result = route_result["result"]
            self.assertEqual(result.returncode, 0, result.stdout)
            self.assertIn(b"backend cuda:fixture ready", result.stdout)
            captured = json.loads(self.capture.read_text(encoding="utf-8"))
            self.assertEqual(captured["env"]["FES_GPU_IDS"], "8")
            self.assertEqual(captured["env"]["CUDA_VISIBLE_DEVICES"], "8")
            self.assertIn(b"FES-GPU-GRANT 8", result.stdout)
            self.assertNotIn(b"FES-GPU-GRANT 7", result.stdout)
            self.assertEqual(captured["argv"][captured["argv"].index("--gpu-device") + 1], "0")
        finally:
            release_holder.write_text("release", encoding="utf-8")
            if holder.poll() is None:
                holder.wait(timeout=5)
            if route_thread is not None:
                route_thread.join(timeout=9)

    def test_non_gpu_nextpnr_does_not_acquire_a_lease(self):
        with patch.object(board, "_gpu_run_path", side_effect=AssertionError("unexpected lease")):
            result = board._run_command(
                (str(self.nextpnr), "--router", "router1"),
                cwd=ROOT,
                env=self.environment(),
                stdout=board.subprocess.PIPE,
                stderr=board.subprocess.STDOUT,
            )
        self.assertEqual(result.returncode, 0, result.stdout)
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertNotIn("GPU_RUN_DEVICE", captured["env"])
        self.assertNotIn("--gpu-device", captured["argv"])

    def test_gpu_device_argument_does_not_narrow_inventory(self):
        result = self.run_gpu_route(GPU_LEASE_IDS="8")
        self.assertEqual(result.returncode, 0, result.stdout)
        lease = json.loads((self.directory / "lease.txt").read_text(encoding="utf-8"))
        self.assertEqual(lease["gpu_ids"], "8")
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertEqual(captured["argv"][captured["argv"].index("--gpu-device") + 1], "0")

    def test_producer_rejects_an_outer_whole_script_lease(self):
        with patch.dict(os.environ, {"GPU_RUN_DEVICE": "0"}):
            with self.assertRaisesRegex(board.BuildError, "invoke the producer outside gpu-run"):
                self.run_gpu_route()
        with patch.dict(os.environ, {"GPU_LEASE_FD": "9"}):
            with self.assertRaisesRegex(board.BuildError, "invoke the producer outside gpu-run"):
                self.run_gpu_route()


# A nextpnr stand-in with the DeanoC/nextpnr#224 client: it routes on the CPU
# first, asks for one GPU phase on GPU_LEASE_FD, then releases it.
PHASE_NEXTPNR = """#!/usr/bin/env python3
import json, os, signal, socket, sys, time
time.sleep(float(os.environ.get('FAKE_CPU_DELAY', '0')))
lease = socket.socket(fileno=int(os.environ['GPU_LEASE_FD']))

def ask(line):
    lease.sendall(line.encode() + b'\\n')
    reply = b''
    while not reply.endswith(b'\\n'):
        chunk = lease.recv(1)
        if not chunk:
            return 'EOF'
        reply += chunk
    return reply.decode().strip()

grant = ask('ACQUIRE 1')
print('GPU lease: ' + grant, flush=True)
if not grant.startswith('GRANT '):
    sys.exit(3)
print('Info: backend cuda:fixture ready', flush=True)
if os.environ.get('FAKE_CRASH_IN_PHASE'):
    os.kill(os.getpid(), signal.SIGKILL)
if os.environ.get('FAKE_CHILD_PID_FILE'):
    import subprocess
    child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(30)'])
    with open(os.environ['FAKE_CHILD_PID_FILE'], 'w', encoding='utf-8') as stream:
        stream.write(str(child.pid))
time.sleep(float(os.environ.get('FAKE_ROUTE_DELAY', '0')))
release = ask('RELEASE')
with open(os.environ['CAPTURE'], 'w', encoding='utf-8') as stream:
    json.dump({'argv': sys.argv[1:], 'env': dict(os.environ), 'grant': grant,
               'release': release}, stream)
print('Info: Program finished normally.', flush=True)
"""


class PhaseLeaseRouteTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        tool_dir = self.directory / "phase-tool"
        tool_dir.mkdir()
        self.nextpnr = tool_dir / "nextpnr-mistral"
        self.nextpnr.write_text(PHASE_NEXTPNR, encoding="utf-8")
        self.nextpnr.chmod(0o755)
        self.capture = self.directory / "nextpnr.json"
        self.lease = self.directory / "leases"
        self.gpu_run = board._gpu_run_path()

    def environment(self, **extra):
        return {
            "PATH": os.environ["PATH"],
            "CAPTURE": str(self.capture),
            "GPU_LEASE_DIR": str(self.lease),
            "GPU_LEASE_IDS": "7",
            "GPU_LEASE_VENDOR": "nvidia",
            "GPU_LEASE_MAX_PER_OWNER": "2",
            "FES_GPU_LEASE_OWNER": "lander",
            **extra,
        }

    def route(self, timeout=None, audit=False, **extra):
        return board._run_command(
            (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
            cwd=ROOT,
            env=self.environment(**extra),
            timeout=timeout,
            audit_source_root=ROOT if audit else None,
            stdout=board.subprocess.PIPE,
            stderr=board.subprocess.STDOUT,
            gpu_label="phase-route",
        )

    def status(self):
        result = subprocess.run(
            [str(self.gpu_run.with_name("gpu-status")), "--json"], env=self.environment(),
            check=True, capture_output=True, text=True,
        )
        return json.loads(result.stdout)

    def hold_gpu(self):
        """Hold GPU 7 with a whole-command lease until the returned callback runs."""
        release = self.directory / "release-holder"
        holder = subprocess.Popen(
            [str(self.gpu_run), "--no-wait", "--owner", "blocker", "--", sys.executable, "-c",
             "import pathlib,sys,time\nwhile not pathlib.Path(sys.argv[1]).exists(): time.sleep(0.01)",
             str(release)],
            env=self.environment(),
        )

        def stop():
            release.write_text("release", encoding="utf-8")
            holder.wait(timeout=10)
        self.addCleanup(stop)
        deadline = time.monotonic() + 5
        while self.status()["gpus"][0]["state"] != "held":
            self.assertLess(time.monotonic(), deadline, "the GPU 7 blocker did not acquire")
            time.sleep(0.02)
        return stop

    def route_in_thread(self, **options):
        outcome = {}

        def run():
            started = time.monotonic()
            try:
                outcome["result"] = self.route(**options)
            except BaseException as error:  # reported by the test thread
                outcome["error"] = error
            outcome["seconds"] = time.monotonic() - started
        thread = threading.Thread(target=run)
        thread.start()
        self.addCleanup(thread.join, 20)
        return thread, outcome

    def wait_for_phase_request(self):
        deadline = time.monotonic() + 5
        while not any(item["label"] == "phase-route" for item in self.status()["queue"]):
            self.assertLess(time.monotonic(), deadline, "the route never asked for its GPU phase")
            time.sleep(0.02)

    def test_gate_detects_the_gpu_lease_fd_client(self):
        self.assertTrue(board._nextpnr_supports_phase_lease(self.nextpnr))
        plain = self.directory / "nextpnr-plain"
        plain.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        self.assertFalse(board._nextpnr_supports_phase_lease(plain))
        # Naming the variable is not enough; the client also sends ACQUIRE.
        mention = self.directory / "nextpnr-mention"
        mention.write_text("#!/bin/sh\necho GPU_LEASE_FD is ignored\n", encoding="utf-8")
        self.assertFalse(board._nextpnr_supports_phase_lease(mention))
        empty = self.directory / "nextpnr-empty"
        empty.write_bytes(b"")
        self.assertFalse(board._nextpnr_supports_phase_lease(empty))
        self.assertFalse(board._nextpnr_supports_phase_lease(self.directory / "missing"))

    def test_phase_route_leases_its_requested_physical_gpu_only_for_the_phase(self):
        result = self.route(timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn(b"GPU lease: GRANT 7", result.stdout)
        self.assertIn(b"FES-GPU-GRANT 7", result.stdout)
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertEqual((captured["grant"], captured["release"]), ("GRANT 7", "OK"))
        # No mask: nextpnr keeps the physical id that gpu-run granted.
        self.assertEqual(captured["argv"][captured["argv"].index("--gpu-device") + 1], "7")
        env = captured["env"]
        self.assertTrue(env["GPU_LEASE_FD"].isdigit())
        self.assertEqual(env["NEXTPNR_REQUIRE_GPU"], "1")
        self.assertEqual(env["CUDA_DEVICE_ORDER"], "PCI_BUS_ID")
        for name in ("CUDA_VISIBLE_DEVICES", "FES_GPU_IDS", "GPU_RUN_DEVICE", "GPU_LEASE_IDS",
                     "GPU_LEASE_MAX_PER_OWNER", "GPU_LEASE_MAX_JOBS_PER_OWNER",
                     "FES_GPU_LEASE_OWNER"):
            self.assertNotIn(name, env)
        holder = json.loads((self.lease / "gpu-7.lock").read_text(encoding="utf-8"))
        self.assertEqual((holder["owner"], holder["label"]), ("lander", "phase-route"))
        self.assertTrue((self.lease / "jobs" / "lander").is_dir())
        self.assertEqual(self.status()["gpus"][0]["state"], "free")

    @unittest.skipUnless(audit.TRACER.is_file(), "strace required for audited routes")
    def test_audited_phase_route_reads_only_the_dispatcher_from_a_narrowed_closure(self):
        result, events = narrowed_source_events(
            lambda: self.route(timeout=10, audit=True), self.directory)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn(b"GPU lease: GRANT 7", result.stdout)
        self.assertEqual(events, {("read", "scripts/nextpnr_lease.py")})
        self.assertEqual(self.status()["gpus"][0]["state"], "free")

    def test_route_budget_excludes_gpu_lease_wait(self):
        release_holder = self.hold_gpu()
        thread, outcome = self.route_in_thread(timeout=1.0, FAKE_CPU_DELAY="0.1",
                                               FAKE_ROUTE_DELAY="0.2")
        self.wait_for_phase_request()
        time.sleep(1.5)
        release_holder()
        thread.join(15)
        self.assertNotIn("error", outcome)
        result = outcome["result"]
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertGreater(outcome["seconds"], 1.5)

    def test_route_budget_counts_cpu_phases_but_not_the_wait(self):
        release_holder = self.hold_gpu()
        thread, outcome = self.route_in_thread(timeout=1.0, FAKE_CPU_DELAY="0.6",
                                               FAKE_ROUTE_DELAY="0.8")
        self.wait_for_phase_request()
        time.sleep(1.5)
        release_holder()
        thread.join(15)
        self.assertNotIn("error", outcome)
        result = outcome["result"]
        self.assertEqual(result.returncode, 124, result.stdout)
        self.assertRegex(result.stdout, rb"timed out after 1 seconds of its own run time \(\d+\.\d s of GPU lease wait excluded\)")
        self.assertFalse(self.capture.exists())
        self.assertEqual(self.status()["gpus"][0]["state"], "free")

    @unittest.skipUnless(audit.TRACER.is_file(), "strace required for audited routes")
    def test_route_budget_expiry_stops_the_route_and_its_children(self):
        child_pid_file = self.directory / "route-child.pid"
        started = time.monotonic()
        result = self.route(timeout=1.0, audit=True, FAKE_ROUTE_DELAY="30",
                            FAKE_CHILD_PID_FILE=str(child_pid_file))
        # The child inherited the captured output; the audited run must not
        # wait for it after the budget expires.
        self.assertLess(time.monotonic() - started, 15)
        self.assertEqual(result.returncode, 124, result.stdout)
        child_pid = int(child_pid_file.read_text(encoding="utf-8"))
        with self.assertRaises(ProcessLookupError):
            os.kill(child_pid, 0)
        self.assertEqual(self.status()["gpus"][0]["state"], "free")

    def test_phase_route_crash_releases_its_gpu(self):
        result = self.route(timeout=10, FAKE_CRASH_IN_PHASE="1")
        self.assertEqual(result.returncode, 128 + 9, result.stdout)
        self.assertEqual(self.status()["gpus"][0]["state"], "free")
        again = subprocess.run(
            [str(self.gpu_run), "--no-wait", "--owner", "other", "--", sys.executable, "-c",
             "import os; print(os.environ['FES_GPU_IDS'])"],
            env=self.environment(), check=False, capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stdout.strip(), "7")

    @unittest.skipUnless(audit.TRACER.is_file(), "strace required for audited routes")
    def test_audited_phase_route_has_no_outer_deadline_on_lease_wait(self):
        release_holder = self.hold_gpu()
        thread, outcome = self.route_in_thread(timeout=1.0, audit=True, FAKE_ROUTE_DELAY="0.1")
        self.wait_for_phase_request()
        time.sleep(1.5)
        release_holder()
        thread.join(15)
        self.assertNotIn("error", outcome)
        self.assertEqual(outcome["result"].returncode, 0, outcome["result"].stdout)
        self.assertGreater(outcome["seconds"], 1.5)


if __name__ == "__main__":
    unittest.main()
