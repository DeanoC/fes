"""Prove aligned-region decoding for every address, kind and write direction."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys')
class RegionDecodeTest(unittest.TestCase):
    def prove(self, mutation=False):
        board = (ROOT / 'cores/fes-pcxt/rtl/pcxt_motherboard.sv').read_text()
        backend = (ROOT / 'cores/fes-pcxt/rtl/pcxt_memory.sv').read_text()
        ram = re.search(r'wire ram = ([^;]+);', board).group(1)
        rom = re.search(r'wire rom = ([^;]+);', board).group(1)
        physical = re.search(r'physical <= (address[^;]+);', backend).group(1)
        if mutation:
            self.assertIn("4'ha", ram)
            ram = ram.replace("4'ha", "4'hb")
        fixture = f"""module proof(input [19:0] request_address, address,
 input [1:0] request_kind, input write_cycle, output ok);
wire ram = {ram};
wire rom = {rom};
wire physical = {physical};
assign ok = (ram == (request_kind == 0 && request_address < 20'ha0000)) &&
            (rom == (request_kind == 0 && request_address >= 20'hf0000)) &&
            (physical == (address < 20'ha0000 ||
                          (address >= 20'hf0000 && !write_cycle)));
endmodule
"""
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            result = subprocess.run([YOSYS, '-Q', '-p',
                'read_verilog -sv proof.sv; prep -top proof; flatten; '
                'sat -verify -prove ok 1 -show-inputs -show-outputs'],
                cwd=work, capture_output=True, text=True, timeout=90)
            output = result.stdout + result.stderr
            if mutation:
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn('proof did fail', output)
            else:
                self.assertEqual(result.returncode, 0, output)

    def test_all_addresses_kinds_and_write_directions(self):
        self.prove()

    def test_ram_hole_admission_mutation_rejected(self):
        self.prove(True)
