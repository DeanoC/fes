"""Canonical DE25 build identity and complete compiler-report admission."""
import copy
import gzip
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib
import unittest

from scripts import build_de25_menu_package as producer
from scripts.core_package import PackageError, encode_manifest
from scripts.export_core_package import build_identity, verify_record_source_at_revision


class DE25MenuPackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='de25-menu-package-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
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

    def test_controlled_environment_excludes_ambient_values_and_restores_on_failure(self):
        before = copy.copy(os.environ)
        with self.assertRaisesRegex(RuntimeError, 'test failure'):
            with producer.controlled_environment({'PATH': '/usr/bin', 'LANG': 'C'}):
                self.assertEqual(dict(os.environ), {'PATH': '/usr/bin', 'LANG': 'C'})
                raise RuntimeError('test failure')
        self.assertEqual(dict(os.environ), before)


if __name__ == '__main__':
    unittest.main()
