"""Prove programmed mode predecode preserves every DMA state/interface bit."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
CONFIGURATIONS = ((20, 0, 1, 0), (24, 1, 0, 0), (24, 1, 0, 1))


@unittest.skipUnless(YOSYS, 'requires Yosys')
class DmaModeDecodeTest(unittest.TestCase):
    def prove(self, mutation=None, configuration=CONFIGURATIONS[0]):
        actual = (ROOT / 'cores/fes-common/rtl/fes_dma8237.sv').read_text()
        start = actual.index('    reg [3:0] mode_invalid, mode_verify, mode_read;')
        end = actual.index('    reg [PAGE_BITS-1:0] page[0:3];', start)
        original = actual[:start] + """    wire [3:0] mode_invalid, mode_verify, mode_read;
    for (genvar c = 0; c < 4; c = c + 1) begin : reference_decode
        assign mode_invalid[c] = mode[c][7:6] == 3 || mode[c][3:2] == 3 ||
            (REFRESH != 0 && c == 0 && mode[c][3:2] == 1);
        assign mode_verify[c] = mode[c][3:2] == 0;
        assign mode_read[c] = mode[c][3:2] == 1;
    end
""" + actual[end:]
        original = original.replace('mode[channel][7:6] == 0 && !hardware_request[channel]',
                                    'transfer_mode == 0 && !hardware_request[channel]')
        original = original.replace('command[0] || mode_invalid[service_channel]',
            'command[0] || transfer_mode == 3 || operation == 3 || (REFRESH != 0 && service_channel == 0 && operation == 1)')
        original = original.replace('if (mode_verify[service_channel])', 'if (operation == 0)')
        original = original.replace('if (mode_read[service_channel])', 'if (operation == 1)')
        if mutation == 'reset':
            actual = actual.replace("mode_verify <= 4'b1111;", "mode_verify <= 4'b0000;")
        elif mutation == 'channel':
            actual = actual.replace('mode_invalid[reg_wdata[1:0]]',
                                    "mode_invalid[reg_wdata[1:0] ^ 2'b01]")
        elif mutation == 'refresh':
            needle = '(REFRESH != 0 && reg_wdata[1:0]'
            self.assertEqual(actual.count(needle), 1)
            actual = actual.replace(needle, '(reg_wdata[1:0]')
        def instrument(source):
            needle = '    input wire clk, reset, guest_ce,'
            self.assertEqual(source.count(needle), 1)
            source = source.replace(needle, '    output wire [11:0] proof_mode_decode,\n' + needle)
            return source.replace('endmodule',
                '    assign proof_mode_decode = {mode_invalid, mode_verify, mode_read};\nendmodule')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(instrument(original))
            (work / 'gate.sv').write_text(instrument(actual))
            settings = ('chparam ' + ' '.join('-set ' + name + ' ' + str(value) for name, value in zip(
                ('ADDRESS_BITS', 'AT_PAGES', 'REFRESH', 'WORD_ADDRESS'), configuration)) + ' fes_dma8237; ')
            command = ('read_verilog -sv gold.sv; ' + settings + 'proc; memory; opt; rename fes_dma8237 gold; '
                       'design -stash gold; read_verilog -sv gate.sv; ' + settings + 'proc; memory; opt; '
                       'rename fes_dma8237 gate; design -copy-from gold -as gold gold; '
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
        for configuration in CONFIGURATIONS:
            with self.subTest(configuration=configuration):
                self.prove(configuration=configuration)

    def test_wrong_reset_decode_rejected(self):
        for configuration in CONFIGURATIONS:
            with self.subTest(configuration=configuration):
                self.prove('reset', configuration)

    def test_wrong_programmed_channel_rejected(self):
        for configuration in CONFIGURATIONS:
            with self.subTest(configuration=configuration):
                self.prove('channel', configuration)

    def test_xt_only_channel_zero_restriction_rejected_for_at(self):
        for configuration in CONFIGURATIONS[1:]:
            with self.subTest(configuration=configuration):
                self.prove('refresh', configuration)
