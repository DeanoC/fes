"""Router selection for the producers that previously hard-coded HIP.

Menu, Pong, RAM Tester and RISC-V took only ``--gpu-device``. They requested
the HIP lane unconditionally and wrote the literal ``hip`` into the sealed
build record, so on a CUDA-only host they demanded a hipcc-built nextpnr that
cannot exist there, and a CUDA build would have attested a backend that did
not route. Each now takes ``--gpu-router``, like the router-selectable
producers, and threads the selection through authentication, the record and
the route evidence.

The default stays HIP, so an invocation that names no router keeps every
recorded parameter it had.
"""

from __future__ import annotations

from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_menu as menu
from scripts import build_fes_menu_package as menu_package
from scripts import build_fes_pong as pong
from scripts import build_fes_ramtest as ramtest
from scripts import build_fes_riscv as riscv
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as board_evidence
from tests.producer_fixture import EXECUTION
from tests import test_build_fes_pong

ROOT = Path(__file__).resolve().parents[1]

# Each producer and the gpu_choice it selects a lane with.
PRODUCERS = (
    ("menu", menu, menu.gpu_choice),
    ("menu_package", menu_package, menu.gpu_choice),
    ("pong", pong, pong.gpu_choice),
    ("ramtest", ramtest, ramtest.gpu_choice),
    ("riscv", riscv, riscv.gpu_choice),
)

PRODUCER_SCRIPTS = (
    "build_fes_menu.py",
    "build_fes_menu_package.py",
    "build_fes_pong.py",
    "build_fes_ramtest.py",
    "build_fes_riscv.py",
)


class GpuRouterSelectionTests(unittest.TestCase):
    def test_default_router_is_hip(self):
        """An unnamed router keeps the HIP lane, so no recorded field moves."""
        self.assertEqual(board.FES_GPU_ROUTER, "HIP")
        for name, _module, gpu_choice in PRODUCERS:
            with self.subTest(producer=name):
                self.assertEqual(gpu_choice(board.FES_GPU_ROUTER)[0], "hip")

    def test_hip_selects_the_unchanged_hip_lane(self):
        """HIP must authenticate exactly the lane text main already used."""
        for name, _module, gpu_choice in PRODUCERS:
            with self.subTest(producer=name):
                backend, architectures, configuration = gpu_choice("HIP")
                self.assertEqual(backend, "hip")
                self.assertEqual(architectures, board.FES_GPU_ARCHITECTURES)
                self.assertEqual(configuration, board.FES_TOOLCHAIN_CONFIGURATION)
                self.assertIn("gpu-router=HIP", configuration)

    def test_cuda_selects_the_cuda_lane_and_unused_architectures(self):
        """CUDA must not attest the gfx list: it is a HIP compiler input."""
        for name, _module, gpu_choice in PRODUCERS:
            with self.subTest(producer=name):
                backend, architectures, configuration = gpu_choice("CUDA")
                self.assertEqual(backend, "cuda")
                self.assertEqual(architectures, "unused")
                self.assertIn("gpu-router=CUDA", configuration)
                self.assertIn("cuda-architectures=", configuration)
                self.assertNotIn("hip-architectures=gfx", configuration)

    def test_unsupported_router_is_refused(self):
        """A sloppy selection must raise, never fall back to a silent HIP."""
        for value in ("cuda", "hip", "METAL", "", "CUDA ", "OFF", None, 0):
            for name, _module, gpu_choice in PRODUCERS:
                with self.subTest(producer=name, router=value):
                    with self.assertRaises(board.BuildError):
                        gpu_choice(value)

    def test_backend_names_map_one_to_one(self):
        self.assertEqual(board.gpu_backend_for_router("HIP"), "hip")
        self.assertEqual(board.gpu_backend_for_router("CUDA"), "cuda")

    def test_producers_hold_no_hard_coded_backend(self):
        """The literal that made a CUDA build record HIP must be gone."""
        for script in PRODUCER_SCRIPTS:
            with self.subTest(script=script):
                text = (ROOT / "scripts" / script).read_text()
                self.assertNotIn('"gpu_backend": "hip"', text)
                self.assertNotIn("'gpu_backend':'hip'", text)
                self.assertNotIn('"gpu_backend":"hip"', text)
                self.assertNotIn("'gpu_backend': 'hip'", text)

    def test_pong_parser_accepts_both_routers_and_refuses_others(self):
        """argparse is the authority on what the CLI accepts."""
        for router in ("HIP", "CUDA"):
            with self.subTest(router=router):
                arguments = pong._parser().parse_args(["--gpu-router", router])
                self.assertEqual(arguments.gpu_router, router)
        self.assertEqual(pong._parser().parse_args([]).gpu_router, "HIP")
        with self.assertRaises(SystemExit):
            pong._parser().parse_args(["--gpu-router", "cuda"])

    def test_every_producer_build_takes_a_router_defaulting_to_hip(self):
        """A selection must reach every producer's build, not just the CLI."""
        import inspect

        for name, build in (("menu", menu.build), ("menu_package", menu_package.build),
                            ("pong", pong.build), ("ramtest", ramtest.build),
                            ("riscv", riscv.build)):
            with self.subTest(producer=name):
                parameter = inspect.signature(build).parameters["gpu_router"]
                self.assertEqual(parameter.default, board.FES_GPU_ROUTER)

    def test_every_producer_main_exposes_the_option(self):
        for script in PRODUCER_SCRIPTS:
            with self.subTest(script=script):
                text = (ROOT / "scripts" / script).read_text()
                self.assertIn("--gpu-router", text)


