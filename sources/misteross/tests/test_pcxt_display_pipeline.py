"""Prove renderer stream retiming and the unchanged MDA status-read cycle."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
REFERENCE_RENDERER = r"""    wire [3:0] gold_glyph_dot = MDA || mode[0] ? dot : {1'b0, dot[3:1]};
    wire gold_glyph = gold_glyph_dot < 8 ? font_data[3'd7-gold_glyph_dot[2:0]] :
                 MDA && video_word[7:5] == 3'b110 && font_data[0];
    wire [7:0] gold_attribute = video_word[15:8];
    wire gold_blink_hidden = mode[5] && gold_attribute[7] && blink_frames[4];
    wire [15:0] gold_graphics_word = {video_word[7:0], video_word[15:8]};
    wire [1:0] gold_pair_colour = 2'(gold_graphics_word >> (4'd14-{dot[3:1],1'b0}));
    reg [3:0] gold_pixel;
    reg gold_ink;
    always @* begin
        gold_pixel = !MDA && !mode[0] && !mode[4] ? colour[3:0] : 4'b0;
        gold_ink = 0;
        if (crtc_de) begin
            if (!MDA && mode[1]) begin
                if (mode[4]) gold_pixel = gold_graphics_word[4'd15-dot] ? colour[3:0] : 4'b0;
                else if (gold_pair_colour == 0) gold_pixel = colour[3:0];
                else if (mode[2]) begin
                    case (gold_pair_colour)
                        1: gold_pixel = {colour[4],3'd3};
                        2: gold_pixel = {colour[4],3'd4};
                        default: gold_pixel = {colour[4],3'd7};
                    endcase
                end else gold_pixel = {colour[4], gold_pair_colour[1], gold_pair_colour[0], colour[5]};
            end else if (MDA) begin
                gold_ink = (gold_glyph || (gold_attribute[2:0] == 1 && gold_attribute[6:4] == 0 && ra == 13)) && !gold_blink_hidden;
                // 00/08/80/88 invisible; 70/78/F0/F8 reverse, remaining attributes normal.
                if (gold_attribute[6:4] == 7 && gold_attribute[2:0] == 0)
                    gold_pixel = gold_ink ? 4'b0 : 4'd7;
                else if (gold_attribute[6:4] == 0 && gold_attribute[2:0] == 0) gold_pixel = 0;
                else gold_pixel = gold_ink ? (gold_attribute[3] ? 4'd15 : 4'd7) : 4'b0;
                if (cursor) gold_pixel = gold_attribute[3] ? 4'd15 : 4'd7;
            end else begin
                gold_pixel = (gold_glyph && !gold_blink_hidden) || cursor ? gold_attribute[3:0] :
                         {gold_attribute[7] && !mode[5], gold_attribute[6:4]};
            end
        end
        if (!mode[3]) gold_pixel = 0;
    end
"""

@unittest.skipUnless(YOSYS, 'requires Yosys')
class DisplayPipelineTest(unittest.TestCase):
    def prove(self, mda, mutation=False):
        source = (ROOT / 'cores/fes-pcxt/expansions/pcxt_display_card.sv').read_text()
        self.assertIn('if (MDA) io_data = {4\'hf, status_video, 2\'b00, crtc_hs};', source)
        block = source[source.index('    reg [38:0] render_inputs;'):
                       source.index('    always @(posedge clk) begin\n        if (clear) begin\n            phase')]
        self.assertIn('    reg [7:0] render_font;', block)
        self.assertIn('        render_font <= font_data;', block)
        if mutation:
            block = block.replace('sof <= render_sof;', 'sof <= first_pixel;')
        fixture = r"""
module proof(input clk, clear, dot_ce, first_pixel, line_end,
 input [15:0] video_word, input [7:0] font_data,
 input [3:0] dot, last_dot, input [4:0] ra, blink_frames,
 input [5:0] mode, colour, input cursor, crtc_de, crtc_hs, crtc_vs, output ok);
 localparam MDA = MDA_VALUE;
 reg pixel_ce, de, hs, vs, sof, eol; reg [3:0] rgbi;
""".replace('MDA_VALUE', str(mda)) + block + REFERENCE_RENDERER + r"""
 reg seen_reset = 0;
 reg gold_ce, gold_de, gold_hs, gold_vs, gold_sof, gold_eol;
 reg [3:0] gold_rgbi;
 reg [9:0] expected;
 always @(posedge clk) begin
   if (clear) begin
     seen_reset <= 1;
     gold_ce<=0;gold_de<=0;gold_hs<=0;gold_vs<=0;gold_sof<=0;gold_eol<=0;gold_rgbi<=0;
     expected <= 0;
   end else begin
     expected <= {gold_ce,gold_de,gold_hs,gold_vs,gold_sof,gold_eol,gold_rgbi};
     gold_ce <= dot_ce; gold_sof<=0; gold_eol<=0;
     if (dot_ce) begin
       gold_rgbi<=gold_pixel; gold_de<=crtc_de;gold_hs<=crtc_hs;gold_vs<=crtc_vs;
       gold_sof<=first_pixel;gold_eol<=dot==last_dot && line_end;
     end
   end
 end
 assign ok = !seen_reset || ({pixel_ce,de,hs,vs,sof,eol,rgbi}==expected &&
                            (!MDA || status_video == (gold_rgbi != 0)));
endmodule
"""
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory);(work/'proof.sv').write_text(fixture)
            return subprocess.run([YOSYS,'-Q','-p',
                'read_verilog -sv proof.sv; prep -top proof -flatten; opt; dffunmap; '
                'sat -verify -seq 4 -tempinduct -maxsteps 16 -prove ok 1 -show-inputs'],
                cwd=work,capture_output=True,text=True,timeout=90)

    def test_cga_and_mda_all_inputs_resets_and_markers(self):
        for mda in (0,1):
            with self.subTest(mda=mda):
                result=self.prove(mda)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_rejects_undelayed_frame_marker(self):
        result=self.prove(0,mutation=True)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('proof did fail',result.stdout+result.stderr)

    def test_prepared_write_eligibility_matches_every_committed_request(self):
        source = (ROOT / 'cores/fes-pcxt/expansions/pcxt_display_card.sv').read_text()
        block = source[source.index('    reg mem_write_eligible, mem_write_high;'):
                       source.index('    always @(posedge clk) begin\n        cpu_word')]
        self.assertIn('if (commit && mem_write_eligible)',source)
        self.assertIn('if (mem_write_high)',source)
        for mda in (0,1):
            fixture=r"""
module proof(input clk, reset, input [47:0] request, output ok);
 localparam MDA=MDA_VALUE;
 wire active,commit;
 wire [47:0] access_request;
 wire [19:0] address=access_request[19:0];
 wire [1:0] kind=access_request[29:28];
 wire writing=access_request[30];
 wire mem_select=active && kind==0 && address[19:15]==(MDA?5'b10110:5'b10111);
 pcxt_isa_endpoint endpoint(.clk(clk),.reset(reset),.request(request),.response(),
  .device_valid(active),.device_request(access_request),.device_claim(1'b1),
  .device_ready(1'b1),.device_rdata(8'b0),.device_commit(commit),.irq(8'b0),.drq(3'b0));
""".replace('MDA_VALUE',str(mda))+block+r"""
 reg seen_reset=0;
 always @(posedge clk) if(reset)seen_reset<=1;
 assign ok=!seen_reset || !commit || (mem_write_eligible==(mem_select&&writing) &&
                                     mem_write_high==address[0]);
endmodule
"""
            with tempfile.TemporaryDirectory() as directory:
                work=Path(directory);(work/'proof.sv').write_text(fixture)
                result=subprocess.run([YOSYS,'-Q','-p',
                    'read_verilog -sv -I '+str(ROOT/'cores/fes-common/generated')+' '+
                    str(ROOT/'cores/fes-pcxt/rtl/pcxt_isa_endpoint.sv')+' proof.sv; '
                    'prep -top proof -flatten; opt; dffunmap; '
                    'sat -verify -seq 8 -tempinduct -maxsteps 20 -prove ok 1 -show-inputs'],
                    cwd=work,capture_output=True,text=True,timeout=90)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
