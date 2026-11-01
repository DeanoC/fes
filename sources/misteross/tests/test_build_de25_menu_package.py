"""Canonical DE25 build identity and complete compiler-report admission."""
import copy
import gzip
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock

from scripts import build_de25_menu_package as producer
from scripts.core_package import PackageError, encode_manifest
from scripts.export_core_package import build_identity, verify_record_source_at_revision


class DE25MenuPackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='de25-menu-package-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / 'fit'
        self.reports = self.root / 'combined/hps/project/output_files'
        self.reports.mkdir(parents=True)
        self.fixture = json.loads(gzip.decompress((Path(__file__).parent /
            'fixtures/de25_menu_timing.json.gz').read_bytes()))
        self.write_reports()

    def write_reports(self, signoff=None, summary=None):
        for name, value in [('golden_top.tq.drc.signoff.rpt', signoff), ('golden_top.sta.summary', summary)]:
            (self.reports / name).write_text(self.fixture[name] if value is None else value)

    def passing_signoff(self):
        text = self.fixture['golden_top.tq.drc.signoff.rpt'].replace('4 of 85 Rules Failed', '0 of 85 Rules Failed')
        text = re.sub(r'(?m)^(; [A-Z]+-\d+ - [^;]+; (?:Critical|High|Medium|Low|Info)\s*;)\s*\d+\s*;', r'\1 0 ;', text)
        text = re.sub(r'(?m)^Status:\s*FAIL', 'Status:\t\tPASS', text)
        return re.sub(r'(?m)^Number of violations:\s*\d+', 'Number of violations:\t0', text)

    def test_actual_compiler_report_preserves_all_four_failed_rule_families(self):
        evidence = producer.timing_evidence(self.root)
        self.assertEqual(evidence['signoff_rules'], 85)
        self.assertEqual(evidence['failed_rules'], 4)
        self.assertEqual(len(evidence['timing_checks']), 74)
        self.assertTrue(evidence['all_reported_timing_pass'])
        self.assertFalse(evidence['package_timing_admission'])
        self.assertEqual({r['id'] for r in evidence['rules'] if r['violations']},
                         {'TMC-20012', 'TMC-20011', 'RDC-50003', 'RES-50101'})

    def test_zero_findings_require_both_summary_and_rule_details(self):
        self.write_reports(signoff=self.passing_signoff())
        self.assertTrue(producer.timing_evidence(self.root)['package_timing_admission'])
        hidden_failure = self.passing_signoff().replace('Status:\t\tPASS', 'Status:\t\tFAIL', 1)
        self.write_reports(signoff=hidden_failure)
        with self.assertRaisesRegex(producer.BuildError, 'differs from rule details'):
            producer.timing_evidence(self.root)

    def test_waived_findings_cannot_admit_a_package(self):
        text = self.passing_signoff()
        text = re.sub(r'(?m)^(; TMC-20012 - [^;]+; High\s*;\s*0\s*;)\s*0\s*;', r'\1 1 ;', text)
        self.write_reports(signoff=text)
        self.assertFalse(producer.timing_evidence(self.root)['package_timing_admission'])

    def test_missing_duplicate_or_inconsistent_signoff_inventory_is_rejected(self):
        text = self.fixture['golden_top.tq.drc.signoff.rpt']
        mutations = [text.replace('4 of 85', '3 of 85'), text.replace('4 of 85', '4 of 84'),
                     re.sub(r'(?m)^; CDC-50001 - [^\n]+\n', '', text),
                     text.replace('CDC-50001 - ', 'CDC-50002 - ')]
        for changed in mutations:
            with self.subTest(change=changed[:40]):
                self.write_reports(signoff=changed)
                with self.assertRaises(producer.BuildError):
                    producer.timing_evidence(self.root)

    def test_every_timing_type_and_clock_must_parse_with_finite_nonnegative_slack(self):
        text = self.fixture['golden_top.sta.summary']
        for changed in (text.replace('Slack : 3.119', 'Slack : -0.001'),
                        text.replace('Slack : 3.119', 'Slack : NaN'),
                        text.replace('Slack : 3.119', 'Slack : Infinity')):
            self.write_reports(summary=changed)
            self.assertFalse(producer.timing_evidence(self.root)['package_timing_admission'])
        for changed in (text.replace("Setup 'pll_inst|iopll_0_outclk0'", "Setup 'unexpected'"),
                        text.replace('Slack : 3.119', 'Slack : missing'),
                        text + text[:text.index("Type  : Setup 'fes_video_inst")]):
            with self.subTest(change=changed[:40]):
                self.write_reports(summary=changed)
                with self.assertRaises(producer.BuildError):
                    producer.timing_evidence(self.root)

    def async_reports(self, directory, name='de25_menu_async_inputs.json.gz'):
        fixture = json.loads(gzip.decompress((Path(__file__).parent /
            'fixtures' / name).read_bytes()))
        directory.mkdir(parents=True, exist_ok=True)
        for name, text in fixture.items():
            (directory / name).write_text(text)
        return fixture

    def prepare_async_fit(self):
        revision, captured, identities, execution = self.prepare_repository()
        record = producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', revision,
                                               identities, execution, captured)
        (self.root.parent / 'build-inputs.json').write_bytes(record)
        hps = self.root / 'combined/hps'
        (hps / 'inputs.json').write_text(json.dumps({'fes_revision': revision,
            'scanout': {'build_id': build_identity(record)}}))
        (hps / 'generation.json').write_text('{}')
        (hps / 'linux-boot.core.rbf').write_bytes(b'fitted RBF fixture')
        (hps / 'project/golden_top.qsf').write_text('fitted project fixture')
        database = hps / 'project/qdb/final.cdb'
        database.parent.mkdir()
        database.write_bytes(b'fitted database fixture')
        return hps

    def collect_async_fixture(self):
        def report(command, **kwargs):
            self.assertEqual(kwargs['timeout'], 240)
            self.assertTrue(kwargs['check'])
            self.assertNotEqual(Path(command[-2]), self.root / 'combined/hps/project')
            self.assertTrue((Path(command[-2]) / 'qdb/final.cdb').is_file())
            self.async_reports(Path(command[-1]))
        with mock.patch.object(producer.async_inputs.subprocess, 'run', side_effect=report):
            return producer.async_inputs.collect(self.root, Path('/usr/bin/python3'))

    def test_actual_fitted_input_reports_require_the_reviewed_topology_timing_and_mtbf(self):
        directory = self.root.parent / 'reports'
        self.async_reports(directory)
        proof = producer.async_inputs.review_reports(directory)
        self.assertEqual(set(proof), {'HDMI_I2C_SDA', 'HDMI_TX_INT'})
        self.assertEqual(proof['HDMI_I2C_SDA']['timing']['hold']['slack_ns'], '0.102')
        self.assertEqual(proof['HDMI_TX_INT']['modeled_worst_case_mtbf_years'], 'Greater than 1 Billion')
        mutations = [
            ('inventory.tsv', 'HDMI_I2C_SDA\tinput-fanout\tfes_video_inst|hdmi_sda_sync[0]',
             'HDMI_I2C_SDA\tinput-fanout\tfes_video_inst|hdmi_sda_sync[0],unreviewed_consumer'),
            ('inventory.tsv', 'HDMI_TX_INT\tstage0-fanout\tfes_video_inst|hdmi_int_sync[1]',
             'HDMI_TX_INT\tstage0-fanout\tunreviewed_consumer'),
            ('inventory.tsv', 'fes_video_inst|hdmi_config|u0|ACK3', 'other_ack'),
            ('inventory.tsv', 'fes_video_inst|hdmi_config|LUT_INDEX[5]', 'other_state'),
            ('inventory.tsv', 'FF_X74_Y10_N28', 'FF_X75_Y10_N28'),
            ('hdmi_sda_sync-setup.rpt', '; 19.590 ', '; -0.001 '),
            ('hdmi_sda_sync-hold.rpt', '; 0.102 ', '; NaN '),
            ('hdmi_int_sync-setup.rpt', 'No SDC Exception on Path', 'False Path'),
            ('hdmi_int_sync-hold.rpt', 'Snapshot:\n    final', 'Snapshot:\n    synthesized'),
            ('metastability.rpt', '; Yes ', '; No '),
            ('metastability.rpt', 'Greater than 1 Billion', '1000000'),
            ('metastability.rpt', '; 6.25 ', '; 0.01 '),
            ('metastability.rpt', '; 50.0 ', '; 25.0 '),
            ('metastability.rpt', 'The MTBF calculation used a default data toggle rate', 'Manual toggle rate'),
        ]
        for name, before, after in mutations:
            with self.subTest(report=name, mutation=after):
                original = self.async_reports(directory)[name]
                self.assertIn(before, original)
                (directory / name).write_text(original.replace(before, after))
                with self.assertRaises(producer.BuildError):
                    producer.async_inputs.review_reports(directory)

    def test_review_preserves_raw_failures_and_blocks_other_rules_and_waivers(self):
        self.prepare_async_fit()
        review = self.collect_async_fixture()
        original = producer.timing_evidence(self.root, async_review=review)
        self.assertEqual(original['failed_rules'], 4)
        self.assertEqual(original['unreviewed_failed_rules'], 3)
        self.assertEqual(original['reviewed_findings'], [{'id': 'TMC-20011', 'violations': 2,
            'ports': ['HDMI_I2C_SDA', 'HDMI_TX_INT']}])
        self.assertFalse(original['package_timing_admission'])
        # A synthetic zero-other-finding report tests the admission decision;
        # it is never used as compiler or package evidence.
        raw = self.fixture['golden_top.tq.drc.signoff.rpt']
        raw_input = next(s for s in re.split(r'(?m)^Status:', raw)[1:] if '; TMC-20011 - ' in s)
        sections = re.split(r'(?m)^Status:', self.passing_signoff())
        text = 'Status:'.join(raw_input if i and '; TMC-20011 - ' in s else s for i, s in enumerate(sections))
        text = text.replace('0 of 85 Rules Failed', '1 of 85 Rules Failed')
        text = re.sub(r'(?m)^(; TMC-20011 - [^;]+; High\s*;)\s*0\s*;', r'\1 2 ;', text)
        self.write_reports(signoff=text)
        # Regenerate the test review because the raw signoff binding changed.
        shutil.rmtree(review)
        review = self.collect_async_fixture()
        self.assertTrue(producer.timing_evidence(self.root, async_review=review)['package_timing_admission'])
        self.assertFalse(producer.timing_evidence(self.root)['package_timing_admission'])
        for changed in (text.replace('; HDMI_TX_INT ', '; OTHER_INPUT '),
                        text.replace('No input delay was set on the input port.', 'Different finding.'),
                        re.sub(r'(?m)^(; TMC-20012 - [^;]+; High\s*;\s*0\s*;)\s*0\s*;', r'\1 1 ;', text)):
            self.write_reports(signoff=changed)
            shutil.rmtree(review)
            review = self.collect_async_fixture()
            try:
                evidence = producer.timing_evidence(self.root, async_review=review)
            except producer.BuildError:
                continue
            self.assertFalse(evidence['package_timing_admission'])

    def test_actual_fit_with_ack_copies_keeps_only_the_reviewed_logical_consumers(self):
        directory = self.root.parent / 'reports'
        fixture = self.async_reports(directory, 'de25_menu_async_inputs_ack_copies.json.gz')
        proof = producer.async_inputs.review_reports(directory)
        self.assertIn('fes_video_inst|hdmi_config|u0|ACK1~DUPLICATE', proof['HDMI_I2C_SDA']['consumers'])
        self.assertIn('fes_video_inst|hdmi_config|u0|ACK3~DUPLICATE', proof['HDMI_I2C_SDA']['consumers'])
        for bad in ('ACK4~DUPLICATE', 'ACK1~DUPLICATE_OTHER'):
            (directory / 'inventory.tsv').write_text(fixture['inventory.tsv'].replace('ACK1~DUPLICATE', bad))
            with self.assertRaises(producer.BuildError):
                producer.async_inputs.review_reports(directory)

    def test_source_artifact_database_and_report_substitution_invalidate_review(self):
        hps = self.prepare_async_fit()
        review = self.collect_async_fixture()
        self.assertEqual(producer.async_inputs.verify(self.root, review)['policy'], producer.async_inputs.POLICY)
        for path in (hps / 'linux-boot.core.rbf', hps / 'project/qdb/final.cdb', hps / 'generation.json',
                     self.root.parent / 'build-inputs.json', review / 'reports/inventory.tsv'):
            with self.subTest(path=path):
                original = path.read_bytes()
                try:
                    path.write_bytes(original + b'\n')
                    with self.assertRaises(ValueError):
                        producer.async_inputs.verify(self.root, review)
                finally:
                    path.write_bytes(original)

    def prepare_repository(self):
        paths = {producer.RECIPE: b'actual recipe', producer.ABI: b'actual ABI',
                 'cores/fes-menu/reader.v': b'module reader; endmodule',
                 'cores/fes-splash/splash.v': b'module splash; endmodule'}
        for name, data in paths.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        subprocess.run(['git', 'add', *paths], cwd=self.root, check=True)
        subprocess.run(['git', '-c', 'user.name=Build Test', '-c', 'user.email=build@example.invalid',
                        'commit', '-qm', 'build inputs'], cwd=self.root, check=True)
        revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=self.root, text=True).strip()
        captured = {'sources/misteross/' + name: data for name, data in paths.items() if not name.startswith('scripts/')}
        identities = {name: name + ' test identity' for name in ('quartus', 'qsys', 'pfg', 'ipgenerate')}
        return revision, captured, identities, {'toolchain': 'quartus', 'test_execution': True}

    def test_canonical_record_verifies_immutable_source_and_binds_tool_execution_bytes(self):
        revision, captured, identities, execution = self.prepare_repository()
        def record(ids=identities, run=execution, selected=revision):
            return producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', selected, ids, run, captured)
        original = record()
        self.assertEqual(json.loads(original)['parameters']['reset_release_timing'],
                         producer.TIMED_RESET_RELEASE)
        self.assertEqual(verify_record_source_at_revision(self.root, original)['source_inputs'], json.loads(original)['source_inputs'])
        self.assertEqual(build_identity(original), build_identity(record(selected='b' * 40)))
        changed = dict(identities, quartus='different actual compiler')
        self.assertNotEqual(build_identity(original), build_identity(record(ids=changed)))
        self.assertNotEqual(build_identity(original), build_identity(record(run=dict(execution, test_execution=False))))
        captured['sources/misteross/' + producer.ABI] = b'changed ABI'
        with self.assertRaisesRegex(producer.BuildError, 'differs from canonical closure'):
            record()

    def test_manifest_selects_only_the_real_de25_menu_contract(self):
        revision, captured, identities, execution = self.prepare_repository()
        record = producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', revision, identities, execution, captured)
        payload = self.root / 'core.rbf'
        payload.write_bytes(b'candidate payload')
        fields = tomllib.loads(producer.manifest(record, payload).decode())
        self.assertEqual(fields['target']['programming_profile'], 'de25-linux-menu-v1')
        self.assertEqual(fields['build']['id'], build_identity(record))
        self.assertEqual({(r['id'], r['major'], r['minor']) for r in fields['interfaces']},
            {('fes.video.fixed-720p60', 1, 0), ('fes.video.menu-display', 1, 0), ('fes.fabric.de25-scanout', 1, 1)})
        for platform, device in (('de10_nano', producer.DEVICE), ('de25_nano', '5CSEBA6U23I7'),
                                 ('de25_nano', 'A5EB013BB23BE4SR1'), ('other_board', producer.DEVICE)):
            changed = copy.deepcopy(fields)
            changed['target'].update(platform=platform, device=device)
            with self.subTest(platform=platform, device=device), self.assertRaises(PackageError):
                encode_manifest(changed)
        for version, extra in ((3, {'rom': {}}), (4, {'roms': [], 'rom_map': {}})):
            changed = copy.deepcopy(fields)
            changed.update(format=version, **extra)
            with self.subTest(format=version), self.assertRaisesRegex(PackageError, 'DE25 packages require format 2'):
                encode_manifest(changed)

    def test_controlled_environment_excludes_ambient_values_and_restores_on_failure(self):
        before = copy.copy(os.environ)
        with self.assertRaisesRegex(RuntimeError, 'test failure'):
            with producer.controlled_environment({'PATH': '/usr/bin', 'LANG': 'C'}):
                self.assertEqual(dict(os.environ), {'PATH': '/usr/bin', 'LANG': 'C'})
                raise RuntimeError('test failure')
        self.assertEqual(dict(os.environ), before)


if __name__ == '__main__':
    unittest.main()
