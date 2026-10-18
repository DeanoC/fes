"""All-state proofs for PIT decrement and the ISA display fractional enables."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / 'cores/fes-pcxt'
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')

# Original nested-borrow equations from the MIT-licensed KF8253 import.
# source.json retains their pinned upstream identity.
REFERENCE = r"""
    function logic [16:0] reference_decrement (input [16:0] count, input is_bcd);
        if (count == 17'b0_0000_0000_0000_0000)
            reference_decrement = 17'b0_0000_0000_0000_0000;
        else
            if (is_bcd == 1'b0)
                reference_decrement = count - 17'b0_0000_0000_0000_0001;
            else begin
                if (count[3:0] == 4'b0000) begin
                    if (count[7:4] == 4'b0000) begin
                        if (count[11:8] == 4'b0000) begin
                            if (count[15:12] == 4'b0000) begin
                                reference_decrement[16]    = 1'b0;
                                reference_decrement[15:12] = 4'd9;
                            end
                            else begin
                                reference_decrement[16]    = count[16];
                                reference_decrement[15:12] = count[15:12] - 4'd1;
                            end
                            reference_decrement[11:8]  = 4'd9;
                        end
                        else begin
                            reference_decrement[16:12] = count[16:12];
                            reference_decrement[11:8]  = count[11:8]  - 4'd1;
                        end
                        reference_decrement[7:4]  = 4'd9;
                    end
                    else begin
                        reference_decrement[16:8] = count[16:8];
                        reference_decrement[7:4]  = count[7:4] - 4'd1;
                    end
                    reference_decrement[3:0]  = 4'd9;
                end
                else begin
                    reference_decrement[16:4] = count[16:4];
                    reference_decrement[3:0]  = count[3:0] - 4'd1;
                end
            end
    endfunction
"""


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class CounterEquivalenceTest(unittest.TestCase):
    def prove(self, fixture, parameters=''):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            command = ('read_verilog -sv proof.sv; ' + parameters +
                       'prep -top proof -flatten; sat -verify -prove ok 1 -show-inputs -show-outputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_saturating_binary_and_bcd_one_and_two_steps(self):
        source = (CORE / 'vendor/kf8253/KF8253_Counter.sv').read_text()
        start = source.index('    function automatic logic [16:0] decrement(')
        function = source[start:source.index('    endfunction', start) + len('    endfunction')]
        fixture = 'module proof(input [16:0] count, input bcd, output ok);\n' + REFERENCE + function + r"""
          wire [16:0] first = reference_decrement(count, bcd);
          wire [16:0] second = reference_decrement(first, bcd);
          assign ok = decrement(count, bcd, 1'b0) == first &&
                      decrement(count, bcd, 1'b1) == second;
        endmodule
        """
        # No assumptions: includes zero, the high sentinel bit, and every
        # non-decimal nibble pattern, not just valid programmed BCD values.
        self.prove(fixture)

    def test_display_phase_and_enable_are_cycle_exact(self):
        source = (CORE / 'expansions/pcxt_display_card.sv').read_text()
        start = source.index('    reg [26:0] phase;')
        logic = source[start:source.index('    reg [3:0] dot;', start)]
        logic = logic.replace('    reg [26:0] phase;', '')
        update = re.search(r'phase <= (dot_ce[^;]+);', source).group(1)
        fixture = r"""module proof #(parameter bit MDA=0)(input [26:0] phase, output ok);
        """ + logic + 'wire [26:0] actual_next = ' + update + ';\n' + r"""
          wire [27:0] old_sum = {1'b0,phase} + (MDA ? 28'd16257000 : 28'd14318180);
          wire old_enable = old_sum >= 28'd100000000;
          wire [26:0] old_next = old_enable ? 27'(old_sum - 28'd100000000) : old_sum[26:0];
          assign ok = dot_ce == old_enable && actual_next == old_next;
        endmodule
        """
        for mda in (0, 1):
            with self.subTest(mda=mda):
                self.prove(fixture, f'chparam -set MDA {mda} proof; ')

    def test_precomputed_decrements_are_current_at_every_count_edge(self):
        vendor = CORE / 'vendor/kf8253'
        source = (vendor / 'KF8253_Counter.sv').read_text()
        assertions = r"""
          always @(posedge clock) begin
            if (!reset && start_counting && count_edge) begin
              assert(dec_count == decrement(count, select_bcd, 1'b0));
              assert(dec2_count == decrement(count, select_bcd, 1'b1));
              assert(count_zero_q == (count == 0));
              assert(count_le_one_q == (count <= 1));
              assert(count_le_two_q == (count <= 2));
              assert(count_le_three_q == (count <= 3));
              assert(count_two_q == (count == 2));
            end
          end
        """
        source = source.replace('endmodule', assertions + '\nendmodule')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'counter.sv').write_text(source)
            command = ('read_verilog -formal -sv -I ' + str(vendor) +
                       ' counter.sv; prep -top KF8253_Counter -flatten; '
                       'memory_map; async2sync; opt; '
                       'sat -verify -seq 4 -tempinduct -maxsteps 20 '
                       '-set-init-zero -prove-asserts -show-inputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
