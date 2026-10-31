"""Prove the real controller IO stage preserves samples with an earlier CS register.

Reference stage: controller bytes retained with the CPU100/board50 baseline.
The proof models event ordering, not propagation delays. Native setup/hold and
external inverter/board checks remain separate. Initial output values before
both final stages have loaded are outside this lemma.
"""
from contextlib import nullcontext
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
ABC = os.environ.get('YOSYS_ABC') or (str(Path(shutil.which(YOSYS) or YOSYS).resolve().with_name('yosys-abc')) if YOSYS else None)

GOLD_PARAMETERS = r"""
    // Preserve the memory tester's full-word writes unless explicitly enabled.
    parameter BYTE_MASK_ENABLED = 0,
    // 1 adds one plain register per SDRAM command/address pin, with no enable,
    // reset or other Q consumer, so the native flow packs it into the pad
    // (FAST_OUTPUT_REGISTER). Write data and DQ OE get the same extra cycle in
    // the fabric, and the read capture waits one cycle longer.
    parameter IO_OUTPUT_REGISTERS = 0,
    // Runtime refresh recovery only. Keep the established tester timing by
    // default; slower fixed-clock clients may select a qualified cycle count.
    // Initialization retains its conservative sixteen-clock recovery.
    parameter [13:0] REFRESH_WAIT_CYCLES = 14'd16,
    // Opt-in completion after read capture / committed write hold. Recovery
    // states still drain before the controller accepts another command.
    parameter EARLY_DONE = 0,
    // Rate 0 normally receives one fabric register after DDR input capture.
    // A caller supplying the raw DDR rising word selects zero; retain that
    // same physical sample, capturing it one fabric edge earlier.
    parameter RATE0_INPUT_REGISTER = 1,
    // Opt-in callers that never address the second SDRAM chip.
    parameter SINGLE_RANK_REFRESH = 0,
    parameter EXTERNAL_REFRESH_WINDOW = 0,
    parameter [11:0] REFRESH_INTERVAL_CYCLES = 12'd0
"""
GOLD_IO_STAGE = r"""    generate
        if (IO_OUTPUT_REGISTERS) begin : io_out
            // No initial value, enable or reset, and Q drives only the pad.
            // Power-up zero is never sampled: SDRAM_CLK first rises on the
            // falling edge after the first rising edge loads these.
            reg cke_p, ncs_p, nras_p, ncas_p, nwe_p, dqml_p, dqmh_p, dq_oe_p;
            reg [1:0] ba_p;
            reg [12:0] a_p;
            reg [15:0] dq_out_p;
            always @(posedge clk) begin
                cke_p <= sdram_cke_r;
                ncs_p <= sdram_ncs_r;
                nras_p <= sdram_nras_r;
                ncas_p <= sdram_ncas_r;
                nwe_p <= sdram_nwe_r;
                dqml_p <= sdram_dqml_r;
                dqmh_p <= sdram_dqmh_r;
                ba_p <= sdram_ba_r;
                a_p <= sdram_a_r;
                // DQ is bidirectional; the native packer keeps these in the
                // fabric, but they must stay aligned with the WRITE command.
                dq_out_p <= dq_out_c;
                dq_oe_p <= dq_oe_r;
            end
            assign {sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe} = {cke_p, ncs_p, nras_p, ncas_p, nwe_p};
            assign {sdram_dqml, sdram_dqmh, sdram_ba, sdram_a} = {dqml_p, dqmh_p, ba_p, a_p};
            assign dq_out = dq_out_p;
            assign dq_oe = dq_oe_p;
        end else begin : direct_out
            assign {sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe} =
                {sdram_cke_r, sdram_ncs_r, sdram_nras_r, sdram_ncas_r, sdram_nwe_r};
            assign {sdram_dqml, sdram_dqmh, sdram_ba, sdram_a} = {sdram_dqml_r, sdram_dqmh_r, sdram_ba_r, sdram_a_r};
            assign dq_out = dq_out_c;
            assign dq_oe = dq_oe_r;
        end
    endgenerate"""

def stage(parameters, block, name):
    if block.count('always @(posedge clk)') != 1:
        raise ValueError('controller IO stage clock structure changed')
    block = block.replace('always @(posedge clk)', 'always @(posedge clk) if (board_step)')
    return 'module ' + name + ' #(' + parameters + r''')(
 input clk, board_step, raw_cs, input [37:0] other_raw, output [38:0] pins);
 wire sdram_cke_r,sdram_ncs_r,sdram_nras_r,sdram_ncas_r,sdram_nwe_r,sdram_dqml_r,sdram_dqmh_r,dq_oe_r;
 wire [1:0] sdram_ba_r; wire[12:0] sdram_a_r; wire[15:0] dq_out_c;
 assign sdram_ncs_r=raw_cs;
 assign {sdram_cke_r,sdram_nras_r,sdram_ncas_r,sdram_nwe_r,sdram_dqml_r,sdram_dqmh_r,sdram_ba_r,sdram_a_r,dq_out_c,dq_oe_r}=other_raw;
 wire sdram_cke,sdram_ncs,sdram_nras,sdram_ncas,sdram_nwe,sdram_dqml,sdram_dqmh,dq_oe;
 wire[1:0] sdram_ba; wire[12:0]sdram_a; wire[15:0]dq_out;
 assign pins={sdram_ncs,sdram_cke,sdram_nras,sdram_ncas,sdram_nwe,sdram_dqml,sdram_dqmh,sdram_ba,sdram_a,dq_out,dq_oe};
''' + block + '\nendmodule\n'


