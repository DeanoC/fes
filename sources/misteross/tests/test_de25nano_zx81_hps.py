"""DE25 HPS-computer build guards, client and real-machine simulation."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano/zx81-hps"


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, BOARD / filename)
    result = importlib.util.module_from_spec(spec)
    sys.modules[name] = result
    spec.loader.exec_module(result)
    return result


HpsZX81BuildTests = module("de25_computer_build_tests", "test_build.py").HpsZX81BuildTests
NativeClientProofTests = module("de25_native_client_proof_tests", "test_client_proof.py").NativeClientProofTests


class ComputerClientTests(unittest.TestCase):
    def test_client_protocol_and_offline_file_backend(self):
        result = subprocess.run([sys.executable, "-m", "unittest", "-v", "test_client"],
                                cwd=BOARD, capture_output=True, text=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


class ComputerSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_complete_frame_ownership_and_clock_recovery(self):
        result = subprocess.run([sys.executable, "video_sim.py"], cwd=BOARD,
                                capture_output=True, text=True, timeout=180)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_actual_native_client_and_rtl_transport(self):
        directory = Path(tempfile.mkdtemp(prefix="fes-de25-client-proof-"))
        args = [sys.executable, "client_proof.py", "--native-only", "--allow-working-copy",
                "--output", str(directory / "proof")]
        for scenario in ("tape-bounds", "wrong-build", "bad-signature", "missing-ack", "late-ack",
                         "post-write-reset", "ambiguous-write", "held-word-change", "late-reset"):
            args += ["--scenario", scenario]
        result = subprocess.run(args, cwd=BOARD, capture_output=True, text=True, timeout=180)
        if result.returncode == 0:
            shutil.rmtree(directory)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr + "\nretained " + str(directory))

    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_keyboard_tape_and_transport_faults_on_real_machine(self):
        result = subprocess.run([sys.executable, "sim.py"], cwd=BOARD,
                                capture_output=True, text=True, timeout=600)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
