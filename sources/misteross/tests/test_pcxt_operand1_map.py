"""Prove the native operand mux map, including non-one-hot select vectors."""
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
class MuxMapTest(unittest.TestCase):
    def test_native_truth_tables_and_two_level_data_paths(self):
        models = Path(shutil.which(YOSYS) or YOSYS).resolve().parents[1] / 'share/yosys/intel_alm/common'
        if not (models / 'alm_sim.v').is_file():
            self.skipTest('requires the selected Yosys Intel ALM simulation models')
        fixture = r"""
          module proof(input [15:0] sel, input [255:0] data, output ok);
            wire [15:0] actual;
            reg [15:0] expected;
            integer i;
            always @* begin
              expected = 0;
              for (i=0; i<16; i=i+1)
                expected = expected | (data[i*16 +: 16] & {16{sel[i]}});
            end
            pcxt_logic_operand1_mux dut(.sel(sel), .data(data), .out(actual));
            assign ok = actual == expected;
          endmodule
        """
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            command = ('read_verilog -sv ' + str(ROOT / 'cores/fes-pcxt/rtl/pcxt_logic_operand1_mux.sv') +
                       ' proof.sv; hierarchy -top proof; proc; techmap -map ' +
                       str(ROOT / 'cores/fes-pcxt/synth/logic_operand1_map.v') + '; '
                       'read_verilog -sv ' + str(models / 'alm_sim.v') + ' ' + str(models / 'misc_sim.v') +
                       '; write_json mapped.json; '
                       'setattr -mod -unset keep_hierarchy pcxt_logic_operand1_mux; '
                       'prep -top proof -flatten; check -assert; '
                       'sat -verify -prove ok 1 -show-inputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            module = json.loads((work / 'mapped.json').read_text())['modules']['proof']
            cells = module['cells']
            self.assertEqual(sum(c['type'] == 'MISTRAL_ALUT6' for c in cells.values()), 96)
            self.assertEqual(sum(c['type'] == 'MISTRAL_ALUT2' for c in cells.values()), 16)
            drivers = {}
            for name, cell in cells.items():
                for port, direction in cell['port_directions'].items():
                    if direction == 'output':
                        for bit in cell['connections'][port]:
                            if isinstance(bit, int):
                                drivers[bit] = name
            def depth(bit):
                if bit not in drivers:
                    return 0
                cell = cells[drivers[bit]]
                return 1 + max((depth(b) for p, d in cell['port_directions'].items()
                                if d == 'input' for b in cell['connections'][p]), default=0)
            self.assertEqual({depth(b) for b in module['netnames']['actual']['bits']}, {2})
