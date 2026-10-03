"""ST compiler boundary, physical connector and synchronous firmware checks."""
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

from scripts import atari_st_slot, build_fes_atari_st_oss as st, rom_map
from scripts.fes_build_common import BuildError

ROOT = Path(__file__).resolve().parents[1]


class AtariSTProducerTests(unittest.TestCase):
    def test_vendor_cpu_adapter_preserves_all_functional_bytes(self):
        original = (ROOT / st.CPU_VENDOR / 'fx68k.sv').read_bytes()
        adapted = st.adapted_cpu_source(ROOT)
        before, after = original.splitlines(keepends=True), adapted.splitlines(keepends=True)
        self.assertEqual(len(before), len(after))
        changes = [(left.strip(), right.strip()) for left, right in zip(before, after) if left != right]
        self.assertEqual(changes, [(b'// synthesis translate off', b'// synthesis translate_off'),
                                   (b'// synthesis translate on', b'// synthesis translate_on')])
        self.assertRegex(adapted.decode(), r'regs68L\[i\]\s*<=')
        self.assertRegex(adapted.decode(), r'regs68H\[i\]\s*<=')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            vendor = root / st.CPU_VENDOR
            shutil.copytree(ROOT / st.CPU_VENDOR, vendor)
            with (vendor / 'nanorom.mem').open('ab') as target:
                target.write(b'\n')
            with self.assertRaisesRegex(BuildError, 'vendor digest changed'):
                st.adapted_cpu_source(root)

    def test_build_uses_locked_slang_single_unit_and_selected_video(self):
        with (ROOT / st.ST_TOOLCHAIN_LOCK).open('rb') as source:
            lock = tomllib.load(source)
        for name, digest in st.ST_TOOL_COMMITS.items():
            self.assertEqual(lock['tool'][name]['commit'], digest)
        for relative in st.PINNED_INPUTS:
            self.assertTrue((ROOT / relative).is_file(), relative)
        tools = {'yosys': Path('/authenticated/install/bin/yosys'),
                 'nextpnr-mistral': Path('/authenticated/install/bin/nextpnr-mistral')}
        command, route = st.build_commands(ROOT, ROOT / st.OUTPUT_RELATIVE, '0' * 32,
                                           tools, video_output='scanlines')
        self.assertIn('--single-unit', command[-1])
        self.assertIn('-set VIDEO_SCANLINES 1', command[-1])
        self.assertIn('fx68k-slang.sv', command[-1])
        self.assertNotIn('--ignore-initial', command[-1])
        self.assertIn('-nolutram -nodsp', command[-1])
        self.assertIn('--router', route)
        self.assertEqual(route[route.index('--router') + 1], 'gpu')
        self.assertEqual(route[route.index('--seed') + 1], '4')
        with self.assertRaises(BuildError):
            st.build_commands(ROOT, ROOT / st.OUTPUT_RELATIVE, '0' * 32, tools, video_output='unknown')

    def test_192_firmware_lanes_match_pinned_synchronous_cells(self):
        source = (ROOT / 'cores/fes-atari-st/rtl/st_rom.v').read_text()
        entries = re.findall(r'BEL = "MISTRAL_M10K\.(\d+)\.(\d+)\.0".*?\) lane(\d+) \(', source, re.S)
        self.assertEqual(len(entries), 192)
        self.assertEqual(tuple((int(x), int(y)) for x, y, _ in entries), st.FIRMWARE_LANE_ROWS)
        self.assertEqual([int(n) for _, _, n in entries], list(range(192)))
        self.assertEqual(len(set(st.FIRMWARE_LANE_ROWS)), 192)
        cells = {f'machine.rom.lane{i}': {'type': 'MISTRAL_M10K',
            'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.{x}.{y}.0'},
            'parameters': {'CFG_ABITS': 10, 'CFG_DBITS': 10, 'CFG_ASYNC_READ': 0, 'INIT': '0'*10240}}
            for i, (x, y) in enumerate(st.FIRMWARE_LANE_ROWS)}
        routed = {'modules': {'top': {'cells': cells}}}
        rom_map.validate_routed_rom(routed, st.FIRMWARE_LANE_ROWS, expected_async_read=0)
        for cell in cells.values():
            cell['connections'] = {'CLK1': [5], 'A1EN': ['1'], 'B1EN': ['1']}
        st.validate_firmware_ports(cells)
        cells['machine.rom.lane0']['connections']['A1BE'] = ['0', '0']
        with self.assertRaisesRegex(BuildError, 'no optional ports'):
            st.validate_firmware_ports(cells)
        del cells['machine.rom.lane0']['connections']['A1BE']
        cells['machine.rom.lane0']['connections']['A1EN'] = ['0']
        with self.assertRaisesRegex(BuildError, 'disabled writes'):
            st.validate_firmware_ports(cells)
        with self.assertRaisesRegex(ValueError, 'CFG_ASYNC_READ'):
            rom_map.validate_routed_rom(routed, st.FIRMWARE_LANE_ROWS)
        cells['machine.rom.lane0']['parameters']['CFG_ASYNC_READ'] = 1
        with self.assertRaisesRegex(ValueError, 'CFG_ASYNC_READ'):
            rom_map.validate_routed_rom(routed, st.FIRMWARE_LANE_ROWS, expected_async_read=0)

    def test_reserved_expansion_accepts_only_its_exact_boundary(self):
        socket = atari_st_slot.SOCKETS[0]
        cells = {socket.instance + name: {'type': 'MISTRAL_FF', 'attributes': {'NEXTPNR_BEL': bel}}
                 for name, bel in atari_st_slot.boundary_bels().items()}
        routed = {'modules': {'top': {'cells': cells}}}
        self.assertEqual(st.validate_routed_shell(routed)['pinned_boundary_cells'], 119)
        cells['rogue'] = {'type': 'MISTRAL_FF', 'attributes': {'NEXTPNR_BEL': 'MISTRAL_FF.25.10.2'}}
        with self.assertRaisesRegex(BuildError, 'inside the slot'):
            st.validate_routed_shell(routed)
        del cells['rogue']
        cells['expansion.plug_response_ff_0']['attributes']['NEXTPNR_BEL'] = 'MISTRAL_FF.24.4.2'
        with self.assertRaisesRegex(BuildError, 'boundary cell'):
            st.validate_routed_shell(routed)

    def test_failed_or_ambiguous_clock_never_qualifies(self):
        fmax = {'clk': {'constraint': 52.224, 'achieved': 52.3}}
        st._frequency_row(fmax, 52.224, 'system')
        for fields in ({'constraint': 52.224, 'achieved': 52.0},
                       {'constraint': 52.224, 'achieved': float('inf')}):
            with self.assertRaises(BuildError):
                st._frequency_row({'clk': fields}, 52.224, 'system')
        fmax['duplicate'] = fmax['clk']
        with self.assertRaises(BuildError):
            st._frequency_row(fmax, 52.224, 'system')

    def test_unused_memory_clock_repair_rejects_writable_or_unrelated_memory(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'synth.json'
            name = 'machine.system.machine.cpu.cpu.nanoRom.nRam.0.0.0'
            cell = {'type': 'MISTRAL_M10K',
                    'parameters': {'CFG_DBITS': 20, 'CFG_BYTE_ENABLE': 1, 'CFG_DUAL_CLOCK': 1},
                    'connections': {'CLK1': ['x'], 'CLK2': [5], 'A1EN': ['0']}}
            def write(key):
                path.write_text(json.dumps({'modules': {'top': {'cells': {key: cell}}}}))
            write(name)
            self.assertEqual(st.clock_read_only_memories(path), [name])
            self.assertEqual(json.loads(path.read_text())['modules']['top']['cells'][name]['connections']['CLK1'], [5])
            cell['connections']['A1EN'] = ['1']
            write(name)
            with self.assertRaisesRegex(BuildError, 'unexpected disconnected'):
                st.clock_read_only_memories(path)
            cell['connections']['A1EN'] = ['0']
            write('unrelated.memory')
            with self.assertRaisesRegex(BuildError, 'unexpected disconnected'):
                st.clock_read_only_memories(path)

    def test_failed_or_cancelled_build_withdraws_unsealed_artifacts(self):
        for failure in (BuildError('compiler failure'), KeyboardInterrupt()):
            with self.subTest(failure=type(failure).__name__), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / st.QSF).parent.mkdir(parents=True)
                (root / st.QSF).write_text('# board pins\n')
                authenticated = {name: SimpleNamespace(identity='test', path=Path('/auth/install/bin') / name)
                                 for name in ('mistral', 'yosys', 'nextpnr-mistral')}
                invocation = SimpleNamespace(inputs={}, env={}, close=Mock())
                def fail_tool(*args, **kwargs):
                    for name in ('core.rbf', 'manifest.toml', 'build-summary.json', 'rom-map.json'):
                        (root / st.OUTPUT_RELATIVE / name).write_bytes(b'unsealed')
                    raise failure
                with patch.object(st, '_require_clean_source', return_value=('repo', 'a'*40)), \
                     patch.object(st, '_authenticate_atari_st_tools', return_value=authenticated), \
                     patch.object(st.rom_map, 'read_database', return_value={}), \
                     patch.object(st, 'FunctionalInvocation', return_value=invocation), \
                     patch.object(st, 'create_build_record', return_value=b'{}'), \
                     patch.object(st, 'prepare_cpu_inputs'), \
                     patch.object(st, 'build_identity', return_value='0'*32), \
                     patch.object(st, '_run_tool', side_effect=fail_tool), \
                     patch.object(st, 'export_package') as export:
                    with self.assertRaises(type(failure)):
                        st.build(root)
                    export.assert_not_called()
                invocation.close.assert_called_once()
                for name in ('core.rbf', 'manifest.toml', 'build-summary.json', 'rom-map.json'):
                    self.assertFalse((root / st.OUTPUT_RELATIVE / name).exists())

    def test_timing_search_uses_atari_bound_without_relaxing_clock_or_repair_gates(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / st.QSF).parent.mkdir(parents=True)
            (root / st.QSF).write_text('# board pins\n')
            authenticated = {name: SimpleNamespace(identity='test', path=Path('/auth/install/bin') / name)
                             for name in ('mistral', 'yosys', 'nextpnr-mistral')}
            invocation = SimpleNamespace(inputs={}, env={}, close=Mock())
            def synth(*args, **kwargs):
                (root / st.OUTPUT_RELATIVE / 'synth.json').write_text('{}')
            def fail_timing(**kwargs):
                (root / st.OUTPUT_RELATIVE / 'core.rbf').write_bytes(b'unqualified')
                raise st.SearchError('no placement met timing')
            with patch.object(st, '_require_clean_source', return_value=('repo', 'a'*40)), \
                 patch.object(st, '_authenticate_atari_st_tools', return_value=authenticated), \
                 patch.object(st.rom_map, 'read_database', return_value={}), \
                 patch.object(st, 'FunctionalInvocation', return_value=invocation), \
                 patch.object(st, 'create_build_record', return_value=b'{}'), \
                 patch.object(st, 'prepare_cpu_inputs'), \
                 patch.object(st, 'build_identity', return_value='0'*32), \
                 patch.object(st, '_run_tool', side_effect=synth), \
                 patch.object(st, 'clock_read_only_memories', return_value=[]), \
                 patch.object(st, 'validate_synth_evidence'), \
                 patch.object(st, 'route_after_synth', side_effect=fail_timing) as route, \
                 patch.object(st, 'export_package') as export:
                with self.assertRaisesRegex(BuildError, 'no placement met timing'):
                    st.build(root)
                self.assertEqual(route.call_args.kwargs['timeout'], 1800)
                self.assertEqual(route.call_args.kwargs['seeds'], (4, 5, 2, 1, 3, 6, 7, 8, 9, 10))
                self.assertEqual(route.call_args.kwargs['required'],
                                 ((None, 52.224), (None, 74.25), (None, 12.288)))
                self.assertEqual(route.call_args.kwargs['extra'], ('--router', 'gpu'))
                export.assert_not_called()
            invocation.close.assert_called_once()
            self.assertFalse((root / st.OUTPUT_RELATIVE / 'core.rbf').exists())

    @unittest.skipUnless(shutil.which('verilator'), 'Verilator required for synchronous ROM simulation')
    def test_rom_big_endian_lane_edges_reset_and_held_request(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'st-firmware.hex').write_text(''.join(f'{(i ^ (i >> 8)) & 255:02x}\n' for i in range(st.FIRMWARE_BYTES)))
            cpp = output / 'rom.cpp'
            cpp.write_text(r'''
#include "Vst_rom.h"
#include <cassert>
static void tick(Vst_rom& d) { d.clk=0;d.eval();d.clk=1;d.eval(); }
static unsigned byte(unsigned a) { return (a^(a>>8))&255; }
static void read(Vst_rom& d,unsigned a) {
 d.req=1;d.address=a/2;
 for(unsigned n=0;n<4;n++){tick(d);assert(!d.ready);}
 tick(d);assert(d.ready);assert(d.rdata==((byte(a)<<8)|byte(a+1)));
 d.address=0;for(unsigned n=0;n<4;n++){tick(d);assert(d.ready);assert(d.rdata==((byte(a)<<8)|byte(a+1)));}
 d.req=0;tick(d);assert(!d.ready);
}
int main(){Vst_rom d;d.reset=1;d.req=0;tick(d);d.reset=0;
 for(unsigned i=0;i<192;i++){read(d,i*1024);read(d,i*1024+1022);}
 d.req=1;d.address=12;tick(d);d.reset=1;tick(d);assert(!d.ready);d.reset=0;d.req=0;tick(d);
 read(d,196606);d.final();}
''')
            command = ['verilator', '--cc', '--exe', '--build', '-O2', '--top-module', 'st_rom',
                       '--Mdir', str(output / 'obj'), str(ROOT / 'cores/fes-atari-st/rtl/st_rom.v'), str(cpp)]
            result = subprocess.run(command, cwd=output, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(output / 'obj/Vst_rom')], cwd=output, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
