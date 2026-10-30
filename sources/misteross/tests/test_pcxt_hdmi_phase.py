"""Prove pixel-bundle order for the actual top-level forwarded HDMI clock.

The two clock events model an in-period sampling edge after each pixel update.
Shifted samples align with the following original capture edge after warmup.
Propagation delay, pulse width, initial pixels and board acceptance are separate.
"""
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
ABC = os.environ.get('YOSYS_ABC') or (str(Path(shutil.which(YOSYS) or YOSYS).resolve().with_name('yosys-abc')) if YOSYS else None)

PHASED_PROFILES = (
    ('package', False, True),
    ('half-rate board', True, False),
)

# Top-level pin assignments at c05fba011141, before the forwarded clock moved.
GOLD_PINS = '''
assign HDMI_TX_CLK = clk_pixel;
assign HDMI_TX_D = video_response[23:0];
assign HDMI_TX_DE = video_response[24];
assign HDMI_TX_HS = video_response[25];
assign HDMI_TX_VS = video_response[26];
'''


def actual_pins(half_rate_board=False, phased_hdmi_clock=False):
    source = (ROOT / 'cores/fes-pcxt/rtl/fes_pcxt_top.sv').read_text()
    source = re.sub(r'//[^\n]*', '', source)
    required = (
        'localparam bit USE_PHASED_HDMI_CLOCK = HALF_RATE_BOARD || PHASED_HDMI_CLOCK;',
        '.number_of_clocks(USE_PHASED_HDMI_CLOCK ? 2 : 1)',
        '.outclk(video_clocks)', 'assign clk_pixel = video_clocks[0];',
    )
    if any(text not in source for text in required):
        raise ValueError('video PLL structure changed; review the clock projection')
    start = source.index('generate if (USE_PHASED_HDMI_CLOCK) begin: phased_hdmi_clock')
    end = source.index('end endgenerate', start) + len('end endgenerate')
    if not re.fullmatch(r'''generate\s+if\s*\(USE_PHASED_HDMI_CLOCK\)\s+begin:\s*phased_hdmi_clock
        \s*MISTRAL_CLKBUF\s+clock_buffer\s*\(\s*\.A\(video_clocks\[1\]\),\s*\.Q\(clk_hdmi\)\s*\);
        \s*end\s+else\s+begin:\s*common_hdmi_clock
        \s*assign\s+clk_hdmi\s*=\s*clk_pixel;\s*end\s+endgenerate''', source[start:end], re.VERBOSE):
        raise ValueError('HDMI clock selection changed; review the clock projection')
    pll = source[source.index('wire [(USE_PHASED_HDMI_CLOCK ? 2 : 1)-1:0] video_clocks;'):]
    phased = bool(half_rate_board or phased_hdmi_clock)
    frequency1 = re.search(
        r'output_clock_frequency1\(USE_PHASED_HDMI_CLOCK \? "([\d.]+) MHz" : "([\d.]+) MHz"\)', pll)
    phase1 = re.search(
        r'phase_shift1\(USE_PHASED_HDMI_CLOCK \? "([\d.]+) ps" : "([\d.]+) ps"\)', pll)
    if frequency1 is None or phase1 is None:
        raise ValueError('video PLL output selection changed; review the clock projection')
    frequency = [
        float(re.search(r'output_clock_frequency0\("([\d.]+) MHz"\)', pll).group(1)),
        float(frequency1.group(1 if phased else 2)),
    ]
    phase = [
        float(re.search(r'phase_shift0\("([\d.]+) ps"\)', pll).group(1)),
        float(phase1.group(1 if phased else 2)),
    ]
    if frequency[0] != 74.25 or phase[0] != 0:
        raise ValueError('expected a phase-zero 74.25 MHz pixel clock')
    if phased and (frequency[1] != frequency[0] or not 0 < phase[1] < 1e6 / frequency[1]):
        raise ValueError('expected equal pixel rates with a distinct in-period HDMI edge')
    if not phased and (frequency[1] != 0 or phase[1] != 0):
        raise ValueError('the default profile must disable the unused HDMI clock output')
    pins = re.findall(r'(?m)^\s*assign HDMI_TX_(?:CLK|D|DE|HS|VS)\s*=[^;]+;', source)
    if len(pins) != 5:
        raise ValueError('HDMI pin assignments changed; review the projection')
    return '\n'.join(pins)