def external_cs_stage():
    """Project the actual top-level final register onto the early clock event."""
    source = (ROOT / 'cores/fes-pcxt/rtl/fes_pcxt_top.sv').read_text()
    start = source.index('    generate if (HALF_RATE_BOARD) begin: early_chip_select')
    end = source.index('    end endgenerate', start) + len('    end endgenerate')
    block = re.sub(r'//[^\n]*', '', source[start:end])
    match = re.fullmatch(r'''\s*generate\s+if\s*\(HALF_RATE_BOARD\)\s+begin:\s*early_chip_select
        \s*(reg\s+cs;\s*always\s*@\(posedge\s+clocks\[3\]\)\s+cs\s*<=\s*controller_cs;)
        \s*assign\s+SDRAM_nCS\s*=\s*cs;
        \s*end\s+else\s+begin:\s*common_chip_select
        \s*assign\s+SDRAM_nCS\s*=\s*controller_cs;
        \s*end\s+endgenerate\s*''', block, re.VERBOSE)
    if match is None:
        raise ValueError('top-level final CS stage changed; review the projection')
    result = match[1].replace('clocks[3]', 'clk')
    result = re.sub(r'\bcs\b', 'early_cs', result).replace('controller_cs', 'b[38]')
    return result.replace('always @(posedge clk)', 'always @(posedge clk) if (early_step)')


def fixture(early=True, missing_stage=False):
    source = (ROOT / 'cores/fes-ramtest/rtl/sdram_addon_port.v').read_text()
    start = source.index('    generate\n        if (IO_OUTPUT_REGISTERS)')
    end = source.index('    endgenerate', start) + len('    endgenerate')
    parameters = source[source.index('#(')+2:source.index(') (', source.index('#('))]
    result = stage(GOLD_PARAMETERS, GOLD_IO_STAGE, 'gold') + stage(parameters, source[start:end], 'gate')
    result += r'''
module proof(input clk, choose_cs, input[37:0] other_raw, output bad);
 reg[1:0] phase=0; reg raw_cs=0; reg seen_board=0,seen_early=0;
 wire board_step=phase==0; wire chip_step=phase==1; wire early_step=phase==3;
 wire[38:0] a,b;
 __EARLY_STAGE__
 always@(posedge clk)begin
   phase<=phase+1'b1;
   if(board_step)begin raw_cs<=choose_cs;seen_board<=1;end
   if(early_step)seen_early<=1;
 end
 gold #(.IO_OUTPUT_REGISTERS(1)) x(clk,board_step,raw_cs,other_raw,a);
 gate #(.IO_OUTPUT_REGISTERS(1)__PARAM__) y(clk,board_step,raw_cs,other_raw,b);
 wire[38:0] actual=__ACTUAL__;
 assign bad=seen_board && __CHECK__ && a!=actual;
endmodule
'''
    return (result.replace('__EARLY_STAGE__', external_cs_stage())
            .replace('__PARAM__', ',.EXTERNAL_CS_REGISTER(1)' if early else '')
            .replace('__ACTUAL__', '{b[38],b[37:0]}' if missing_stage else
                     '{early_cs,b[37:0]}' if early else 'b')
            .replace('__CHECK__', '(seen_early && chip_step)' if early else "1'b1"))


def prove(early=True, missing_stage=False, output=None):
    if output is not None:
        Path(output).mkdir(parents=True, exist_ok=False)
    context = nullcontext(str(output)) if output is not None else tempfile.TemporaryDirectory(prefix='pcxt-cs-stage-')
    with context as directory:
        work=Path(directory)
        (work/'proof.sv').write_text(fixture(early, missing_stage))
        command = ('read_verilog -formal -sv proof.sv; prep -top proof -flatten; opt; '
                   'techmap; opt; dffunmap; simplemap; setundef -undriven -anyseq; '
                   'aigmap; write_json proof.json; write_aiger -zinit proof.aig')
        result = subprocess.run([YOSYS,'-Q','-p',command],cwd=work,capture_output=True,text=True,timeout=60)
        (work/'yosys.log').write_text(result.stdout+result.stderr)
        if result.returncode:
            return result
        model=json.loads((work/'proof.json').read_text())
        if model['modules']['proof']['ports']['bad']['bits']==['0']:
            return subprocess.CompletedProcess(['constant-bad-check'],0,'Bad output is identically zero.','')
        result=subprocess.run([ABC,'-c','read_aiger proof.aig; pdr'],cwd=work,capture_output=True,text=True,timeout=60)
        (work/'abc.log').write_text(result.stdout+result.stderr)
        return result


@unittest.skipUnless(YOSYS and ABC and Path(ABC).is_file(), 'requires pinned Yosys and yosys-abc')
class ChipSelectStageProofTests(unittest.TestCase):
    def test_default_stage_preserves_all_command_address_data_and_oe_samples(self):
        result=prove(early=False)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertTrue('identically zero' in result.stdout or 'Property proved.' in result.stdout,result.stdout)

    def test_earlier_cs_keeps_every_chip_capture_value(self):
        result=prove()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertIn('Property proved.',result.stdout)

    def test_missing_external_register_produces_counterexample(self):
        result=prove(missing_stage=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertIn('was asserted in frame',result.stdout)


if __name__=='__main__':
    unittest.main()
