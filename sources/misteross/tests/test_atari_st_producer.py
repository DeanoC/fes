"""ST compiler boundary, physical connector and synchronous firmware checks."""
import hashlib
import copy
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

from scripts import atari_st_slot, atari_st_video_parts, build_fes_atari_st_oss as st, rom_map
from scripts.fes_build_common import BuildError

ROOT = Path(__file__).resolve().parents[1]


def ram_database():
    # Same documented SX120F coordinate space and table cardinalities; include
    # controls at y0/y85 so an INIT-only check cannot satisfy this fixture.
    controls = [(0, y) for y in range(86)] + [(1, y) for y in range(40)]
    text = ''.join(f'g control{i} b- {x}.{y}\n' for i, (x, y) in enumerate(controls[:74]))
    text += 'g control74 r-:52 ' + ' '.join(f'{x}.{y}' for x, y in controls[74:]) + '\n'
    text += 'm ram r-:40\n' + ''.join(
        '  * ' + ' '.join(f'{word+3}.{bit+19}' for bit in range(40)) + '\n'
        for word in range(256))
    kinds = ['T_M10K' if x in (5, 14, 26, 38) else 'T_EMPTY'
             for x in range(len(rom_map.SX120F.x_to_bx))]
    die = ('7605, 7024, // cram size\n// x to bit x\n{' +
           ','.join(map(str, rom_map.SX120F.x_to_bx)) + '}\n// column types\n{' +
           ','.join(kinds) + '}\n')
    return {'data/m10k-mux.txt': text.encode(), 'libmistral/cvd-sx120f.cc': die.encode(),
            'libmistral/cyclonev.h': b'y = 2 + 86 * pos.y();'}


def database_pins(database):
    return {name: hashlib.sha256(data).hexdigest() for name, data in database.items()}


def pinned_cache_cells(*, routed=True):
    attribute = 'NEXTPNR_BEL' if routed else 'BEL'
    return {name: {'type': 'MISTRAL_M10K', 'attributes': {attribute: bel}}
            for name, bel in st.CACHE_BELS.items()}


