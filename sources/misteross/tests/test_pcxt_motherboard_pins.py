"""Prove the registered I/O pins preserve the original state/phase equations."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from sim_fes_pcxt import CHIPSET_SOURCES, CHIP_VENDORS, vendor_dir

YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class MotherboardPinTest(unittest.TestCase):
    def test_unbounded_original_pin_equations_after_reset(self):
        source = (ROOT / 'cores/fes-pcxt/rtl/pcxt_motherboard.sv').read_text()
        # Instrument the actual motherboard, keeping its transition logic and
        # peripherals. The ghost register is the original PIC select latch;
        # PIT/PPI retain their existing readback select latches in the design.
        source = source.replace('    input wire clk, reset, guest_ce,',
                                '    output wire proof_ok,\n    input wire clk, reset, guest_ce,')
        instrument = r'''
    reg proof_pic_select;
    always @(posedge clk)
        if (reset) proof_pic_select <= 0;
        else if (state == IDLE && request_valid) proof_pic_select <= pic;
    assign proof_ok = reset || (
        (strobe_q == (state == LOCAL && phase >= 1 && phase <= 8)) &&
        (pic_selected == (state == LOCAL && proof_pic_select)) &&
        (pit_selected == (state == LOCAL && select_pit)) &&
        (ppi_selected == (state == LOCAL && select_ppi)) &&
        (strobe == (local_active && phase >= 1 && phase <= 8)) &&
        (pic_cs_n == !(local_active && proof_pic_select)) &&
        (pit_cs_n == !(local_active && select_pit)) &&
        (ppi_cs_n == !(local_active && select_ppi)) &&
        (rd_n == !(local_active && phase >= 1 && phase <= 8 && !write_cycle && !select_inta)) &&
        (wr_n == !(local_active && phase >= 1 && phase <= 8 && write_cycle && !select_inta)));
'''
        source = source.replace('endmodule', instrument + 'endmodule')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(source)
            command = ('read_verilog -sv ' +
                       ' '.join('-I ' + str(vendor_dir(n)) for n in CHIP_VENDORS) + ' ' +
                       ' '.join(str(ROOT / s) for s in CHIPSET_SOURCES[:-2]) +
                       ' proof.sv; prep -top pcxt_motherboard -flatten; memory; async2sync; opt; '
                       'sat -verify -seq 4 -tempinduct -maxsteps 8 -set-at 1 reset 1 -prove proof_ok 1')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('Induction step proven: SUCCESS!', result.stdout)
