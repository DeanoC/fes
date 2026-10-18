"""Prove predecoded control-ROM reads and operand routing against the import."""
import importlib.util
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / 'cores/fes-pcxt/vendor/mcl86'
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
original_path = sys.path[:]
sys.path.insert(0, str(ROOT / 'scripts'))
try:
    spec = importlib.util.spec_from_file_location('pcxt_rom_inputs', ROOT / 'scripts/sim_fes_pcxt.py')
    inputs = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(inputs)
finally:
    sys.path[:] = original_path

# Original MIT-licensed MCL86 operand equations. Vendor RTL retains the
# Ted Fried/Marcus Andrade notices and source.json pins their original bytes.
REFERENCE = r"""
    function automatic [15:0] mux4(input [1:0] sel, input [15:0] d0, input [15:0] d1,
                                   input [15:0] d2, input [15:0] d3);
        case (sel)
            2'd0:    mux4 = d0;
            2'd1:    mux4 = d1;
            2'd2:    mux4 = d2;
            default: mux4 = d3;
        endcase
    endfunction
    wire [15:0] eu_op0_grp0 /* synthesis keep */;
    wire [15:0] eu_op0_grp1 /* synthesis keep */;
    wire [15:0] eu_op0_grp2 /* synthesis keep */;
    wire [15:0] eu_op0_grp3 /* synthesis keep */;
    assign eu_op0_grp0 = mux4(eu_rom_sel[5:4], eu_register_ax, eu_register_bx, eu_register_cx, eu_register_dx);
    assign eu_op0_grp1 = mux4(eu_rom_sel[5:4], eu_register_sp, eu_register_bp, eu_register_si, eu_register_di);
    assign eu_op0_grp2 = mux4(eu_rom_sel[5:4], eu_flags,       eu_register_r0, eu_register_r1, eu_register_r2);
    assign eu_op0_grp3 = mux4(eu_rom_sel[5:4], eu_register_r3, eu_biu_command, system_signals, 16'h0);
    assign expected0 = mux4(eu_rom_sel[7:6], eu_op0_grp0, eu_op0_grp1, eu_op0_grp2, eu_op0_grp3);
    wire [15:0] eu_op1_grp0 /* synthesis keep */;
    wire [15:0] eu_op1_grp1 /* synthesis keep */;
    wire [15:0] eu_op1_grp2 /* synthesis keep */;
    wire [15:0] eu_op1_grp3 /* synthesis keep */;
    assign eu_op1_grp0 = mux4(eu_rom_sel[1:0], BIU_REGISTER_ES,       BIU_REGISTER_SS,  BIU_REGISTER_CS,  BIU_REGISTER_DS);
    assign eu_op1_grp1 = mux4(eu_rom_sel[1:0], {8'h00, PFQ_TOP_BYTE}, BIU_REGISTER_RM,  BIU_REGISTER_REG, BIU_RETURN_DATA);
    assign eu_op1_grp2 = mux4(eu_rom_sel[1:0], PFQ_ADDR_OUT,          eu_register_r0,   eu_register_r1,   eu_register_r2);
    assign eu_op1_grp3 = mux4(eu_rom_sel[1:0], eu_register_r3,        eu_alu_last_result, system_signals, eu_opcode_immediate);
    assign expected1 = mux4(eu_rom_sel[3:2], eu_op1_grp0, eu_op1_grp1, eu_op1_grp2, eu_op1_grp3);
"""
DATA_NAMES = ['BIU_REGISTER_CS', 'BIU_REGISTER_DS', 'BIU_REGISTER_ES', 'BIU_REGISTER_REG', 'BIU_REGISTER_RM', 'BIU_REGISTER_SS', 'BIU_RETURN_DATA', 'PFQ_ADDR_OUT', 'PFQ_TOP_BYTE', 'eu_alu_last_result', 'eu_biu_command', 'eu_flags', 'eu_opcode_immediate', 'eu_register_ax', 'eu_register_bp', 'eu_register_bx', 'eu_register_cx', 'eu_register_di', 'eu_register_dx', 'eu_register_r0', 'eu_register_r1', 'eu_register_r2', 'eu_register_r3', 'eu_register_si', 'eu_register_sp', 'system_signals']


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class CpuSelectionTest(unittest.TestCase):
    def test_operand_values_for_every_selection_and_register_value(self):
        source = (VENDOR / 'mcl86_eu_core.sv').read_text()
        start = source.index('    function automatic [15:0] select16(')
        function = source[start:source.index('    endfunction', start) + len('    endfunction')]
        vectors = '\n'.join(re.search(r'    wire \[255:0\] eu_sources' + str(i) + r' = .*?;', source, re.S).group(0) for i in (0, 1))
        adds = '\n'.join(re.search(r'    assign eu_add_operand' + str(i) + r' = .*?;', source, re.S).group(0) for i in (0, 1))
        expressions = '\n'.join(re.search(r'    assign eu_operand' + str(i) + r' = select16\(.*?;',
                                           source, re.S).group(0) for i in (0, 1))
        adder = source[source.index('    mcl86_adder u_adder ('):source.index('    );', source.index('    mcl86_adder u_adder ('))]
        actual_add = [re.search(r'\.' + port + r'\s*\(\s*(\w+)\s*\)', adder)[1] for port in ('a', 'b')]
        ports = ', '.join('input [' + ('7:0' if n == 'PFQ_TOP_BYTE' else '15:0') + '] ' + n
                          for n in DATA_NAMES)
        # Follow the real ROM-instance bindings as well as the real operand
        # expressions, so swapping the two control ports also fails the proof.
        bindings = [re.search(r'\.operand_select' + str(i) + r'\s*\(\s*(\w+)\s*\)',
                              source).group(1) for i in (0, 1)]
        controls = (f"wire [15:0] {bindings[0]} = 16'h1 << eu_rom_sel[7:4];\n"
                    f"wire [15:0] {bindings[1]} = 16'h1 << eu_rom_sel[3:0];\n")
        for i in (0, 1):
            binding = re.search(r'\.add_select' + str(i) + r'\s*\(\s*(\w+)\s*\)', source)[1]
            controls += f"wire [15:0] {binding} = 16'h1 << eu_rom_sel[{'7:4' if i == 0 else '3:0'}];\n"
        fixture = 'module proof(input [7:0] eu_rom_sel, ' + ports + ', output ok);\n' + controls + r"""
          `include "mcl86_facts.vh"
          wire [15:0] eu_operand0, eu_operand1, eu_add_operand0, eu_add_operand1, expected0, expected1;
        """ + function + vectors + REFERENCE + expressions + adds + f'wire [15:0] actual_add0 = {actual_add[0]}, actual_add1 = {actual_add[1]};\n' + r"""
          wire valid_add = FES_MC_ADD_OP0_MASK[eu_rom_sel[7:4]] && FES_MC_ADD_OP1_MASK[eu_rom_sel[3:0]];
          assign ok = eu_operand0 == expected0 && eu_operand1 == expected1 &&
            (!valid_add || (actual_add0 == expected0 && actual_add1 == expected1));
        endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            inputs.cpu_roms(work)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; '
                'sat -verify -prove ok 1 -show-inputs'], cwd=work, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_jump_boolean_for_all_states(self):
        source = (VENDOR / 'mcl86_eu_core.sv').read_text()
        start = source.index('    wire eu_last_zero')
        end = source.index(';', source.index('    assign eu_jump_boolean', start)) + 1
        fixture = r"""
          module proof(input [15:0] eu_alu_last_result, eu_register_ax, eu_register_dx,
                       input [12:0] eu_rom_address, input [7:0] idiv_opcode,
                       input [3:0] eu_opcode_jump_cond, output ok);
            wire eu_jump_boolean;
        """ + source[start:end] + r"""
            wire expected =
              ((idiv_opcode == 'h1) && (eu_rom_address == 'h0E76) && (eu_register_ax[15:7] != 'h0)) ? 1'b1 :
              ((idiv_opcode == 'h1) && (eu_rom_address == 'h0F02) && ((eu_register_dx != 'h0) || (eu_register_ax[15] != 'h0))) ? 1'b1 :
              (eu_opcode_jump_cond == 4'h0) ? 1'b1 :
              (eu_opcode_jump_cond == 4'h1 && eu_alu_last_result != 16'h0) ? 1'b1 :
              (eu_opcode_jump_cond == 4'h2 && eu_alu_last_result == 16'h0) ? 1'b1 : 1'b0;
            assign ok = eu_jump_boolean == expected;
          endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; sat -verify -prove ok 1'],
                cwd=work, capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_rom_contents_and_two_phase_registered_read_alignment(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            inputs.cpu_roms(work)
            self.assertEqual((work / 'mcl86_ucode.mem').read_bytes(),
                             (VENDOR / 'mcl86_ucode.mem').read_bytes())
            words = [int(s, 16) for s in (work / 'mcl86_ucode.mem').read_text().split()]
            controls = [int(s, 16) for s in (work / 'mcl86_operand_select.mem').read_text().split()]
            self.assertEqual(len(words), 4096)
            self.assertEqual(controls, [((1 << ((w >> 20) & 15)) << 16) |
                                       (1 << ((w >> 16) & 15)) for w in words])
            # Cut only the immutable table lookup, after exhaustively checking
            # its contents above. Preserve the actual RTL clocks, read stages
            # and output bindings. A symbolic 32-bit lookup value registered
            # on clk models any pure ROM at the EU's registered address,
            # including arbitrary changes between successive addresses.
            source = (VENDOR / 'mcl86_ucode.sv').read_text()
            source, count = re.subn(r'input\s+wire\s+clk,',
                'input wire [31:0] rom_payload, input wire clk,', source)
            self.assertEqual(count, 1)
            for memory in ('operand_select_mem', 'mem'):
                source, count = re.subn(r'logic \[31:0\] ' + memory + r'\[0:4095\];', '', source)
                self.assertEqual(count, 1)
                filename = 'mcl86_operand_select.mem' if memory == 'operand_select_mem' else 'mcl86_ucode.mem'
                source, count = re.subn(r'initial begin\s*\$readmemh\("' + re.escape(filename) + r'", ' + memory + r'\);\s*end', '', source)
                self.assertEqual(count, 1)
                payload = ("{(16'h1 << rom_payload[23:20]), (16'h1 << rom_payload[19:16])}"
                           if memory == 'operand_select_mem' else 'rom_payload')
                source, count = re.subn(r'\b' + memory + r'\[addr\]', payload, source)
                self.assertEqual(count, 1)
            (work / 'rom.sv').write_text(source)
            (work / 'proof.sv').write_text(r"""
              module proof(input clk, input [31:0] next_payload, output ok);
                // The real EU drives this port exclusively from its
                // rising-edge microcode-PC register. Inputs may otherwise
                // change arbitrarily on every formal half-cycle.
                reg [31:0] payload;
                always @(posedge clk) payload <= next_payload;
                reg [31:0] original_word;
                always @(posedge clk) original_word <= payload;
                wire [31:0] word;
                wire [15:0] s0, s1, a0, a1;
                mcl86_ucode dut(.rom_payload(payload), .clk(clk), .clk_rom(~clk), .addr(12'b0), .dout(word),
                    .operand_select0(s0), .operand_select1(s1), .add_select0(a0), .add_select1(a1));
                assign ok = word == original_word &&
                            s0 == (16'h1 << original_word[23:20]) &&
                            s1 == (16'h1 << original_word[19:16]) && a0 == s0 && a1 == s1;
              endmodule
            """)
            command = ('read_verilog -sv rom.sv proof.sv; prep -top proof -flatten; '
                       'clk2fflogic; opt; '
                       'sat -verify -seq 6 -set-init-zero -prove-skip 4 -prove ok 1 '
                       + ' '.join(f'-set-at {step} clk {int(step % 2 == 0)}'
                                  for step in range(1, 7)) + ' -show-inputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


class MicrocodeFactsTest(unittest.TestCase):
    def test_facts_cover_every_control_word(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            inputs.cpu_roms(work)
            self.assertEqual((work / 'mcl86_ucode.mem').read_bytes(),
                             (VENDOR / 'mcl86_ucode.mem').read_bytes())
            words = [int(s, 16) for s in (work / 'mcl86_ucode.mem').read_text().split()]
            facts = (work / 'mcl86_facts.vh').read_text()
            masks = [int(re.search(r'FES_MC_ADD_OP' + str(i) + r"_MASK = 16'h([0-9a-f]+)", facts)[1], 16) for i in (0, 1)]
            expected = [0, 0]
            for word in words:
                if ((word >> 28) & 7) == 2:
                    for i, shift in enumerate((20, 16)):
                        expected[i] |= 1 << ((word >> shift) & 15)
            self.assertEqual(masks, expected)
            jump7 = any(((w >> 28) & 7) == 1 and ((w >> 20) & 7) == 7 for w in words)
            self.assertIn("FES_MC_JUMP7_USED = 1'b" + str(int(jump7)), facts)
            self.assertEqual(len(words), 4096)
