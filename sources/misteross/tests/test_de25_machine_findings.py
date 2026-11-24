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
                             'cpu_biu_reset~_Duplicate_2', 'cpu_biu_reset~_Duplicate', 'reset~DUPLICATE',
                             'machine_at_decoder_state[1]', 'machine_at_decoder_state[0]~DUPLICATE',
                             'machine_at_decoder_phase[5]', 'machine_at_decoder_phase[2]~DUPLICATE',
                             'machine_at_decoder_cycle_port[15]', 'audio_dsp_write', 'audio_dsp_write~_Duplicate_1')]
        [entry] = mf.review(report({'TMC-20602': (header, rows)}), '', self.bases('cpu-control-tension'))
        self.assertEqual(entry['groups'][0]['rows'], 14)
        # A fitter copy is admitted only for a name the basis already names; neighbouring decoder,
        # audio and machine names are not.
        for name in ('machine_at_pic_mask[3]~_Duplicate_2', 'cpu_biu_reset~_Duplicate_', 'cpu_biu_reset_Duplicate_2',
                     'machine_at_decoder_cycle_write', 'machine_at_decoder_next_port[3]~DUPLICATE',
                     'machine_at_decoder_phase_x[1]', 'audio_dsp_read~DUPLICATE', 'audio_dsp_write_data[3]',
                     'machine_audio_dsp_write', 'machine_at_decoder_state_x[1]',
                     'cpu_md_busy~Duplicate', 'reset~DUPLICATE~DUPLICATE'):
            copy = rows + [[S + name, '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
            with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'TMC-20602': (header, copy)}), '', self.bases('cpu-control-tension'))
        outside = rows + [['fes_video_inst|output_release[0]', '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'TMC-20602': (header, outside)}), '', self.bases('cpu-control-tension'))

    def test_the_lint_bases_admit_their_drivers_and_fitter_copies_of_them_only(self):
        header = ['Driver', 'Asynchronous Reset Signal', 'Synchronous Reset Signal', 'Clock Enable Signal',
                  'Number of Asynchronous Reset Signals', 'Number of Synchronous Reset Signals',
                  'Number of Clock Enable Signals', 'Waived']

        def row(driver):
            return [S + driver, '--', S + 'cpu_int_take|SCLR', S + 'cpu_q16|ENA', '0', '1482', '1', '']

        bases = self.bases('biu-reset', 'machine-reset')
        # slat23b seed 2's three rows, then the other copy spellings.
        drivers = ['cpu_biu_reset~_Duplicate_1', 'cpu_biu_reset~_Duplicate', 'reset', 'cpu_biu_reset',
                   'cpu_biu_reset~DUPLICATE', 'reset~DUPLICATE~_Duplicate_3']
        [entry] = mf.review(report({'LNT-30010': (header, [row(d) for d in drivers])}), '', bases)
        self.assertEqual([g['rows'] for g in entry['groups']], [4, 2])
        for driver in ('machine_at_decoder_state[1]~DUPLICATE', 'cpu_biu_reset~_Duplicate_', 'cpu_md_busy~DUPLICATE',
                       'reset_n~DUPLICATE', 'cpu_biu_reset_q', 'reset~_Duplicate~DUPLICATE'):
            with self.subTest(driver), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'LNT-30010': (header, [row(driver)])}), '', bases)

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

    def presenter_cdc_50001(self):
        return self.capture() + [
            cdc(f'machine_presenter_epoch[{i}]', f'machine_presenter_f_epoch[{i}]', PIXEL, MACHINE)
            for i in range(35)] + [
            cdc(f'machine_presenter_loaded_in[{i}]', f'machine_presenter_loaded[{i}]') for i in range(12)]

    def window_groups(self, rows):
        [entry] = mf.review(report({'CDC-50001': (CDC, rows)}), sdc_text(), TABLE.BASES)
        return {g['name']: g['rows'] for g in entry['groups'] if g['name'].startswith('window-')}

    def test_the_window_bases_admit_their_bundles_in_any_number_and_nothing_else(self):
        w = S.replace('system|', '') + 'window_control|'
        # i486DX slice 1 seed 1's nine rows, verbatim; #2036's own fits report none.
        base = [[w + f'base_lo[{i}]', w + f'base_machine[{i}]', TABLE.SYS, MACHINE, 'Asynchronous transfer', '']
                for i in (13, 14, 15, 16, 22, 28, 29, 30, 31)]
        fault = [[w + f'fault_launch[{i}]', w + f'machine_fault[{i}]', MACHINE, TABLE.SYS, 'Asynchronous transfer', '']
                 for i in range(5)]
        duplicate = [[w + 'base_lo[20]~DUPLICATE', w + 'base_machine[20]', TABLE.SYS, MACHINE,
                      'Asynchronous transfer', '']]
        presenter = self.presenter_cdc_50001()
        self.assertEqual(self.window_groups(presenter), {'window-base': 0, 'window-fault-record': 0})
        self.assertEqual(self.window_groups(presenter + base + fault + duplicate),
                         {'window-base': 10, 'window-fault-record': 5})
        unreviewed = {
            'a base_lo outside window_control': [S + 'base_lo[13]', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'an arbiter base_lo': [S.replace('system|', '') + 'arbiter|base_lo[13]', w + 'base_machine[13]',
                                   TABLE.SYS, MACHINE],
            'base_lo into another sink': [w + 'base_lo[3]', S.replace('system|', '') + 'arbiter|w_address[3]',
                                          TABLE.SYS, MACHINE],
            'base_hi': [w + 'base_hi[13]', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'the base in reverse': [w + 'base_lo[13]', w + 'base_machine[13]', MACHINE, TABLE.SYS],
            'the base from the pixel clock': [w + 'base_lo[13]', w + 'base_machine[13]', PIXEL, MACHINE],
            'the fault record in reverse': [w + 'fault_launch[0]', w + 'machine_fault[0]', TABLE.SYS, MACHINE],
            'the fault record into the base': [w + 'fault_launch[0]', w + 'base_machine[0]', MACHINE, TABLE.SYS],
        }
        for label, row in unreviewed.items():
            with self.subTest(label), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                self.window_groups(presenter + base + [row + ['Asynchronous transfer', '']])
        for basis, line in (('window-base', TABLE.WINDOW_BASE_SDC[0]), ('window-fault-record', TABLE.WINDOW_FAULT_SDC[0])):
            with self.subTest(basis), self.assertRaisesRegex(mf.BuildError, basis + ' constraint not in the compiled SDC'):
                mf.review(report({'CDC-50001': (CDC, presenter)}), sdc_text().replace(line + '\n', ''), TABLE.BASES)

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