class RecordRouterTests(unittest.TestCase):
    """The record must state the backend that was actually selected."""

    def _parameters(self, fields):
        return fields["parameters"]

    def test_ramtest_record_fields_follow_the_router(self):
        hip = ramtest.record_fields(ROOT, "repo", "a" * 40, {}, memory_mhz=100)
        cuda = ramtest.record_fields(ROOT, "repo", "a" * 40, {}, memory_mhz=100,
                                     gpu_router="CUDA")
        self.assertEqual(self._parameters(hip)["gpu_backend"], "hip")
        self.assertEqual(self._parameters(hip)["gpu_architectures"],
                         board.FES_GPU_ARCHITECTURES)
        self.assertEqual(self._parameters(cuda)["gpu_backend"], "cuda")
        self.assertEqual(self._parameters(cuda)["gpu_architectures"], "unused")

    def test_riscv_record_fields_follow_the_router(self):
        hip = riscv.record_fields(ROOT, "repo", "a" * 40, {})
        cuda = riscv.record_fields(ROOT, "repo", "a" * 40, {}, gpu_router="CUDA")
        self.assertEqual(self._parameters(hip)["gpu_backend"], "hip")
        self.assertEqual(self._parameters(hip)["gpu_architectures"],
                         board.FES_GPU_ARCHITECTURES)
        self.assertEqual(self._parameters(cuda)["gpu_backend"], "cuda")
        self.assertEqual(self._parameters(cuda)["gpu_architectures"], "unused")

    def test_ramtest_default_record_fields_are_unchanged_by_the_option(self):
        """Naming HIP explicitly must give the same fields as naming nothing."""
        self.assertEqual(
            ramtest.record_fields(ROOT, "repo", "a" * 40, {}, memory_mhz=100),
            ramtest.record_fields(ROOT, "repo", "a" * 40, {}, memory_mhz=100,
                                  gpu_router="HIP"),
        )


