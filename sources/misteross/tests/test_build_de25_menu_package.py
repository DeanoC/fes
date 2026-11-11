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
from types import SimpleNamespace
from unittest.mock import patch
from unittest import mock

from boards.de25nano.producers import build_menu_package as producer
from boards.de25nano.package.core_package import PackageError, encode_manifest
from boards.de25nano.package.export_core_package import build_identity, verify_record_source_at_revision


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
        text = re.sub(r'4 of (85|88) Rules Failed', r'0 of \1 Rules Failed',
                      self.fixture['golden_top.tq.drc.signoff.rpt'])
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

    def test_pr_revision_reports_are_independent_and_refuse_inherited_reviews(self):
        self.fixture = json.loads(gzip.decompress((Path(__file__).parent /
            'fixtures/de25_static_timing.json.gz').read_bytes()))
        for revision in ('base', 'persona_a', 'persona_b'):
            (self.reports / (revision + '.tq.drc.signoff.rpt')).write_text(self.passing_signoff())
            (self.reports / (revision + '.sta.summary')).write_text(self.fixture['golden_top.sta.summary'])
            evidence = producer.timing_evidence(self.root, revision=revision)
            self.assertTrue(evidence['package_timing_admission'])
            self.assertEqual(set(evidence['reports_sha256']),
                {revision + '.sta.summary', revision + '.tq.drc.signoff.rpt'})
            for kwargs in ({'async_review': 'foreign'}, {'audio_review': 'foreign'}):
                with self.assertRaisesRegex(producer.BuildError, 'cannot inherit'):
                    producer.timing_evidence(self.root, revision=revision, **kwargs)
        # A failing revision must not read another revision's passing reports.
        (self.reports / 'persona_b.tq.drc.signoff.rpt').write_text(
            self.fixture['golden_top.tq.drc.signoff.rpt'])
        self.assertFalse(producer.timing_evidence(self.root, revision='persona_b')['package_timing_admission'])
        self.assertTrue(producer.timing_evidence(self.root, revision='base')['package_timing_admission'])
        with self.assertRaisesRegex(producer.BuildError, 'unknown DE25 timing revision'):
            producer.timing_evidence(self.root, revision='../base')
        missing = self.passing_signoff().replace('BBD-60000', 'BBD-69999')
        (self.reports / 'base.tq.drc.signoff.rpt').write_text(missing)
        with self.assertRaisesRegex(producer.BuildError, 'missing PR partition-boundary'):
            producer.timing_evidence(self.root, revision='base')

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

    def test_setup_data_delay_is_checked_when_present_without_replacing_required_families(self):
        # This family is emitted by the real BRAM ZX81 fit, while the MENU
        # fixture has none. It grants no exception to a failing timing row.
        row = ("Type  : Setup Data Delay 'fes_video_inst|video_clock|iopll_0_outclk0'\n"
               "Slack : 5.944\nTNS   : 0.000\nCorner: Slow fix4 0C Model\n\n")
        summary = self.fixture['golden_top.sta.summary']
        self.write_reports(signoff=self.passing_signoff(), summary=summary + row)
        result = producer.timing_evidence(self.root)
        self.assertTrue(result['package_timing_admission'])
        self.assertEqual(result['timing_checks'][-1]['type'], 'Setup Data Delay')
        for before, after in (('5.944', '-0.001'), ('5.944', 'NaN'),
                              ('5.944', 'Infinity'), ('0.000', '-0.001')):
            with self.subTest(mutation=after):
                self.write_reports(signoff=self.passing_signoff(), summary=summary + row.replace(before, after))
                self.assertFalse(producer.timing_evidence(self.root)['package_timing_admission'])
        for changed in (summary + row + row, summary + row.replace('TNS   : 0.000\n', ''),
                        summary + row.replace('Setup Data Delay', 'Unknown Data Delay'),
                        summary.replace("Setup 'pll_inst|iopll_0_outclk0'", "Setup Data Delay 'pll_inst|iopll_0_outclk0'")):
            with self.subTest(summary=changed[-100:]):
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

    def test_vendor_output_review_preserves_reset_failures_and_raw_io_findings(self):
        proof = {'vendor_hdmi_reference': {'policy': producer.async_inputs.vendor_reference.POLICY,
                                         'independent_pcb_timing_qualified': False}}
        with mock.patch.object(producer.async_inputs, 'verify', return_value=proof):
            evidence = producer.timing_evidence(self.root, async_review='unit-wiring-control')
        self.assertEqual(evidence['failed_rules'], 4)
        self.assertEqual(evidence['unreviewed_failed_rules'], 2)
        self.assertEqual({r['id'] for r in evidence['reviewed_findings']}, {'TMC-20011', 'TMC-20012'})
        self.assertFalse(evidence['package_timing_admission'])
        self.assertEqual(next(r for r in evidence['rules'] if r['id'] == 'TMC-20012')['violations'], 30)
        # Only the two I/O findings remain in this synthetic admission control.
        raw = self.fixture['golden_top.tq.drc.signoff.rpt']
        io = {name: next(s for s in re.split(r'(?m)^Status:', raw)[1:] if '; ' + name + ' - ' in s)
              for name in ('TMC-20011', 'TMC-20012')}
        sections = re.split(r'(?m)^Status:', self.passing_signoff())
        for index, section in enumerate(sections):
            for name, replacement in io.items():
                if index and '; ' + name + ' - ' in section:
                    sections[index] = replacement
        text = 'Status:'.join(sections).replace('0 of 85 Rules Failed', '2 of 85 Rules Failed')
        for name, count in (('TMC-20011', 2), ('TMC-20012', 30)):
            text = re.sub(r'(?m)^(; ' + name + r' - [^;]+; High\s*;)\s*0\s*;',
                          lambda m: m[1] + f' {count} ;', text)
        self.write_reports(signoff=text)
        with mock.patch.object(producer.async_inputs, 'verify', return_value=proof):
            self.assertTrue(producer.timing_evidence(self.root, async_review='unit-wiring-control')['package_timing_admission'])
        self.assertFalse(producer.timing_evidence(self.root)['package_timing_admission'])

    def test_vendor_output_review_cannot_cover_an_extra_changed_or_waived_port(self):
        raw = self.fixture['golden_top.tq.drc.signoff.rpt']
        reference = producer.async_inputs.vendor_reference
        self.assertEqual(set(reference.output_findings(raw)), reference.OUTPUTS)
        for text in (raw.replace('; HDMI_TX_D[0] ', '; OTHER_OUTPUT '),
                     raw.replace('; LED[7] ', '; LED[6] '),
                     raw.replace('; HDMI_TX_D[1] ', '; HDMI_TX_D[0] '),
                     re.sub(r'(?m)^(; HDMI_TX_DE\s*;[^;]+;)\s*;', r'\1 Yes ;', raw, count=1),
                     raw.replace('No output delay was set on the output port.', 'Different finding.')):
            self.assertNotEqual(text, raw)
            with self.assertRaises(producer.BuildError):
                reference.output_findings(text)

    def test_audio_review_adds_only_two_outputs_and_cannot_bypass_numerical_timing(self):
        from boards.de25nano.scanout import audio_reference as audio
        raw = self.fixture['golden_top.tq.drc.signoff.rpt']
        io = {name: next(s for s in re.split(r'(?m)^Status:', raw)[1:] if '; ' + name + ' - ' in s)
              for name in ('TMC-20011', 'TMC-20012')}
        io['TMC-20012'] = re.sub(r'Number of violations:\s*30', 'Number of violations: 32', io['TMC-20012'])
        io['TMC-20012'] += ''.join('; ' + port + ' ; No output delay was set on the output port. ; ;\n'
                                  for port in sorted(audio.PORTS))
        sections = re.split(r'(?m)^Status:', self.passing_signoff())
        for index, section in enumerate(sections):
            for name, replacement in io.items():
                if index and '; ' + name + ' - ' in section:
                    sections[index] = replacement
        text = 'Status:'.join(sections).replace('0 of 85 Rules Failed', '2 of 85 Rules Failed')
        for name, count in (('TMC-20011', 2), ('TMC-20012', 32)):
            text = re.sub(r'(?m)^(; ' + name + r' - [^;]+; High\s*;)\s*0\s*;',
                          lambda match: match[1] + f' {count} ;', text)
        self.write_reports(signoff=text)
        # These mocks isolate the admission decision. Actual fit/source/tool
        # verification is exercised separately and remains required in builds.
        with mock.patch.object(producer.async_inputs, 'verify', return_value={'vendor_hdmi_reference': {}}), \
                mock.patch.object(audio, 'verify', return_value={'policy': audio.POLICY}):
            with self.assertRaises(producer.BuildError):
                producer.timing_evidence(self.root, async_review='unit-input')
            with self.assertRaises(producer.BuildError):
                producer.timing_evidence(self.root, audio_review='unit-audio')
            evidence = producer.timing_evidence(self.root, async_review='unit-input', audio_review='unit-audio')
            self.assertTrue(evidence['package_timing_admission'])
            self.assertEqual(evidence['failed_rules'], 2)
            self.assertEqual(evidence['unreviewed_failed_rules'], 0)
            self.assertEqual(evidence['reviewed_findings'][-1]['violations'], 32)
            summary = re.sub(r'(?m)^(Slack\s*:\s*)\S+', r'\g<1>-0.001',
                             self.fixture['golden_top.sta.summary'], count=1)
            self.write_reports(signoff=text, summary=summary)
            self.assertFalse(producer.timing_evidence(self.root, async_review='unit-input',
                audio_review='unit-audio')['package_timing_admission'])

    def prepare_repository(self):
        paths = {producer.RECIPE: b'# actual recipe\n', producer.ABI: b'actual ABI',
                 'cores/fes-menu/rtl/reader.v': b'module reader; endmodule',
                 'cores/fes-splash/rtl/splash.v': b'module splash; endmodule'}
        paths.update({name: b'# fitted review fixture\n' for name in producer.REVIEW_INPUTS})
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
        self.assertEqual(json.loads(original)['parameters']['fabric_initiator_policy'],
                         producer.MANAGED_FABRIC)
        self.assertEqual(json.loads(original)['parameters']['fifo_reset_sharing'],
                         producer.SHARED_FIFO_RESETS)
        self.assertEqual(verify_record_source_at_revision(self.root, original)['source_inputs'], json.loads(original)['source_inputs'])
        self.assertEqual(build_identity(original), build_identity(record(selected='b' * 40)))
        changed = dict(identities, quartus='different actual compiler')
        self.assertNotEqual(build_identity(original), build_identity(record(ids=changed)))
        self.assertNotEqual(build_identity(original), build_identity(record(run=dict(execution, test_execution=False))))
        captured['sources/misteross/' + producer.ABI] = b'changed ABI'
        with self.assertRaisesRegex(producer.BuildError, 'differs from canonical closure'):
            record()

    def test_shared_producer_api_keeps_native_record_and_explicit_tool_identity(self):
        revision, captured, identities, execution = self.prepare_repository()
        expected = producer.create_build_record(self.root, 'https://github.com/DeanoC/fes',
            revision, identities, execution, captured)
        builder = SimpleNamespace(capture=lambda selected: captured if selected == revision else None)
        paths = {name: self.root / name for name in identities}
        with patch.object(producer, 'scanout', return_value=builder), \
                patch.object(producer, 'authenticate_tools', return_value=(paths, identities)) as authenticate, \
                patch.dict(os.environ, {'QUARTUS_ROOTDIR': '/explicit-pro'}):
            actual = producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', revision,
                identities, execution=execution, identity_version=2)
            self.assertEqual(actual, expected)
            tools = producer._authenticate_tools(self.root, cache_root=self.root / 'unrelated-oss-cache')
            authenticate.assert_called_once_with(builder, '/explicit-pro')
            self.assertEqual({name: tool.path for name, tool in tools.items()}, paths)
            self.assertEqual({name: tool.identity for name, tool in tools.items()}, identities)
        for version in (1, True, 2.0):
            with self.subTest(identity_version=version), self.assertRaises(producer.BuildError):
                producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', revision,
                    identities, execution=execution, captured=captured, identity_version=version)

    def test_shared_clean_source_check_cannot_change_pinned_inputs(self):
        with patch.object(producer, 'require_clean_source', return_value=('source', 'a' * 40)) as check:
            producer._require_clean_source(self.root, identity_version=2)
            check.assert_called_once_with(self.root, identity_version=2,
                pinned_inputs=(producer.RECIPE, producer.ABI))
            check.reset_mock()
            with self.assertRaises(producer.BuildError):
                producer._require_clean_source(self.root, pinned_inputs=('other',))
            check.assert_not_called()

    def test_shared_cli_refuses_missing_archive_before_tool_or_output_work(self):
        output = self.root / 'not-created'
        with patch.dict(os.environ, {}, clear=True), patch.object(producer, 'authenticate_tools') as authenticate, \
                patch('sys.stderr'), self.assertRaises(SystemExit) as stopped:
            producer.main(['--root', str(self.root), '--identity-version', '2',
                           '--cache-root', str(self.root / 'cache'), '--package-output', str(output)])
        self.assertEqual(stopped.exception.code, 1)
        authenticate.assert_not_called()
        self.assertFalse(output.exists())

    def test_manifest_selects_only_the_real_de25_menu_contract(self):
        revision, captured, identities, execution = self.prepare_repository()
        record = producer.create_build_record(self.root, 'https://github.com/DeanoC/fes', revision, identities, execution, captured)
        payload = self.root / 'core.rbf'
        payload.write_bytes(b'candidate payload')
        fields = tomllib.loads(producer.manifest(record, payload).decode())
        self.assertEqual(fields['target']['programming_profile'], 'de25-linux-menu-v1')
        self.assertEqual(fields['build']['id'], build_identity(record))
        self.assertEqual({(r['id'], r['major'], r['minor']) for r in fields['interfaces']},
            {('fes.video.fixed-720p60', 1, 0), ('fes.video.menu-display', 1, 0), ('fes.fabric.de25-scanout', 1, 3)})
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
