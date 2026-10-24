"""Prove programmed mode predecode preserves every DMA state/interface bit."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys')
class DmaModeDecodeTest(unittest.TestCase):
    def prove(self, mutation=None):
        actual = (ROOT / 'cores/fes-pcxt/rtl/pcxt_dma.sv').read_text()
        start = actual.index('    reg [3:0] mode_invalid, mode_verify, mode_read;')
        end = actual.index('    reg [3:0] page[0:3];', start)
        original = actual[:start] + """    wire [3:0] mode_invalid, mode_verify, mode_read;
    for (genvar c = 0; c < 4; c = c + 1) begin : reference_decode
        assign mode_invalid[c] = mode[c][7:6] == 3 || mode[c][3:2] == 3 ||
            (c == 0 && mode[c][3:2] == 1);
        assign mode_verify[c] = mode[c][3:2] == 0;
        assign mode_read[c] = mode[c][3:2] == 1;
    end
""" + actual[end:]
        original = original.replace('mode[channel][7:6] == 0 && !hardware_request[channel]',
                                    'transfer_mode == 0 && !hardware_request[channel]')
        original = original.replace('command[0] || mode_invalid[service_channel]',
            'command[0] || transfer_mode == 3 || operation == 3 || (service_channel == 0 && operation == 1)')
        original = original.replace('if (mode_verify[service_channel])', 'if (operation == 0)')
        original = original.replace('if (mode_read[service_channel])', 'if (operation == 1)')
        if mutation == 'reset':
            actual = actual.replace("mode_verify <= 4'b1111;", "mode_verify <= 4'b0000;")
        elif mutation == 'channel':
            actual = actual.replace('mode_invalid[reg_wdata[1:0]]',
                                    "mode_invalid[reg_wdata[1:0] ^ 2'b01]")
        def instrument(source):
            source = source.replace('module pcxt_dma (',
                'module pcxt_dma (\n    output wire [11:0] proof_mode_decode,')
            return source.replace('endmodule',
                '    assign proof_mode_decode = {mode_invalid, mode_verify, mode_read};\nendmodule')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(instrument(original))
            (work / 'gate.sv').write_text(instrument(actual))
            command = ('read_verilog -sv gold.sv; proc; memory; opt; rename pcxt_dma gold; '
                       'design -stash gold; read_verilog -sv gate.sv; proc; memory; opt; '
                       'rename pcxt_dma gate; design -copy-from gold -as gold gold; '
                       'equiv_make gold gate equiv; hierarchy -top equiv; '
                       'equiv_simple; equiv_induct -seq 4; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            output = result.stdout + result.stderr
            if mutation:
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn('unproven $equiv cells', output)
                self.assertIn('state_gold', output)
            else:
                self.assertEqual(result.returncode, 0, output)
                self.assertIn('Equivalence successfully proven!', output)

    def test_all_state_and_interface_bits(self):
        self.prove()

    def test_wrong_reset_decode_rejected(self):
        self.prove('reset')

    def test_wrong_programmed_channel_rejected(self):
        self.prove('channel')
