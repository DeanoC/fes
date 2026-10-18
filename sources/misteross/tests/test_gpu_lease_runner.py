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


ROOT = Path(__file__).resolve().parents[1]


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
        self.assertEqual(lease["gpu_ids"], "7")
        self.assertIn("--gpus", lease["argv"])
        self.assertEqual(lease["argv"][lease["argv"].index("--gpus") + 1], "1")
        self.assertEqual(lease["argv"][lease["argv"].index("--owner") + 1], "lander")
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        device = captured["argv"].index("--gpu-device")
        self.assertEqual(captured["argv"][device + 1], "0")
        self.assertEqual(captured["env"]["FES_GPU_IDS"], "7")
        self.assertEqual(captured["env"]["CUDA_VISIBLE_DEVICES"], "7")
        self.assertEqual(captured["env"]["NEXTPNR_REQUIRE_GPU"], "1")
        self.assertNotIn("GPU_LEASE_MAX_PER_OWNER", captured["env"])
        self.assertNotIn("FES_GPU_LEASE_OWNER", captured["env"])
        self.assertNotIn("FES_MISTEROSS_PYTHONPATH", captured["env"])

    def test_dispatch_import_does_not_depend_on_working_directory(self):
        result = self.run_gpu_route(cwd=self.directory)
        self.assertEqual(result.returncode, 0, result.stdout)
        captured = json.loads(self.capture.read_text(encoding="utf-8"))
        self.assertEqual(captured["env"]["CUDA_VISIBLE_DEVICES"], "7")

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

    def test_pinned_route_lease_backfills_a_disjoint_gpu(self):
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
                    gpu_label="per-invocation-pinned-route",
                )
            except BaseException as error:
                route_result["error"] = error

        route_thread = None
        try:
            held = wait_for(
                lambda payload: payload["gpus"][0]["state"] == "held",
                "the GPU 7 blocker did not acquire its lease",
            )
            self.assertEqual(held["gpus"][0]["owner"], "blocker")

            route_thread = threading.Thread(target=run_route)
            route_thread.start()
            queued = wait_for(
                lambda payload: any(
                    item["label"] == "per-invocation-pinned-route"
                    for item in payload["queue"]
                ),
                "the pinned nextpnr invocation did not enter the queue",
            )
            route_ticket = next(
                item for item in queued["queue"]
                if item["label"] == "per-invocation-pinned-route"
            )
            self.assertEqual(route_ticket["eligible_ids"], ["7"])

            later = subprocess.run(
                [str(gpu_run), "--wait", "--timeout", "5", "--owner", "backfill",
                 "--label", "disjoint-backfill", "--", sys.executable, "-c",
                 "import os; print(os.environ['FES_GPU_IDS'])"],
                env=lease_env, check=False, capture_output=True, text=True, timeout=7,
            )
            self.assertEqual(later.returncode, 0, later.stderr)
            self.assertEqual(later.stdout.strip(), "8")
            remaining = status()
            self.assertTrue(any(
                item["label"] == "per-invocation-pinned-route"
                for item in remaining["queue"]
            ))

            release_holder.write_text("release", encoding="utf-8")
            holder.wait(timeout=5)
            route_thread.join(timeout=9)
            self.assertFalse(route_thread.is_alive(), "the per-invocation route did not finish")
            self.assertNotIn("error", route_result)
            self.assertEqual(route_result["result"].returncode, 0, route_result["result"].stdout)
            self.assertIn(b"backend cuda:fixture ready", route_result["result"].stdout)
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

    def test_requested_gpu_must_be_in_the_configured_inventory(self):
        with patch.object(board, "_gpu_run_path", side_effect=AssertionError("must not queue")):
            with self.assertRaisesRegex(board.BuildError, "outside GPU_LEASE_IDS"):
                board._run_command(
                    (str(self.nextpnr), "--router", "gpu", "--gpu-device", "7"),
                    cwd=ROOT,
                    env=self.environment(GPU_LEASE_IDS="8"),
                )

    def test_producer_rejects_an_outer_whole_script_lease(self):
        with patch.dict(os.environ, {"GPU_RUN_DEVICE": "0"}):
            with self.assertRaisesRegex(board.BuildError, "invoke the producer outside gpu-run"):
                self.run_gpu_route()


if __name__ == "__main__":
    unittest.main()
