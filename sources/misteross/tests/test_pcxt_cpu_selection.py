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
        expressions = '\n'.join(re.search(r'    assign eu_operand' + str(i) + r' = select16\(.*?;',
                                           source, re.S).group(0) for i in (0, 1))
        ports = ', '.join('input [' + ('7:0' if n == 'PFQ_TOP_BYTE' else '15:0') + '] ' + n
                          for n in DATA_NAMES)
        # Follow the real ROM-instance bindings as well as the real operand
        # expressions, so swapping the two control ports also fails the proof.
        bindings = [re.search(r'\.operand_select' + str(i) + r'\s*\(\s*(\w+)\s*\)',
                              source).group(1) for i in (0, 1)]
        controls = (f"wire [15:0] {bindings[0]} = 16'h1 << eu_rom_sel[7:4];\n"
                    f"wire [15:0] {bindings[1]} = 16'h1 << eu_rom_sel[3:0];\n")
        fixture = 'module proof(input [7:0] eu_rom_sel, ' + ports + ', output ok);\n' + controls + r"""
          wire [15:0] eu_operand0, eu_operand1, expected0, expected1;
        """ + function + REFERENCE + expressions + r"""
          assign ok = eu_operand0 == expected0 && eu_operand1 == expected1;
        endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; '
                'sat -verify -prove ok 1 -show-inputs'], cwd=work, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_all_rom_addresses_keep_contents_and_registered_select_latency(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            inputs.cpu_roms(work)
            (work / 'proof.sv').write_text(r"""
              module proof(input clk, input [11:0] addr, output ok);
                wire [31:0] word;
                wire [15:0] s0, s1;
                mcl86_ucode dut(.clk(clk), .addr(addr), .dout(word),
                    .operand_select0(s0), .operand_select1(s1));
                assign ok = s0 == (16'h1 << word[23:20]) && s1 == (16'h1 << word[19:16]);
              endmodule
            """)
            command = ('read_verilog -sv ' + str(VENDOR / 'mcl86_ucode.sv') +
                       ' proof.sv; prep -top proof -flatten; memory_map; opt; '
                       'sat -verify -seq 2 -prove-skip 1 -prove ok 1 -show-inputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
