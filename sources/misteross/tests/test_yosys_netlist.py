"""scripts/yosys_netlist.py: the one Yosys netlist program (#1581 S4).

The Yosys case runs only when FES_YOSYS names a Yosys whose rename -unescape
has DeanoC/yosys#75 (the pc386 lock's, once it pins that fix)."""
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest

from scripts import build_fes_pc386_quartus as pc386
from scripts import quartus_fes_pcxt as pcxt
from scripts import test_fes_i386_lockstep as lockstep
from scripts import yosys_netlist

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / 'tests/fixtures/pcxt-elaboration-programs.json'
YOSYS = shutil.which(os.environ.get('FES_YOSYS', '')) if os.environ.get('FES_YOSYS') else None


class ProgramTest(unittest.TestCase):
    def test_pcxt_programs_are_byte_identical_to_before_the_helper(self):
        # The programs the board fitter wrote before S4, with roots /S, /W and /L.
        golden = json.loads(FIXTURE.read_text())
        library = Path('/L/megafunction_bb.v')
        rendered = {
            'board': pcxt.elaboration(Path('/S'), Path('/W'), library),
            'package': pcxt.elaboration(Path('/S'), Path('/W'), library, package_profile=True, build_id='0' * 31 + '1'),
            'dos': pcxt.elaboration(Path('/S'), Path('/W'), library, package_profile=True, build_id='ab' * 16,
                                    dos_profile=True),
        }
        self.assertEqual(rendered, golden)

    def test_the_seal_program_unescapes_in_the_same_run(self):
        program = yosys_netlist.program(yosys_netlist.SEAL, top='t', sources=('a.sv',), netlist='n.v',
                                        parameters={'X': 1})
        self.assertEqual(program, 'read_verilog -sv a.sv; hierarchy -top t -chparam X 1; proc -norom; flatten; '
                                  'opt -purge; memory_collect; memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d; '
                                  'opt -purge; attrmap -remove src; attrmap -remove hdlname; rename -unescape; '
                                  'write_verilog n.v')

    def test_the_canonical_program_leaves_out_what_a_call_adds(self):
        canonical = yosys_netlist.canonical(yosys_netlist.SEAL)
        for placeholder in yosys_netlist.PLACEHOLDERS.values():
            self.assertIn(placeholder, canonical)
        self.assertNotIn('fes_i386', canonical)
        # One digest for the seal and the lockstep, whatever top, sources and rates they pass.
        self.assertRegex(yosys_netlist.program_sha256(yosys_netlist.SEAL), '^[0-9a-f]{64}$')

    def test_bad_names_are_refused(self):
        with self.assertRaises(yosys_netlist.NetlistError):
            yosys_netlist.program(yosys_netlist.SEAL, top='a;b', sources=(), netlist='n.v')
        with self.assertRaises(yosys_netlist.NetlistError):
            yosys_netlist.program(yosys_netlist.SEAL, top='t', sources=(), netlist='n.v', parameters={'X;': 1})
        with self.assertRaises(yosys_netlist.NetlistError):
            yosys_netlist.program(yosys_netlist.SEAL, top='t', sources=(), netlist='n.v', includes=('a b',))
        with self.assertRaises(ValueError):
            yosys_netlist.Recipe(memory_policy='none', attributes='keep')

    def test_the_cpu_is_one_list(self):
        self.assertEqual(pc386.MACHINE_SOURCES[:len(yosys_netlist.CPU_SOURCES)], yosys_netlist.CPU_SOURCES)
        self.assertEqual(lockstep.CPU_RTL, tuple(Path(p).name for p in yosys_netlist.CPU_SOURCES))
        for path in yosys_netlist.CPU_SOURCES:
            self.assertTrue((ROOT / path).is_file(), path)

    def test_the_cpu_source_digest_covers_content(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'a.sv').write_text('module a; endmodule\n')
            first = yosys_netlist.cpu_source_sha256(root, ('a.sv',))
            (root / 'a.sv').write_text('module a(); endmodule\n')
            self.assertNotEqual(first, yosys_netlist.cpu_source_sha256(root, ('a.sv',)))

    def test_the_pcxt_closure_reads_the_helper_without_the_tool_authenticator(self):
        self.assertIn(Path('scripts/yosys_netlist.py'), pcxt.input_paths(ROOT))
        text = (ROOT / 'scripts/yosys_netlist.py').read_text()
        self.assertNotIn('\nfrom scripts import fes_build_common', text)

    def test_flattened_names_follow_the_bits(self):
        with tempfile.TemporaryDirectory() as temporary:
            before, after = Path(temporary) / 'before.json', Path(temporary) / 'after.json'
            mem = {'type': '$mem_v2', 'parameters': {'MEMID': '\\u0.pal'}}
            before.write_text(json.dumps({'modules': {'t': {
                'netnames': {'cpu.ex_out': {'bits': [2, 3]}, 'cpu.ex.out': {'bits': [4]}, 'clk': {'bits': [5]}},
                'cells': {'u0.pal': mem}}}}))
            after.write_text(json.dumps({'modules': {'t': {
                'netnames': {'cpu_ex_out': {'bits': [2, 3]}, 'cpu_ex_out_1': {'bits': [4]}, 'clk': {'bits': [5]}},
                'cells': {'u0_pal': {**mem, 'parameters': {'MEMID': '\\u0_pal'}}}}}}))
            self.assertEqual(yosys_netlist.flattened_names(before, after, 't'),
                             {'cpu_ex_out': 'cpu.ex_out', 'cpu_ex_out_1': 'cpu.ex.out', 'u0_pal': 'u0.pal'})


@unittest.skipUnless(YOSYS, 'set FES_YOSYS to a Yosys with DeanoC/yosys#75')
class YosysTest(unittest.TestCase):
    def test_the_seal_program_writes_plain_names_for_colliding_hierarchy(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            # cpu.ex.t and cpu.ex_t both map to cpu_ex_t; the memory cpu.pal is escaped too.
            (work / 'top.sv').write_text(
                'module ex(input logic i, output logic out); (* keep *) logic t; assign t = ~i; assign out = t;'
                ' endmodule\n'
                'module cpu(input logic clk, input logic i, input logic [1:0] a, output logic o,'
                ' output logic [7:0] q);\n'
                '  (* keep *) logic ex_t; logic r; ex ex (.i(i), .out(r)); assign ex_t = r ^ i; assign o = ex_t;\n'
                '  logic [7:0] pal [0:3];\n'
                '  always_ff @(posedge clk) pal[a] <= {8{i}};\n'
                '  assign q = pal[a];\n'
                'endmodule\n'
                'module top(input logic clk, input logic i, input logic [1:0] a, output logic o,'
                ' output logic [7:0] q);\n'
                '  cpu cpu (.clk(clk), .i(i), .a(a), .o(o), .q(q));\n'
                'endmodule\n')
            netlist = yosys_netlist.seal_netlist(YOSYS, work / 'out', work, sources=('top.sv',), top='top')
            text = netlist.read_text()
            self.assertRegex(text, r'wire cpu_ex_t;')
            self.assertRegex(text, r'wire cpu_ex_t_1;')
            self.assertRegex(text, r'reg \[7:0\] cpu_pal \[3:0\];')
            self.assertNotIn('\\cpu.', text)
            self.assertEqual((work / 'out/top.ys').read_text().strip(), yosys_netlist.program(
                yosys_netlist.SEAL, top='top', sources=[str(work / 'top.sv')], includes=yosys_netlist.PC386_INCLUDES,
                netlist='top.netlist.v'))


if __name__ == '__main__':
    unittest.main()
