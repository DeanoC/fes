"""Exercise the disabled-by-default reference timing experiment on the real governor."""
from __future__ import annotations

from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
BENCH = ROOT / 'cores/fes-common/sim/i386/reference_governor_tb.sv'


@unittest.skipUnless(shutil.which('verilator') and shutil.which('c++'), 'Verilator and C++ are needed')
class ReferenceDiagnosticTest(unittest.TestCase):
    def test_committed_boundaries_and_default_modes(self):
        with tempfile.TemporaryDirectory(prefix='fes-i386-reference-test-') as work:
            command = [
                'verilator', '--binary', '--timing', '-Wno-TIMESCALEMOD',
                '--top-module', 'governor_integration_tb', '--Mdir', work,
                *(str(RTL / name) for name in (
                    'fes_i386_pkg.sv', 'fes_i386_gov_pkg.sv', 'fes_i386_gov_rom.sv', 'fes_i386_governor.sv')),
                str(BENCH),
            ]
            build = subprocess.run(command, capture_output=True, text=True, timeout=120)
            self.assertEqual(build.returncode, 0, build.stdout + build.stderr)
            result = subprocess.run([str(Path(work) / 'Vgovernor_integration_tb')],
                                    capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS reference governor integration', result.stdout)


if __name__ == '__main__':
    unittest.main()
