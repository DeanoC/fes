"""Prove the PC/XT audio held-data contract under arbitrary digital clocks.

PDR uses the exact shared RTL; analog metastability and physical delays remain
separate. A synchronizer-bypass mutation and post-relock cover guard the proof.
"""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
ABC = os.environ.get('YOSYS_ABC') or (str(Path(shutil.which(YOSYS) or YOSYS).resolve().with_name('yosys-abc')) if YOSYS else None)
EXPECTED = 'e686a035484907d1690d7e0cd8e91d6ace86146b38bf4d85d4f500690ddfb0dd'


def projection():
    raw = (ROOT / 'cores/fes-common/rtl/fes_audio_output.v').read_bytes()
    if hashlib.sha256(raw).hexdigest() != EXPECTED:
        raise ValueError('audio RTL drift: re-review the proof binding')
    src = raw.decode()
    if src.count('    wire tick;') != 1 or src.count('    wire serial_data;') != 1 or not src.endswith('endmodule\n'):
        raise ValueError('audio proof projection no longer matches the RTL')
    base = src[:src.index('    wire serial_data;')]
    for old, new in (
        ('input wire source_clk, audio_clk, locked, hold,', 'input wire source_clk, audio_clk, locked, hold, tick,'),
        ('input wire [15:0] left_sample, right_sample,\n    output wire sclk, lrclk, sdata', 'input wire [15:0] left_sample, right_sample'),
        ('    wire tick;', '')):
        if base.count(old) != 1:
            raise ValueError('audio proof projection changed: ' + old)
        base = base.replace(old, new)
    return base


proof = '''    reg [31:0] formal_previous1 = 0;
    reg [31:0] formal_previous2 = 0;
    always @(posedge audio_clk) begin
        if (ack_sync == request) begin
            assert(held_sample == formal_previous1);
            assert(held_sample == formal_previous2);
            assert(request_sync == acknowledge);
        end
        formal_previous2 <= formal_previous1;
        formal_previous1 <= held_sample;
    end
endmodule
'''
reach = '''    reg formal_seen_locked = 0;
    reg formal_seen_loss = 0;
    reg formal_seen_relock = 0;
    reg formal_requested = 0;
    reg formal_source_transfer = 0;
    always @(posedge source_clk) begin
        if (formal_seen_relock && request_sync != acknowledge)
            formal_source_transfer <= 1;
    end
    always @(posedge audio_clk) begin
        if (locked) formal_seen_locked <= 1;
        if (formal_seen_locked && !locked) formal_seen_loss <= 1;
        if (formal_seen_loss && locked) formal_seen_relock <= 1;
        if (formal_seen_relock && tick && ack_sync == request) formal_requested <= 1;
        assert(!(formal_seen_relock && formal_requested && formal_source_transfer &&
                 ack_sync == request && held_sample != 0));
    end
endmodule
'''


@unittest.skipUnless(YOSYS and ABC and Path(ABC).is_file(), 'Yosys and ABC required')
class AudioCdcProofTests(unittest.TestCase):
    def run_tool(self, command, work, timeout=120):
        result = subprocess.run(command, cwd=work, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                text=True, timeout=timeout, check=False)
        self.assertEqual(result.returncode, 0, result.stdout)
        return result.stdout

    def pdr(self, mutant=False):
        source = projection() + proof
        if mutant:
            old = 'ack_meta <= acknowledge; ack_sync <= ack_meta;'
            self.assertEqual(source.count(old), 1)
            source = source.replace(old, 'ack_meta <= acknowledge; ack_sync <= acknowledge;')
        with tempfile.TemporaryDirectory(prefix='pcxt-audio-proof-') as directory:
            work = Path(directory)
            (work / 'proof.v').write_text(source)
            command = ('read_verilog -formal -sv proof.v; prep -top fes_audio_output; async2sync; '
                       'clk2fflogic; opt; techmap; opt; aigmap; write_aiger -zinit -symbols proof.aig')
            self.run_tool([YOSYS, '-T', '-p', command], work)
            out = self.run_tool([ABC, '-c', 'read_aiger proof.aig; strash; pdr' +
                                 ('; write_cex mutant.cex' if mutant else '')], work)
            if mutant:
                self.assertIn('was asserted in frame', out)
                self.assertTrue((work / 'mutant.cex').is_file())
            else:
                self.assertIn('Property proved.', out)

    def test_all_clock_interleavings_preserve_two_edge_data_stability(self):
        self.pdr()

    def test_bypassing_ack_synchronizer_is_detected(self):
        self.pdr(mutant=True)

    def test_nonzero_transfer_after_lock_loss_and_relock_is_reachable(self):
        with tempfile.TemporaryDirectory(prefix='pcxt-audio-cover-') as directory:
            work = Path(directory)
            (work / 'reach.v').write_text(projection() + reach)
            command = ('read_verilog -formal -sv reach.v; prep -top fes_audio_output; async2sync; clk2fflogic; opt; '
                       'sat -seq 60 -set-init-zero -set-def-inputs -prove-asserts -falsify -show-inputs -show-regs '
                       '-show held_sample -show request -show acknowledge -show ack_sync -show formal_seen_relock '
                       '-show formal_source_transfer -dump_vcd reach.vcd -timeout 60')
            out = self.run_tool([YOSYS, '-T', '-p', command], work, timeout=90)
            self.assertIn('SAT proof finished - model found: FAIL!', out)
            self.assertTrue((work / 'reach.vcd').is_file())


if __name__ == '__main__':
    unittest.main()
