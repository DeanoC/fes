"""Pack the actual video PLL/clock block in both PC/XT profile settings."""
from contextlib import nullcontext
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
NEXTPNR = os.environ.get('NEXTPNR_MISTRAL') or shutil.which('nextpnr-mistral')


def fixture(half_rate, phased_hdmi=False, mutation=None):
    source = (ROOT / 'cores/fes-pcxt/rtl/fes_pcxt_top.sv').read_text()
    start = source.index('    localparam bit USE_PHASED_HDMI_CLOCK =')
    end = source.index('    wire locked =', start)
    source = source[start:end]
    if mutation == 'unused_frequency':
        source = source.replace('"74.25 MHz" : "0 MHz"', '"74.25 MHz" : "74.25 MHz"')
    elif mutation == 'unused_phase':
        source = source.replace('"8979 ps" : "0 ps"', '"8979 ps" : "8979 ps"')
    elif mutation is not None:
        raise ValueError('unknown mutation')
    return '''module top(input FPGA_CLK1_50,output HDMI_TX_CLK,output[1:0]HDMI_TX_D);
localparam HALF_RATE_BOARD=HALF_VALUE;
localparam PHASED_HDMI_CLOCK=PHASE_VALUE;
wire clk_pixel,clk_hdmi,video_locked;
''' .replace('HALF_VALUE', str(int(half_rate))).replace('PHASE_VALUE', str(int(phased_hdmi))) + source + '''
reg[1:0] pixel_counter=0;
always @(posedge clk_pixel) pixel_counter<=pixel_counter+1'b1;
assign HDMI_TX_D=pixel_counter;
assign HDMI_TX_CLK=clk_hdmi;
endmodule
'''


def memory_fixture(half_rate):
    source = (ROOT / 'cores/fes-pcxt/rtl/fes_pcxt_top.sv').read_text()
    start = source.index('    wire [(HALF_RATE_BOARD ? 4 : 2)-1:0] clocks;')
    end = source.index('    localparam bit USE_PHASED_HDMI_CLOCK =')
    source = source[start:end]
    return '''module top(input FPGA_CLK1_50, output wire [3:0] SDRAM_A);
localparam HALF_RATE_BOARD=HALF_VALUE;
wire clk,clk_memory,clk_capture,memory_locked;
''' .replace('HALF_VALUE', str(int(half_rate))) + source + '''
wire [(HALF_RATE_BOARD ? 4 : 2)-1:0] buffered_clocks;
generate for (genvar i=0;i<(HALF_RATE_BOARD ? 4 : 2);i=i+1) begin: output_clock_buffers
    MISTRAL_CLKBUF buffer (.A(clocks[i]),.Q(buffered_clocks[i]));
end endgenerate
assign SDRAM_A=buffered_clocks;
endmodule
'''


def pack(half_rate=False, phased_hdmi=False, mutation=None, output=None):
    if output is not None:
        Path(output).mkdir(parents=True, exist_ok=False)
    context = nullcontext(str(output)) if output is not None else tempfile.TemporaryDirectory(prefix='pcxt-hdmi-profile-')
    with context as directory:
        work = Path(directory)
        (work / 'fixture.sv').write_text(fixture(half_rate, phased_hdmi, mutation))
        constraints = (ROOT / 'cores/fes-pcxt/constraints-board50.qsf').read_text()
        pins = [line for line in constraints.splitlines() if re.fullmatch(
            r'set_location_assignment PIN_\w+ -to (FPGA_CLK1_50|HDMI_TX_CLK|HDMI_TX_D\[[01]\])', line)]
        if len(pins) != 4:
            raise ValueError('expected reference clock and three actual HDMI pad locations')
        (work / 'pins.qsf').write_text('\n'.join(pins) + '\n')
        command = ('read_verilog -sv fixture.sv; synth_intel_alm -nolutram -nodsp -top top; '
                   'check -assert; write_json synth.json')
        result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'yosys.log').write_text(result.stdout + result.stderr)
        if result.returncode:
            raise AssertionError(result.stdout + result.stderr)
        result = subprocess.run([NEXTPNR, '--device', '5CSEBA6U23I7', '--json', 'synth.json',
                                 '--qsf', 'pins.qsf', '--pack-only', '--write', 'packed.json'],
                                cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'nextpnr.log').write_text(result.stdout + result.stderr)
        if result.returncode == 0:
            model = json.loads((work / 'packed.json').read_text())['modules']['top']
            pll = model['cells']['video_clock']['parameters']
            phase_enabled = half_rate or phased_hdmi
            if int(pll['number_of_clocks'], 2) != (2 if phase_enabled else 1):
                raise AssertionError('wrong number of packed video clocks')
            if pll['output_clock_frequency1'] != ('74.25 MHz' if phase_enabled else '0 MHz'):
                raise AssertionError('wrong selected second-output frequency')
            if pll['phase_shift1'] != ('8979 ps' if phase_enabled else '0 ps'):
                raise AssertionError('wrong selected second-output phase')
            if phase_enabled:
                buffer = model['cells'].get('phased_hdmi_clock.clock_buffer')
                if buffer is None or buffer['type'] != 'MISTRAL_CLKBUF':
                    raise AssertionError('phase-shifted profiles must buffer the forwarded HDMI clock')
                if buffer['connections']['A'] != model['cells']['video_clock']['connections']['outclk[1]']:
                    raise AssertionError('forwarded HDMI clock must use the phase-shifted PLL output')
                output_pad = next((cell for cell in model['cells'].values()
                                   if cell['type'] == 'MISTRAL_OB' and
                                   cell['connections']['PAD'] == model['ports']['HDMI_TX_CLK']['bits']), None)
                if output_pad is None or buffer['connections']['Q'] != output_pad['connections']['I']:
                    raise AssertionError('phase-shifted clock buffer must drive the HDMI clock output pad')
        return result


