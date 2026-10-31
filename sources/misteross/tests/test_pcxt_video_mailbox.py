"""Check the real video ownership logic under arbitrary digital clock interleavings.

Frame completion and output SOF are overapproximated so complete handshakes are
reachable without simulating entire rasters. RAM data and analogue metastability
are outside this proof. Reset clears both logical domains; independent reset is
not supported by the RTL contract.
"""
import os
from contextlib import nullcontext
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
ABC = os.environ.get('YOSYS_ABC') or (str(Path(shutil.which(YOSYS) or YOSYS).resolve().with_name('yosys-abc')) if YOSYS else None)

MONITORS = r'''
    reg seen_reset = 0, past_reset;
    reg [1:0] past_published, past_front, pixel_bank1, pixel_bank2;
    reg past_sent, past_waiting, past_pixel_step, past_adopt;
    reg pixel_sent1, pixel_sent2;
    reg saw_publish = 0, saw_adopt = 0, saw_ack = 0, saw_queued = 0;
    reg [1:0] publications = 0;
    always @(posedge proof_clk) begin
        past_reset <= reset_sys;
        past_published <= published_bank; past_front <= front;
        past_sent <= sent; past_waiting <= waiting;
        past_pixel_step <= proof_pixel_step; past_adopt <= adopt;
        if (reset_sys) begin
            seen_reset <= 1;
            pixel_bank1 <= 0; pixel_bank2 <= 0;
            pixel_sent1 <= 0; pixel_sent2 <= 0;
            saw_publish <= 0; saw_adopt <= 0; saw_ack <= 0; saw_queued <= 0;
            publications <= 0;
        end else begin
            if (proof_pixel_step) begin
                pixel_bank1 <= published_bank; pixel_bank2 <= pixel_bank1;
                pixel_sent1 <= sent; pixel_sent2 <= pixel_sent1;
                if (adopt) saw_adopt <= 1;
            end
            if (seen_reset && !past_reset && sent != past_sent) begin
                saw_publish <= 1;
                if (publications < 2) publications <= publications + 1'b1;
            end
            if (proof_sys_step && waiting && ack_sync == sent) saw_ack <= 1;
            if (queued) saw_queued <= 1;
        end
    end
    assign proof_ok = !seen_reset || reset_sys || (
        write_bank < 3 && published_bank < 3 && source_front < 3 && front < 3 &&
        write_bank != source_front && (!waiting || write_bank != published_bank) &&
        (waiting || source_front == front) &&
        (past_reset || !past_waiting || published_bank == past_published) &&
        (past_reset || front == past_front ||
            (past_pixel_step && past_adopt && front == past_published)) &&
        (!adopt || (sent_sync == sent && published_bank == pixel_bank1 &&
            published_bank == pixel_bank2 && sent == pixel_sent1 && sent == pixel_sent2))
    );
    assign proof_covered = saw_publish && saw_adopt && saw_ack && saw_queued && publications == 2;
'''

RAM_STUB = r'''
module coleco_video_dpram #(parameter DATAWIDTH=8, ADDRWIDTH=1, NUMWORDS=2)(
 input clock_a, clock_b, wren_a, wren_b,
 input [ADDRWIDTH-1:0] address_a,address_b,
 input [DATAWIDTH-1:0] data_a,data_b,
 output [DATAWIDTH-1:0] q_a,q_b);
 (* anyseq *) reg [DATAWIDTH-1:0] arbitrary_a, arbitrary_b;
 assign q_a=arbitrary_a; assign q_b=arbitrary_b;
endmodule
'''


