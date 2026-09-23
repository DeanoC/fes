import unittest
from scripts.rom_map import parse_ram_offsets, zx81_blocks


def mux_text():
    return 'm ram r-:40\n' + ''.join(
        '  * ' + ' '.join(f'{word+3}.{bit+19}' for bit in range(40)) + '\n'
        for word in range(256)) + 'g unrelated b- 0.0\n'


def routed_rom():
    return {'modules': {'top': {'cells': {
        f'machine.rom.lane{i}': {'type': 'MISTRAL_M10K',
            'attributes': {'NEXTPNR_BEL': f'MISTRAL_M10K.5.{73+i}.0'},
            'parameters': {'CFG_ABITS': '00001010', 'CFG_DBITS': '00001010',
                           'CFG_ASYNC_READ': '1', 'INIT': '0'*10240}}
        for i in range(8)}}}}


class ROMMapTests(unittest.TestCase):
    def test_routed_rom_rejects_misplacement_missing_lane_and_nonblank_init(self):
        from scripts import rom_map
        self.assertTrue(hasattr(rom_map, 'validate_routed_rom'), 'routed ROM validation is missing')
        rom_map.validate_routed_rom(routed_rom())
        for field, value in [('NEXTPNR_BEL', 'MISTRAL_M10K.5.74.0'),
                             ('INIT', '1'+'0'*10239), ('CFG_DBITS', '00101000')]:
            design = routed_rom()
            cell = design['modules']['top']['cells']['machine.rom.lane0']
            cell['attributes' if field == 'NEXTPNR_BEL' else 'parameters'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                rom_map.validate_routed_rom(design)
        design = routed_rom()
        del design['modules']['top']['cells']['machine.rom.lane7']
        with self.assertRaises(ValueError):
            rom_map.validate_routed_rom(design)

    def test_database_authentication_rejects_tamper_and_symlinks(self):
        import tempfile
        import hashlib
        from pathlib import Path
        from scripts import rom_map
        self.assertTrue(hasattr(rom_map, 'read_database'), 'authenticated database reader is missing')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pins = {}
            for name in ('data/m10k-mux.txt', 'libmistral/cvd-sx120f.cc', 'libmistral/cyclonev.h'):
                path = root/name
                path.parent.mkdir(exist_ok=True)
                path.write_bytes(b'pinned')
                pins[name] = hashlib.sha256(b'pinned').hexdigest()
            self.assertEqual(set(rom_map.read_database(root, pins)), set(pins))
            path.write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'digest'):
                rom_map.read_database(root, pins)
            path.unlink()
            path.symlink_to(root/'data/m10k-mux.txt')
            with self.assertRaisesRegex(ValueError, 'symlink'):
                rom_map.read_database(root, pins)

    def test_database_coordinates_and_lane_offsets(self):
        blocks = zx81_blocks(parse_ram_offsets(mux_text()))
        self.assertEqual(len(blocks), 8)
        self.assertEqual(blocks[0]['bel'], 'M10K.005.073')
        self.assertEqual(blocks[-1]['source_offset'], 7168)
        self.assertEqual(blocks[0]['word_bits'][0][0], (2+86*73+19)*7605+318)
        self.assertEqual(blocks[-1]['word_bits'][-1][-1], (2+86*80+58)*7605+573)
        self.assertEqual(len({b for block in blocks for word in block['word_bits'] for b in word}), 81920)

    def test_reject_missing_truncated_duplicate_and_out_of_tile_offsets(self):
        source = mux_text()
        for text in ['', source.replace('m ram r-:40', 'm ram r-:39'),
                     source.replace('3.19 ', '', 1),
                     source.replace('4.19', '3.19', 1),
                     source.replace('3.19', '300.19', 1),
                     source.replace('3.19', '3.86', 1)]:
            with self.subTest(text=text[:50]), self.assertRaises(ValueError):
                parse_ram_offsets(text)

    def test_build_map_binds_base_and_rejects_nonblank(self):
        import hashlib
        import tempfile
        from pathlib import Path
        from unittest.mock import patch
        from types import SimpleNamespace
        from scripts.rom_map import build_rom_map
        from scripts.cyclonev_rbf import SX120F
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'data').mkdir()
            (root/'libmistral').mkdir()
            (root/'data/m10k-mux.txt').write_text(mux_text())
            die = ('7605, 7024, // cram size\n// x to bit x\n{' +
                   ','.join(map(str, SX120F.x_to_bx)) + '}\n// column types\n{' +
                   ','.join(['T_EMPTY']*5+['T_M10K']) + '}\n' +
                   'sx120f_bel_spans_info[] = {1, 9, 1, 73, 80, 0xff};')
            (root/'libmistral/cvd-sx120f.cc').write_text(die)
            (root/'libmistral/cyclonev.h').write_text('y = 2 + 86 * pos2y(pos);')
            cram = bytearray(b'\xff')*((SX120F.cram_sx*SX120F.cram_sy+7)//8)
            with patch('scripts.rom_map.rbf_load', return_value=SimpleNamespace(cram=cram)):
                mapping, evidence = build_rom_map(root, b'base')
                self.assertEqual(mapping['base_sha256'], hashlib.sha256(b'base').hexdigest())
                self.assertEqual(mapping['source_size'], 8192)
                self.assertEqual(len(evidence['database_sha256']), 3)
                bit = mapping['blocks'][0]['word_bits'][0][0]
                cram[bit>>3] &= ~(1 << (bit & 7))
                with self.assertRaisesRegex(ValueError, 'not a blank'):
                    build_rom_map(root, b'base')

    def test_optional_mistral_oracle(self):
        import os
        import tempfile
        import json
        from pathlib import Path
        from scripts.rom_map_oracle import generate
        binary = os.environ.get('FES_ROM_ORACLE_MISTRAL')
        source = os.environ.get('FES_ROM_ORACLE_SOURCE')
        if not binary or not source:
            self.skipTest('set FES_ROM_ORACLE_MISTRAL and FES_ROM_ORACLE_SOURCE for host oracle')
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            generate(Path(binary), Path(source), output)
            evidence = json.loads((output/'oracle-provenance.json').read_text())
            self.assertEqual(set(evidence['checks']), {'blank', 'ones', 'ramp', 'walking', 'random'})
            for checks in evidence['checks'].values():
                self.assertTrue(checks['mistral_readback'])
                self.assertEqual(checks['outside_map_changes'], 0)
