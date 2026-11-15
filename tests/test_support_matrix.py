import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    'support_matrix', ROOT / 'docs' / 'support-matrix.py'
)
SUPPORT_MATRIX = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUPPORT_MATRIX)


class SupportMatrixTests(unittest.TestCase):
    def test_pcxt_dos_records_live_media_and_the_qualification_reruns(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.pcxt-dos')
        self.assertTrue(row['recipe'])
        self.assertEqual('current', row['tier'])
        self.assertEqual('play', row['tier_role'])
        self.assertEqual('quartus', row['primary_toolchain'])
        self.assertEqual('experimental', row['library_standing'])
        self.assertFalse(row['in_bootstrap_image'])
        self.assertIn('experimental', row['notes'])
        self.assertEqual('live', row['signals']['media_gen'])
        # The 8-10 November #1529 reruns supersede the 5 November smoke.
        self.assertEqual('2026-11-10', row['signals']['last_hil_pass'])
        self.assertEqual('docs/validation/2026-11-08-pcxt-qualification-reruns/summary.md',
                         row['signals']['hil_note'])
        self.assertIn('precommitted #1529 plan', row['signals']['hil_scope'])
        self.assertIn('the power-loss case ran on the earlier package 4dd6d431',
                      row['signals']['hil_scope'])
        self.assertIn('docs/validation/2026-11-08-pcxt-qualification-reruns/summary.md',
                      row['evidence'])
        self.assertIn('docs/validation/2026-11-04-pcxt-game-and-speaker-kit-a.md',
                      row['evidence'])
        for identity in ('e73a0e87', '8d9b71fab', '4dd6d431'):
            self.assertIn(identity, row['notes'])
        self.assertIn('loaded at runtime through the mailbox', row['features'])
        self.assertIn('write sectors', row['features'])
        self.assertIn('keyboard', row['features'])
        # The reruns are scoped: the power loss ran on another package, and
        # external timing, physical ISA and the session display stay open.
        self.assertIn('not exact-artifact acceptance of e73a0e87', row['notes'])
        self.assertIn('#835', row['notes'])
        # Closed issues stay off the row.
        for closed in ('#1402', '#1561', '#1613'):
            self.assertNotIn(closed, row['signals']['open_issues'])
        self.assertEqual('Quartus', SUPPORT_MATRIX.de10_cell(row))
        self.assertIn('| PC/XT (FreeDOS) | Current | Quartus |', SUPPORT_MATRIX.render(data))

    def test_pc386_records_the_sealed_development_package_on_a_kit(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.pc386')
        # Not a recipe and not a catalog package, but no longer simulation only.
        self.assertFalse(row['recipe'])
        self.assertEqual('bring-up', row['tier'])
        self.assertEqual('de10', row['board'])
        self.assertEqual('quartus', row['primary_toolchain'])
        self.assertEqual('live', row['signals']['media_gen'])
        self.assertEqual('2026-11-12', row['signals']['last_hil_pass'])
        self.assertEqual('docs/validation/2026-11-12-pc386-windows-kit/summary.md',
                         row['signals']['hil_note'])
        self.assertIn('Kit B', row['signals']['hil_scope'])
        for issue in row['signals']['open_issues']:
            self.assertIn(issue, ('#1368', '#1494', '#1235'))
        self.assertIn('Windows for Workgroups 3.11', row['notes'])
        self.assertIn('Linux 0.99.15, 1.2.13 and 2.0.0', row['notes'])
        self.assertIn('not a catalog package', row['notes'])
        self.assertIn('Phase 6 (#1768)', row['features'])
        # Each kit result names its own artifact; the level exits are the
        # 6 November run, not the 10 November seal.
        self.assertIn('254b2bf5', row['notes'])
        self.assertIn('were not repeated on the later seal', row['notes'])
        self.assertEqual('Quartus', SUPPORT_MATRIX.de10_cell(row))
        self.assertIn('| pc386 | Bring-up | Quartus | Quartus | disk, keys, mouse, HDMI |',
                      SUPPORT_MATRIX.render(data))

    def test_check_level_is_derived_from_the_quality_ladder(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, data))
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual('proposed', data['check_policy']['status'])
        self.assertIsNone(data['check_policy']['decided_by'])
        # Tier 1 and tier 2 cells carry full evidence; the rest carry it only
        # for changes aimed at them.
        self.assertEqual('full', by_id['fes.pcxt-dos']['check_level'])
        self.assertEqual('1-pending', by_id['fes.pc386']['quality_tier'])
        self.assertEqual('full', by_id['fes.pc386']['check_level'])
        self.assertEqual('full', by_id['fes.menu']['check_level'])
        for core_id in ('fes.pong', 'fes.ramtest', 'fes.riscv', 'fes.coleco',
                        'fes.sms', 'fes.sg1000', 'fes.apple2'):
            with self.subTest(core_id=core_id):
                self.assertEqual('reduced', by_id[core_id]['check_level'])
                self.assertFalse(by_id[core_id]['in_bootstrap_image'])
                self.assertEqual('held-back', by_id[core_id]['release'])
        self.assertEqual('via-consumer', by_id['i386']['check_level'])
        self.assertEqual('none', by_id['fes.c64-ntsc']['check_level'])
        self.assertIn('- **Reduced checks:** `fes.pong`', SUPPORT_MATRIX.render(data))
        self.assertIn(SUPPORT_MATRIX.CHECK_NOT_IN_FORCE, SUPPORT_MATRIX.render(data))

    def test_a_hand_set_check_level_or_quality_tier_is_rejected(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        by_id['fes.riscv']['check_level'] = 'full'
        by_id['fes.c64']['quality_tier'] = '2-pending'
        by_id['fes.menu']['quality_tier'] = '1'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn("fes.riscv check_level 'full' != derived 'reduced'", errors)
        self.assertTrue(any(error.startswith('quality_tier 1 rows') for error in errors), errors)
        self.assertTrue(any(error.startswith('quality_tier 2-pending rows') for error in errors), errors)
        self.assertIn(
            "fes.menu is not on the forward plan's ladder but records quality_tier '1'", errors
        )

    def test_a_reduced_row_cannot_be_shipped_or_in_the_image(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        by_id['fes.sms']['release'] = 'v1'
        by_id['fes.riscv']['in_bootstrap_image'] = True
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn("fes.sms is release 'v1' and cannot be reduced", errors)
        self.assertIn('fes.riscv is in the bootstrap image and cannot be reduced', errors)

    def test_the_check_policy_cannot_claim_to_be_accepted_on_its_own(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        data['check_policy']['status'] = 'accepted'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertTrue(
            any('check_policy status is not proposed' in error for error in errors), errors
        )

    def test_the_quality_ladder_citation_must_still_be_in_the_forward_plan(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        cases = {
            # The ladder is gone.
            'no ladder here\n': 'no longer says',
            # The ladder's sentences moved out of the Tiers section.
            plan.replace('## Tiers', '## Something else'): 'has no',
            # A second statement of the same placement may supersede it.
            plan + '\n' + quote + '\n': 'may supersede',
            # The plan stops naming a pending placement's condition.
            plan.replace('#1368 and #1494 close', 'its blockers close'): 'no longer names',
        }
        for text, expected in cases.items():
            with self.subTest(expected=expected), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / 'docs' / 'plans').mkdir(parents=True)
                (root / 'docs' / 'plans' / 'forward-plan.md').write_text(text)
                errors = SUPPORT_MATRIX.validate_check_policy(root, data, by_id, None)
                self.assertTrue(
                    any(expected in error for error in errors), (expected, errors)
                )

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
        for core_id in ('sim.pong', 'i386', 'z80', 'cpu6502', 'fx68k', 'tv80'):
            with self.subTest(core_id=core_id):
                self.assertNotEqual('de10', by_id[core_id]['board'])
                self.assertEqual('—', SUPPORT_MATRIX.de25_cell(by_id[core_id]))
        # pc386's sealed development package has a DE10-Nano bitstream, so its
        # DE25 cell follows the policy line like every other board row.
        self.assertEqual('de10', by_id['fes.pc386']['board'])
        self.assertEqual('Quartus', SUPPORT_MATRIX.de25_cell(by_id['fes.pc386']))

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
        last_oss = next(row for row in reversed(recipes)
                        if row.get('board', 'de10-nano') == 'de10-nano'
                        and row.get('toolchain', 'oss') == 'oss')
        last_lock = ROOT / 'sources/misteross' / last_oss['lock_path']
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

    def test_de25_recipe_does_not_replace_de10_matrix_inputs(self):
        raw = (ROOT / 'config/core-recipes.toml').read_text()
        loads = tomllib.loads
        document = loads(raw)
        de25 = [row for row in document['recipes'] if row.get('board') == 'de25-nano']
        de10 = [row for row in document['recipes'] if row.get('board', 'de10-nano') == 'de10-nano']
        self.assertTrue(de25)
        for entries in (de10 + de25, de25 + de10):
            with self.subTest(first_board=entries[0].get('board', 'de10-nano')):
                def selected(text):
                    return dict(document, recipes=entries) if text == raw else loads(text)
                with patch.object(SUPPORT_MATRIX.tomllib, 'loads', side_effect=selected):
                    self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, SUPPORT_MATRIX.load_json(ROOT)))

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
