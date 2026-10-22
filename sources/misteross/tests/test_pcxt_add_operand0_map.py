"""Prove the ADD-op0 native map for arbitrary data and selector vectors."""
from collections import Counter
from contextlib import redirect_stdout
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
NEXTPNR = os.environ.get('NEXTPNR') or shutil.which('nextpnr-mistral')
RTL = ROOT / 'cores/fes-pcxt/rtl/pcxt_add_operand0_mux.sv'
MAP = ROOT / 'cores/fes-pcxt/synth/add_operand0_map.v'


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class AddOperand0MapTest(unittest.TestCase):
    def map_and_prove(self, work, selected='sel', payload='data', mapped=True):
        models = Path(shutil.which(YOSYS) or YOSYS).resolve().parents[1] / 'share/yosys/intel_alm/common'
        if not (models / 'alm_sim.v').is_file():
            self.skipTest('requires the selected Yosys Intel ALM simulation models')
        (work / 'proof.sv').write_text(r'''
            module proof(input [15:0] sel, input [255:0] data, output ok);
                wire [15:0] selected = SELECTED;
                wire [255:0] payload = PAYLOAD;
                wire [15:0] actual;
                reg [15:0] expected;
                integer i;
                always @* begin
                    expected = 0;
                    for (i=0; i<16; i=i+1)
                        expected = expected | (payload[i*16 +: 16] & {16{selected[i]}});
                end
                pcxt_add_operand0_mux dut(.sel(selected), .data(payload), .out(actual));
                assign ok = actual == expected;
            endmodule
        '''.replace('SELECTED', selected).replace('PAYLOAD', payload))
        command = f'read_verilog -sv {RTL} proof.sv; hierarchy -top proof; proc; opt_expr; opt_clean; '
        if mapped:
            command += (f'techmap -map {MAP}; read_verilog -sv {models / "alm_sim.v"} '
                        f'{models / "misc_sim.v"}; write_json mapped.json; ')
        command += ('setattr -mod -unset keep_hierarchy pcxt_add_operand0_mux; '
                    'prep -top proof -flatten; check -assert; sat -verify -prove ok 1 -show-inputs')
        result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                capture_output=True, text=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        if mapped:
            return json.loads((work / 'mapped.json').read_text())['modules']['proof']

    def check_shape(self, module, terms):
        cells = {n: c for n, c in module['cells'].items() if c['type'].startswith('MISTRAL_ALUT')}
        counts = Counter(c['type'] for c in cells.values())
        groups = (terms + 2) // 3
        expected = Counter()
        if terms:
            expected['MISTRAL_ALUT6'] = 16 * (terms // 3)
            if terms % 3:
                expected['MISTRAL_ALUT' + str(2 * (terms % 3))] += 16
            if groups > 1:
                expected['MISTRAL_ALUT' + str(groups)] += 16
        self.assertEqual(+counts, +expected)
        drivers = {}
        for name, cell in cells.items():
            width = int(cell['type'].removeprefix('MISTRAL_ALUT'))
            self.assertEqual(set(cell['connections']), set('ABCDEF'[:width]) | {'Q'})
            self.assertEqual(cell['port_directions'], {**{p: 'input' for p in 'ABCDEF'[:width]}, 'Q': 'output'})
            for port, bits in cell['connections'].items():
                self.assertEqual(len(bits), 1)
                if port == 'Q':
                    self.assertNotIn(bits[0], drivers)
                    drivers[bits[0]] = name
        def depth(bit):
            if bit not in drivers:
                return 0
            cell = cells[drivers[bit]]
            return 1 + max(depth(b) for p, bs in cell['connections'].items() if p != 'Q' for b in bs)
        self.assertEqual({depth(b) for b in module['netnames']['actual']['bits']},
                         {0 if terms == 0 else 1 if groups == 1 else 2})
        return cells

    def test_generic_and_native_all_control_and_data_values(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            self.map_and_prove(work, mapped=False)
            self.check_shape(self.map_and_prove(work), 16)
            # Constant one controls must retain OR semantics, not priority/mux semantics.
            self.map_and_prove(work, selected="16'hffff")

    def test_every_active_count_and_partial_constant_data(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            for terms in range(17):
                with self.subTest(terms=terms):
                    mask = (1 << terms) - 1
                    module = self.map_and_prove(work, selected=f"sel & 16'h{mask:04x}")
                    cells = self.check_shape(module, terms)
                    inputs = {b for c in cells.values() for p, bs in c['connections'].items() if p != 'Q' for b in bs}
                    self.assertTrue(set(module['ports']['data']['bits'][terms * 16:]).isdisjoint(inputs))
            # Different zero data in different bit lanes exercises per-lane compaction.
            self.map_and_prove(work, payload="data & 256'hffff0000aa550000ffff000055aa0000ffff0000aa550000ffff000055aa0000")
            self.map_and_prove(work, selected="16'h0000", payload="256'hffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

    def test_current_source_mask_has_four_triples_and_one_or_per_lane(self):
        with tempfile.TemporaryDirectory() as directory:
            module = self.map_and_prove(Path(directory), selected="sel & 16'h9eff", payload="{16'b0, data[239:0]}")
            cells = self.check_shape(module, 12)
            inputs = {b for c in cells.values() for p, bs in c['connections'].items() if p != 'Q' for b in bs}
            for index in (8, 13, 14, 15):
                self.assertNotIn(module['ports']['sel']['bits'][index], inputs)
                self.assertTrue(set(module['ports']['data']['bits'][index*16:(index+1)*16]).isdisjoint(inputs))

    @unittest.skipUnless(NEXTPNR, 'requires nextpnr-mistral; set NEXTPNR to the selected executable')
    def test_native_device_packing(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'fixture.sv').write_text(r"""
                module top(input clk, output result);
                    (* keep *) reg [15:0] sel;
                    (* keep *) reg [255:0] data;
                    wire [15:0] out;
                    always @(posedge clk) begin
                        data <= {data[254:0], data[255] ^ out[0]};
                        sel <= {sel[14:0], sel[15] ^ out[1]};
                    end
                    assign result = ^out;
                    pcxt_add_operand0_mux dut(.sel(sel & 16'h9eff),
                        .data({16'b0, data[239:0]}), .out(out));
                endmodule
            """)
            # Use the real synthesis sequence, including native IO buffers and
            # hierarchy preservation, before the post-synthesis map and packer.
            command = (f'read_verilog -sv {RTL} fixture.sv; '
                       'synth_intel_alm -nolutram -nodsp -top top; '
                       f'techmap -map {MAP}; hierarchy -top top; opt_clean; '
                       'check -assert; write_json native.json')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            (work / 'fixture.qsf').write_text(
                'set_location_assignment PIN_V11 -to clk\n'
                'set_location_assignment PIN_AG5 -to result\n')
            result = subprocess.run([NEXTPNR, '--device', '5CSEBA6U23I7', '--json', 'native.json',
                                     '--qsf', 'fixture.qsf', '--pack-only', '--write', 'packed.json'], cwd=work,
                                    capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            packed = json.loads((work / 'packed.json').read_text())['modules']['top']
            self.assertFalse(any('pcxt_' in c['type'] for c in packed['cells'].values()))
            self.assertEqual(sum(c['type'].startswith('MISTRAL_ALUT') and n.startswith('dut.lanes[')
                                 for n, c in packed['cells'].items()), 80)


class ProducerMapTest(unittest.TestCase):
    def load_script(self, script):
        previous = sys.path[:]
        sys.path.insert(0, str(ROOT / 'scripts'))
        sys.path.insert(0, str(ROOT))
        try:
            spec = importlib.util.spec_from_file_location('pcxt_add_map_' + script, ROOT / 'scripts' / (script + '.py'))
            build = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(build)
        finally:
            sys.path[:] = previous
        return build

    def test_producer_maps_both_boundaries_after_generic_synthesis(self):
        build = self.load_script('build_fes_pcxt')
        command = build.yosys_program(Path('/tmp/pcxt-add-map-fixture'), False, False, 'SYNTH_MARKER')
        self.assertIn(str(RTL), command)
        self.assertEqual(command.count('techmap -map ' + str(MAP)), 1)
        self.assertLess(command.index('SYNTH_MARKER'), command.index('techmap -map ' + str(MAP)))
        self.assertIn('cores/fes-pcxt/synth/logic_operand1_map.v', command)
        self.assertIn('cores/fes-pcxt/synth/add_operand0_map.v', build.CPU_MUX_MAPS)

    def test_fixture_synthesis_imports_maps_and_hashes_for_cpu_and_machine_only(self):
        synth = self.load_script('synth_fes_pcxt')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            # Use actual source bytes and verification, while keeping all outputs
            # in the temporary tree. Authentication and execution are separate
            # responsibilities; this test checks the real command/receipt path.
            (root / 'cores').symlink_to(ROOT / 'cores', target_is_directory=True)
            (root / 'scripts').symlink_to(ROOT / 'scripts', target_is_directory=True)
            for suite in ('cpu', 'machine', 'isa', 'memory', 'display', 'video-cga', 'video-mda'):
                with self.subTest(suite=suite):
                    commands = []
                    def capture(command, **kwargs):
                        commands.append(command)
                        top = 'pcxt_video_output' if suite.startswith('video-') else suite + '_top'
                        if suite == 'memory':
                            top = 'pcxt_memory'
                        (kwargs['cwd'] / 'synth.json').write_text(json.dumps({'modules': {top: {'cells': {}}}}))
                    tools = {'yosys': SimpleNamespace(path='/fixture/yosys', identity={'fixture': True})}
                    with mock.patch.object(synth, 'ROOT', root), \
                            mock.patch.object(synth, 'authenticate_tools', return_value=tools), \
                            mock.patch.object(synth.subprocess, 'run', side_effect=capture), \
                            mock.patch.object(sys, 'argv', ['synth_fes_pcxt.py', '--cache-root', '/fixture', '--suite', suite]), \
                            redirect_stdout(io.StringIO()) as output:
                        synth.main()
                    self.assertEqual(len(commands), 1)
                    command = commands[0][-1]
                    receipt = json.loads(output.getvalue())
                    for path in synth.CPU_MUX_MAPS:
                        if suite in ('cpu', 'machine'):
                            self.assertEqual(command.count('techmap -map ' + str(root / path)), 1)
                            self.assertLess(command.index('synth_intel_alm'), command.index('techmap -map ' + str(root / path)))
                            self.assertEqual(receipt['sources'][path], hashlib.sha256((ROOT / path).read_bytes()).hexdigest())
                        else:
                            self.assertNotIn('techmap -map ' + str(root / path), command)
                            self.assertNotIn(path, receipt['sources'])
                    helper = 'cores/fes-pcxt/rtl/pcxt_add_operand0_mux.sv'
                    self.assertEqual(helper in receipt['sources'], suite in ('cpu', 'machine'))
