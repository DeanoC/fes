import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import status


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


class StatusTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        git(self.root, 'init', '-q')
        git(self.root, 'config', 'user.name', 'Test')
        git(self.root, 'config', 'user.email', 'test@example.invalid')
        (self.root / '.gitignore').write_text('/out/\n')
        for name in status.source_status.COMPONENTS:
            module = self.root / 'sources' / name
            module.mkdir(parents=True)
            (module / 'README').write_text(name)
        config = self.root / 'config'
        config.mkdir()
        (config / 'core-recipes.toml').write_text('version=1\nrecipes=[]\n')
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-qm', 'fixture')
        self.head = git(self.root, 'rev-parse', 'HEAD')
        self.output = self.root / 'out/native-integration-dev'
        self.output.mkdir(parents=True)

    def host(self, revision=None):
        metadata = {'sources': {'FogCast': revision or self.head}}
        raw = json.dumps(metadata, sort_keys=True).encode()
        (self.output / 'host-inputs.json').write_bytes(raw)
        for name in ('fogcast', 'fogcast-api'):
            (self.output / name).write_bytes(b'binary')
        receipt = {'inputs': hashlib.sha256(raw).hexdigest(), 'fes_revision': revision or self.head,
                   'files': dict.fromkeys(('fogcast', 'fogcast-api'), hashlib.sha256(b'binary').hexdigest()), 'os': 'linux', 'arch': 'amd64'}
        (self.output / 'host.json').write_text(json.dumps(receipt))
        return receipt

    def test_offline_unknowns_preserve_worktree_index_and_refs(self):
        (self.root / 'sources/FogCast/local').write_text('local changes')
        before = ((self.root / '.git/index').read_bytes(), git(self.root, 'show-ref'), git(self.root, 'status', '--porcelain'))
        original = status.source_status.git
        def no_network(root, *args, **kwargs):
            self.assertNotIn('ls-remote', args)
            return original(root, *args, **kwargs)
        with patch.object(status.source_status, 'git', side_effect=no_network):
            observed = status.report(self.root, offline=True)
        self.assertEqual(observed['latest_merged']['remote_state'], 'offline')
        self.assertEqual(observed['working_tree']['checkout_state'], 'dirty')
        for field in ('ci_verified', 'hardware_qualified', 'deployed'):
            self.assertEqual(observed[field]['state'], 'unknown')
        self.assertEqual(before, ((self.root / '.git/index').read_bytes(), git(self.root, 'show-ref'), git(self.root, 'status', '--porcelain')))

    def test_verified_old_bytes_are_separate_from_source_freshness(self):
        self.host('a' * 40)
        observed = status.artifact_receipt(self.output, 'host', self.head)
        self.assertEqual(observed['state'], 'bytes-verified')
        self.assertEqual(observed['source_linkage'], 'different-from-selected')
        self.assertEqual(observed['input_closure']['binding'], 'matches')
        self.assertEqual(observed['qualification'], 'not assessed')
        (self.output / 'fogcast').write_bytes(b'tampered')
        self.assertEqual(status.artifact_receipt(self.output, 'host', self.head)['state'], 'digest-mismatch')

    def test_required_outputs_and_receipt_schema_enforced(self):
        receipt = self.host()
        receipt['files'].pop('fogcast-api')
        (self.output / 'host.json').write_text(json.dumps(receipt))
        self.assertIn('required outputs', status.artifact_receipt(self.output, 'host', self.head)['error'])
        image = {'fes_revision': self.head, 'inputs': 'a' * 64, 'files': {'foo.txt': 'b' * 64}}
        (self.output / 'image.json').write_text(json.dumps(image))
        self.assertIn('linux.img', status.artifact_receipt(self.output, 'image', self.head)['error'])
        receipt = self.host()
        receipt['os'] = 'invalid'
        (self.output / 'host.json').write_text(json.dumps(receipt))
        self.assertEqual(status.artifact_receipt(self.output, 'host', self.head)['state'], 'unavailable-or-invalid')

    def test_image_derived_inputs_and_software_evidence_are_independently_bound(self):
        import build
        package = {'inputs': {'selection': {'package_id': 'a' * 64}, 'selection_sha256': 'b' * 64}}
        fingerprint, inputs = build.image_fingerprint('c' * 64, {'sources': {}}, package)
        (self.output / 'inputs.json').write_text(json.dumps(inputs))
        (self.output / 'linux.img').write_bytes(b'image fixture')
        image_digest = hashlib.sha256(b'image fixture').hexdigest()
        (self.output / 'image.json').write_text(json.dumps({'fes_revision': self.head, 'inputs': fingerprint,
            'files': {'linux.img': image_digest}}))
        observed = status.artifact_receipt(self.output, 'image', self.head)
        self.assertEqual(observed['state'], 'bytes-verified')
        self.assertEqual(observed['input_closure']['binding'], 'matches')
        self.assertEqual(observed['software_verification']['state'], 'unknown')
        (self.output / 'qemu-smoke.log').write_bytes(b'QEMU passed\n')
        (self.output / 'reproducibility.txt').write_text('run_1_sha256=' + image_digest + '\nrun_2_sha256=' + image_digest + '\n')
        verification = {'image_sha256': image_digest, 'structural': 'pass', 'qemu_packaging': 'pass',
                        'two_pass_reproducibility': 'pass', 'qemu_log_sha256': hashlib.sha256(b'QEMU passed\n').hexdigest()}
        (self.output / 'verification.json').write_text(json.dumps(verification))
        observed = status.artifact_receipt(self.output, 'image', self.head)
        self.assertEqual(observed['software_verification']['state'], 'verified-recorded-software-evidence')
        (self.output / 'qemu-smoke.log').write_bytes(b'changed log')
        observed = status.artifact_receipt(self.output, 'image', self.head)
        self.assertEqual(observed['state'], 'bytes-verified')
        self.assertEqual(observed['software_verification']['state'], 'invalid-or-unavailable')
        inputs['fpga_packages'][0]['selection_sha256'] = 'd' * 64
        (self.output / 'inputs.json').write_text(json.dumps(inputs))
        observed = status.artifact_receipt(self.output, 'image', self.head)
        self.assertEqual(observed['state'], 'bytes-verified')
        self.assertEqual(observed['input_closure']['state'], 'invalid-or-unavailable')

    def test_missing_malformed_and_symlink_receipts_are_actionable(self):
        for raw in (None, '{', '[]', '{"files":{}}'):
            if raw is not None:
                (self.output / 'host.json').write_text(raw)
            observed = status.artifact_receipt(self.output, 'host', self.head)
            self.assertIn('action', observed)
        (self.output / 'host.json').unlink()
        (self.output / 'host.json').symlink_to(self.root / '.gitignore')
        self.assertIn('symlink', status.artifact_receipt(self.output, 'host', self.head)['error'])

    def test_artifact_traversal_and_symlinks_rejected(self):
        receipt = self.host()
        (self.output / 'fogcast').unlink()
        (self.output / 'fogcast').symlink_to(self.root / '.gitignore')
        self.assertIn('symlink', status.artifact_receipt(self.output, 'host', self.head)['error'])
        receipt['files'] = {'fogcast': 'a' * 64, 'fogcast-api': 'b' * 64, '../outside': 'a' * 64}
        (self.output / 'host.json').write_text(json.dumps(receipt))
        self.assertIn('required outputs', status.artifact_receipt(self.output, 'host', self.head)['error'])

    def test_working_lock_change_does_not_relabel_committed_digest(self):
        path = self.root / 'config/core-recipes.toml'
        original = hashlib.sha256(path.read_bytes()).hexdigest()
        path.write_text('version=1\nrecipes=[]\n# local\n')
        row = next(item for item in status.locks(self.root)['files'] if item['path'] == 'config/core-recipes.toml')
        self.assertEqual(row['committed_sha256'], original)
        self.assertFalse(row['matches_selected'])
        path.unlink()
        row = next(item for item in status.locks(self.root)['files'] if item['path'] == 'config/core-recipes.toml')
        self.assertEqual(row['committed_sha256'], original)
        self.assertEqual(row['state'], 'unavailable')

    def test_existing_qualification_receipt_reports_exact_scope_not_deployment(self):
        archive = self.root / 'package.fcore'
        archive.write_bytes(b'archive')
        path = self.root / 'acceptance.json'
        data = {'format': 1, 'success': True, 'mode': 'lifecycle-only', 'archive_path': str(archive),
                'archive_sha256': hashlib.sha256(b'archive').hexdigest(), 'package_id': 'a' * 64,
                'target_id': 'test-kit', 'created_at_utc': '2026-09-20T12:00:00Z', 'revisions': {}}
        path.write_text(json.dumps(data))
        observed = status.report(self.root, offline=True, qualification_files=[path])
        qualified = observed['hardware_qualified'][0]
        self.assertEqual(qualified['state'], 'recorded-pass')
        self.assertEqual(qualified['scope'], 'lifecycle-only')
        self.assertEqual(qualified['artifact_bytes']['state'], 'bytes-verified')
        self.assertEqual(observed['deployed']['state'], 'unknown')
        archive.write_bytes(b'changed')
        self.assertEqual(status.qualification(path)['artifact_bytes']['state'], 'digest-mismatch')

    def test_ci_exact_tested_identity_and_base_remain_distinct(self):
        path = self.root / 'ci.json'
        data = {'format': 1, 'kind': 'ci-observation', 'source_revision': 'a' * 40,
                'integration_base': 'b' * 40, 'observed_at': '2026-09-20T12:00:00Z',
                'checks': [{'name': 'integration', 'result': 'success'}]}
        path.write_text(json.dumps(data))
        observed = status.supplied_observation(path, 'ci-observation', self.head)
        self.assertEqual(observed['state'], 'recorded-success')
        self.assertEqual(observed['source_linkage'], 'different-from-selected')
        self.assertEqual(observed['observation']['integration_base'], 'b' * 40)
        data['integration_base'] = None
        path.write_text(json.dumps(data))
        self.assertEqual(status.supplied_observation(path, 'ci-observation', self.head)['state'], 'incomplete-observation')

    def test_old_deployment_not_candidate_current_and_absent_identities_unknown(self):
        path = self.root / 'deployed.json'
        data = {'format': 1, 'kind': 'deployment-observation', 'kit': 'kit-a',
                'observed_at': '2026-09-20T12:00:00Z', 'identities': {'agent': {'revision': 'a' * 40}}}
        path.write_text(json.dumps(data))
        observed = status.supplied_observation(path, 'deployment-observation', self.head)
        self.assertEqual(observed['state'], 'incomplete-observation')
        self.assertEqual(observed['identity_comparisons']['agent']['source_linkage'], 'different-from-selected')
        self.assertEqual(observed['identity_comparisons']['image']['state'], 'unknown')
        data['identities']['agent'] = 'latest'
        path.write_text(json.dumps(data))
        self.assertEqual(status.supplied_observation(path, 'deployment-observation', self.head)['state'], 'invalid-evidence')

    def test_invalid_root_returns_actionable_unknown_not_traceback(self):
        observed = status.report(self.output, offline=True)
        self.assertEqual(observed['selected']['state'], 'unknown')
        self.assertIn('action', observed['selected'])

    def test_malformed_observation_and_timeout_are_rejected(self):
        path = self.root / 'observation.json'
        ci = {'format': 1, 'kind': 'ci-observation', 'source_revision': '0' * 40,
              'integration_base': self.head, 'observed_at': '2026-09-20T12:00:00Z',
              'checks': [{'name': 'components', 'result': 'success'}]}
        path.write_text(json.dumps(ci))
        self.assertEqual(status.supplied_observation(path, 'ci-observation', self.head)['state'], 'invalid-evidence')
        for timeout in (-1, 0, 121):
            run = subprocess.run([sys.executable, status.__file__, '--offline', '--timeout', str(timeout)], capture_output=True)
            self.assertEqual(run.returncode, 2)
            self.assertIn(b'--timeout must', run.stderr)
        data = {'format': 1, 'success': True, 'mode': 'lifecycle-only', 'archive_sha256': 'a' * 64,
                'package_id': 'b' * 64, 'target_id': 'kit', 'created_at_utc': 'invalid', 'revisions': []}
        path.write_text(json.dumps(data))
        self.assertEqual(status.qualification(path)['state'], 'invalid-evidence')
        data['revisions'] = {}
        path.write_text(json.dumps(data))
        self.assertEqual(status.qualification(path)['state'], 'invalid-evidence')

    def test_cli_summary_default_and_full_json_option(self):
        command = [sys.executable, status.__file__, '--root', str(self.root), '--offline']
        text = subprocess.check_output(command, text=True)
        for label in ('Working tree:', 'Latest merged:', 'Selected:', 'CI evidence:', 'Built:', 'Hardware evidence:', 'Deployment evidence:'):
            self.assertIn(label, text)
        self.assertLessEqual(len(text.splitlines()), 8)
        report = json.loads(subprocess.check_output(command + ['--json'], text=True))
        self.assertEqual(report['selected']['revision'], self.head)
        self.assertEqual(report['deployed']['state'], 'unknown')

    def test_aggregate_emission_retains_failure_and_unknown_base(self):
        source = Path(__file__).resolve().parents[1]
        environment = dict(os.environ, RESULTS=json.dumps({'host': {'result': 'failure'}}),
                           PYTHONPATH=str(source),
                           TESTED_REVISION=self.head, INTEGRATION_BASE='0' * 40,
                           REPOSITORY='test/fes', RUN_URL='https://example.invalid/runs/1')
        run = subprocess.run([sys.executable, '-m', 'scripts.ci_gate'], cwd=self.root,
                             env=environment, capture_output=True)
        self.assertNotEqual(run.returncode, 0)
        observed = status.supplied_observation(self.root / 'ci-observation.json', 'ci-observation', self.head)
        self.assertEqual(observed['state'], 'incomplete-observation')
        self.assertEqual(observed['observation']['checks'][0]['result'], 'failure')
        self.assertIsNone(observed['observation']['integration_base'])

    def test_workflow_emits_tested_checkout_identity_and_retains_observation(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        self.assertIn('TESTED_REVISION: ${{ github.sha }}', workflow)
        self.assertIn('run: python3 -m scripts.ci_gate', workflow)
        self.assertIn('path: ci-observation.json', workflow)

    def test_validated_skipped_lanes_emit_successful_status_evidence(self):
        from scripts.affected import LANES
        from scripts.ci_gate import JOB_LANES
        results = {job: {'result': 'skipped'} for job in
                   (*JOB_LANES, 'simulation-tools', 'fpga-simulation')}
        results['plan'] = {'result': 'success', 'outputs': {
            'lanes': json.dumps({lane: False for lane in LANES}),
            'cores': '[]', 'simulations': '{"include": []}'}}
        environment = dict(os.environ, RESULTS=json.dumps(results),
                           PYTHONPATH=str(Path(__file__).resolve().parents[1]),
                           TESTED_REVISION=self.head, INTEGRATION_BASE=self.head,
                           REPOSITORY='test/fes', RUN_URL='https://example.invalid/runs/2')
        run = subprocess.run([sys.executable, '-m', 'scripts.ci_gate'], cwd=self.root,
                             env=environment, capture_output=True)
        self.assertEqual(run.returncode, 0, run.stderr)
        observed = status.supplied_observation(self.root / 'ci-observation.json',
                                              'ci-observation', self.head)
        self.assertEqual(observed['state'], 'recorded-success')
        self.assertEqual(observed['observation']['checks'], [{'name': 'plan', 'result': 'success'}])


if __name__ == '__main__':
    unittest.main()
