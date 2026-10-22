"""Exercise the board's native ROM clock mapping with a real Yosys frontend."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class RomClockMapTest(unittest.TestCase):
    def test_only_disabled_write_clock_is_rebound(self):
        fixture = '''module top(input clk, wrclk, enable, input [1:0] a, input [1:0] d,
                             output [1:0] rom_q, ram_q, enabled_q);
          MISTRAL_M10K #(.CFG_ABITS(2), .CFG_DBITS(2), .CFG_DUAL_CLOCK(1), .INIT(8'hb9)) rom
            (.CLK1(1'bx), .CLK2(clk), .A1ADDR(a), .B1ADDR(a), .A1DATA(d), .A1EN(1'b1), .B1EN(1'b1), .B1DATA(rom_q));
          MISTRAL_M10K #(.CFG_ABITS(2), .CFG_DBITS(2), .CFG_DUAL_CLOCK(1)) ram
            (.CLK1(wrclk), .CLK2(clk), .A1ADDR(a), .B1ADDR(a), .A1DATA(d), .A1EN(enable), .B1EN(1'b1), .B1DATA(ram_q));
          MISTRAL_M10K #(.CFG_ABITS(2), .CFG_DBITS(2), .CFG_DUAL_CLOCK(1)) enabled
            (.CLK1(1'bx), .CLK2(clk), .A1ADDR(a), .B1ADDR(a), .A1DATA(d), .A1EN(1'b0), .B1EN(1'b1), .B1DATA(enabled_q));
        endmodule
        (* blackbox *) module MISTRAL_M10K(input CLK1, CLK2,
          input [CFG_ABITS-1:0] A1ADDR, B1ADDR, input [CFG_DBITS-1:0] A1DATA,
          input A1EN, B1EN, output [CFG_DBITS-1:0] B1DATA);
          parameter CFG_ABITS=2, CFG_DBITS=2, CFG_DUAL_CLOCK=1, CFG_BYTE_ENABLE=0,
            CFG_ASYNC_READ=0, CFG_MIXED_WIDTH=0, CFG_RD_ABITS=CFG_ABITS, CFG_RD_DBITS=CFG_DBITS, INIT=0;
        endmodule'''
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'fixture.v').write_text(fixture)
            command = ('read_verilog fixture.v; hierarchy -top top; write_json before.json; '
                       'techmap -map ' + str(ROOT / 'cores/fes-pcxt/synth/rom_clock_map.v') +
                       '; opt_clean; check -assert; write_json after.json')
            subprocess.run([YOSYS, '-Q', '-p', command], cwd=work, check=True, capture_output=True)
            before = json.loads((work / 'before.json').read_text())['modules']['top']['cells']
            after = json.loads((work / 'after.json').read_text())['modules']['top']['cells']
        self.assertEqual(set(before), set(after))
        for name in before:
            # Every ROM data/address/enable/output connection and INIT survives;
            # writable cells keep both clocks and all their controls unchanged.
            expected = dict(before[name]['connections'])
            if name == 'rom':
                expected['CLK1'] = expected['CLK2']
            self.assertEqual(after[name]['connections'], expected, name)
            for key, value in before[name]['parameters'].items():
                self.assertEqual(after[name]['parameters'][key], value, (name, key))
