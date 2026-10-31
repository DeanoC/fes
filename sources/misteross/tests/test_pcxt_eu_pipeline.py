"""Check the actual two-phase EU boundary, ROM holds and full-rate completion."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / 'cores/fes-pcxt/vendor/mcl86'
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys')
class EuPipelineTest(unittest.TestCase):
    def prove(self, source, *extra):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(source)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv ' + ' '.join(map(str, extra)) + ' proof.sv; '
                'prep -top proof -flatten; opt; dffunmap; '
                'sat -verify -seq 4 -tempinduct -maxsteps 12 -prove ok 1 -show-inputs'],
                cwd=work, capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_operand_read_execute_and_alu_for_arbitrary_data_and_resets(self):
        source = (VENDOR / 'mcl86_eu_core.sv').read_text()
        stage = source[source.index('    always_ff @(posedge CORE_CLK_INT) begin : eu_operand_stage'):
                       source.index('    reg  [12:0] eu_rom_address;')]
        alu = source[source.index('    assign eu_alu2 ='):
                     source.index('    //--------------------------------------------------------------------------\n    // 16-bit Adder')]
        adder = re.search(r'    mcl86_adder u_adder \(.*?    \);', source, re.S)[0]
        # Bind the property to the live consumers and to the ROM enable; changing
        # only the latches would otherwise leave a bypassed adder untested.
        self.assertRegex(adder, r'\.a\s*\(eu_execute_add_operand0\)')
        self.assertRegex(adder, r'\.b\s*\(eu_execute_add_operand1\)')
        self.assertIn('.RESET_WORD(FES_MC_RESET_WORD)', source)
        self.assertRegex(source, r'\.advance\s*\(\s*eu_execute\s*\)')
        self.assertRegex(source, r'else if \(eu_execute\) begin\s+eu_rom_address <= eu_rom_address_next;')
        fixture = r'''
module proof(input CORE_CLK_INT, RESET_INT, input [7:0] PFQ_TOP_BYTE,
  input [15:0] eu_operand0, eu_operand1, eu_add_operand0, eu_add_operand1, eu_flags,
  input [2:0] eu_opcode_type, input FAKE286_FLAGS, eu_pushf_serialize, output ok);
  reg eu_execute; reg [7:0] eu_dispatch_byte;
  reg [15:0] eu_execute_operand0, eu_execute_operand1;
  reg [15:0] eu_execute_add_operand0, eu_execute_add_operand1;
  wire EU_BIU_STROBE;
  wire [15:0] adder_out, eu_alu2, eu_alu3, eu_alu4, eu_alu5, eu_alu6, eu_alu7, eu_alu_logic, eu_alu_out;
  wire [16:0] carry;
''' + stage + alu + adder + r'''
  reg seen_reset = 0, phase;
  reg [7:0] byte_at_read;
  reg [15:0] left, right, add_left, add_right;
  always @(posedge CORE_CLK_INT) begin
    if (RESET_INT) begin
      seen_reset <= 1;
      phase <= 0; byte_at_read <= 0;
      left <= 0; right <= 0; add_left <= 0; add_right <= 0;
    end else begin
      phase <= !phase;
      if (phase == 0) begin
        left <= eu_operand0; right <= eu_operand1;
        add_left <= eu_add_operand0; add_right <= eu_add_operand1;
        byte_at_read <= PFQ_TOP_BYTE;
      end
    end
  end
  wire [15:0] sum = add_left + add_right;
  wire [16:0] expected_carry;
  assign expected_carry[0] = 0;
  for (genvar bitno=1; bitno<=16; bitno=bitno+1) begin: carries
    wire [bitno:0] partial_sum = {1'b0,add_left[bitno-1:0]} + {1'b0,add_right[bitno-1:0]};
    assign expected_carry[bitno] = partial_sum[bitno];
  end
  wire [15:0] expected = eu_opcode_type == 2 ? sum :
    eu_opcode_type == 3 ? {left[7:0],left[15:8]} :
    eu_opcode_type == 4 ? left & right :
    eu_opcode_type == 5 ? ((FAKE286_FLAGS && eu_pushf_serialize) ?
      {4'b0,eu_flags[11:2],1'b1,eu_flags[0]} : left | right) :
    eu_opcode_type == 6 ? left ^ right : {1'b0,left[15:1]};
  assign ok = !seen_reset || (eu_execute == phase &&
    EU_BIU_STROBE == (!RESET_INT && !phase) && eu_dispatch_byte == byte_at_read &&
    eu_execute_operand0 == left && eu_execute_operand1 == right &&
    eu_execute_add_operand0 == add_left && eu_execute_add_operand1 == add_right &&
    eu_alu_out == expected && carry == expected_carry);
endmodule
'''
        self.prove(fixture, VENDOR / 'mcl86_adder.sv')

    def test_rom_word_and_all_controls_hold_and_advance_together(self):
        source = (VENDOR / 'mcl86_ucode.sv').read_text()
        source = source.replace('input wire clk,',
            'input wire clk, input wire [31:0] payload, output wire [31:0] raw_payload,')
        source = source.replace('logic [31:0] data_half;',
            'logic [31:0] data_half; assign raw_payload = data_half;')
        source, count = re.subn(r'initial begin.*?end\n', '', source, flags=re.S)
        self.assertEqual(count, 1)
        source, count = re.subn(r'logic \[7:0\] rom\[0:4095\];', '', source)
        self.assertEqual(count, 1)
        source, count = re.subn(r'rom\[read_address\]', 'payload[lane*8 +: 8]', source)
        self.assertEqual(count, 1)
        reset_word = int((VENDOR / 'mcl86_ucode.mem').read_text().split()[0x20], 16)
        fixture = source + r'''
module proof(input clk, reset, advance, input [31:0] payload, output ok);
  localparam [31:0] EXPECTED_RESET = RESET_VALUE;
  wire [31:0] raw_payload, word;
  wire [15:0] s0,s1,a0,a1;
  mcl86_ucode #(.RESET_WORD(EXPECTED_RESET)) dut(.clk(clk), .reset(reset), .advance(advance),
    .payload(payload), .raw_payload(raw_payload), .next_addr(12'b0), .dout(word),
    .operand_select0(s0), .operand_select1(s1), .add_select0(a0), .add_select1(a1));
  reg sampled = 0;
  reg [31:0] expected;
  always @(posedge clk) if (reset || advance) begin
    sampled <= 1;
    expected <= reset ? EXPECTED_RESET : raw_payload;
  end
  assign ok = !sampled || (word == expected &&
    s0 == (16'h1 << expected[23:20]) && s1 == (16'h1 << expected[19:16]) && a0==s0 && a1==s1);
endmodule
'''
        self.prove(fixture.replace('RESET_VALUE', f"32'h{reset_word:08x}"))

    def test_biu_strobe_is_masked_and_completion_captures_every_clock(self):
        eu = (VENDOR / 'mcl86_eu_core.sv').read_text()
        biu = (VENDOR / 'mcl86_biu_max.sv').read_text()
        wrapper = (VENDOR / 'i8088.sv').read_text()
        self.assertEqual(len(re.findall(r'\.EU_BIU_STROBE\s*\(\s*t_eu_biu_strobe\s*\)', wrapper)), 2)
        strobe = re.search(r'    assign eu_biu_strobe\[1:0\].*?;', biu, re.S)[0]
        capture = eu[eu.index('    always_ff @(posedge CORE_CLK_INT) begin : eu_biu_completion_stage'):
                     eu.index('    always_ff @(posedge CORE_CLK_INT) begin : proc_eu_microsequencer')]
        # The sticky completion must not depend on read/execute phase. Pulses
        # and request cancellation may occur on either physical clock.
        self.assertNotIn('eu_execute', capture)
        self.prove(r'''
module proof(input CORE_CLK_INT, RESET_INT, BIU_DONE, eu_biu_req, EU_BIU_STROBE,
  input [15:0] EU_BIU_COMMAND, output ok);
  wire [1:0] eu_biu_strobe;
  reg biu_done_d1, biu_done_d2, eu_biu_req_d1, biu_done_caught;
''' + strobe + capture + r'''
  reg seen_reset = 0, done_previous, done_older, request_previous, pending;
  always @(posedge CORE_CLK_INT) begin
    if (RESET_INT) begin
      seen_reset <= 1; done_previous <= 0; done_older <= 0;
      request_previous <= 0; pending <= 0;
    end else begin
      done_previous <= BIU_DONE; done_older <= done_previous;
      request_previous <= eu_biu_req;
      if (done_previous && !done_older) pending <= 1;
      else if (request_previous && !eu_biu_req) pending <= 0;
    end
  end
  assign ok = (eu_biu_strobe == (EU_BIU_STROBE ? EU_BIU_COMMAND[13:12] : 2'b0)) &&
    (!seen_reset || (biu_done_caught == pending && biu_done_d1 == done_previous &&
      biu_done_d2 == done_older && eu_biu_req_d1 == request_previous));
endmodule
''')
