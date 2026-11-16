"""Exercise the standalone DE25 x87 backend in the FPGA software lane."""
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import unittest


BACKEND = Path(__file__).resolve().parents[1] / 'boards/de25nano/x87'
VERILATOR = os.environ.get('VERILATOR', 'verilator')
X86 = platform.machine().lower() in ('x86_64', 'amd64', 'i386', 'i686')


class X87PipelineSimulationTests(unittest.TestCase):
    @unittest.skipUnless(X86, 'the independent host-x87 reference requires an x86 host')
    @unittest.skipUnless(shutil.which(VERILATOR) and shutil.which('c++'),
                         'DE25 x87 simulation requires Verilator and a C++ compiler')
    def test_arithmetic_and_pipeline_protocol(self):
        # Zero random samples retains the exhaustive directed operand-class
        # and minnormal-threshold matrices in every PC/RC mode, plus the full
        # order/backpressure/flush and throughput/dependency checks.
        result = subprocess.run([sys.executable, str(BACKEND / 'run.py'),
                                 '--verilator', VERILATOR, '--samples', '0'],
                                cwd=BACKEND, capture_output=True, text=True, timeout=180)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
