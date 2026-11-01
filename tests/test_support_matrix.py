import importlib.util
from pathlib import Path
import tomllib
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    'support_matrix', ROOT / 'docs' / 'support-matrix.py'
)
SUPPORT_MATRIX = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUPPORT_MATRIX)


class SupportMatrixTests(unittest.TestCase):
    def test_pcxt_dos_is_an_experimental_unaccepted_quartus_recipe(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.pcxt-dos')
        self.assertTrue(row['recipe'])
        self.assertEqual('bring-up', row['tier'])
        self.assertEqual('quartus', row['primary_toolchain'])
        self.assertEqual('experimental', row['library_standing'])
        self.assertIn('Experimental', row['notes'])
        self.assertIsNone(row['signals']['last_hil_pass'])
        self.assertIn('no exact-artifact hardware acceptance', row['features'])
        self.assertIn('loaded at runtime through the mailbox', row['features'])
        self.assertNotIn('no runtime floppy insertion', row['features'])
        self.assertIn('a93e3921', row['features'])
        self.assertIn('72febda79', row['features'])
        self.assertIn('no exact-artifact hardware acceptance', row['features'])
        self.assertIn('sealed QSF', row['notes'])
        self.assertEqual('Quartus', SUPPORT_MATRIX.de10_cell(row))
        self.assertIn('| PC/XT (FreeDOS) | Bring-up | Quartus |', SUPPORT_MATRIX.render(data))

    def test_de25_cell_is_blank_for_rows_without_board_bitstreams(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        for core_id in ('sim.pong', 'fes.pc386', 'i386', 'z80', 'cpu6502', 'fx68k', 'tv80'):
            with self.subTest(core_id=core_id):
                self.assertNotEqual('de10', by_id[core_id]['board'])
                self.assertEqual('—', SUPPORT_MATRIX.de25_cell(by_id[core_id]))

    def test_optional_atari_st_row_shows_derived_tier(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.atari-st')
        self.assertTrue(row['optional'])
        self.assertEqual('experimental', row['library_standing'])
        self.assertEqual('experimental', row['signals']['catalog_standing'])
        self.assertEqual('current', row['tier'])
        line = next(
            line for line in SUPPORT_MATRIX.render(data).splitlines()
            if line.startswith('| Atari ST (520ST) |')
        )
        self.assertIn('| Current |', line)
        self.assertNotIn('experimental', line)

    def test_deano_overrides_replace_derived_tiers(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        coleco = by_id['fes.coleco']
        self.assertEqual('unresolved', SUPPORT_MATRIX.derive_tier(coleco))
        self.assertEqual('aging', coleco['tier'])
        self.assertEqual('Deano 2026-10-29', coleco['tier_override']['reason'])
        rendered = SUPPORT_MATRIX.render(data)
        self.assertIn('| `fes.coleco` | Aging |', rendered)
        self.assertNotIn('Unresolved', rendered)
        self.assertIn('Spectrum (48K, 128K, +2, +2A/+3, Pentagon)', rendered)
        self.assertNotIn('Distinct model', rendered)
        self.assertNotIn('fes.spectrum-plus2', by_id)
        plus2 = next(item for item in by_id['fes.spectrum128']['variants'] if item['name'] == '+2')
        self.assertEqual('rom', plus2['kind'])
        self.assertIn('| C64 (NTSC) | Bring-up (queued) |', rendered)
        self.assertIn('| Atari ST (STE) | Bring-up (queued) |', rendered)
        self.assertEqual('stale', SUPPORT_MATRIX.derive_tier(by_id['fes.c64-ntsc']))
        self.assertEqual('bring-up-queued', by_id['fes.c64-ntsc']['tier'])
        self.assertEqual('current', SUPPORT_MATRIX.derive_tier(by_id['fes.atari-st']))
        self.assertEqual('current', by_id['fes.spectrum']['tier_override']['tier'])

    def test_missing_lock_is_reported_without_reusing_other_core_pins(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        recipes = tomllib.loads((ROOT / 'config/core-recipes.toml').read_text())['recipes']
        last_lock = ROOT / 'sources/misteross' / recipes[-1]['lock_path']
        last_pins = SUPPORT_MATRIX._pins(last_lock)
        row = next(
            row for row in data['cores']
            if row.get('oss_lock') and any(
                row['oss_lock'].get(name) != last_pins[name]
                for name in last_pins
            )
        )
        row_id = row['id']
        row['oss_lock']['path'] = 'sources/misteross/cores/missing/toolchain.lock'

        errors = SUPPORT_MATRIX.validate(ROOT, data)

        self.assertIn(f'{row_id} lock missing: {row["oss_lock"]["path"]}', errors)
        self.assertFalse(any(
            error.startswith(f'{row_id} {name} pin ')
            for error in errors
            for name in last_pins
        ))


if __name__ == '__main__':
    unittest.main()
