"""Prove local read staging retains the original sample and completed value.

The original return register is a ghost driven by the actual motherboard and
peripherals. The phase bound is proved alongside data equality, never assumed.
CPU and DMA consume the return payload only on their completion strobes.
"""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from sim_fes_pcxt import CHIPSET_SOURCES
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')

REFERENCE = r'''
    reg [7:0] proof_original_rdata;
    always @(posedge clk) begin
        if (reset) proof_original_rdata <= 8'hff;
        else case (state)
            IDLE: if (request_valid) proof_original_rdata <= 8'hff;
            MEMORY_SEND: if (memory_ready && memory_done) proof_original_rdata <= memory_rdata;
            MEMORY_WAIT: if (memory_done) proof_original_rdata <= memory_rdata;
            ISA_SEND: if (isa_ready && isa_done) proof_original_rdata <= isa_rdata;
            ISA_WAIT: if (isa_done) proof_original_rdata <= isa_rdata;
            LOCAL: if (phase == 8 && !write_cycle) begin
                if (select_dma) proof_original_rdata <= dma_reg_data;
                else if (select_page) proof_original_rdata <= 8'hff;
                else if (select_ppi) proof_original_rdata <= ppi_data;
                else if (select_pit) proof_original_rdata <= pit_data;
                else proof_original_rdata <= pic_bus_input ? 8'hff : pic_data;
            end
            default: begin end
        endcase
    end
    wire [7:0] proof_pending_rdata = state == LOCAL && phase >= 9 && !write_cycle ?
        local_read_snapshot : cycle_rdata;
    assign proof_ok = reset || ((state != LOCAL || phase <= 17) && proof_pending_rdata == proof_original_rdata &&
        (!(cycle_done || dma_done) || cycle_rdata == proof_original_rdata));
'''


@unittest.skipUnless(YOSYS, 'requires Yosys')
class MotherboardReadSnapshotTest(unittest.TestCase):
    def prove(self, mutation=None):
        source = (ROOT / 'cores/fes-pcxt/rtl/pcxt_motherboard.sv').read_text()
        if mutation == 'payload':
            source = source.replace('cycle_rdata <= local_read_snapshot;',
                                    "cycle_rdata <= local_read_snapshot ^ 8'h01;")
        elif mutation == 'sample':
            source = source.replace('else if (state == LOCAL && phase == 8 && !write_cycle)',
                                    'else if (state == LOCAL && phase == 9 && !write_cycle)')
        source = source.replace('    input wire clk, reset, guest_ce,',
                                '    output wire proof_ok,\n    input wire clk, reset, guest_ce,')
        source = source.replace('endmodule', REFERENCE + 'endmodule')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(source)
            command = ('read_verilog -sv ' +
                       ' '.join('-I ' + str(ROOT / 'cores/fes-pcxt/vendor' / n)
                                for n in ('kf8259', 'kf8253', 'kf8255')) + ' ' +
                       ' '.join(str(ROOT / s) for s in CHIPSET_SOURCES[:-2]) +
                       ' proof.sv; prep -top pcxt_motherboard -flatten; memory; async2sync; opt; '
                       'sat -verify -seq 4 -tempinduct -maxsteps 20 '
                       '-set-at 1 reset 1 -prove proof_ok 1')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            output = result.stdout + result.stderr
            if mutation:
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn('ERROR: Called with -verify and proof did fail!', output)
            else:
                self.assertEqual(result.returncode, 0, output)
                self.assertIn('Induction step proven: SUCCESS!', output)

    def test_original_read_sample_and_completed_payload(self):
        self.prove()

    def test_wrong_completed_payload_rejected(self):
        self.prove('payload')

    def test_late_peripheral_sample_rejected(self):
        self.prove('sample')
