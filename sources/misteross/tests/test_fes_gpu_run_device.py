from __future__ import annotations

import unittest
import tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from scripts.fes_build_common import (
    BuildError,
    gpu_device_index,
    gpu_run_environment,
    gpu_visibility_environment,
    nextpnr_gpu_command,
)
from scripts.search_placer_qor import _run_nextpnr


class GpuRunDeviceTests(unittest.TestCase):
    def test_masked_physical_devices_map_to_logical_zero(self):
        for physical in (0, 1):
            for visibility in (
                "HIP_VISIBLE_DEVICES",
                "ROCR_VISIBLE_DEVICES",
                "CUDA_VISIBLE_DEVICES",
            ):
                with self.subTest(physical=physical, visibility=visibility):
                    env = {
                        "GPU_RUN_DEVICE": "0",
                        visibility: str(physical),
                    }
                    self.assertEqual(gpu_device_index(physical, env), 0)
                    command = nextpnr_gpu_command(
                        ("nextpnr-mistral", "--gpu-device", str(physical)), env
                    )
                    self.assertEqual(command[-2:], ("--gpu-device", "0"))
                    self.assertEqual(
                        nextpnr_gpu_command(
                            ("nextpnr-mistral", f"--gpu-device={physical}"), env
                        )[-1],
                        "--gpu-device=0",
                    )
                    self.assertEqual(gpu_run_environment(env), env)
                    self.assertEqual(
                        gpu_visibility_environment(
                            env, physical, variable=visibility
                        ),
                        env,
                    )

    def test_unwrapped_selection_keeps_the_configured_physical_index(self):
        env = {"CUDA_VISIBLE_DEVICES": "1"}
        self.assertEqual(gpu_device_index(1, env), 1)
        self.assertEqual(
            nextpnr_gpu_command(("nextpnr-mistral", "--gpu-device", "1"), env)[-1],
            "1",
        )
        self.assertEqual(gpu_run_environment(env), {})
        self.assertEqual(
            gpu_visibility_environment(
                {"ROCR_VISIBLE_DEVICES": "9"}, 1,
                variable="HIP_VISIBLE_DEVICES",
            ),
            {"ROCR_VISIBLE_DEVICES": "9", "HIP_VISIBLE_DEVICES": "1"},
        )

    def test_invalid_wrapper_device_fails_closed(self):
        with self.assertRaisesRegex(BuildError, "GPU_RUN_DEVICE"):
            gpu_device_index(1, {"GPU_RUN_DEVICE": "one"})

    def test_generic_search_route_uses_the_leased_logical_device(self):
        for physical in (0, 1):
            for visibility in ("ROCR_VISIBLE_DEVICES", "CUDA_VISIBLE_DEVICES"):
                with self.subTest(physical=physical, visibility=visibility), \
                     tempfile.TemporaryDirectory() as directory:
                    env = {"GPU_RUN_DEVICE": "0", visibility: str(physical)}
                    with patch("scripts.search_placer_qor.subprocess.run",
                               return_value=SimpleNamespace(returncode=1)) as run:
                        candidate = _run_nextpnr(
                            Path("/tools/nextpnr-mistral"), Path("/fixture/synth.json"),
                            Path(directory), device="5CSEBA6U23I7", qsf=Path("/fixture/pins.qsf"),
                            sdc=None, freq=None, seed=1, weight=100, critexp=2,
                            extra=("--router", "gpu", "--gpu-device", str(physical)),
                            timeout=1, env=env,
                        )
                    command = run.call_args.args[0]
                    device_index = command.index("--gpu-device")
                    self.assertEqual(command[device_index + 1], "0")
                    candidate_device_index = candidate.argv.index("--gpu-device")
                    self.assertEqual(candidate.argv[candidate_device_index + 1], "0")
                    self.assertEqual(candidate.environment[visibility], str(physical))


if __name__ == "__main__":
    unittest.main()