def fixture(mda=0, mutation=None):
    source = (ROOT / 'cores/fes-pcxt/rtl/pcxt_video.sv').read_text()

    def replace(old, new):
        nonlocal source
        if source.count(old) != 1:
            raise ValueError('video proof projection no longer matches RTL: ' + old)
        source = source.replace(old, new)

    replace('    output wire [31:0] video_request', '''    input wire proof_clk, proof_sys_step, proof_pixel_step,
    input wire proof_sample_sent, proof_sample_ack, proof_frame_tail, proof_output_sof,
    output wire proof_ok, proof_covered,
    output wire [31:0] video_request''')
    source = source.replace('always @(posedge clk_sys)',
                            'always @(posedge proof_clk) if (proof_sys_step || reset_sys)')
    source = source.replace('always @(posedge clk_pixel)',
                            'always @(posedge proof_clk) if (proof_pixel_step || reset_pixel)')
    replace("sy == 10'(HEIGHT-1) && pos == 19'(WIDTH*HEIGHT)", 'proof_frame_tail')
    replace('wire output_sof = horizontal == 0 && vertical == 0;',
            'wire output_sof = proof_output_sof;')
    replace('ack_meta <= received;', 'if (proof_sample_ack) ack_meta <= received;')
    replace('sent_meta <= sent;', 'if (proof_sample_sent) sent_meta <= sent;')
    if mutation == 'change_pending_bank':
        replace('ack_sync <= ack_meta;',
                'ack_sync <= ack_meta; if (waiting) published_bank <= write_bank;')
    elif mutation == 'bypass_toggle_synchronizer':
        replace('sent_sync <= sent_meta;', 'sent_sync <= sent;')
    source = source.replace('endmodule', MONITORS + '\nendmodule')
    return source + RAM_STUB + r'''
module proof(input clk, reset, sys_step, pixel_step, sample_sent, sample_ack,
 input frame_tail, output_sof, hold, pixel_ce, source_de, source_sof, source_eol,
 input [3:0] rgbi, output ok, covered);
 pcxt_video #(.MDA(MDA_VALUE)) dut(.clk_sys(clk),.clk_pixel(clk),
 .reset_sys(reset),.reset_pixel(reset),.hold(hold),.pixel_ce(pixel_ce),
 .source_de(source_de),.source_sof(source_sof),.source_eol(source_eol),.rgbi(rgbi),
 .proof_clk(clk),.proof_sys_step(sys_step),.proof_pixel_step(pixel_step),
 .proof_sample_sent(sample_sent),.proof_sample_ack(sample_ack),
 .proof_frame_tail(frame_tail),.proof_output_sof(output_sof),
 .proof_ok(ok),.proof_covered(covered),.video_request());
endmodule
'''.replace('MDA_VALUE', str(mda))


def run_proof(mda=0, mutation=None, cover=False, output=None):
    if output is not None:
        Path(output).mkdir(parents=True, exist_ok=False)
    context = (nullcontext(str(output)) if output is not None else
               tempfile.TemporaryDirectory(prefix='pcxt-video-mailbox-'))
    with context as directory:
        work = Path(directory)
        source = fixture(mda, mutation)
        if not cover:
            source = source.replace('input [3:0] rgbi, output ok, covered);',
                                    'input [3:0] rgbi, output bad);\n'
                                    'wire ok, covered; assign bad = !ok;')
        (work / 'proof.sv').write_text(source)
        command = ('read_verilog -formal -sv -I ' + str(ROOT / 'cores/fes-common/generated') +
                   ' proof.sv; prep -top proof -flatten; opt; ')
        command += ('dffunmap; opt_clean; sat -falsify -seq 24 -set-at 1 reset 1 '
                    '-prove covered 0 -show-inputs' if cover else
                    'techmap; opt; dffunmap; simplemap; setundef -undriven -anyseq; '
                    'aigmap; write_aiger -zinit proof.aig')
        result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                capture_output=True, text=True, timeout=90)
        (work / 'yosys.log').write_text(result.stdout + result.stderr)
        if result.returncode or cover:
            return result
        result = subprocess.run([ABC, '-c', 'read_aiger proof.aig; pdr'], cwd=work,
                                capture_output=True, text=True, timeout=180)
        (work / 'abc.log').write_text(result.stdout + result.stderr)
        return result


@unittest.skipUnless(YOSYS and ABC and Path(ABC).is_file(), 'requires pinned Yosys and yosys-abc')
class VideoMailboxProofTests(unittest.TestCase):
    def test_cga_and_mda_arbitrary_interleavings_and_sampling_stalls(self):
        for mda in (0, 1):
            with self.subTest(mda=mda):
                result = run_proof(mda)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('Property proved.', result.stdout)

    def test_queued_publish_adopt_ack_and_republish_are_reachable(self):
        result = run_proof(cover=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('model found', result.stdout)

    def test_rejects_pending_bank_change(self):
        result = run_proof(mutation='change_pending_bank')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('was asserted in frame', result.stdout)

    def test_rejects_shortened_toggle_synchronizer(self):
        result = run_proof(mutation='bypass_toggle_synchronizer')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('was asserted in frame', result.stdout)


if __name__ == '__main__':
    unittest.main()
