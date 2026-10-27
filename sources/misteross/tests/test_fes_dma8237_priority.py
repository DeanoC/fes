"""Prove the parallel DMA priority rewrite with unchanged sequential logic."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
REFERENCE = r"""    reg chosen_valid;
    reg [1:0] chosen, candidate;
    integer n;
    always @* begin
        chosen_valid = 0; chosen = 0; candidate = 0;
        for (integer k=0; k<4; k=k+1) begin
            candidate = command[4] ? lowest + 2'(k) + 2'd1 : 2'(k);
            if (!chosen_valid && eligible[candidate]) begin chosen=candidate; chosen_valid=1; end
        end
    end
"""

@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class DmaPriorityTest(unittest.TestCase):
    def test_whole_dma_state_and_interface_equivalence(self):
        source = (ROOT / 'cores/fes-common/rtl/fes_dma8237.sv').read_text()
        start = source.index('    wire chosen_valid =')
        end = source.index('    // Resolve a new service', start)
        original = source[:start] + REFERENCE + source[end:]
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'original.sv').write_text(original)
            (work / 'actual.sv').write_text(source)
            command = ('read_verilog -sv original.sv; proc; memory; opt; rename fes_dma8237 gold; '
                       'design -stash gold; read_verilog -sv actual.sv; proc; memory; opt; '
                       'rename fes_dma8237 gate; design -copy-from gold -as gold gold; '
                       'equiv_make gold gate equiv; hierarchy -top equiv; equiv_simple; '
                       'equiv_induct -seq 4; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
