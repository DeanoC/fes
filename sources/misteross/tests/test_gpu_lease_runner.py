import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

from scripts import fes_build_common as board


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
            "with open(os.environ['CAPTURE'], 'w', encoding='utf-8') as stream:\n"
            "    json.dump({'argv': sys.argv[1:], 'env': dict(os.environ)}, stream)\n"
            "time.sleep(float(os.environ.get('FAKE_ROUTE_DELAY', '0')))\n",
            encoding="utf-8",
        )
        self.nextpnr.chmod(self.nextpnr.stat().st_mode | stat.S_IXUSR)
        self.capture = self.directory / "nextpnr.json"

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

    def test_route_timeout_starts_after_the_lease_is_granted(self):
        result = self.run_gpu_route(
            timeout=1.0,
            FAKE_LEASE_DELAY="1.1",
            FAKE_ROUTE_DELAY="2.0",
        )
        self.assertEqual(result.returncode, 124, result.stdout)
        self.assertIn(b"GPU lease already granted", result.stdout)
        self.assertTrue(self.capture.is_file(), "route did not start after the fake lease wait")

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
