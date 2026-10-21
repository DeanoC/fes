"""Check async_reg intent preservation without changing the mapped hardware."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from build_fes_pcxt import SYNTHESIS, synthesis_command

YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class ResetMetadataTest(unittest.TestCase):
    def test_async_intent_reaches_ffs_without_logic_changes(self):
        # Reset chains plus pcxt_video's toggle-mailbox and level crossings.
        # The complete board comparison is docs/validation/2026-10-21-pcxt-reset-metadata.
        fixture = '''module top(input clk, pixel_clk, locked, publish, hold, output rst, prst,
                                output adopt, held, input [1:0] addr, input [7:0] din, input we,
                                output [7:0] dout);
          (* async_reg="true" *) reg [1:0] sys_reset=2'b11;
          (* async_reg="true" *) reg [1:0] pixel_reset=2'b11;
          always @(posedge clk or negedge locked)
            if (!locked) sys_reset<=2'b11; else sys_reset<={sys_reset[0],1'b0};
          always @(posedge pixel_clk or negedge locked)
            if (!locked) pixel_reset<=2'b11; else pixel_reset<={pixel_reset[0],1'b0};
          assign rst=sys_reset[1]; assign prst=pixel_reset[1];
          reg sent, received;
          (* async_reg="true" *) reg ack_meta, ack_sync, sent_meta, sent_sync, hold_meta, hold_sync;
          always @(posedge clk)
            if (rst) begin sent<=0; ack_meta<=0; ack_sync<=0; end
            else begin ack_meta<=received; ack_sync<=ack_meta; if (publish && ack_sync==sent) sent<=!sent; end
          always @(posedge pixel_clk)
            if (prst) begin received<=0; sent_meta<=0; sent_sync<=0; hold_meta<=0; hold_sync<=0; end
            else begin
              sent_meta<=sent; sent_sync<=sent_meta; hold_meta<=hold; hold_sync<=hold_meta;
              if (adopt) received<=sent_sync;
            end
          assign adopt=sent_sync!=received; assign held=hold_sync;
          reg [7:0] mem [0:3];
          always @(posedge clk) if (we) mem[addr]<=din;
          assign dout=mem[addr];
        endmodule'''
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'fixture.v').write_text(fixture)
            results = []
            for command in (SYNTHESIS, synthesis_command()):
                subprocess.run(
                    [YOSYS, '-Q', '-p', 'read_verilog fixture.v; ' + command +
                     '; check -assert; write_json mapped.json'],
                    cwd=work, check=True, capture_output=True)
                results.append(json.loads((work / 'mapped.json').read_text())['modules']['top'])
        before, after = results
        marked = {name: cell for name, cell in after['cells'].items()
                  if cell['attributes'].get('async_reg') == 'true'}
        self.assertEqual(len(marked), 10)
        self.assertTrue(all(cell['type'] == 'MISTRAL_FF' for cell in marked.values()))
        self.assertGreater(sum(cell['type'] == 'MISTRAL_FF'
                               for cell in after['cells'].values()), 10)
        for module in results:
            for cell in module['cells'].values():
                cell['attributes'].pop('async_reg', None)
        # Includes all nets, initialization, parameters, controls and polarity.
        self.assertEqual(before, after)
