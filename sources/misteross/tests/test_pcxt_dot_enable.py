"""Inductively prove a registered dot-enable predictor preserves pulse cycles."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys')
class DotEnableTest(unittest.TestCase):
    def prove(self, mda, mutation=False, clock_hz=100000000):
        source = (ROOT / 'cores/fes-pcxt/expansions/pcxt_display_card.sv').read_text()
        # Reset establishes the invariant. The one-step proof preserves both
        # the phase range and equality with the original combinational flag.
        self.assertIn('phase <= 0; dot <= 0;', source)
        self.assertIn('if (clear) dot_ce <= 0;', source)
        rate = re.search(r'localparam integer DOT_RATE = ([^;]+);', source).group(1)
        wrap = re.search(r'localparam integer DOT_WRAP = ([^;]+);', source).group(1)
        phase = re.search(r'phase <= (dot_ce \?[^;]+);', source).group(1)
        enable = re.search(r'else dot_ce <= ([^;]+);', source).group(1)
        if mutation:
            self.assertIn('DOT_WRAP - DOT_RATE', enable)
            enable = enable.replace('DOT_WRAP - DOT_RATE', 'DOT_WRAP - DOT_RATE + 1')
        fixture = f"""module proof(input [26:0] phase, input dot_ce, clear, output ok);
localparam MDA = {mda};
localparam integer CLOCK_HZ = {clock_hz};
localparam integer DOT_RATE = {rate};
localparam integer DOT_WRAP = {wrap};
wire invariant = phase < 27'(CLOCK_HZ) && dot_ce == (phase >= 27'(DOT_WRAP));
wire [26:0] next_phase = clear ? 27'd0 : {phase};
wire next_ce = clear ? 1'b0 : {enable};
assign ok = (!clear && !invariant) ||
    (next_phase < 27'(CLOCK_HZ) && next_ce == (next_phase >= 27'(DOT_WRAP)));
endmodule
"""
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof; flatten; '
                'sat -verify -prove ok 1 -show-inputs -show-outputs'],
                cwd=work, capture_output=True, text=True, timeout=90)
            output = result.stdout + result.stderr
            if mutation:
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn('proof did fail', output)
            else:
                self.assertEqual(result.returncode, 0, output)
                self.assertIn('SUCCESS', output)

    def test_reset_and_inductive_pulse_identity_for_both_rates(self):
        for clock_hz in (50000000, 100000000):
            for mda in (0, 1):
                with self.subTest(mda=mda, clock_hz=clock_hz):
                    self.prove(mda, clock_hz=clock_hz)

    def test_wrong_threshold_rejected_for_both_rates(self):
        for clock_hz in (50000000, 100000000):
            for mda in (0, 1):
                with self.subTest(mda=mda, clock_hz=clock_hz):
                    self.prove(mda, True, clock_hz=clock_hz)
