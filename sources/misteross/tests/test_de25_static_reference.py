"""Exercise the approved static review on retained real reports and corruptions.

The reports are observations from unadmitted fits, not package fixtures.
"""
import copy
import json
from pathlib import Path
import shutil
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
from scripts import build_de25_zx81_static_package as producer
review = producer.reviewer(ROOT)


class StaticReferenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)
        with tarfile.open(ROOT / 'tests/fixtures/de25_static_reference.tar.gz') as archive:
            archive.extractall(self.work, filter='data')
        self.project = self.work / 'selected/combined/hps/project'
        (self.project / 'output_files').mkdir(parents=True)
        for p in (self.work / 'timing').iterdir():
            shutil.copyfile(p, self.project / 'output_files' / p.name)
        self.raw = review.timing.timing_evidence(self.project.parents[2], revision='base')
        self.drc = (self.work / 'timing/base.tq.drc.signoff.rpt').read_text()

    def test_real_raw_findings_keep_every_numerical_check_and_refuse_export(self):
        self.assertEqual((self.raw['signoff_rules'], len(self.raw['timing_checks'])), (88, 98))
        self.assertTrue(self.raw['all_reported_timing_pass'])
        self.assertFalse(self.raw['package_timing_admission'])
        self.assertEqual(len(review.findings(self.drc, self.raw)), 4)
        self.assertEqual(review.reset_reports(self.work / 'reset_direct/direct-inventory.tsv',
            self.work / 'reset_paths/reset-paths.tsv')['timed_paths'], 2680)
        self.assertEqual(len(review.audio_reports(self.work / 'audio')['conditional_external_budgets']), 8)

    def test_real_fitter_replication_change_retains_counts_and_every_physical_path(self):
        newer = self.work / 'new-fit'
        with tarfile.open(ROOT / 'tests/fixtures/de25_static_reference_fit_4dc.tar.gz') as archive:
            archive.extractall(newer, filter='data')
        for p in (newer / 'timing').iterdir():
            shutil.copyfile(p, self.project / 'output_files' / p.name)
        raw = review.timing.timing_evidence(self.project.parents[2], revision='base')
        reviewed = review.findings((newer / 'timing/base.tq.drc.signoff.rpt').read_text(), raw)
        reset = review.reset_reports(newer / 'reset_direct/direct-inventory.tsv',
                                     newer / 'reset_paths/reset-paths.tsv')
        self.assertEqual((reset['direct_async'], reset['logical_async'], reset['direct_ena'],
                          reset['timed_paths']), (327, 313, 1, 2624))
        self.assertEqual(review.reset_finding_count(reviewed, reset), 278)
        previous = review.reset_reports(self.work / 'reset_direct/direct-inventory.tsv',
                                        self.work / 'reset_paths/reset-paths.tsv')
        self.assertEqual((previous['direct_async'], previous['logical_async']), (334, 313))
        self.assertEqual(review.reset_finding_count(review.findings(self.drc, self.raw), previous), 279)

    def test_approved_two_register_scope_uses_every_exact_physical_enable_path(self):
        newer = self.work / 'b0d-fit'
        with tarfile.open(ROOT / 'tests/fixtures/de25_static_reference_fit_b0d.tar.gz') as archive:
            archive.extractall(newer, filter='data')
        inventory = newer / 'reset_direct/direct-inventory.tsv'
        paths = newer / 'reset_paths/reset-paths.tsv'
        physical = review.rows(inventory, ['kind', 'name'])
        enabled = {r['name'] for r in physical if r['kind'] == 'direct_ena'}
        self.assertEqual(len(enabled), 3)
        timing = review.rows(paths, ['corner', 'check', 'from', 'to', 'slack_ns',
                                    'from_clock', 'to_clock'])
        self.assertEqual(len(timing), 2656)
        for corner in review.audio.CORNERS:
            for check in ('setup', 'hold'):
                selected = [r for r in timing if (r['corner'], r['check']) == (corner, check)]
                self.assertEqual({r['to'] for r in selected}, enabled)
                self.assertTrue(all(review.audio.number(r['slack_ns']) > 0 for r in selected))
        reset = review.reset_reports(inventory, paths)
        self.assertEqual((reset['direct_async'], reset['logical_async'], reset['direct_ena'],
                          reset['timed_paths']), (329, 313, 3, 2656))
        lnt = json.loads((newer / 'raw-LNT.json').read_text())
        old, new = lnt['old'][1], lnt['new'][1]
        self.assertEqual((old[6], new[6]), ('1', '3'))
        original = next(line for line in self.drc.splitlines()
                        if line.startswith('; ') and review.RESET in line and 'first_seen|ENA' in line)
        fields = [f.strip() for f in original.split(';')[1:-1]]
        self.assertEqual(fields, old)
        drc = self.drc.replace(original, '; ' + ' ; '.join(new) + ' ;')
        reviewed = review.findings(drc, self.raw)
        self.assertEqual(review.reset_finding_count(reviewed, reset), 279)
        self.assertEqual(next(r for r in reviewed if r['id'] == 'LNT-30010')['raw_clock_enable_signals'], 3)

    def test_unknown_enable_or_missing_required_transport_consumer_is_refused(self):
        inventory = self.work / 'reset_direct/direct-inventory.tsv'
        paths = self.work / 'reset_paths/reset-paths.tsv'
        original = inventory.read_text()
        for name in ['unknown_first_seen', review.OPTIONAL_ENABLE + '_extra',
                     review.OPTIONAL_ENABLE + '~DUPLICATEbad']:
            inventory.write_text(original + 'direct_ena\t' + name + '\nall_ena\t' + name + '\n')
            with self.subTest(name=name), self.assertRaisesRegex(review.BuildError, 'changed direct reset/enable consumers: direct_ena'):
                review.reset_reports(inventory, paths)
        transport = review.bound('reviewed-topology.json')['reset']['direct_ena'][0]
        inventory.write_text(original.replace('direct_ena\t'+transport+'\n',
                                              'direct_ena\t'+review.OPTIONAL_ENABLE+'\n'))
        with self.assertRaisesRegex(review.BuildError, 'changed direct reset/enable consumers: direct_ena'):
            review.reset_reports(inventory, paths)

    def test_enable_copy_needs_all_paired_paths_and_exact_raw_quantity(self):
        inventory = self.work / 'reset_direct/direct-inventory.tsv'
        paths = self.work / 'reset_paths/reset-paths.tsv'
        base = review.bound('reviewed-topology.json')['reset']['direct_ena'][0]
        duplicate = base + '~DUPLICATE99'
        inventory.write_text(inventory.read_text() + 'direct_ena\t'+duplicate+'\nall_ena\t'+duplicate+'\n')
        with self.assertRaisesRegex(review.BuildError, 'missing direct reset/enable timed endpoint'):
            review.reset_reports(inventory, paths)
        original = paths.read_text()
        additions = []
        for row in original.splitlines()[1:]:
            fields = row.split('\t')
            if fields[3] == base:
                fields[3] = duplicate
                additions.append('\t'.join(fields))
        self.assertEqual(len(additions), 8)
        paths.write_text(original+'\n'.join(additions)+'\n')
        reset = review.reset_reports(inventory, paths)
        self.assertEqual((reset['direct_ena'], reset['timed_paths']), (2, 2688))
        reviewed = review.findings(self.drc, self.raw)
        with self.assertRaisesRegex(review.BuildError, 'raw enable quantity differs'):
            review.reset_finding_count(reviewed, reset)
        next(r for r in reviewed if r['id'] == 'LNT-30010')['raw_clock_enable_signals'] = 2
        self.assertEqual(review.reset_finding_count(reviewed, reset), 279)
        paths.write_text(original+'\n'.join(additions[:-1])+'\n')
        with self.assertRaisesRegex(review.BuildError, 'missing direct reset/enable timed endpoint'):
            review.reset_reports(inventory, paths)

    def test_new_physical_copy_requires_all_corner_recovery_and_removal(self):
        inventory = self.work / 'reset_direct/direct-inventory.tsv'
        paths = self.work / 'reset_paths/reset-paths.tsv'
        base = review.bound('reviewed-topology.json')['reset']['direct_async'][0]
        copy_name = base + '~DUPLICATE99'
        inventory.write_text(inventory.read_text() + 'direct_async\t' + copy_name + '\nall_async\t' + copy_name + '\n')
        with self.assertRaisesRegex(review.BuildError, 'missing direct reset/enable timed endpoint'):
            review.reset_reports(inventory, paths)
        additions = []
        for row in paths.read_text().splitlines()[1:]:
            fields = row.split('\t')
            if fields[3] == base:
                fields[3] = copy_name
                additions.append('\t'.join(fields))
        self.assertEqual(len(additions), 8)
        original = paths.read_text()
        paths.write_text(original + '\n'.join(additions) + '\n')
        reset = review.reset_reports(inventory, paths)
        self.assertEqual((reset['direct_async'], reset['logical_async'], reset['timed_paths']), (335, 313, 2688))
        paths.write_text(original + '\n'.join(additions[:-1]) + '\n')
        with self.assertRaisesRegex(review.BuildError, 'missing direct reset/enable timed endpoint'):
            review.reset_reports(inventory, paths)

    def test_unreviewed_logical_copy_or_removed_logical_consumer_is_refused(self):
        inventory = self.work / 'reset_direct/direct-inventory.tsv'
        paths = self.work / 'reset_paths/reset-paths.tsv'
        original = inventory.read_text()
        inventory.write_text(original + 'direct_async\tunreviewed~DUPLICATE\nall_async\tunreviewed~DUPLICATE\n')
        with self.assertRaisesRegex(review.BuildError, 'changed direct reset/enable consumers'):
            review.reset_reports(inventory, paths)
        base = review.bound('reviewed-topology.json')['reset']['direct_async'][0]
        def removed(name):
            return name == base or name.startswith(base + '~DUPLICATE')
        inventory.write_text('\n'.join(line for line in original.splitlines()
                             if not removed(line.split('\t')[-1])) + '\n')
        paths.write_text('\n'.join(line for line in paths.read_text().splitlines()
                         if not removed(line.split('\t')[3])) + '\n')
        with self.assertRaisesRegex(review.BuildError, 'changed direct reset/enable consumers'):
            review.reset_reports(inventory, paths)

    def test_raw_reset_quantity_is_reported_and_bounded_not_used_as_coverage(self):
        reset = review.reset_reports(self.work / 'reset_direct/direct-inventory.tsv',
                                     self.work / 'reset_paths/reset-paths.tsv')
        original = next(line for line in self.drc.splitlines()
                        if line.startswith('; ') and review.RESET in line and 'first_seen|ENA' in line)
        fields = [f.strip() for f in original.split(';')[1:-1]]
        for index in (4, 6):
            for count in ['0', '-1', 'unknown', '999']:
                changed = list(fields);changed[index] = count
                drc = self.drc.replace(original, '; ' + ' ; '.join(changed) + ' ;')
                with self.subTest(index=index, count=count), self.assertRaises(review.BuildError):
                    review.reset_finding_count(review.findings(drc, self.raw), reset)

    def test_changed_findings_counts_severity_waivers_or_other_rule_refused(self):
        for key, value in [('violations', 34), ('severity', 'Medium'), ('waived', 1)]:
            changed = copy.deepcopy(self.raw)
            next(r for r in changed['rules'] if r['id'] == 'TMC-20012')[key] = value
            with self.subTest(key=key), self.assertRaises(review.BuildError):
                review.findings(self.drc, changed)
        changed = copy.deepcopy(self.raw)
        next(r for r in changed['rules'] if not r['violations'])['violations'] = 1
        with self.assertRaises(review.BuildError):
            review.findings(self.drc, changed)
        for old, new in [('HDMI_I2S', 'HDMI_EXTRA'), ('CE-Type CDC Bus', 'Unknown CDC Bus'),
                         ('transport|first_seen|ENA', 'transport|another|ENA')]:
            self.assertIn(old, self.drc)
            with self.subTest(old=old), self.assertRaises(review.BuildError):
                review.findings(self.drc.replace(old, new), self.raw)

    def test_audio_missing_duplicate_negative_nonfinite_or_slow_path_refused(self):
        path = self.work / 'audio/paths.tsv'
        original = path.read_text()
        rows = original.splitlines()
        for text in ['\n'.join(rows[:-1]) + '\n', original + rows[1] + '\n',
                     original.replace(rows[1].split('\t')[-1], 'NaN', 1)]:
            path.write_text(text)
            with self.subTest(text=text[:90]), self.assertRaises(review.BuildError):
                review.audio_reports(self.work / 'audio')
        for delay in ['-1', '1000']:
            changed = list(rows)
            for i, line in enumerate(changed):
                columns = line.split('\t')
                if columns[1:4] == ['data', 'output', 'max']:
                    columns[-1] = delay
                    changed[i] = '\t'.join(columns)
                    break
            path.write_text('\n'.join(changed) + '\n')
            with self.subTest(delay=delay), self.assertRaises(review.BuildError):
                review.audio_reports(self.work / 'audio')

    def test_audio_additional_output_source_is_refused(self):
        path = next((self.work / 'audio').glob('*_data_all_sources.rpt'))
        text = path.read_text()
        self.assertIn('Report Path: Found 2 paths', text)
        path.write_text(text.replace('Report Path: Found 2 paths', 'Report Path: Found 3 paths'))
        with self.assertRaises(review.BuildError):
            review.audio_reports(self.work / 'audio')

    def test_reset_missing_duplicate_wrong_clock_source_and_negative_path_refused(self):
        inventory = self.work / 'reset_direct/direct-inventory.tsv'
        path = self.work / 'reset_paths/reset-paths.tsv'
        original = path.read_text()
        lines = original.splitlines()
        for text in ['\n'.join(lines[:-1]) + '\n', original + lines[1] + '\n',
                     original.replace(review.SYSTEM_CLOCK, 'wrong_clock'),
                     original.replace(review.RESET, 'wrong_source')]:
            path.write_text(text)
            with self.subTest(text=text[:90]), self.assertRaises(review.BuildError):
                review.reset_reports(inventory, path)
        for value in ['-0.001', 'NaN']:
            changed = list(lines)
            columns = changed[1].split('\t'); columns[4] = value; changed[1] = '\t'.join(columns)
            path.write_text('\n'.join(changed) + '\n')
            with self.subTest(value=value), self.assertRaises(review.BuildError):
                review.reset_reports(inventory, path)
        path.write_text(original)
        inventory.write_text(inventory.read_text() + 'direct_ena\tnew_consumer\n')
        with self.assertRaises(review.BuildError):
            review.reset_reports(inventory, path)

    def test_only_reviewed_profile_sources_regions_and_personas_are_eligible(self):
        fields = {'parameters': {'core_id': 'fes.zx81', 'programming_profile': 'de25-linux-static-computer-v1',
            'static_profile': 'fes.de25-standard-zx81-empty-ram16k.v1', 'standard_core': True,
            'expansion_socket': True, 'signoff_policy': review.POLICY, 'device': 'A5EB013BB23BE4SCS',
            'static_default_persona_id': 0xc001a001, 'placement': 'X2 Y7 X26 Y12', 'routing': 'X1 Y6 X27 Y13'},
            'source_inputs': review.bound('reviewed-inputs.json')}
        self.assertEqual(review.eligible(json.dumps(fields)), fields)
        for key, value in [('signoff_policy', 'de25-static-whole-board-no-exceptions-v1'),
                           ('programming_profile', 'de25-linux-computer-v1'), ('expansion_socket', 1),
                           ('static_default_persona_id', 0xc002b002), ('placement', 'X3 Y7 X26 Y12')]:
            changed = copy.deepcopy(fields);changed['parameters'][key] = value
            with self.subTest(key=key), self.assertRaises(review.BuildError):
                review.eligible(json.dumps(changed))
        for key in fields['source_inputs']:
            changed = copy.deepcopy(fields);changed['source_inputs'][key] = '0' * 64
            with self.subTest(source=key), self.assertRaises(review.BuildError):
                review.eligible(json.dumps(changed))

    def test_raw_report_inventory_and_collector_adaptation_are_closed(self):
        names = review.expected_reports()
        self.assertEqual(len(names), 249)
        self.assertTrue(all(any(name.startswith('reports/' + r + '/') for name in names)
                            for r in ['base', 'persona_a', 'persona_b']))
        original = (review.async_inputs.HERE / 'async_inputs.tcl').read_bytes()
        self.assertEqual(review.async_collector().replace(
            b'project_open -preserve_revision_order -revision [lindex $quartus(args) 2] fes_link_proof',
            b'project_open golden_top'), original)

    def test_ordinary_or_historical_policy_and_missing_receipt_cannot_admit_static(self):
        for policy in ['de25-static-whole-board-no-exceptions-v1',
                       'de25-static-stock-reference-v1',
                       'de25-standard-stock-audio-clocks-v1', None]:
            with self.subTest(policy=policy), self.assertRaisesRegex(review.BuildError, 'approved source-bound'):
                producer.timing_evidence(ROOT, self.project,
                    json.dumps({'parameters': {'signoff_policy': policy}}))
        with self.assertRaisesRegex(review.BuildError, 'missing fitted static board review'):
            producer.timing_evidence(ROOT, self.project,
                json.dumps({'parameters': {'signoff_policy': review.POLICY}}))


if __name__ == '__main__':
    unittest.main()
