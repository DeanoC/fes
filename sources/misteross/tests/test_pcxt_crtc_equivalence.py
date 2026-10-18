"""Prove the cursor-offset retiming against the original whole CRTC."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')

@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class CrtcEquivalenceTest(unittest.TestCase):
    def test_all_state_and_outputs_including_simultaneous_cursor_writes_and_wrap(self):
        actual = (ROOT / 'cores/fes-pcxt/expansions/pcxt_6845.sv').read_text()
        start = actual.index('    // Predict the next row/target values')
        end = actual.index('    integer i;', start)
        original = actual[:start] + '''    assign cursor = display_enable && ma == cursor_address && cursor_blink &&
                    scan >= registers[10][4:0] && scan <= registers[11][4:0];
''' + actual[end:]
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(original)
            (work / 'gate.sv').write_text(actual)
            command = ('read_verilog -sv gold.sv; proc; memory; opt; rename pcxt_6845 gold; '
                       'design -stash gold; read_verilog -sv gate.sv; proc; memory; opt; '
                       'rename pcxt_6845 gate; design -copy-from gold -as gold gold; '
                       'equiv_make gold gate proof; hierarchy -top proof; equiv_simple; '
                       'equiv_induct -seq 4; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
