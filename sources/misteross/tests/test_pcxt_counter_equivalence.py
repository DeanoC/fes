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
KF8253 = ROOT / 'cores/fes-common/vendor/kf8253'
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
    def test_cpu_phase_pin_clock_and_guest_enable_are_cycle_exact(self):
        source = (CORE / 'rtl/pcxt_cpu.sv').read_text()
        gate = source[source.index('    reg [7:0] phase;'):source.index('    wire [19:0] ad;')]
        gold = r"""
          reg [7:0] phase;
          reg pin_clock;
          wire [8:0] phase_sum = {1'b0, phase} + 9'd21;
          assign guest_ce = !reset && phase_sum >= 9'd220 && !pin_clock;
          always @(posedge clk) begin
            if (reset) begin phase <= 0; pin_clock <= 0; end
            else if (phase_sum >= 9'd220) begin
              phase <= 8'(phase_sum - 9'd220);
              pin_clock <= !pin_clock;
            end else phase <= phase_sum[7:0];
          end
        """
        ports = '(input clk, reset, output guest_ce, output [7:0] phase_out, output pin_out);\n'
        outputs = '\nassign phase_out = phase; assign pin_out = pin_clock; endmodule\n'
        fixture = 'module gold' + ports + gold + outputs + 'module gate' + ports + gate + outputs
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            command = ('read_verilog -sv proof.sv; proc; opt; '
                       'equiv_make gold gate proof; prep -top proof -flatten; '
                       'equiv_simple; equiv_induct -seq 2; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

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
        source = (KF8253 / 'KF8253_Counter.sv').read_text()
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
        vendor = KF8253
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

    def test_terminal_flags_preserve_next_count_period_and_mode2_output(self):
        vendor = KF8253
        source = (vendor / 'KF8253_Counter.sv').read_text()
        logic = source[source.index('    wire preset_zero ='):source.index('    // Update Count')]
        # Freshness at consumption is proved separately above. Here supply
        # exact candidates and prove the real mux/flag equations for arbitrary
        # counts, presets and controls, including all non-decimal BCD patterns.
        fixture = (vendor / 'KF8253_Definitions.svh').read_text() + r"""
          module proof(input [16:0] count, count_preset_load,
            input [2:0] select_mode,
            input select_bcd, counter_gate, load_edge, gate_edge,
                  first_count_edge, counter_out, output ok);
        """ + REFERENCE + r"""
          wire [16:0] dec_count = reference_decrement(count, select_bcd);
          wire [16:0] dec2_count = reference_decrement(dec_count, select_bcd);
          wire count_zero_q = count == 0;
          wire count_le_one_q = count <= 1;
          wire count_le_two_q = count <= 2;
          wire count_le_three_q = count <= 3;
          wire count_two_q = count == 2;
          reg [16:0] count_next, expected_next;
          reg count_period, expected_period;
        """ + logic + r"""
          always_comb begin
            expected_next = dec_count;
            expected_period = 0;
            casez (select_mode)
              `KF8253_CONTROL_MODE_0, `KF8253_CONTROL_MODE_4: begin
                if (!counter_gate) expected_next = count;
                if (load_edge) expected_next = count_preset_load;
              end
              `KF8253_CONTROL_MODE_1, `KF8253_CONTROL_MODE_5:
                if (gate_edge) expected_next = count_preset_load;
              `KF8253_CONTROL_MODE_2: begin
                if (!counter_gate) expected_next = count;
                if (expected_next == 0) expected_next = count_preset_load;
                if (gate_edge) expected_next = count_preset_load;
              end
              `KF8253_CONTROL_MODE_3: begin
                if (count[0]) begin
                  if (!counter_out) expected_next = {dec2_count[16:1], 1'b0};
                end else expected_next = dec2_count;
                if (!counter_gate) expected_next = count;
                if (expected_next == 0) begin
                  expected_period = !first_count_edge;
                  expected_next = count_preset_load;
                end
                if (gate_edge) expected_next = count_preset_load;
              end
              default: begin end
            endcase
            if (expected_next == 0) expected_period = 1;
          end
          // Mode-2 output consults the one flag only while the gate is high.
          assign ok = count_next == expected_next &&
                      count_period == expected_period &&
                      (!((select_mode & 3) == 2 && counter_gate) ||
                       mode2_next_one == (expected_next == 1));
          endmodule
        """
        self.prove(fixture)
