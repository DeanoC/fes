import importlib.util
import json
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
        self.assertIn('| PC/XT (FreeDOS) | Current | Quartus |', SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))

    def test_pc386_records_the_sealed_development_package_on_a_kit(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.pc386')
        # Registration does not promote the existing artifact evidence.
        self.assertTrue(row['recipe'])
        self.assertEqual('experimental', row['library_standing'])
        self.assertEqual('experimental', row['signals']['catalog_standing'])
        self.assertEqual('1', row['quality_tier'])
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
        self.assertIn('#1977', row['notes'])
        self.assertIn('#1932', row['notes'])
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
        # bring-up here is a profile, not an absent recipe: fes.pcxt-dos has one.
        meaning = next(t['meaning'] for t in SUPPORT_MATRIX.load_json(ROOT)['tiers']
                       if t['id'] == 'bring-up')
        # It claims neither an absent recipe (fes.pcxt-dos has one), nor an
        # absent catalog standing (fes.pcxt-dos is experimental in the library),
        # nor a separate playable row (pc386 has none).
        self.assertNotIn('No recipe', meaning)
        self.assertNotIn('No catalog package', meaning)
        self.assertNotIn('separate row', meaning)
        self.assertIn('dated evidence on the row', meaning)
        self.assertIn('| pc386 | Bring-up | Quartus | Quartus | disk, keys, mouse, HDMI |',
                      SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))

    def test_check_level_is_derived_from_the_quality_ladder(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, data))
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual('accepted', data['check_policy']['status'])
        self.assertEqual('Deano 2026-11-15', data['check_policy']['decided_by'])
        # Tier 1 and tier 2 cells carry full evidence; the rest carry it only
        # for changes aimed at them.
        self.assertEqual('full', by_id['fes.pcxt-dos']['check_level'])
        self.assertEqual('1', by_id['fes.pc386']['quality_tier'])
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
        self.assertIn('- **Reduced checks:** `fes.pong`', SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))
        self.assertIn(SUPPORT_MATRIX.CHECK_IN_FORCE, SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))
        self.assertNotIn(SUPPORT_MATRIX.CHECK_NOT_IN_FORCE, SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))

    def test_long_check_level_meaning_wraps_without_losing_text(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        meaning = next(
            level['meaning'] for level in data['check_policy']['levels']
            if level['id'] == 'full'
        )
        lines = SUPPORT_MATRIX.render_check_policy(data)
        start = lines.index('| Checks | Expected evidence |') + 2
        full_rows = []
        in_full = False
        for line in lines[start:]:
            if not line.startswith('| '):
                break
            cells = [part.strip() for part in line.split('|')]
            label, value = cells[1], cells[2]
            if label == 'Full':
                in_full = True
            elif label and in_full:
                break
            if in_full:
                full_rows.append(value)

        self.assertEqual(meaning, ' '.join(full_rows))
        self.assertGreater(len(full_rows), 1)
        self.assertTrue(all(
            len(line) <= SUPPORT_MATRIX.HUMAN_TABLE_ROW_LIMIT
            for line in lines
            if line.startswith('| ') and not line.startswith('| ---')
        ))

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

    def test_an_unclassified_playable_core_fails_closed(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.coleco')
        row['quality_tier'] = None

        self.assertEqual('full', SUPPORT_MATRIX.derive_check_level(row))
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn('fes.coleco is playable but has no quality_tier placement', errors)
        self.assertIn("fes.coleco check_level 'reduced' != derived 'full'", errors)

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
            ('rule commented out', agents.replace(bullet, f'<!--\n{bullet}\n-->')),
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

    def test_a_commented_agents_rule_is_not_the_rule(self):
        agents = (ROOT / SUPPORT_MATRIX.AGENTS).read_text()
        bullet = next(line for line in agents.splitlines()
                      if SUPPORT_MATRIX.AGENTS_RULE_CITE in line)
        commented = agents.replace(bullet, f'<!--\n{bullet}\n-->')
        self.assertTrue(any('no longer carries' in error
                            for error in SUPPORT_MATRIX.agents_rule_errors(commented)))

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
            plan.replace('once they meet the port', 'after porting'): 'no longer names',
            # Removing the non-blocking #1494 disposition changes the policy.
            plan.replace('non-blocking for DE10 pc386 tier 1. #1494 stays open;',
                         'pending pin-timing disposition;'): 'no longer says',
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

    def test_commented_forward_plan_ladder_is_not_recognized_or_counted(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        plan = (ROOT / SUPPORT_MATRIX.FORWARD_PLAN).read_text()
        quote = SUPPORT_MATRIX.LADDER_QUOTES[0]
        agents = (ROOT / SUPPORT_MATRIX.AGENTS).read_text()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'docs' / 'plans').mkdir(parents=True)
            plan_path = root / SUPPORT_MATRIX.FORWARD_PLAN
            (root / SUPPORT_MATRIX.AGENTS).write_text(agents)

            # A commented copy beside the real ladder is not a duplicate.
            plan_path.write_text(plan + f'\n<!--\n{quote}\n-->\n')
            errors = SUPPORT_MATRIX.validate_check_policy(root, data, by_id, None)
            self.assertFalse(any('may supersede' in error for error in errors), errors)

            # Commenting out the plan removes its heading and its cited words.
            plan_path.write_text(f'<!--\n{plan}\n-->\n')
            errors = SUPPORT_MATRIX.validate_check_policy(root, data, by_id, None)
            self.assertTrue(any('exactly one' in error for error in errors), errors)

    def test_every_release_package_records_its_scope(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        self.assertEqual([], SUPPORT_MATRIX.validate(ROOT, data))
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual('v1', by_id['fes.atari-st']['release'])
        self.assertEqual('v2', by_id['fes.pc386']['release'])
        self.assertEqual('held-back', by_id['fes.coleco']['release'])
        self.assertNotIn('release', by_id['fes.menu'])
        self.assertIn('- **v2:** `fes.pcxt-dos`, `fes.pc386`', SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)))
        del by_id['fes.sms']['release']
        by_id['z80']['release'] = 'v1'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn("fes.sms release None is not one of v1, v2, held-back", errors)
        self.assertIn('z80 is not a release package but records release', errors)

    def test_de25_cells_follow_producers_and_distinguish_partial_groups(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        ids = SUPPORT_MATRIX.de25_core_ids(ROOT)
        for core_id in ('fes.c64-ntsc', 'fes.c64-early-ntsc', 'fes.c64c',
                        'fes.c64c-ntsc', 'fes.pong', 'sim.pong', 'i386'):
            with self.subTest(core_id=core_id):
                self.assertEqual('—', SUPPORT_MATRIX.de25_cell([by_id[core_id]], ids))
        for core_id in ('fes.menu', 'fes.c64', 'fes.pcxt-dos', 'fes.pc386',
                        'fes.spectrum128', 'fes.spectrum-plus3', 'fes.pentagon'):
            with self.subTest(core_id=core_id):
                self.assertEqual('Quartus', SUPPORT_MATRIX.de25_cell([by_id[core_id]], ids))
        rendered = SUPPORT_MATRIX.render(data, ids)
        self.assertIn('| ZX81 (ZX81, ZX80, TS1000, TS1500) | Current | Quartus | Quartus (fes.zx81 only) |', rendered)
        self.assertIn('| Spectrum (48K, 128K, +2, +2A/+3, Pentagon) | Current | OSS | Quartus |', rendered)

    def test_de25_ids_follow_registry_and_literal_machine_models(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'config').mkdir()
            (root / 'config/core-recipes.toml').write_text(
                '[[recipes]]\ncore_id = "registered"\nboard = "de25-nano"\n'
                '[[recipes]]\ncore_id = "de10-only"\nboard = "de10-nano"\n')
            producers = root / SUPPORT_MATRIX.DE25 / 'producers'
            producers.mkdir(parents=True)
            producer = producers / 'build_example_package.py'
            producer.write_text("MACHINE = Machine(core_id='dev', descriptions={'other': 'Other'}, "
                                "model_core_ids={'other': 'dev.other'})\n")
            self.assertEqual({'registered', 'dev', 'dev.other'}, SUPPORT_MATRIX.de25_core_ids(root))
            producer.write_text("MACHINE = Machine(core_id='dev', descriptions={}, "
                                "model_core_ids={'removed': 'dev.other'})\n")
            with self.assertRaisesRegex(ValueError, 'malformed producer core IDs'):
                SUPPORT_MATRIX.de25_core_ids(root)
            producer.write_text("MACHINE = Machine(core_id=dynamic_id)\n")
            with self.assertRaisesRegex(ValueError, 'producer core IDs must be literal'):
                SUPPORT_MATRIX.de25_core_ids(root)

    def test_optional_atari_st_row_shows_derived_tier(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.atari-st')
        self.assertTrue(row['optional'])
        self.assertEqual('experimental', row['library_standing'])
        self.assertEqual('experimental', row['signals']['catalog_standing'])
        self.assertEqual('current', row['tier'])
        line = next(
            line for line in SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT)).splitlines()
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
        rendered = SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT))
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

    def test_only_rows_the_dated_decision_named_may_cite_it(self):
        # The decision's rows are the overrides in the snapshot it was made on.
        snapshot = json.loads(subprocess.run(
            ['git', '-C', str(ROOT), 'show',
             f'{SUPPORT_MATRIX.AS_OF_COMMIT}:docs/support-matrix.json'],
            check=True, capture_output=True, text=True).stdout)
        decided = {row['id'] for row in snapshot['cores']
                   if (row.get('tier_override') or {}).get('reason') == SUPPORT_MATRIX.OVERRIDE_REASON}
        self.assertEqual(SUPPORT_MATRIX.OVERRIDE_IDS, decided)
        # A later row queued under that decision, as fes.c64c once was, is
        # rejected, and nothing else about the row is.
        data = SUPPORT_MATRIX.load_json(ROOT)
        row = next(row for row in data['cores'] if row['id'] == 'fes.c64c')
        row.update(tier='bring-up-queued', tier_role='play', check_level='none', tier_override={
            'tier': 'bring-up-queued', 'reason': SUPPORT_MATRIX.OVERRIDE_REASON, 'note': 'Queued.'})
        self.assertEqual(
            [f'fes.c64c has a tier override, but {SUPPORT_MATRIX.OVERRIDE_REASON} did not decide its tier'],
            SUPPORT_MATRIX.validate(ROOT, data))

    def test_c64c_rows_are_bring_up_with_full_checks(self):
        # Their packages exist, so they are not queued; their own role sets
        # the tier, and the derived level keeps them in closure sweeps.
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        rendered = SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT))
        for core_id, display in (('fes.c64c', 'C64C (PAL)'), ('fes.c64c-ntsc', 'C64C (NTSC)')):
            with self.subTest(core_id=core_id):
                row = by_id[core_id]
                self.assertEqual('bring-up', row['tier_role'])
                self.assertEqual('bring-up', row['tier'])
                self.assertNotIn('tier_override', row)
                self.assertEqual('held-back', row['release'])
                self.assertEqual('full', row['check_level'])
                self.assertIn('No hardware acceptance is recorded', row['notes'])
                self.assertIn(f'| {display} | Bring-up |', rendered)

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

    def test_de25_progress_covers_every_tier1_core_once(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        by_id = {row['id']: row for row in data['cores']}
        self.assertEqual([], SUPPORT_MATRIX.validate_de25_progress(ROOT, data, by_id))
        machines = data['de25_tier1']['machines']
        spectrum = next(m for m in machines if m['label'] == 'Spectrum family')
        del spectrum['cells']['fes.pentagon']
        machines[0]['cells']['fes.c64'] = 'builds'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn('de25_tier1 has no cell for tier 1 core fes.pentagon', errors)
        self.assertIn('de25_tier1 records fes.c64 twice', errors)

    def test_de25_progress_state_must_match_producers_and_recipes(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        cells = {cid: m['cells'] for m in data['de25_tier1']['machines'] for cid in m['cells']}
        cells['fes.zx80']['fes.zx80'] = 'builds'
        cells['fes.c64']['fes.c64'] = 'none'
        cells['fes.atari-st']['fes.atari-st'] = 'kit-accepted'
        cells['fes.pcxt-dos']['fes.pcxt-dos'] = 'shipped'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn('de25_tier1 fes.zx80 says builds but no DE25 producer builds it', errors)
        self.assertIn('de25_tier1 fes.c64 says none but a DE25 producer builds it', errors)
        self.assertIn('de25_tier1 fes.atari-st says kit-accepted without the package that ran on Kit D25', errors)
        self.assertIn("de25_tier1 fes.pcxt-dos state 'shipped' is not a DE25 state", errors)
        # A built cell cannot claim sealed unless its recipe is also registered for DE25.
        cells['fes.pc386']['fes.pc386'] = 'sealed'
        original = SUPPORT_MATRIX.de25_registered_ids
        SUPPORT_MATRIX.de25_registered_ids = lambda root: original(root) - {'fes.pc386'}
        try:
            errors = SUPPORT_MATRIX.validate(ROOT, data)
        finally:
            SUPPORT_MATRIX.de25_registered_ids = original
        self.assertIn('de25_tier1 fes.pc386 says sealed without a registered DE25 recipe', errors)

    def test_de25_registered_ids_include_models_of_registered_producers_only(self):
        registered = SUPPORT_MATRIX.de25_registered_ids(ROOT)
        built = SUPPORT_MATRIX.de25_core_ids(ROOT)
        self.assertIn('fes.pentagon', registered)
        self.assertIn('fes.pc386', built)
        self.assertIn('fes.pc386', registered)

    def test_de25_progress_renders_states_per_machine(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        rendered = SUPPORT_MATRIX.render(data, SUPPORT_MATRIX.de25_core_ids(ROOT))
        self.assertIn('## DE25 tier 1 progress', rendered)
        self.assertIn('| pc386 | builds: `fes.pc386` |', rendered)
        self.assertIn('none: `fes.zx80`, `fes.ts1000`, `fes.ts1500`; builds: `fes.zx81`', rendered)

    def test_de25_sealed_needs_the_exact_package_in_a_smoke_record(self):
        data = SUPPORT_MATRIX.load_json(ROOT)
        machine = next(m for m in data['de25_tier1']['machines'] if 'fes.atari-st' in m['cells'])
        machine['cells']['fes.atari-st'] = 'sealed'
        errors = SUPPORT_MATRIX.validate(ROOT, data)
        self.assertIn('de25_tier1 fes.atari-st says sealed without the package that ran on Kit D25', errors)

    def test_de25_smoke_receipt_must_match_core_package_kit_and_pass(self):
        check = SUPPORT_MATRIX.de25_package_errors
        sha = 'ab' * 32
        good = {'kit_serial': 'TRWU8J8G', 'core_id': 'fes.atari-st', 'package_sha256': sha, 'verdict': 'PASS'}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            def run(receipt, name='smoke.json', digest=sha):
                (root / name).write_text(receipt if isinstance(receipt, str) else json.dumps(receipt))
                return check(root, 'fes.atari-st', 'sealed', {'sha256': digest, 'evidence': name})
            self.assertEqual([], run(good))
            self.assertIn('package sha256 is not a lowercase SHA-256', run(good, digest=sha.upper())[0])
            self.assertIn('not JSON', run('ab' * 32, name='record.md')[0])
            self.assertIn('outside the repository',
                          check(root / 'inner', 'fes.atari-st', 'sealed', {'sha256': sha, 'evidence': '../smoke.json'})[0])
            self.assertIn('outside the repository',
                          check(root, 'fes.atari-st', 'sealed', {'sha256': sha, 'evidence': str(root / 'smoke.json')})[0])
            self.assertIn('unreadable', run('{', name='bad.json')[0])
            for key, value in (('core_id', 'fes.zx81'), ('package_sha256', 'cd' * 32),
                               ('kit_serial', 'TRWU8CBL'), ('verdict', 'FAIL')):
                with self.subTest(key=key):
                    errors = run({**good, key: value})
                    self.assertEqual(1, len(errors), errors)
                    self.assertIn(f'has {key} {value!r}', errors[0])

    def test_de25_kit_acceptance_needs_its_own_per_core_receipt(self):
        check = SUPPORT_MATRIX.de25_package_errors
        sha = 'ab' * 32
        smoke = {'kit_serial': 'TRWU8J8G', 'core_id': 'fes.pentagon', 'package_sha256': sha, 'verdict': 'PASS'}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'smoke.json').write_text(json.dumps(smoke))
            package = {'sha256': sha, 'evidence': 'smoke.json'}
            self.assertEqual([], check(root, 'fes.pentagon', 'sealed', package))
            errors = check(root, 'fes.pentagon', 'kit-accepted', package, acceptance=True)
            self.assertIn('acceptance receipt is missing or not JSON', errors[0])
            accepted = {**smoke, 'conformance': 'PASS', 'corpus': 'PASS'}
            (root / 'accept.json').write_text(json.dumps({**accepted, 'core_id': 'fes.spectrum'}))
            package['acceptance'] = 'accept.json'
            errors = check(root, 'fes.pentagon', 'kit-accepted', package, acceptance=True)
            self.assertEqual(1, len(errors), errors)
            self.assertIn("has core_id 'fes.spectrum'", errors[0])
            (root / 'accept.json').write_text(json.dumps({**accepted, 'corpus': 'FAIL'}))
            self.assertIn("has corpus 'FAIL'", check(root, 'fes.pentagon', 'kit-accepted', package, acceptance=True)[0])
            (root / 'accept.json').write_text(json.dumps(accepted))
            self.assertEqual([], check(root, 'fes.pentagon', 'kit-accepted', package, acceptance=True))

    def test_de25_acceptance_needs_persistence_for_cores_that_save(self):
        check = SUPPORT_MATRIX.de25_package_errors
        sha = 'ab' * 32
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for core_id, persistent in (('fes.atari-st', True), ('fes.c64', False)):
                with self.subTest(core_id=core_id):
                    smoke = {'kit_serial': 'TRWU8J8G', 'core_id': core_id, 'package_sha256': sha, 'verdict': 'PASS'}
                    (root / 'smoke.json').write_text(json.dumps(smoke))
                    (root / 'accept.json').write_text(json.dumps({**smoke, 'conformance': 'PASS', 'corpus': 'PASS'}))
                    package = {'sha256': sha, 'evidence': 'smoke.json', 'acceptance': 'accept.json'}
                    errors = check(root, core_id, 'kit-accepted', package, acceptance=True)
                    if persistent:
                        self.assertEqual(1, len(errors), errors)
                        self.assertIn('has persistence None', errors[0])
                    else:
                        self.assertEqual([], errors)

    def test_de25_released_needs_a_release_receipt_on_the_de10_tag(self):
        check = SUPPORT_MATRIX.de25_package_errors
        sha = 'ab' * 32
        smoke = {'kit_serial': 'TRWU8J8G', 'core_id': 'fes.c64', 'package_sha256': sha, 'verdict': 'PASS'}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'smoke.json').write_text(json.dumps(smoke))
            (root / 'accept.json').write_text(json.dumps({**smoke, 'conformance': 'PASS', 'corpus': 'PASS'}))
            package = {'sha256': sha, 'evidence': 'smoke.json', 'acceptance': 'accept.json'}
            run = lambda: check(root, 'fes.c64', 'released', package, acceptance=True, release=True)
            self.assertIn('release receipt is missing or not JSON', run()[0])
            package['release'] = 'release.json'
            release = {'core_id': 'fes.c64', 'package_sha256': sha, 'board': 'de25-nano', 'tag': 'v1.1', 'de10_tag': 'v1.0'}
            (root / 'release.json').write_text(json.dumps(release))
            self.assertIn('does not name one tag shared with DE10', run()[0])
            (root / 'release.json').write_text(json.dumps({**release, 'de10_tag': 'v1.1', 'package_sha256': 'cd' * 32}))
            self.assertIn("has package_sha256 'cdcd", run()[0])
            (root / 'release.json').write_text(json.dumps({**release, 'de10_tag': 'v1.1'}))
            self.assertEqual([], run())


if __name__ == '__main__':
    unittest.main()
