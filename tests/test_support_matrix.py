import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
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

    def test_every_release_package_records_its_scope(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, data))
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual('v1', by_id['fes.atari-st']['release'])
        self.assertEqual('v2', by_id['fes.pc386']['release'])
        self.assertEqual('held-back', by_id['fes.coleco']['release'])
        self.assertNotIn('release', by_id['fes.menu'])
        self.assertIn('- **v2:** `fes.pcxt`, `fes.pcxt-dos`, `fes.pc386`', SUPPORT_MATRIX.render(data))
        del by_id['fes.sms']['release']
        by_id['z80']['release'] = 'v1'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn("fes.sms release None is not one of v1, v2, held-back", errors)
        self.assertIn('z80 is not a release package but records release', errors)

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

    def test_duplicate_tier_ids_are_reported(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        data['tiers'].append(dict(data['tiers'][0]))
        self.assertIn('duplicate tier ids', SUPPORT_MATRIX.validate(ROOT, data))

    def test_invalid_rows_report_errors_instead_of_crashing(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        data['cores'][0]['tier'] = 'nonsense'
        data['cores'].append(dict(data['cores'][1], id='fes.unknown'))
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn(f"{data['cores'][0]['id']} uses unknown tier nonsense", errors)
        self.assertFalse(any('has unknown tier' in error for error in errors))
        self.assertTrue(any(error.startswith('highlight keys') for error in errors))

    def test_invalid_tier_does_not_hide_unrelated_validation_errors(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        data['cores'][0]['tier'] = 'nonsense'
        data['cores'][0]['notes'] += ' Distinct model'
        data['policy'] = []
        by_id['fes.coleco']['notes'] = 'Coleco notes.'
        plus2 = next(item for item in by_id['fes.spectrum128']['variants'] if item['name'] == '+2')
        plus2['kind'] = 'model'
        by_id['fes.riscv'].update(board='de10', primary_toolchain='quartus')

        errors = SUPPORT_MATRIX.validate(ROOT, data)

        self.assertIn(f"{data['cores'][0]['id']} uses unknown tier nonsense", errors)
        self.assertTrue(any(error.startswith('human page cannot be rendered:') for error in errors))
        self.assertIn("support matrix still says 'Distinct model'", errors)
        self.assertIn("fes.spectrum128 must record +2 as a ROM variant", errors)
        self.assertIn("fes.riscv DE10 cell says Quartus without the primary policy", errors)
        self.assertIn("policy section is missing 'policy: Deano 2026-10-29'", errors)
        self.assertIn("fes.coleco notes are missing 'Adam'", errors)

    def test_shared_rows_record_the_owned_rtl_commit(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        z80 = SUPPORT_MATRIX.owned_rtl(ROOT, by_id['z80'])
        self.assertEqual(z80, (
            'e4d7402bf6611aeae723eaccfcde91c735004e1f',
            '2026-10-08',
            'Share Z80 DAA magnitude bit tests with exhaustive equivalence proof',
        ))
        self.assertEqual(40, len(z80[0]))
        # A two-file edit inside a 43-path C64 commit is not owned cpu6502 RTL.
        self.assertIsNone(SUPPORT_MATRIX.owned_rtl(ROOT, by_id['cpu6502']))
        by_id['z80']['signals'].update(last_rtl_change='2026-10-24', last_rtl_sha='e93b15b24')
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertTrue(any(error.startswith(
            'z80 owned RTL is e4d7402bf6611aeae723eaccfcde91c735004e1f on 2026-10-08'
        ) for error in errors))

    def test_shared_row_rejects_a_different_commit_with_the_same_date(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        recorded = by_id['i386']['signals']
        self.assertEqual('8f06961a9', recorded['last_rtl_sha'])
        self.assertEqual(
            'i386 Phase 1: BT, BTS, BTR, BTC by an immediate, BSF and BSR (#1185)',
            recorded['last_rtl_subject'],
        )
        recorded.update(
            last_rtl_sha='eea51e94e',
            last_rtl_subject="i386 Phase 1: interrupts between a REP string's elements (#1181)",
        )

        errors = SUPPORT_MATRIX.validate(ROOT, data)

        self.assertTrue(any(error.startswith(
            'i386 owned RTL is 8f06961a9ec899963eda280c96af4d7149feadcb on 2026-10-29'
        ) for error in errors))

    def test_owned_rtl_uses_the_pinned_history_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)

            def git(*args, env=None):
                return subprocess.run(
                    ['git', '-C', str(root), *args], check=True,
                    capture_output=True, text=True, env=env,
                ).stdout.strip()

            git('init', '-q')
            git('config', 'user.name', 'Support Matrix Test')
            git('config', 'user.email', 'support-matrix@example.invalid')
            relative = Path('sources/misteross/cores/fes-common/rtl/z80/core.sv')
            source = root / relative
            source.parent.mkdir(parents=True)

            def commit(message, author_date, committer_date):
                env = os.environ.copy()
                env.update({
                    'GIT_AUTHOR_DATE': author_date,
                    'GIT_COMMITTER_DATE': committer_date,
                })
                git('add', str(relative), env=env)
                git('commit', '-m', message, env=env)
                return git('rev-parse', 'HEAD')

            first_date = '2026-10-20T12:00:00+00:00'
            source.write_text('module z80_core; endmodule\n')
            snapshot = commit('Z80 ownership baseline', first_date, first_date)
            source.write_text('module z80_core; wire later_change; endmodule\n')
            commit(
                'Z80 backdated follow-up',
                first_date,
                '2026-11-01T12:00:00+00:00',
            )

            previous_snapshot = SUPPORT_MATRIX.AS_OF_COMMIT
            SUPPORT_MATRIX.AS_OF_COMMIT = snapshot
            try:
                owned = SUPPORT_MATRIX.owned_rtl(root, {
                    'id': 'z80',
                    'paths': ['sources/misteross/cores/fes-common/rtl/z80'],
                })
            finally:
                SUPPORT_MATRIX.AS_OF_COMMIT = previous_snapshot

        self.assertEqual((snapshot, '2026-10-20', 'Z80 ownership baseline'), owned)

    def test_group_features_mark_what_only_some_models_have(self):
        self.assertEqual('tape, disk (some), keys, HDMI', SUPPORT_MATRIX.feature_union(
            ['fes.spectrum', 'fes.spectrum128', 'fes.spectrum-plus3', 'fes.pentagon']))
        self.assertEqual('tape, keys, HDMI', SUPPORT_MATRIX.feature_union(['fes.zx81', 'fes.zx80']))


if __name__ == '__main__':
    unittest.main()
