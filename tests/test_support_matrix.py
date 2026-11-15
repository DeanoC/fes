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
        # The 387 is built as far as slice 2; its arithmetic is not.
        self.assertIn('slice 2 (#1768)', row['features'])
        self.assertIn('arithmetic is slices 3 to 5 and is not built', row['features'])
        # Each kit result names its own artifact; the level exits are the
        # 6 November run, not the 10 November seal.
        # Each kit result names its own seal, and the rerun's gaps are kept.
        for artifact in ('254b2bf5', '650bc477', '6ef05309', '9d784de2'):
            self.assertIn(artifact, row['notes'])
        self.assertIn('the Wolfenstein 3D playthrough was not rerun', row['notes'])
        self.assertIn('seven launches of eight', row['notes'])
        # The pitch pass is a regression comparison; against the named
        # reference it fails (#1837), and the pace work is #1840 and #1853.
        self.assertIn('not a reference', row['notes'])
        for issue in ('#1837', '#1840', '#1853'):
            self.assertIn(issue, row['notes'])
        self.assertEqual('Quartus', SUPPORT_MATRIX.de10_cell(row))
        # bring-up here is a profile, not an absent recipe: fes.pcxt has one.
        meaning = next(t['meaning'] for t in SUPPORT_MATRIX.load_json(ROOT)['tiers']
                       if t['id'] == 'bring-up')
        # It claims neither an absent recipe (fes.pcxt has one), nor an
        # absent catalog standing (fes.pcxt is experimental in the library),
        # nor a separate playable row (pc386 has none).
        self.assertNotIn('No recipe', meaning)
        self.assertNotIn('No catalog package', meaning)
        self.assertNotIn('separate row', meaning)
        self.assertIn('dated evidence on the row', meaning)
        self.assertIn('| pc386 | Bring-up | Quartus | Quartus | disk, keys, mouse, HDMI |',
                      SUPPORT_MATRIX.render(data))

    def test_check_level_is_derived_from_the_quality_ladder(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, data))
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual('accepted', data['check_policy']['status'])
        self.assertEqual('Deano 2026-11-15', data['check_policy']['decided_by'])
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
        self.assertIn(SUPPORT_MATRIX.CHECK_IN_FORCE, SUPPORT_MATRIX.render(data))
        self.assertNotIn(SUPPORT_MATRIX.CHECK_NOT_IN_FORCE, SUPPORT_MATRIX.render(data))

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

    def test_an_accepted_check_policy_needs_its_rule_in_agents_md(self):
        """Accepted means AGENTS.md carries the obligation. Without that rule the
        levels on this page would claim something nothing states."""
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        agents = (ROOT / SUPPORT_MATRIX.AGENTS).read_text()
        cite = SUPPORT_MATRIX.AGENTS_RULE_CITE
        self.assertIn(cite, agents)
        bullet = next(line for line in agents.splitlines() if cite in line)
        # Gutting the rule around its marker must not pass, and nor must a
        # second, possibly contradictory, copy of it.
        cases = [
            # Gutted around its marker.
            ('rule gutted around the marker',
             agents.replace(bullet, f'- The marker `check_level: reduced` states no obligation. {cite}')),
            # A clause dropped.
            ('one clause dropped', agents.replace('puts that core back under the full rule', 'is fine')),
            # Every clause kept, and then negated or weakened by what follows:
            # this is why the rule is compared whole and not by its clauses.
            ('negation appended',
             agents.replace(bullet, bullet + ' This rule states no obligation despite the above.')),
            ('exception appended',
             agents.replace('never lower a gate on work being done to a core',
                            'never lower a gate on work being done to a core, except where a lane owner says so')),
            ('cite gone', agents.replace(cite, '')),
            ('rule stated twice', agents + '\n' + bullet + '\n'),
            ('file gone', None),
        ]
        for label, text in cases:
            with self.subTest(label), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / 'docs' / 'plans').mkdir(parents=True)
                (root / 'docs' / 'plans' / 'forward-plan.md').write_text(plan)
                if text is not None:
                    (root / 'AGENTS.md').write_text(text)
                errors = SUPPORT_MATRIX.validate_check_policy(root, data, by_id, None)
                self.assertTrue(
                    any('AGENTS.md' in error for error in errors), (label, errors)
                )

    def test_a_rewrapped_agents_rule_still_passes(self):
        """A formatting-only edit is not a change to the rule."""
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        agents = (ROOT / SUPPORT_MATRIX.AGENTS).read_text()
        bullet = next(line for line in agents.splitlines()
                      if SUPPORT_MATRIX.AGENTS_RULE_CITE in line)
        words = bullet.removeprefix('- ').split()
        wrapped = '- ' + ' '.join(words[:12]) + '\n'
        for i in range(12, len(words), 12):
            wrapped += '  ' + ' '.join(words[i:i + 12]) + '\n'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'docs' / 'plans').mkdir(parents=True)
            (root / 'docs' / 'plans' / 'forward-plan.md').write_text(plan)
            (root / 'AGENTS.md').write_text(agents.replace(bullet + '\n', wrapped))
            self.assertEqual([], SUPPORT_MATRIX.agents_rule_errors(
                (root / 'AGENTS.md').read_text()
            ))
            errors = SUPPORT_MATRIX.validate_check_policy(root, data, by_id, None)
            self.assertEqual([], [e for e in errors if 'AGENTS.md' in e], errors)

    def test_heading2_follows_commonmark(self):
        """`##Tiers` is ordinary text; `## Tiers ##` is the closed form."""
        cases = {
            '## Tiers': 'Tiers',
            '  ## Tiers': 'Tiers',
            '## Tiers ##': 'Tiers',
            '## Tiers (superseded)': 'Tiers (superseded)',
            '##': '',
            '##Tiers': None,
            '### Tiers': None,
            '    ## Tiers': None,
            '- a bullet': None,
        }
        for line, expected in cases.items():
            with self.subTest(line=line):
                self.assertEqual(expected, SUPPORT_MATRIX.heading2(line))

    def test_the_ladder_heading_must_be_a_real_heading(self):
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        # Not a heading at all, so there is no section to read.
        self.assertIsNone(SUPPORT_MATRIX.ladder_section(plan.replace('## Tiers', '##Tiers', 1)))
        # The closed form is the same heading.
        closed = SUPPORT_MATRIX.ladder_section(plan.replace('## Tiers', '## Tiers ##', 1))
        self.assertIsNotNone(closed)
        self.assertIn(quote, closed)

    def test_a_later_heading_ends_the_ladder_section(self):
        """A level-two section ends at any heading of its level or above, and
        Markdown allows up to three spaces before the hashes. Otherwise the old
        ladder text keeps answering from under an unrelated later section."""
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        for label, inserted in (
            ('level two', '   ## Later'),
            ('level one', '# Replacement section'),
            ('level one indented', '  # Replacement section'),
        ):
            with self.subTest(label):
                moved = plan.replace('## Tiers', f'## Tiers\n\n{inserted}\n', 1)
                section = SUPPORT_MATRIX.ladder_section(moved)
                self.assertIsNotNone(section)
                self.assertNotIn(quote, section)
        # A deeper heading is inside the section, not a boundary.
        deeper = plan.replace('## Tiers', '## Tiers\n\n### Inside\n', 1)
        self.assertIn(quote, SUPPORT_MATRIX.ladder_section(deeper))

    def test_a_setext_heading_ends_the_ladder_section(self):
        """A paragraph underlined with `=` or `-` is a heading, so the section
        ends at the line above the underline, not after it."""
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        for label, underline in (('level two', '-----------'),
                                 ('level one', '==========='),
                                 ('single dash', '-'),
                                 ('indented', '   ---')):
            with self.subTest(label):
                moved = plan.replace(
                    '## Tiers', f'## Tiers\n\nReplacement\n{underline}\n', 1
                )
                section = SUPPORT_MATRIX.ladder_section(moved)
                self.assertIsNotNone(section)
                self.assertNotIn(quote, section)
                # The heading's own text is not part of the section either.
                self.assertNotIn('Replacement', section)

    def test_a_fenced_block_is_not_markdown(self):
        """Example Markdown inside a fence is neither a heading to read nor a
        second heading to complain about."""
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        section = plan[plan.index('## Tiers'):]
        # The whole section kept inside a fence for history, with no real one
        # left: there is no section to read, which fails closed.
        for fence in ('```', '~~~~', '   ```'):
            with self.subTest(f'fenced away with {fence!r}'):
                self.assertIsNone(SUPPORT_MATRIX.ladder_section(
                    f'# Plan\n\n{fence}\n{section}\n{fence.strip()}\n'
                ))
        # A legitimate fenced example beside the real section must not count as
        # a second heading, or it would fail the build on valid content.
        for opener in ('```markdown', '~~~ markdown', '```'):
            with self.subTest(f'example in {opener!r}'):
                closer = '~~~' if opener.startswith('~') else '```'
                example = plan.replace(
                    '## Tiers',
                    f'{opener}\n## Tiers\n\nnot the real one\n{closer}\n\n## Tiers', 1
                )
                read = SUPPORT_MATRIX.ladder_section(example)
                self.assertIsNotNone(read)
                self.assertIn(quote, read)
                self.assertNotIn('not the real one', read)

    def test_a_fenced_agents_rule_is_not_the_rule(self):
        """A fenced quotation of the rule cannot stand in for it, nor collide
        with the real one."""
        agents = (ROOT / SUPPORT_MATRIX.AGENTS).read_text()
        bullet = next(line for line in agents.splitlines()
                      if SUPPORT_MATRIX.AGENTS_RULE_CITE in line)
        # Quoted in a fence, with the real rule gone.
        quoted = agents.replace(bullet, f'```\n{bullet}\n```')
        self.assertTrue(any('no longer carries' in error
                            for error in SUPPORT_MATRIX.agents_rule_errors(quoted)), quoted[:0])
        # Quoted in a fence beside the real rule: still exactly one rule.
        beside = agents.replace(bullet, f'```\n{bullet}\n```\n{bullet}')
        self.assertEqual([], SUPPORT_MATRIX.agents_rule_errors(beside))

    def test_setext_underline_only_matches_underlines(self):
        cases = {'---': True, '===': True, '-': True, '  ----  ': True,
                 '    ----': False, '- a bullet': False, '--- text': False,
                 '': False, '| --- | --- |': False}
        for line, expected in cases.items():
            with self.subTest(line=line):
                self.assertEqual(expected, SUPPORT_MATRIX.setext_underline(line))

    def test_the_real_ladder_section_has_no_underline_lines(self):
        """The guard above must not be truncating the live section: a Markdown
        table's `| --- |` row is not an underline, and nothing else matches."""
        section = SUPPORT_MATRIX.ladder_section(
            (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        )
        for quote in SUPPORT_MATRIX.LADDER_QUOTES:
            self.assertIn(quote, section)

    def test_ends_section_only_matches_real_headings(self):
        cases = {'# Top': True, '## Two': True, '   # Indented': True, '##': True,
                 '#Top': False, '### Three': False, '    # Four spaces': False,
                 'text': False, '- a bullet': False}
        for line, expected in cases.items():
            with self.subTest(line=line):
                self.assertEqual(expected, SUPPORT_MATRIX.ends_section(line))

    def test_an_accepted_check_policy_names_who_decided_it(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        data['check_policy']['decided_by'] = None
        errors = SUPPORT_MATRIX.validate_check_policy(ROOT, data, by_id, None)
        self.assertIn('check_policy is accepted but does not say who decided it', errors)

    def test_an_unknown_check_policy_status_is_rejected(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        data['check_policy']['status'] = 'in review'
        errors = SUPPORT_MATRIX.validate_check_policy(ROOT, data, by_id, None)
        self.assertTrue(
            any("status 'in review' is not one of" in error for error in errors), errors
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
            plan.replace('## Tiers', '## Something else'): 'exactly one',
            # A superseded section kept beside a new authoritative one must not
            # keep answering for the ladder: only the exact `## Tiers` heading
            # is read, so the old section's quotes no longer validate.
            plan.replace('## Tiers', '## Tiers (superseded)') + '\n## Tiers\n\nnew ladder here\n': 'no longer says',
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
