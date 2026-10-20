"""Check reset intent preservation without changing the mapped hardware."""
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
from build_fes_pcxt import synthesis_command

YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class ResetMetadataTest(unittest.TestCase):
    def test_async_intent_reaches_ffs_without_logic_changes(self):
        fixture = '''module top(input clk, pixel_clk, locked, output rst, prst);
          (* async_reg="true" *) reg [1:0] sys_reset=2'b11;
          (* async_reg="true" *) reg [1:0] pixel_reset=2'b11;
          always @(posedge clk or negedge locked)
            if (!locked) sys_reset<=2'b11; else sys_reset<={sys_reset[0],1'b0};
          always @(posedge pixel_clk or negedge locked)
            if (!locked) pixel_reset<=2'b11; else pixel_reset<={pixel_reset[0],1'b0};
          assign rst=sys_reset[1]; assign prst=pixel_reset[1];
        endmodule'''
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'fixture.v').write_text(fixture)
            results = []
            for command in ('synth_intel_alm -nolutram -nodsp -top top',
                            synthesis_command()):
                subprocess.run(
                    [YOSYS, '-Q', '-p', 'read_verilog fixture.v; ' + command +
                     '; check -assert; write_json mapped.json'],
                    cwd=work, check=True, capture_output=True)
                results.append(json.loads((work / 'mapped.json').read_text())['modules']['top'])
        before, after = results
        marked = {name: cell for name, cell in after['cells'].items()
                  if cell['attributes'].get('async_reg') == 'true'}
        self.assertEqual(len(marked), 4)
        self.assertTrue(all(cell['type'] == 'MISTRAL_FF' for cell in marked.values()))
        self.assertEqual(set(marked),
                         {name for name, cell in after['cells'].items()
                          if cell['type'] == 'MISTRAL_FF'})
        for module in results:
            for cell in module['cells'].values():
                cell['attributes'].pop('async_reg', None)
        # Includes all nets, initialization, parameters, controls and polarity.
        self.assertEqual(before, after)
