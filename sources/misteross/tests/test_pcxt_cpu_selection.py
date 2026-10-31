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
                                           source, re.S).group(0) for i in (0,))
        expressions += '\n' + re.search(r'    pcxt_logic_operand1_mux u_operand1_mux \(.*?    \);', source, re.S)[0]
        adder = source[source.index('    mcl86_adder u_adder ('):source.index('    );', source.index('    mcl86_adder u_adder ('))]
        actual_add = [re.search(r'\.' + port + r'\s*\(\s*(\w+)\s*\)', adder)[1] for port in ('a', 'b')]
        self.assertEqual(actual_add, ['eu_execute_add_operand0', 'eu_execute_add_operand1'])
        actual_add = ['eu_add_operand0', 'eu_add_operand1']
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
                'read_verilog -sv ' + str(ROOT / 'cores/fes-pcxt/rtl/pcxt_logic_operand1_mux.sv') +
                ' proof.sv; setattr -mod -unset keep_hierarchy pcxt_logic_operand1_mux; prep -top proof -flatten; '
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

    def test_prefetch_empty_for_every_pointer_value(self):
        source = (VENDOR / 'mcl86_biu_max.sv').read_text()
        actual = re.search(r'    assign pfq_empty = .*?;', source, re.S)[0]
        fixture = r"""
          module proof(input [15:0] pfq_addr_in, pfq_addr_out, output ok);
            wire pfq_empty;
            wire [3:0] pfq_used = pfq_addr_in[3:0] - pfq_addr_out[3:0];
        """ + actual + r"""
            assign ok = pfq_empty == (pfq_used == 4'd0);
          endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; sat -verify -prove ok 1'],
                cwd=work, capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def cut_rom_lookup(self):
        source = (VENDOR / 'mcl86_ucode.sv').read_text()
        for lane in range(3):
            self.assertIn(f'if (lane == {lane}) $readmemh("mcl86_ucode_byte{lane}.mem", rom);', source)
        self.assertIn('else $readmemh("mcl86_ucode_byte3.mem", rom);', source)
        source, count = re.subn(r'input\s+wire\s+clk,',
            'input wire [31:0] rom_payload, input wire clk,', source)
        self.assertEqual(count, 1)
        source, count = re.subn(r'logic \[7:0\] rom\[0:4095\];', '', source)
        self.assertEqual(count, 1)
        source, count = re.subn(r'initial begin.*?end\n', '', source, flags=re.S)
        self.assertEqual(count, 1)
        source, count = re.subn(r'rom\[read_address\]', 'rom_payload[lane*8 +: 8]', source)
        self.assertEqual(count, 1)
        return source

    def test_rom_contents_and_two_phase_registered_read_alignment(self):
        source = (VENDOR / 'mcl86_ucode.sv').read_text()
        self.assertIn('always_ff @(posedge clk) begin : read_mem', source)
        self.assertNotIn('clk_rom', source)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            inputs.cpu_roms(work)
            self.assertEqual((work / 'mcl86_ucode.mem').read_bytes(),
                             (VENDOR / 'mcl86_ucode.mem').read_bytes())
            words = [int(s, 16) for s in (work / 'mcl86_ucode.mem').read_text().split()]
            lanes = [[int(s, 16) for s in (work / f'mcl86_ucode_byte{lane}.mem').read_text().split()]
                     for lane in range(4)]
            self.assertEqual(len(words), 4096)
            self.assertTrue(all(len(lane) == 4096 for lane in lanes))
            self.assertEqual(words, [sum(lanes[lane][row] << (8*lane) for lane in range(4))
                                    for row in range(4096)])
            (work / 'rom.sv').write_text(self.cut_rom_lookup())
            (work / 'proof.sv').write_text(r"""
              module proof(input clk, reset, input [31:0] next_payload, output ok);
                // An immutable lookup changes only when the registered ROM
                // address changes on execute. Read on the intervening edge.
                reg phase = 0, seen_reset = 0;
                reg [31:0] payload, expected_word;
                always @(posedge clk) begin
                  if (reset) begin
                    phase <= 0; seen_reset <= 1;
                    payload <= 0; expected_word <= 0;
                  end else begin
                    phase <= !phase;
                    if (phase) begin
                      payload <= next_payload;
                      expected_word <= payload;
                    end
                  end
                end
                wire [31:0] word;
                wire [15:0] s0,s1,a0,a1;
                mcl86_ucode dut(.rom_payload(payload), .clk(clk),
                    .reset(reset), .advance(phase), .next_addr(12'b0), .dout(word),
                    .operand_select0(s0), .operand_select1(s1), .add_select0(a0), .add_select1(a1));
                assign ok = !seen_reset || (word == expected_word &&
                            s0 == (16'h1 << expected_word[23:20]) &&
                            s1 == (16'h1 << expected_word[19:16]) && a0 == s0 && a1 == s1);
              endmodule
            """)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv rom.sv proof.sv; prep -top proof -flatten; opt; '
                'dffunmap; sat -verify -seq 6 -tempinduct -maxsteps 16 '
                '-prove ok 1 -show-inputs'], cwd=work,
                capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_local_rom_addresses_match_the_microcode_pc_after_reset(self):
        source = self.cut_rom_lookup().replace('input wire [31:0] rom_payload,',
            'output wire [47:0] read_addresses, input wire [31:0] rom_payload,')
        source = source.replace('reg [11:0] read_address;',
            'reg [11:0] read_address; assign read_addresses[lane*12 +: 12] = read_address;')
        fixture = r"""
          module proof(input clk, reset, advance, input [11:0] next_addr, output ok);
            reg [11:0] pc;
            reg seen_reset = 0;
            always @(posedge clk) if (reset) seen_reset <= 1;
            always @(posedge clk) if (reset) pc <= 12'h020; else if (advance) pc <= next_addr;
            wire [47:0] addresses;
            mcl86_ucode dut(.clk(clk), .reset(reset), .advance(advance), .next_addr(next_addr),
              .rom_payload(32'b0), .read_addresses(addresses), .dout(),
              .operand_select0(), .operand_select1(), .add_select0(), .add_select1());
            assign ok = !seen_reset || addresses == {4{pc}};
          endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'rom.sv').write_text(source)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv rom.sv proof.sv; prep -top proof -flatten; opt; '
                'dffunmap; sat -verify -seq 2 -tempinduct -maxsteps 8 '
                '-prove ok 1 -show-inputs'],
                cwd=work, capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_microcode_next_address_matches_original_sequencer(self):
        source = (VENDOR / 'mcl86_eu_core.sv').read_text()
        block = source[source.index('    always_comb begin : next_microcode_pc'):
                       source.index('    mcl86_ucode #')]
        writes = re.findall(r'\beu_rom_address\s*<=\s*(.*?);', source, re.S)
        self.assertEqual([w.strip() for w in writes], ["13'h0020", 'eu_rom_address_next'])
        self.assertRegex(source, r'\.next_addr\s*\(\s*eu_rom_address_next\[11:0\]\s*\)')
        self.assertRegex(source, r'\.reset\s*\(\s*RESET_INT\s*\)')
        fixture = r"""
          module proof(input [12:0] eu_rom_address, input [51:0] eu_calling_address,
            input [15:0] eu_opcode_immediate, eu_biu_dataout, input [7:0] PFQ_TOP_BYTE,
            input eu_stall_pipeline, eu_jump_boolean, FES_MC_JUMP7_USED,
            input [2:0] eu_opcode_type, eu_opcode_jump_src, output ok);
            reg [12:0] eu_rom_address_next, expected;
            wire [7:0] eu_dispatch_byte = PFQ_TOP_BYTE;
        """ + block + r"""
            always @* begin
              if (!eu_stall_pipeline && eu_opcode_type == 3'h1 && eu_jump_boolean) begin
                case (eu_opcode_jump_src)
                  0: expected = eu_opcode_immediate[12:0];
                  1: expected = {4'b0, 1'b1, PFQ_TOP_BYTE};
                  2: expected = {eu_opcode_immediate[4:0], PFQ_TOP_BYTE[7:6], PFQ_TOP_BYTE[2:0], 3'b0};
                  3: expected = eu_calling_address[12:0];
                  4: expected = {eu_opcode_immediate[7:0], eu_biu_dataout[3:0], 1'b0};
                  5: expected = {eu_opcode_immediate[6:0], eu_biu_dataout[3:0], 2'b0};
                  6: expected = {eu_opcode_immediate[12:3], eu_biu_dataout[5:3]};
                  default: expected = FES_MC_JUMP7_USED ? eu_rom_address : eu_rom_address + 1'b1;
                endcase
              end else expected = eu_rom_address + 1'b1;
            end
            assign ok = eu_rom_address_next == expected;
          endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; sat -verify -prove ok 1'],
                cwd=work, capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_microcode_pc_stack_and_stall_match_original(self):
        source = (VENDOR / 'mcl86_eu_core.sv').read_text()
        start = source.index('            if (eu_stall_pipeline ==', source.index('// JUMP Opcode'))
        end = source.index('\n        end\n    end\n', start)
        actual = source[start:end]
        self.assertEqual(len(re.findall(r'\beu_stall_pipeline\s*<=', source)), 3)
        self.assertEqual(len(re.findall(r'\beu_calling_address(?:\[[^]]+\])?\s*<=', source)), 3)
        next_pc = source[source.index('    always_comb begin : next_microcode_pc'):
                         source.index('    mcl86_ucode #')]
        # Original sequential JUMP block, including overlapping call/return
        # nonblocking writes and the reserved-source hold used by this import.
        reference = r"""
          if (eu_stall_pipeline == 0 && eu_opcode_type == 1 && eu_jump_boolean == 1) begin
            eu_stall_pipeline <= 1;
            if (eu_opcode_jump_call)
              eu_calling_address[51:0] <= {eu_calling_address[38:0], eu_rom_address[12:0]};
            case (eu_opcode_jump_src)
              0: eu_rom_address <= eu_opcode_immediate[12:0];
              1: eu_rom_address <= {4'b0, 1'b1, PFQ_TOP_BYTE};
              2: eu_rom_address <= {eu_opcode_immediate[4:0], PFQ_TOP_BYTE[7:6], PFQ_TOP_BYTE[2:0], 3'b0};
              3: begin
                eu_rom_address <= eu_calling_address[12:0];
                eu_calling_address[38:0] <= eu_calling_address[51:13];
              end
              4: eu_rom_address <= {eu_opcode_immediate[7:0], eu_biu_dataout[3:0], 1'b0};
              5: eu_rom_address <= {eu_opcode_immediate[6:0], eu_biu_dataout[3:0], 2'b0};
              6: eu_rom_address <= {eu_opcode_immediate[12:3], eu_biu_dataout[5:3]};
              default: eu_rom_address <= FES_MC_JUMP7_USED ? eu_rom_address : eu_rom_address + 1'b1;
            endcase
          end else begin
            eu_stall_pipeline <= 0;
            eu_rom_address <= eu_rom_address + 1'b1;
          end
        """
        def module(name, body, next_logic=''):
            return f'module {name}' + r"""(input clk, RESET_INT,
              input [15:0] eu_opcode_immediate, eu_biu_dataout, input [7:0] PFQ_TOP_BYTE,
              input eu_jump_boolean, eu_opcode_jump_call, FES_MC_JUMP7_USED,
              input [2:0] eu_opcode_type, eu_opcode_jump_src,
              output reg [12:0] eu_rom_address, output reg [51:0] eu_calling_address,
              output reg eu_stall_pipeline, input eu_execute);
              wire [7:0] eu_dispatch_byte = PFQ_TOP_BYTE;
              reg [12:0] eu_rom_address_next;
            """ + next_logic + r"""
              always @(posedge clk) begin
                if (RESET_INT) begin
                  eu_rom_address <= 13'h0020;
                  eu_calling_address <= 0;
                  eu_stall_pipeline <= 0;
                end else if (eu_execute) begin
            """ + body + ' end end endmodule\n'
        fixture = module('reference', reference) + module('actual',
            'eu_rom_address <= eu_rom_address_next;\n' + actual, next_pc)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; proc; opt; equiv_make reference actual equiv; '
                'hierarchy -top equiv; equiv_simple; equiv_induct -seq 4; equiv_status -assert'],
                cwd=work, capture_output=True, text=True, timeout=60)
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
            self.assertIn(f"FES_MC_RESET_WORD = 32'h{words[0x20]:08x}", facts)
            self.assertEqual(len(words), 4096)