def stage(pins, name):
    return 'module ' + name + '''(input clk_pixel,clk_hdmi,input[26:0]video_response,
 output clock,output[26:0]pins);
wire HDMI_TX_CLK,HDMI_TX_DE,HDMI_TX_HS,HDMI_TX_VS;wire[23:0]HDMI_TX_D;
''' + pins + '''
assign clock=HDMI_TX_CLK;assign pins={HDMI_TX_VS,HDMI_TX_HS,HDMI_TX_DE,HDMI_TX_D};
endmodule
'''


def fixture(shifted=True, mutation=None, half_rate_board=False, phased_hdmi_clock=False):
    pins = actual_pins(half_rate_board, phased_hdmi_clock)
    if mutation == 'corrupt_payload':
        pins = pins.replace('video_response[23:0]', "video_response[23:0] ^ 24'd1")
    source = stage(GOLD_PINS, 'gold') + stage(pins, 'gate')
    source += '''module proof(input clk,input[26:0] next_pixel,output bad);
reg phase=0;wire pixel_step=!phase;wire shift_step=phase;
reg[26:0] video_response;reg[26:0] sampled;reg previous_clock=0,seen=0;
wire aclock,bclock;wire[26:0] a,b;
gold x(pixel_step,pixel_step,video_response,aclock,a);
gate y(pixel_step,__CLOCK__,video_response,bclock,b);
always@(posedge clk)begin
 phase<=!phase;
 if(pixel_step)video_response<=next_pixel;
 previous_clock<=bclock;
 if(bclock&&!previous_clock)begin sampled<=b;seen<=1;end
end
assign bad=__BAD__;
endmodule
'''
    return (source.replace('__CLOCK__', 'shift_step' if shifted and mutation != 'wrong_slot' else 'pixel_step')
            .replace('__BAD__', 'pixel_step && seen && sampled!=a' if shifted else 'a!=b || aclock!=bclock'))


def prove(shifted=True, mutation=None, output=None, half_rate_board=False, phased_hdmi_clock=False):
    if output is not None:
        Path(output).mkdir(parents=True, exist_ok=False)
    context = nullcontext(str(output)) if output is not None else tempfile.TemporaryDirectory(prefix='pcxt-hdmi-phase-')
    with context as directory:
        work = Path(directory)
        (work / 'proof.sv').write_text(fixture(shifted, mutation, half_rate_board, phased_hdmi_clock))
        command = ('read_verilog -formal -sv proof.sv; prep -top proof -flatten; opt; '
                   'techmap; opt; dffunmap; simplemap; setundef -undriven -anyseq; '
                   'aigmap; write_json proof.json; write_aiger -zinit proof.aig')
        result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'yosys.log').write_text(result.stdout + result.stderr)
        if result.returncode:
            return result
        model = json.loads((work / 'proof.json').read_text())
        if model['modules']['proof']['ports']['bad']['bits'] == ['0']:
            return subprocess.CompletedProcess(['constant-bad-check'], 0, 'Bad output is identically zero.', '')
        result = subprocess.run([ABC, '-c', 'read_aiger proof.aig; pdr'], cwd=work, capture_output=True, text=True, timeout=60)
        (work / 'abc.log').write_text(result.stdout + result.stderr)
        return result


@unittest.skipUnless(YOSYS and ABC and Path(ABC).is_file(), 'requires pinned Yosys and yosys-abc')
class HdmiPhaseProofTests(unittest.TestCase):
    def test_default_keeps_the_original_clock_and_all_video_pins(self):
        result = prove(shifted=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue('identically zero' in result.stdout or 'Property proved.' in result.stdout, result.stdout)

    def test_shifted_capture_preserves_every_pixel_bundle_in_order(self):
        for name, half_rate_board, phased_hdmi_clock in PHASED_PROFILES:
            with self.subTest(profile=name):
                result = prove(half_rate_board=half_rate_board, phased_hdmi_clock=phased_hdmi_clock)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('Property proved.', result.stdout)

    def test_sampling_on_the_wrong_clock_edge_has_a_counterexample(self):
        for name, half_rate_board, phased_hdmi_clock in PHASED_PROFILES:
            with self.subTest(profile=name):
                result = prove(mutation='wrong_slot', half_rate_board=half_rate_board,
                               phased_hdmi_clock=phased_hdmi_clock)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('was asserted in frame', result.stdout)

    def test_corrupting_a_pixel_has_a_counterexample(self):
        for name, half_rate_board, phased_hdmi_clock in PHASED_PROFILES:
            with self.subTest(profile=name):
                result = prove(mutation='corrupt_payload', half_rate_board=half_rate_board,
                               phased_hdmi_clock=phased_hdmi_clock)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('was asserted in frame', result.stdout)


if __name__ == '__main__':
    unittest.main()