def pack_memory(half_rate=False):
    with tempfile.TemporaryDirectory(prefix='pcxt-memory-pll-profile-') as directory:
        work = Path(directory)
        (work / 'fixture.sv').write_text(memory_fixture(half_rate))
        constraints = (ROOT / 'cores/fes-pcxt/constraints-board50.qsf').read_text()
        pins = [line for line in constraints.splitlines() if re.fullmatch(
            r'set_location_assignment PIN_\w+ -to (FPGA_CLK1_50|SDRAM_A\[[0-3]\])', line)]
        if len(pins) != 5:
            raise ValueError('expected reference clock and four memory clock output locations')
        (work / 'pins.qsf').write_text('\n'.join(pins) + '\n')
        command = ('read_verilog -sv fixture.sv; synth_intel_alm -nolutram -nodsp -top top; '
                   'check -assert; write_json synth.json')
        result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'yosys.log').write_text(result.stdout + result.stderr)
        if result.returncode:
            raise AssertionError(result.stdout + result.stderr)
        result = subprocess.run([NEXTPNR, '--device', '5CSEBA6U23I7', '--json', 'synth.json',
                                 '--qsf', 'pins.qsf', '--pack-only', '--write', 'packed.json'],
                                cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'nextpnr.log').write_text(result.stdout + result.stderr)
        if result.returncode == 0:
            model = json.loads((work / 'packed.json').read_text())['modules']['top']
            pll = model['cells']['memory_clock']['parameters']
            expected_outputs = (
                ('50.0 MHz', '50.0 MHz', '50.0 MHz') if half_rate
                else ('100.0 MHz', '0 MHz', '0 MHz')
            )
            expected_phases = (
                ('10000 ps', '0 ps', '18000 ps') if half_rate
                else ('5000 ps', '0 ps', '0 ps')
            )
            if int(pll['number_of_clocks'], 2) != (4 if half_rate else 2):
                raise AssertionError('wrong number of packed memory clocks')
            if [pll[f'output_clock_frequency{index}'] for index in range(1, 4)] != list(expected_outputs):
                raise AssertionError('wrong packed memory clock frequencies')
            if [pll[f'phase_shift{index}'] for index in range(1, 4)] != list(expected_phases):
                raise AssertionError('wrong packed memory clock phases')
        return result


@unittest.skipUnless(YOSYS and NEXTPNR, 'requires pinned Yosys and nextpnr-mistral')
class HdmiProfilePackTests(unittest.TestCase):
    def test_default_profile_packs_one_clock_with_unused_output_disabled(self):
        result = pack()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_half_rate_profile_packs_the_two_actual_video_clocks(self):
        result = pack(half_rate=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_package_profile_phases_hdmi_without_slowing_the_board(self):
        result = pack(phased_hdmi=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_nonzero_unused_output_frequency_is_rejected(self):
        result = pack(mutation='unused_frequency')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("output_clock_frequency1' for an unused output", result.stdout + result.stderr)

    def test_nonzero_unused_output_phase_is_rejected(self):
        result = pack(mutation='unused_phase')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("phase_shift1' for an unused output", result.stdout + result.stderr)

    def test_memory_pll_packs_default_and_half_rate_profiles(self):
        for half_rate in (False, True):
            with self.subTest(half_rate=half_rate):
                result = pack_memory(half_rate)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
