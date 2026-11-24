"""The DE25 pc386 reviewed signoff findings and the gate's per-rule coverage.

The DRC text below is synthetic, in the signoff report's table format; it
exercises the matching rules and never stands in for a fitted report.
"""
import json
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.producers import build_menu_package as common
from boards.de25nano.scanout import machine_findings as mf

TABLE = mf.bases()

S = 'soc_inst|subsys_periph|fes_scanout|fes_scanout|system|'
MACHINE, PIXEL = mf.MACHINE, mf.PIXEL
ROOT = Path(__file__).resolve().parents[1]


def report(rules):
    """rules: {rule: (header, rows)}; the summary counts each rule's rows."""
    summary = ''.join(f'; {rule} - Title ; High ; {len(rows)} ; 0 ; tags ;\n' for rule, (_, rows) in rules.items())
    detail = ''
    for rule, (header, rows) in rules.items():
        detail += (f'Status:\t\tFAIL\tSeverity:\tHigh\tNumber of violations: \t{len(rows)}\n'
                   f'+---\n; {rule} - Title ;\n+---\n; ' + ' ; '.join(header) + ' ;\n+---\n' +
                   ''.join('; ' + ' ; '.join(row) + ' ;\n' for row in rows))
    return summary + detail


CDC = ['From', 'To', 'From Clock', 'To Clock', 'Reason', 'Waived']


def cdc(source, sink, launch=MACHINE, latch=PIXEL):
    return [S + source, S + sink, launch, latch, 'Asynchronous transfer', '']


def sdc_text():
    return '\n'.join({line for basis in TABLE.BASES for line in basis['sdc']}) + '\n'


class PerRuleCoverageTests(unittest.TestCase):
    def test_several_entries_for_one_rule_cannot_hide_another_rule(self):
        rows = [('CDC-50001', 'High', '90', '0'), ('CDC-50002', 'High', '3', '0'), ('PAS-00001', 'Low', '0', '0')]
        # The old count (failed - entries) would read 2 - 2 = 0 here.
        two_for_one = [{'id': 'CDC-50001'}, {'id': 'CDC-50001'}]
        with self.assertRaisesRegex(common.BuildError, 'one distinct failed rule'):
            common.unreviewed_rules(two_for_one, rows)
        self.assertEqual(common.unreviewed_rules([{'id': 'CDC-50001'}], rows), 1)
        self.assertEqual(common.unreviewed_rules([{'id': 'CDC-50001'}, {'id': 'CDC-50002'}], rows), 0)
        with self.assertRaisesRegex(common.BuildError, 'one distinct failed rule'):
            common.unreviewed_rules([{'id': 'PAS-00001'}], rows)  # a rule that passed