class EvidenceBackendTests(unittest.TestCase):
    """Route evidence must require the backend that was selected."""

    def test_producers_forward_the_selected_backend_to_the_board_validator(self):
        """The selection must reach the shared route-evidence check."""
        with patch.object(pong, "validate_board_evidence", return_value={}) as board_call:
            pong.validate_build_evidence(Path("/out"), ROOT, gpu_backend="cuda")
        self.assertEqual(board_call.call_args.kwargs["gpu_backend"], "cuda")

        with patch.object(pong, "validate_board_evidence", return_value={}) as board_call:
            pong.validate_build_evidence(Path("/out"), ROOT)
        self.assertEqual(board_call.call_args.kwargs["gpu_backend"],
                         board.FES_GPU_BACKEND)

    def test_require_gpu_backend_rejects_the_other_backend(self):
        cuda_log = "Info: backend cuda: device 0 ready\nInfo: Program finished normally."
        hip_log = "Info: backend hip: device 0 ready\nInfo: Program finished normally."
        self.assertEqual(board._require_gpu_backend(cuda_log, "cuda"), "cuda")
        self.assertEqual(board._require_gpu_backend(hip_log, "hip"), "hip")
        with self.assertRaises(board.BuildError):
            board._require_gpu_backend(cuda_log, "hip")
        with self.assertRaises(board.BuildError):
            board._require_gpu_backend(hip_log, "cuda")

    def test_validator_takes_a_backend_and_defaults_to_hip(self):
        import inspect

        signature = inspect.signature(board_evidence.validate_build_evidence)
        self.assertEqual(signature.parameters["gpu_backend"].default,
                         board.FES_GPU_BACKEND)
        for name, validator in (("pong", pong.validate_build_evidence),
                                ("menu", menu.validate_build_evidence)):
            with self.subTest(producer=name):
                parameters = inspect.signature(validator).parameters
                self.assertEqual(parameters["gpu_backend"].default,
                                 board.FES_GPU_BACKEND)


class AuthenticationLaneTests(unittest.TestCase):
    """The lane each producer requests is the defect that broke CUDA hosts.

    A HIP request on an NVIDIA-only host fails with "required host command is
    missing: hipcc", so what matters is the configuration, router and
    architectures each wrapper forwards to shared authentication.
    """

    def _forwarded(self, module, attribute, call, **kwargs):
        with patch.object(module, attribute, return_value={}) as authenticate:
            call(**kwargs)
        return authenticate.call_args.kwargs

    def test_cuda_requests_the_cuda_lane(self):
        expected_configuration = board.cuda_toolchain_configuration()
        cases = (
            ("pong", pong, "_authenticate_default",
             lambda **kw: pong._authenticate_tools(ROOT, **kw)),
            ("ramtest", board, "_authenticate_tools",
             lambda **kw: ramtest._authenticate_tools(ROOT, **kw)),
            ("riscv", board, "_authenticate_tools",
             lambda **kw: riscv._authenticate_tools(ROOT, **kw)),
            ("menu", board, "_authenticate_tools",
             lambda **kw: menu.authenticate(ROOT, None, "ddr", kw["gpu_router"])),
            ("menu_package", board, "_authenticate_tools",
             lambda **kw: menu_package._authenticate_tools(ROOT, **kw)),
        )
        for name, module, attribute, call in cases:
            with self.subTest(producer=name):
                forwarded = self._forwarded(module, attribute, call, gpu_router="CUDA")
                self.assertEqual(forwarded["gpu_router"], "CUDA")
                self.assertEqual(forwarded["hip_architectures"], "unused")
                self.assertEqual(forwarded["expected_configuration"],
                                 {"nextpnr": expected_configuration})

    def test_default_requests_the_hip_lane_main_already_used(self):
        cases = (
            ("pong", pong, "_authenticate_default",
             lambda: pong._authenticate_tools(ROOT)),
            ("ramtest", board, "_authenticate_tools",
             lambda: ramtest._authenticate_tools(ROOT)),
            ("riscv", board, "_authenticate_tools",
             lambda: riscv._authenticate_tools(ROOT)),
            ("menu", board, "_authenticate_tools",
             lambda: menu.authenticate(ROOT, None, "ddr")),
            ("menu_package", board, "_authenticate_tools",
             lambda: menu_package._authenticate_tools(ROOT)),
        )
        for name, module, attribute, call in cases:
            with self.subTest(producer=name):
                with patch.object(module, attribute, return_value={}) as authenticate:
                    call()
                forwarded = authenticate.call_args.kwargs
                self.assertEqual(forwarded["gpu_router"], "HIP")
                self.assertEqual(forwarded["hip_architectures"],
                                 board.FES_GPU_ARCHITECTURES)
                self.assertEqual(forwarded["expected_configuration"],
                                 {"nextpnr": board.FES_TOOLCHAIN_CONFIGURATION})