def sector_memory_cells(*, packed=False):
    name = 'machine.system.io.floppy.writer.sector.0.0.0'
    zero = 900 if packed else '0'
    pins = {'A1ADDR': list(range(10, 18)) + [zero],
            'B1ADDR': list(range(20, 28)) + [zero],
            'A1DATA': list(range(30, 46)) + [zero] * 4,
            'B1DATA': list(range(50, 70)), 'A1EN': [71],
            'B1EN': [72], 'A1BE': [71, 71], 'CLK1': [73], 'CLK2': [73]}
    cells = {name: {'type': 'MISTRAL_M10K', 'connections': pins,
                    'port_directions': {port: 'output' if port == 'B1DATA' else 'input'
                                        for port in pins},
                    'parameters': {'CFG_ABITS': f'{9:032b}', 'CFG_DBITS': f'{20:032b}',
                                   'CFG_BYTE_ENABLE': f'{1:032b}', 'CFG_DUAL_CLOCK': f'{1:032b}'}},
             'machine.rom.lane0': {'connections': {'CLK1': [73]},
                                   'port_directions': {'CLK1': 'input'}}}
    live_inputs = {bit for port, bits in pins.items() if port != 'B1DATA'
                   for bit in bits if type(bit) is int and bit != 900}
    for bit in live_inputs:
        cells[f'driver{bit}'] = {'type': 'MISTRAL_CLKBUF' if bit == 73 else 'MISTRAL_FF',
                               'connections': {'Q': [bit]}, 'port_directions': {'Q': 'output'}}
    if packed:
        cells['$PACKER_GND_DRV'] = {'type': 'MISTRAL_CONST', 'parameters': {'LUT': '0' * 32},
                                  'connections': {'Q': [900]}, 'port_directions': {'Q': 'output'}}
    return cells


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
                                           tools, video_output='direct')
        self.assertIn('--single-unit', command[-1])
        self.assertIn('-DFES_ST_SLANG_IMPORT=1', command[-1])
        self.assertIn('-G ENABLE_FLOPPY_WRITE=1', command[-1])
        self.assertIn('st_video_socket.sv', command[-1].split('read_slang')[0])
        self.assertIn('st_media_port.sv', command[-1].split('read_slang')[0])
        self.assertIn('-set VIDEO_SCANLINES 0', command[-1])
        with self.assertRaisesRegex(BuildError, 'sealed video part'):
            st.build_commands(ROOT, ROOT / st.OUTPUT_RELATIVE, '0' * 32, tools, video_output='scanlines')
        self.assertIn('fx68k-slang.sv', command[-1])
        self.assertNotIn('--ignore-initial', command[-1])
        self.assertIn('-nolutram -nodsp', command[-1])
        for name, bel in st.CACHE_BELS.items():
            self.assertIn(f'setattr -set BEL "{bel}" top/{name};', command[-1])
        qsf = st.socket_qsf('# physical board pins\n')
        self.assertEqual(qsf.count(f'FES_RESERVED_RECT "{atari_st_slot.SOCKETS[0].placement}"'), 1)
        self.assertEqual(qsf.count('FES_RESERVED_RECT "ram_guard 26 19 26 19"'), 1)
        for rectangle in st.VIDEO_RAM_GUARD_RESERVATIONS:
            self.assertEqual(qsf.count(f'FES_RESERVED_RECT "{rectangle}"'), 1)
        self.assertEqual(qsf.count(f'FES_RESERVED_RECT "{atari_st_video_parts.PLACEMENT}"'), 1)
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
        cells = {}
        for index, (name, bel) in enumerate(atari_st_slot.boundary_bels().items()):
            name = socket.instance + name
            cells[name] = {'type': 'MISTRAL_FF', 'attributes': {'NEXTPNR_BEL': bel},
                           'connections': {'DATAIN': [2000 + index], 'CLK': [5]}}
            cells[name + '$ROUTETHRU'] = {'type': 'MISTRAL_BUF',
                'attributes': {'NEXTPNR_BEL': st.boundary_route_buffer_bel(bel)},
                'port_directions': {'A': 'input', 'Q': 'output'},
                'connections': {'A': [1000 + index], 'Q': [2000 + index]}}
        routed = {'modules': {'top': {'cells': cells}}}
        self.assertEqual(st.validate_routed_shell(routed)['pinned_boundary_cells'], 119)
        self.assertEqual(st.validate_routed_shell(routed)['pinned_boundary_route_buffers'], 119)
        for mutation in ('site', 'name', 'type', 'missing', 'disconnected', 'multiple', 'wrong_ff', 'shared_output'):
            with self.subTest(mutation=mutation):
                changed = json.loads(json.dumps(routed))
                altered = changed['modules']['top']['cells']
                name = 'expansion.plug_request_ff_0$ROUTETHRU'
                buffer = altered[name]
                if mutation == 'site': buffer['attributes']['NEXTPNR_BEL'] = 'MISTRAL_COMB.25.1.0'
                elif mutation == 'name': altered[name + '_fake'] = altered.pop(name)
                elif mutation == 'type': buffer['type'] = 'MISTRAL_ALUT6'
                elif mutation == 'missing': del altered[name]
                elif mutation == 'disconnected': buffer['connections']['Q'] = []
                elif mutation == 'multiple': buffer['connections']['A'].append(999)
                elif mutation == 'wrong_ff': buffer['connections']['Q'] = [2001]
                elif mutation == 'shared_output':
                    altered['expansion.plug_request_ff_1$ROUTETHRU']['connections']['Q'] = [2000]
                    altered['expansion.plug_request_ff_1']['connections']['DATAIN'] = [2000]
                with self.assertRaisesRegex(BuildError, 'route buffer'):
                    st.validate_routed_shell(changed)
        # This real mismatch is invisible to the existing LAB/BEL exclusion:
        # row19 is outside the reserved LABs, yet all of its RAM bits are inside.
        cells['cache'] = {'type': 'MISTRAL_M10K',
                          'attributes': {'NEXTPNR_BEL': 'MISTRAL_M10K.26.19.0'}}
        st.validate_routed_shell(routed)
        database = ram_database()
        with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)), \
             self.assertRaisesRegex(BuildError, 'configuration footprint.*overlaps slot'):
            st.validate_m10k_configurations(routed, database)
        del cells['cache']
        cells['rogue'] = {'type': 'MISTRAL_FF', 'attributes': {'NEXTPNR_BEL': 'MISTRAL_FF.25.10.2'}}
        with self.assertRaisesRegex(BuildError, 'inside the slot'):
            st.validate_routed_shell(routed)
        del cells['rogue']
        cells['expansion.plug_response_ff_0']['attributes']['NEXTPNR_BEL'] = 'MISTRAL_FF.24.4.2'
        with self.assertRaisesRegex(BuildError, 'boundary cell'):
            st.validate_routed_shell(routed)

    def test_ram_bel_outside_video_still_rejects_overlapping_configuration(self):
        database = ram_database()
        for row in (40, 41, 58, 59):
            routed = {'modules': {'top': {'cells': {'sector': {
                'type': 'MISTRAL_M10K', 'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.26.{row}.0'}}}}}}
            with self.subTest(row=row), patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)), \
                 self.assertRaisesRegex(BuildError, 'overlaps video CRAM'):
                st.validate_m10k_configurations(routed, database)
        for row in (22, 39, 60):
            routed = {'modules': {'top': {'cells': {'sector': {
                'type': 'MISTRAL_M10K', 'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.26.{row}.0'}}}}}}
            with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)):
                self.assertEqual(st.validate_m10k_configurations(routed, database)['status'], 'pass')

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

    def test_cache_constraints_require_both_real_mapped_ram_cells(self):
        for routed in (False, True):
            with self.subTest(routed=routed):
                cells = pinned_cache_cells(routed=routed)
                self.assertEqual(st.validate_cache_placements(cells, routed=routed), st.CACHE_BELS)
                cells['video.cache0.0.0.0']['attributes']['NEXTPNR_BEL' if routed else 'BEL'] = 'MISTRAL_M10K.26.19.0'
                with self.assertRaisesRegex(BuildError, 'must occupy'):
                    st.validate_cache_placements(cells, routed=routed)
                cells = pinned_cache_cells(routed=routed)
                cells['video.cache0.0.0.0']['type'] = 'MISTRAL_FF'
                with self.assertRaisesRegex(BuildError, 'exactly the two'):
                    st.validate_cache_placements(cells, routed=routed)

    def test_sector_stage_rejects_flip_flops_and_invalid_memory_ports(self):
        name = 'machine.system.io.floppy.writer.sector.0.0.0'
        cells = sector_memory_cells()
        cells[name + '_B1ADDR_MISTRAL_ALUT2_Q'] = {'type': 'MISTRAL_ALUT2'}
        self.assertEqual(st.validate_sector_memory(cells)['words'], 256)
        mutations = [('type', 'MISTRAL_FF'), ('CLK2', [74]), ('B1EN', ['1']),
                     ('A1BE', [71, '1']), ('A1ADDR', list(range(10, 19))),
                     ('A1DATA', list(range(30, 50))), ('B1DATA', ['x'] * 20),
                     ('CFG_DBITS', f'{16:032b}')]
        for key, value in mutations:
            with self.subTest(key=key):
                changed = copy.deepcopy(cells)
                target = (changed[name] if key == 'type' else
                          changed[name]['parameters'] if key.startswith('CFG_') else
                          changed[name]['connections'])
                target[key] = value
                with self.assertRaises(BuildError):
                    st.validate_sector_memory(changed)
        for changed in ({}, cells | {name + '_MISTRAL_FF_Q': {'type': 'MISTRAL_FF'}}):
            with self.assertRaisesRegex(BuildError, 'exactly one M10K'):
                st.validate_sector_memory(changed)

    def test_sector_stage_accepts_literal_and_proven_packed_zero_padding(self):
        for packed in (False, True):
            with self.subTest(packed=packed):
                self.assertEqual(st.validate_sector_memory(sector_memory_cells(packed=packed)),
                                 {'status': 'pass', 'cell': 'machine.system.io.floppy.writer.sector.0.0.0',
                                  'words': 256, 'bits_per_word': 16})

    def test_sector_stage_rejects_unproven_packed_padding(self):
        name = 'machine.system.io.floppy.writer.sector.0.0.0'
        for mutation in ('undriven', 'high', 'unknown_lut', 'wide_lut', 'integer_lut',
                         'multiple_zero', 'contradictory', 'multiple_ordinary', 'ordinary_driver', 'unknown_direction',
                         'inout', 'wide_output', 'unknown_literal', 'boolean'):
            with self.subTest(mutation=mutation):
                cells = sector_memory_cells(packed=True)
                ground = cells['$PACKER_GND_DRV']
                if mutation == 'undriven': del cells['$PACKER_GND_DRV']
                elif mutation == 'high': ground['parameters']['LUT'] = f'{1:032b}'
                elif mutation == 'unknown_lut': ground['parameters']['LUT'] = 'x' * 32
                elif mutation == 'wide_lut': ground['parameters']['LUT'] = f'{2:032b}'
                elif mutation == 'integer_lut': ground['parameters']['LUT'] = 0
                elif mutation in ('multiple_zero', 'contradictory', 'multiple_ordinary'):
                    cells['extra'] = copy.deepcopy(ground)
                    if mutation == 'contradictory': cells['extra']['parameters']['LUT'] = f'{1:032b}'
                    elif mutation == 'multiple_ordinary': cells['extra']['type'] = 'MISTRAL_FF'
                elif mutation == 'ordinary_driver': ground['type'] = 'MISTRAL_FF'
                elif mutation == 'unknown_direction': del ground['port_directions']['Q']
                elif mutation == 'inout': ground['port_directions']['Q'] = 'inout'
                elif mutation == 'wide_output': ground['connections']['Q'].append(901)
                elif mutation == 'unknown_literal': cells[name]['connections']['A1ADDR'][8] = 'x'
                elif mutation == 'boolean': cells[name]['connections']['B1ADDR'][8] = False
                with self.assertRaises(BuildError):
                    st.validate_sector_memory(cells)

    def test_sector_stage_rejects_constant_undriven_and_multiple_live_signals(self):
        name = 'machine.system.io.floppy.writer.sector.0.0.0'
        for port in ('A1ADDR', 'B1ADDR', 'A1DATA', 'B1DATA', 'A1EN', 'B1EN', 'CLK1'):
            for mutation in ('zero', 'one', 'undriven', 'multiple', 'unknown_direction', 'inout'):
                with self.subTest(port=port, mutation=mutation):
                    cells = sector_memory_cells(packed=True)
                    pins = cells[name]['connections']
                    bit = pins[port][0]
                    if mutation in ('zero', 'one'):
                        # Replace input drivers with a constant. A constant on
                        # read data conflicts with the RAM's own output driver.
                        if port == 'B1DATA':
                            pins[port][0] = 901
                            bit = 901
                        else:
                            del cells[f'driver{bit}']
                        cells['live_constant'] = {'type': 'MISTRAL_CONST',
                            'connections': {'Q': [bit]}, 'port_directions': {'Q': 'output'},
                            'parameters': {'LUT': f'{int(mutation == "one"):032b}'}}
                    elif mutation == 'undriven':
                        if port == 'B1DATA':
                            # A read-data port with unresolved direction cannot
                            # establish a live output merely from its net ID.
                            del cells[name]['port_directions'][port]
                        else:
                            del cells[f'driver{bit}']
                    elif mutation == 'multiple':
                        cells['extra'] = {'type': 'MISTRAL_FF', 'connections': {'Q': [bit]},
                                          'port_directions': {'Q': 'output'}}
                    else:
                        source = cells[name] if port == 'B1DATA' else cells[f'driver{bit}']
                        output = port if port == 'B1DATA' else 'Q'
                        if mutation == 'unknown_direction': del source['port_directions'][output]
                        else: source['port_directions'][output] = 'inout'
                    with self.assertRaises(BuildError):
                        st.validate_sector_memory(cells)

    def test_all_ram_configuration_guard_catches_ram_outside_lab_reservation(self):
        database = ram_database()
        cells = pinned_cache_cells()
        for i, (x, y) in enumerate(st.FIRMWARE_LANE_ROWS):
            cells[f'machine.rom.lane{i}'] = {'type': 'MISTRAL_M10K',
                'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.{x}.{y}.0'}}
        for i in range(7):
            cells[f'cpu.rom{i}'] = {'type': 'MISTRAL_M10K',
                'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.5.{15+i}.0'}}
        routed = {'modules': {'top': {'cells': cells}}}
        with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)):
            self.assertEqual(st.m10k_configuration_bounds(database), (0, 0, 259, 86))
            evidence = st.validate_m10k_configurations(routed, database)
            self.assertEqual(evidence['checked_cells'], 201)
            self.assertEqual(evidence['mode_control_bits_per_cell'], 126)
            # The lower safe site's minimum Y is exactly the exclusive fence edge.
            self.assertEqual(evidence['cells']['video.cache0.0.0.0']['configuration_bounds'][1], 1722)
            for name in ('cpu.rom0', 'machine.rom.lane0', 'video.cache0.0.0.0'):
                with self.subTest(name=name):
                    changed = json.loads(json.dumps(routed))
                    changed['modules']['top']['cells'][name]['attributes']['NEXTPNR_BEL'] = 'MISTRAL_M10K.26.19.0'
                    with self.assertRaisesRegex(BuildError, 'configuration footprint.*overlaps slot'):
                        st.validate_m10k_configurations(changed, database)
            # An inferred TDP uses the same hardware RAM configuration footprint.
            cells['cpu.rom0']['type'] = 'MISTRAL_M10K_TDP'
            st.validate_m10k_configurations(routed, database)
            cells['cpu.rom0']['attributes']['NEXTPNR_BEL'] = 'MISTRAL_M10K.26.19.0'
            with self.assertRaisesRegex(BuildError, 'configuration footprint.*overlaps slot'):
                st.validate_m10k_configurations(routed, database)

    def test_control_configuration_bits_are_guarded_separately_from_init(self):
        database = ram_database()
        x, y = rom_map.SX120F.x_to_bx[26], 2 + 86 * 20
        socket = SimpleNamespace(slot=1, cram=(x, y, x+3, y+1))
        init = rom_map.rom_blocks(rom_map.parse_ram_offsets(database['data/m10k-mux.txt'].decode()), ((26, 20),))
        self.assertTrue(all(bit % 7605 >= x+3 for word in init[0]['word_bits'] for bit in word))
        routed = {'modules': {'top': {'cells': pinned_cache_cells()}}}
        with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)), \
             patch.object(atari_st_slot, 'SOCKETS', (socket,)):
            with self.assertRaisesRegex(BuildError, 'configuration footprint.*overlaps slot'):
                st.validate_m10k_configurations(routed, database)

    def test_ram_geometry_rejects_unpinned_missing_controls_and_bad_sites(self):
        database = ram_database()
        with self.assertRaisesRegex(BuildError, 'pinned Mistral'):
            st.m10k_configuration_bounds(database)
        for mutation in ('missing_control', 'outside_tile'):
            changed = dict(database)
            text = changed['data/m10k-mux.txt'].decode()
            if mutation == 'missing_control': text = text.replace('g control0 b- 0.0\n', '')
            else: text = text.replace('0.85', '0.86')
            changed['data/m10k-mux.txt'] = text.encode()
            with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(changed)), self.assertRaises(BuildError):
                st.m10k_configuration_bounds(changed)
        for bel in ('', 'MISTRAL_M10K.26.20.1', 'MISTRAL_M10K.26.81.0', 'MISTRAL_M10K.25.20.0'):
            routed = {'modules': {'top': {'cells': {'ram': {'type': 'MISTRAL_M10K',
                'attributes': {'NEXTPNR_BEL': bel}}}}}}
            with patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)), self.assertRaises(BuildError):
                st.validate_m10k_configurations(routed, database)

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

    def test_ram_overlap_rejects_seal_even_after_a_timing_winner(self):
        database = ram_database()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / st.QSF).parent.mkdir(parents=True)
            (root / st.QSF).write_text('# board pins\n')
            authenticated = {name: SimpleNamespace(identity='test', path=Path('/auth/install/bin') / name)
                             for name in ('mistral', 'yosys', 'nextpnr-mistral')}
            invocation = SimpleNamespace(inputs={}, env={}, close=Mock())
            def synth(*args, **kwargs):
                (root / st.OUTPUT_RELATIVE / 'synth.json').write_text('{}')
            def timing_winner(**kwargs):
                output = root / st.OUTPUT_RELATIVE
                cells = pinned_cache_cells()
                cells['cpu.rom'] = {'type': 'MISTRAL_M10K',
                    'attributes': {'NEXTPNR_BEL': 'MISTRAL_M10K.26.19.0'}}
                (output / 'routed.json').write_text(json.dumps({'modules': {'top': {'cells': cells}}}))
                (output / 'core.rbf').write_bytes(b'unqualified')
                return SimpleNamespace(seed=4, weight=2000)
            with patch.object(st, '_require_clean_source', return_value=('repo', 'a'*40)), \
                 patch.object(st, '_authenticate_atari_st_tools', return_value=authenticated), \
                 patch.object(st.rom_map, 'read_database', return_value=database), \
                 patch.object(st, 'ROM_DATABASE_SHA256', database_pins(database)), \
                 patch.object(st, 'FunctionalInvocation', return_value=invocation), \
                 patch.object(st, 'create_build_record', return_value=b'{}'), \
                 patch.object(st, 'prepare_cpu_inputs'), \
                 patch.object(st, 'build_identity', return_value='0'*32), \
                 patch.object(st, '_run_tool', side_effect=synth), \
                 patch.object(st, 'clock_read_only_memories', return_value=[]), \
                 patch.object(st, 'validate_synth_evidence'), \
                 patch.object(st, '_i2c_evidence'), \
                 patch.object(st, 'validate_routed_shell'), \
                 patch.object(st, 'route_after_synth', side_effect=timing_winner), \
                 patch.object(st, 'export_package') as export:
                with self.assertRaisesRegex(BuildError, 'configuration footprint.*overlaps slot'):
                    st.build(root)
                export.assert_not_called()
            invocation.close.assert_called_once()
            for name in ('core.rbf', 'manifest.toml', 'rom-map.json', 'build-summary.json'):
                self.assertFalse((root / st.OUTPUT_RELATIVE / name).exists())

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