class MachineFindingsTests(unittest.TestCase):
    def capture(self):
        return [cdc(f'frame_width[{i}]', f'machine_presenter_latest_width[{i}]') for i in range(10)] + [
            cdc(f'frame_height[{i}]', f'machine_presenter_latest_height[{i}]') for i in range(10)]

    def bases(self, *names):
        return [b for b in TABLE.BASES if b['name'] in names]

    def test_every_row_matches_exactly_one_basis_with_its_constraint(self):
        drc = report({'CDC-50001': (CDC, self.capture())})
        [entry] = mf.review(drc, sdc_text(), self.bases('presenter-capture'))
        self.assertEqual((entry['id'], entry['violations'], entry['basis']), ('CDC-50001', 20, mf.POLICY))
        [group] = entry['groups']
        self.assertEqual((group['name'], group['rows']), ('presenter-capture', 20))
        self.assertIn('machine_presenter_sequence_sync', group['qualifier'])
        self.assertTrue(any('frame_sequence' in line for line in group['constraint']))

    def test_an_unreviewed_row_a_wrong_count_a_wrong_clock_or_a_missing_constraint_refuses(self):
        bases = self.bases('presenter-capture')
        extra = self.capture() + [[S.replace('system|', '') + 'window_control|base_lo[3]',
                                   S.replace('system|', '') + 'arbiter|w_address[3]',
                                   'pll_inst|iopll_0_outclk0', MACHINE, 'Asynchronous transfer', '']]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50001': (CDC, extra)}), sdc_text(), bases)
        with self.assertRaisesRegex(mf.BuildError, 'covers 19 rows, reviewed 20'):
            mf.review(report({'CDC-50001': (CDC, self.capture()[:-1])}), sdc_text(), bases)
        reversed_clock = self.capture()[:-1] + [cdc('frame_height[9]', 'machine_presenter_latest_height[9]',
                                                    PIXEL, MACHINE)]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50001': (CDC, reversed_clock)}), sdc_text(), bases)
        missing = sdc_text().replace(TABLE.CAPTURE_SDC[2] + '\n', '')
        with self.assertRaisesRegex(mf.BuildError, 'not in the compiled SDC'):
            mf.review(report({'CDC-50001': (CDC, self.capture())}), missing, bases)
        waived = self.capture()
        waived[0] = waived[0][:-1] + ['Yes']
        with self.assertRaisesRegex(mf.BuildError, 'waived'):
            mf.review(report({'CDC-50001': (CDC, waived)}), sdc_text(), bases)

    def test_advisories_bind_by_hierarchy_and_record_their_count(self):
        header = ['Register Name', 'Register Location', 'Number of Endpoints', 'Endpoint Centroid',
                  'Average Distance of Endpoints to Centroid', 'Total Distance of Endpoints to Centroid (Tension)',
                  'Waived']
        rows = [[S + name, '(1, 1)', '9000', '(2, 2)', '20', '200000', '']
                for name in ('cpu_md_busy', 'cpu_wb_fault[4]', '_36426_[140]~DUPLICATE', 'reset',
                             'cpu_biu_reset~_Duplicate_2')]
        [entry] = mf.review(report({'TMC-20602': (header, rows)}), '', self.bases('cpu-control-tension'))
        self.assertEqual(entry['groups'][0]['rows'], 5)
        # A fitter copy is admitted only for a name the basis already names.
        for name in ('machine_at_pic_mask[3]~_Duplicate_2', 'cpu_biu_reset~_Duplicate_', 'cpu_biu_reset_Duplicate_2'):
            copy = rows + [[S + name, '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
            with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'TMC-20602': (header, copy)}), '', self.bases('cpu-control-tension'))
        outside = rows + [['fes_video_inst|output_release[0]', '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'TMC-20602': (header, outside)}), '', self.bases('cpu-control-tension'))

    def test_the_toggle_basis_needs_its_direction_and_the_board_assignments(self):
        rows = [cdc('machine_frame_sequence[0]', 'machine_presenter_sequence_sync[0]'),
                cdc('machine_presenter_loaded_flag', 'machine_presenter_loaded_sync[0]'),
                cdc('machine_presenter_request_toggle', 'machine_presenter_request_sync[0]', PIXEL, MACHINE)]
        bases = self.bases('presenter-toggles-to-pixel', 'presenter-toggle-to-machine')
        [entry] = mf.review(report({'CDC-50002': (CDC, rows)}), sdc_text(), bases)
        self.assertEqual([g['rows'] for g in entry['groups']], [2, 1])
        rows[2] = cdc('machine_presenter_request_toggle', 'machine_presenter_request_sync[0]')
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50002': (CDC, rows)}), sdc_text(), bases)
        qsf = ''.join('set_instance_assignment -name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS '
                      f'-to "{S}{name}"\n' for name in TABLE.TOGGLE_ASSIGNMENT)
        mf.assignments(qsf, TABLE.TOGGLE_ASSIGNMENT)
        with self.assertRaisesRegex(mf.BuildError, 'machine_presenter_loaded_sync'):
            mf.assignments(qsf.replace('machine_presenter_loaded_sync', 'other'), TABLE.TOGGLE_ASSIGNMENT)
        from boards.de25nano.producers import build_pc386_package as pc386
        spec = pc386.MACHINE.spec(ROOT)
        self.assertEqual({node for _, _, node in spec.ASSIGNMENTS} - {'system|machine_pixel_reset_sync[*]'},
                         {'system|' + name for name in TABLE.TOGGLE_ASSIGNMENT})

    def test_the_bases_are_bound_to_the_handshake_sources_the_record_captured(self):
        import hashlib
        record = {'source_inputs': {**TABLE.SOURCES, TABLE.TABLE: hashlib.sha256(
            (ROOT / TABLE.TABLE).read_bytes()).hexdigest()}}
        self.assertEqual(set(mf.sources(record, TABLE)), {*TABLE.SOURCES, TABLE.TABLE})
        for name in TABLE.SOURCES:
            self.assertEqual(hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), TABLE.SOURCES[name])
            changed = {'source_inputs': {**record['source_inputs'], name: '0' * 64}}
            with self.assertRaisesRegex(mf.BuildError, 'review it again'):
                mf.sources(changed, TABLE)
        with self.assertRaisesRegex(mf.BuildError, 'basis table'):
            mf.sources({'source_inputs': {**record['source_inputs'], TABLE.TABLE: '0' * 64}}, TABLE)

    def test_a_rule_that_passed_has_no_entry(self):
        self.assertEqual(mf.review(report({'CDC-50001': (CDC, [])}), sdc_text(), TABLE.BASES), [])

    def test_only_pc386_selects_the_policy_and_the_pc386_record_does(self):
        with tempfile.TemporaryDirectory() as temporary:
            fit = Path(temporary) / 'signoff'
            fit.mkdir()
            for core, policy, selected in (('fes.pc386', mf.POLICY, True), ('fes.spectrum', None, False)):
                parameters = {'core_id': core, **({'signoff_findings_review_policy': policy} if policy else {})}
                (fit.parent / 'build-inputs.json').write_text(json.dumps({'parameters': parameters}))
                self.assertIs(mf.selected(fit), selected)
            (fit.parent / 'build-inputs.json').write_text(json.dumps(
                {'parameters': {'core_id': 'fes.spectrum', 'signoff_findings_review_policy': mf.POLICY}}))
            with self.assertRaisesRegex(mf.BuildError, 'another core'):
                mf.selected(fit)
        from boards.de25nano.producers import build_pc386_package as pc386
        self.assertEqual(pc386.MACHINE.spec(ROOT).record_parameters(ROOT)['signoff_findings_review_policy'],
                         mf.POLICY)


if __name__ == '__main__':
    unittest.main()