class CudaRecordTests(unittest.TestCase):
    """Pong and the menu package must record the backend they selected."""

    REPOSITORY = "https://github.com/DeanoC/misteross.git"

    def _parameters(self, record):
        import json

        return json.loads(record)["parameters"]

    def test_pong_record_follows_the_router(self):
        identities = {"mistral": "commit=" + "b" * 40 + "; sha256=" + "1" * 64,
                      "nextpnr-mistral": "commit=" + "c" * 40 + "; sha256=" + "2" * 64,
                      "yosys": "commit=" + "d" * 40 + "; sha256=" + "3" * 64}
        hip = pong.create_build_record(ROOT, self.REPOSITORY, "a" * 40, identities,
                                       execution=EXECUTION)
        cuda = pong.create_build_record(ROOT, self.REPOSITORY, "a" * 40, identities,
                                        execution=EXECUTION, gpu_router="CUDA")
        self.assertEqual(self._parameters(hip)["gpu_backend"], "hip")
        self.assertEqual(self._parameters(hip)["gpu_architectures"],
                         board.FES_GPU_ARCHITECTURES)
        self.assertEqual(self._parameters(cuda)["gpu_backend"], "cuda")
        self.assertEqual(self._parameters(cuda)["gpu_architectures"], "unused")

    def test_menu_package_record_follows_the_router(self):
        identities = {"mistral": "commit=" + "b" * 40 + "; sha256=" + "1" * 64,
                      "nextpnr-mistral": "commit=" + "c" * 40 + "; sha256=" + "2" * 64,
                      "yosys": "commit=" + "d" * 40 + "; sha256=" + "3" * 64}
        hip = menu_package.create_build_record(ROOT, self.REPOSITORY, "a" * 40, identities,
                                                execution=EXECUTION)
        cuda = menu_package.create_build_record(ROOT, self.REPOSITORY, "a" * 40, identities,
                                                 execution=EXECUTION, gpu_router="CUDA")
        self.assertEqual(self._parameters(hip)["gpu_backend"], "hip")
        self.assertEqual(self._parameters(cuda)["gpu_backend"], "cuda")
        self.assertEqual(self._parameters(cuda)["gpu_architectures"], "unused")


class RouteLogBackendTests(unittest.TestCase):
    """A real evidence fixture, not a mock: the route log must prove the backend.

    The passing-output fixture lives on Pong's own suite. Reach it through the
    module, not by importing the TestCase: binding that class here would make
    unittest discovery collect its tests in this module too.
    """

    def _write_passing_outputs(self, directory):
        test_build_fes_pong.BuildFesPongTests._write_passing_outputs(self, directory)

    def _cuda_output(self, directory):
        self._write_passing_outputs(directory)
        log = directory / "nextpnr.log"
        text = log.read_text(encoding="utf-8")
        self.assertIn("backend hip:", text)
        log.write_text(text.replace("backend hip:", "backend cuda:"), encoding="utf-8")
        return directory

    def test_cuda_log_passes_under_cuda_and_fails_under_the_default(self):
        import tempfile

        with tempfile.TemporaryDirectory() as temporary:
            output = self._cuda_output(Path(temporary) / "out")
            evidence = pong.validate_build_evidence(output, ROOT, gpu_backend="cuda")
            self.assertEqual(evidence["route"]["gpu_backend"], "cuda")
            with self.assertRaises(board.BuildError):
                pong.validate_build_evidence(output, ROOT)

    def test_hip_log_still_passes_under_the_default(self):
        import tempfile

        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "out"
            self._write_passing_outputs(output)
            evidence = pong.validate_build_evidence(output, ROOT)
            self.assertEqual(evidence["route"]["gpu_backend"], "hip")
            with self.assertRaises(board.BuildError):
                pong.validate_build_evidence(output, ROOT, gpu_backend="cuda")


if __name__ == "__main__":
    unittest.main()
