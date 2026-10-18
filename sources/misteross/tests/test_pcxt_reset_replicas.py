"""Check actual reset bindings and the unchanged system reset cycle."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')

@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class ResetReplicasTest(unittest.TestCase):
    def test_both_modes_and_actual_system_consumer_bindings(self):
        machine = (ROOT / 'cores/fes-pcxt/rtl/pcxt_machine.sv').read_text()
        system = (ROOT / 'cores/fes-pcxt/rtl/pcxt_system.sv').read_text()
        fragment = machine[machine.index('    wire reset_cpu,'):machine.index('    wire [1:0] cpu_fault')]
        request = re.search(r'    wire reset_request = .*?;', system, re.S)[0]
        self.assertIn('.REGISTER_RESET(1)', system)
        self.assertRegex(system, r'\.reset\(reset_request\), \.memory_valid')
        self.assertIn('machine_reset <= reset_request;', system)
        for instance, reset in (('cpu', 'reset_cpu'), ('board', 'reset_board'), ('isa', 'reset_isa')):
            binding = machine[machine.index(' ' + instance + ' ('):]
            binding = binding[:binding.index('    );')]
            self.assertIn('.reset(' + reset + ')', binding)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            for mode in (0, 1):
                reference = ('reg expected = 1; always @(posedge clk) expected <= raw;' if mode else
                             'wire expected = raw;')
                fixture = '''module proof(input clk,cold_reset,warm_reset,hold,firmware_loaded,initialized,output ok);
''' + f'localparam REGISTER_RESET={mode};\n' + request + '\nwire reset=reset_request;\n' + fragment + '''
wire raw = cold_reset || warm_reset || hold || !firmware_loaded || !initialized;
''' + reference + '''
assign ok = reset_cpu == expected && reset_board == expected && reset_isa == expected;
endmodule
'''
                (work / 'proof.sv').write_text(fixture)
                result = subprocess.run([YOSYS, '-Q', '-p',
                    'read_verilog -sv proof.sv; prep -top proof -flatten; '
                    'sat -verify -tempinduct -seq 2 -prove ok 1 -show-inputs'],
                    cwd=work, capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
