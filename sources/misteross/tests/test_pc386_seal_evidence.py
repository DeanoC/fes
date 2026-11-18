"""Synthetic pc386 receipt exercises the real historic-source and seal gates."""
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools'))
import build_pc386_product as seal
import pc386_seal_evidence as evidence
from scripts import source_closure
from scripts.core_package import package_identity
from scripts.export_core_package import build_identity
from tests.test_build_fes_pc386_quartus import CLOCK_TABLE, MET, UNCONSTRAINED, FIT_SUMMARY

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'https://github.com/DeanoC/fes.git'


def sha(data): return hashlib.sha256(data).hexdigest()


class EvidenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.repo = Path(cls.temp.name) / 'repo'
        cls.root = cls.repo / 'sources/misteross'
        cls.root.mkdir(parents=True)
        paths = set(seal.PINNED_INPUTS) | source_closure.imports('build_fes_pc386_quartus', ROOT)
        paths |= {'scripts/source_closure.py', 'scripts/bootstrap.sh', 'scripts/toolchain_cache.py'}
        for path in sorted(paths):
            target = cls.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / path, target)
        (cls.repo / 'config').mkdir()
        shutil.copy2(ROOT.parents[1] / 'config/core-recipes.toml', cls.repo / 'config/core-recipes.toml')
        def git(*args):
            return subprocess.check_output(['git', '-C', str(cls.repo), *args], text=True).strip()
        git('init', '-q')
        git('config', 'user.name', 'Evidence fixture')
        git('config', 'user.email', 'fixture@example.invalid')
        git('remote', 'add', 'origin', REPOSITORY)
        (cls.repo / '.gitignore').write_text('__pycache__/\nbuild/\n')
        git('add', '.')
        git('commit', '-qm', 'synthetic source snapshot')
        cls.revision = git('rev-parse', 'HEAD')
        cls.tools = {'yosys': 'synthetic-yosys', 'quartus': 'synthetic-quartus'}
        # The synthetic checkout contains byte-identical product source. This
        # unit fixture does not execute a compiler from that checkout.
        with mock.patch.object(seal, 'ROOT', cls.root):
            cls.record = seal.create_build_record(cls.root, REPOSITORY, cls.revision, cls.tools,
                        execution={'toolchain': 'quartus', 'gpu_device': 'lease'})
        cls.fields = json.loads(cls.record)
        cls.build_id = build_identity(cls.record)
        tool = seal.board.AuthenticatedTool(Path('/unused'), cls.tools['yosys'])
        verilator = seal.lockfile.load_lock(cls.root / seal.I386_LOCK)['verilator'].commit
        cls.report = {
            'run': {'passed': True, 'default_suite': True, 'complete_corpus': True, 'arguments': {}},
            'oracle': {**seal.oracle_identity(cls.root),
                       'linked_sha256': {n: '1' * 64 for n in seal.i386_oracle.LINKED_NAMES},
                       'testbench_sha256': '2' * 64},
            'verilator': {'lock': seal.I386_LOCK, 'commit': verilator, 'sha256': '3' * 64},
            'netlist': {'program_sha256': seal.yosys_netlist.program_sha256(seal.yosys_netlist.SEAL),
                        'cpu_source_sha256': seal.yosys_netlist.cpu_source_sha256(cls.root),
                        'netlist_sha256': '4' * 64,
                        'yosys': seal.yosys_netlist.lock_record(cls.root, tool, 'HIP', seal.TOOLCHAIN_LOCK)}}
        cls.report_bytes = json.dumps(cls.report).encode()
        cls.lockstep = seal.check_lockstep_metadata(cls.root, cls.report_bytes, tool, 'HIP')
        cls.payload = b'synthetic pc386 payload'
        cls.sta = (CLOCK_TABLE + MET + UNCONSTRAINED).encode()
        cls.fit = FIT_SUMMARY.encode()
        cls.qsf = seal.project_qsf(cls.root, cls.root / seal.OUTPUT / 'project', cls.build_id, 1, threads=2).encode()
        cls.summary = {
            'status': 'pass', 'build_id': cls.build_id, 'device': seal.board.TARGET, 'top': seal.board.TOP,
            'seed': 1, 'gpu_backend': 'hip', 'guest_rates': seal.guest_rates(),
            'rbf': {'sha256': sha(cls.payload), 'size': len(cls.payload)},
            'timing': seal.validate_timing_report(cls.sta.decode()), 'resources': seal.fit_resources(cls.fit.decode()),
            'firmware': {**seal.firmware_digests(cls.root), 'rom_hex': '6' * 64},
            'inputs': {p: cls.fields['source_inputs'][p] for p in sorted(seal.PINNED_INPUTS)},
            'tools': cls.tools, 'toolchain': seal.manifest_toolchain(cls.record), 'lockstep': cls.lockstep,
            'netlist': {'program_sha256': cls.report['netlist']['program_sha256'],
                        'cpu_source_sha256': cls.report['netlist']['cpu_source_sha256'],
                        'yosys': cls.report['netlist']['yosys'], 'machine_netlist_sha256': '7' * 64}}
        cls.files = {'build-summary.json': json.dumps(cls.summary).encode(),
                     'lockstep-report.json': cls.report_bytes, 'top.sta.rpt': cls.sta,
                     'top.fit.rpt': cls.fit, 'top.qsf': cls.qsf,
                     'quartus-version.log': (seal.quartus_lock(cls.root).version + '\n').encode()}
        encoded = seal.manifest(cls.root, cls.record, {'rbf': cls.summary['rbf']}, REPOSITORY, cls.revision, cls.tools)
        cls.package_id = package_identity(encoded, cls.payload, None, format_version=2)
        cls.package = cls.repo / cls.package_id
        cls.package.mkdir()
        for name, data in {'manifest.toml': encoded, 'core.rbf': cls.payload}.items():
            (cls.package / name).write_bytes(data)
            (cls.package / name).chmod(0o444)
        cls.package.chmod(0o555)
        cls.record_path = cls.repo / 'build-inputs.json'
        cls.record_path.write_bytes(cls.record)
        cls.record_path.chmod(0o444)

    @classmethod
    def tearDownClass(cls):
        cls.package.chmod(0o755)
        cls.temp.cleanup()

    def setUp(self):
        self.temp_case = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_case.cleanup)
        self.ev = Path(self.temp_case.name) / 'evidence'
        self.ev.mkdir()
        self.write(self.files)

    def write(self, files, *, make_receipt=True):
        self.ev.chmod(0o755)
        for old in self.ev.iterdir(): old.unlink()
        all_files = dict(files)
        if make_receipt:
            all_files['receipt.json'] = evidence.make_receipt(self.package, self.record, files)
        for name, data in all_files.items():
            (self.ev / name).write_bytes(data)
            (self.ev / name).chmod(0o444)
        self.ev.chmod(0o555)

    def test_matching_receipt_rechecks_original_source_and_reports(self):
        self.assertEqual(evidence.validate(self.root, self.package, self.record_path, self.ev),
                         sha((self.ev / 'receipt.json').read_bytes()))

    def test_historical_summary_does_not_depend_on_todays_packaging_list(self):
        with mock.patch.object(evidence.seal, 'PINNED_INPUTS',
                               tuple(evidence.seal.PINNED_INPUTS) + ('new-packaging-helper.py',)):
            self.assertEqual(evidence.validate(self.root, self.package, self.record_path, self.ev),
                             sha((self.ev / 'receipt.json').read_bytes()))
        summary = copy.deepcopy(self.summary)
        summary['inputs'].pop(seal.TOOLCHAIN_LOCK)
        files = dict(self.files)
        files['build-summary.json'] = json.dumps(summary).encode()
        self.write(files)
        with self.assertRaisesRegex(ValueError, 'source inputs differ'):
            evidence.validate(self.root, self.package, self.record_path, self.ev)

    def test_rehashed_bad_reports_cannot_bypass_semantic_checks(self):
        changes = []
        report = copy.deepcopy(self.report); report['run']['complete_corpus'] = False
        changes.append(('lockstep-report.json', json.dumps(report).encode()))
        report = copy.deepcopy(self.report); report['netlist']['cpu_source_sha256'] = '0' * 64
        changes.append(('lockstep-report.json', json.dumps(report).encode()))
        report = copy.deepcopy(self.report); report['netlist']['yosys']['tool'] = 'other'
        changes.append(('lockstep-report.json', json.dumps(report).encode()))
        report = copy.deepcopy(self.report); report['oracle']['patch_sha256'] = '0' * 64
        changes.append(('lockstep-report.json', json.dumps(report).encode()))
        changes.append(('top.sta.rpt', self.sta.replace(b'0.212', b'-0.212')))
        summary = copy.deepcopy(self.summary); summary['firmware']['bios'] = '0' * 64
        changes.append(('build-summary.json', json.dumps(summary).encode()))
        changes.append(('top.qsf', self.qsf.replace(b'NUM_PARALLEL_PROCESSORS 2', b'NUM_PARALLEL_PROCESSORS 3')))
        for name, value in changes:
            with self.subTest(name=name):
                files = dict(self.files); files[name] = value
                self.write(files)
                with self.assertRaises((ValueError, seal.board.BuildError)):
                    evidence.validate(self.root, self.package, self.record_path, self.ev)

    def test_missing_altered_and_swapped_evidence_refuses(self):
        self.ev.chmod(0o755)
        (self.ev / 'top.fit.rpt').unlink()
        self.ev.chmod(0o555)
        with self.assertRaisesRegex(ValueError, 'missing or has extra'):
            evidence.validate(self.root, self.package, self.record_path, self.ev)
        self.write(self.files)
        self.ev.chmod(0o755)
        (self.ev / 'receipt.json').chmod(0o644)
        (self.ev / 'receipt.json').write_bytes(b'{}')
        (self.ev / 'receipt.json').chmod(0o444)
        self.ev.chmod(0o555)
        with self.assertRaisesRegex(ValueError, 'does not bind'):
            evidence.validate(self.root, self.package, self.record_path, self.ev)

    def test_real_resolver_uses_only_complete_validated_local_and_cache_entries(self):
        store = self.root / 'build/packages'
        selected = evidence.publish_local(self.root, store, self.package, self.record_path, self.ev)
        cache = Path(self.temp_case.name) / 'cache'
        selection = Path(self.temp_case.name) / 'selection.toml'
        program = r'''
import json,sys
from pathlib import Path
from scripts import bundle
from scripts.recipes import recipe_for
def refuse_build(*args, **kwargs):
    raise RuntimeError('fixture must never launch a compiler')
bundle._build_package = refuse_build
result = bundle.resolve_core_package(Path(sys.argv[1]), 'f' * 40, Path(sys.argv[2]),
                                     recipe=recipe_for('fes.pc386'))
print(json.dumps({'package': result['directory'].name, 'source': result['inputs']['source_selection']}))
'''
        env = dict(os.environ, FES_ARTIFACT_CACHE_ROOT=str(cache), PYTHONDONTWRITEBYTECODE='1')
        args = [os.sys.executable, '-c', program, str(self.root), str(selection)]
        def resolve():
            return subprocess.run(args, cwd=ROOT.parents[1], env=env, text=True, capture_output=True)
        first = resolve()
        self.assertEqual(first.returncode, 0, first.stderr)
        selected_data = json.loads(first.stdout)
        self.assertEqual(selected_data['package'], self.package_id)
        self.assertEqual(selected_data['source']['pc386_evidence_sha256'], sha((self.ev / 'receipt.json').read_bytes()))
        # Drop the complete local entry; the immutable shared entry must be
        # independently inspected and selected without starting a compiler.
        entry = selected.parent
        entry.chmod(0o755)
        for directory in (entry / self.package_id, entry / 'evidence'):
            directory.chmod(0o755)
        shutil.rmtree(entry)
        # A later source revision changes only a packaging helper and a
        # toolchain lock comment. The original artifact and its historical
        # lockstep source must remain bound and reusable for the same design.
        for path in ('cores/fes-pc386/tools/pc386_seal_evidence.py', seal.TOOLCHAIN_LOCK):
            with (self.root / path).open('ab') as stream:
                stream.write(b'\n# synthetic later revision\n')
        git = ['git', '-C', str(self.repo)]
        subprocess.run(git + ['add', '.'], check=True)
        subprocess.run(git + ['commit', '-qm', 'synthetic toolchain and packaging drift'], check=True)
        second = resolve()
        self.assertEqual(second.returncode, 0, second.stderr)
        reused = json.loads(second.stdout)['source']
        self.assertNotEqual(reused['selected_revision'], reused['original_revision'])
        self.assertTrue(reused['toolchain_update_available'])
        cached = next(cache.glob('design-v1/*/*/evidence/receipt.json'))
        cached.chmod(0o644)
        cached.write_bytes(b'{}')
        cached.chmod(0o444)
        damaged = resolve()
        self.assertNotEqual(damaged.returncode, 0)
        self.assertIn('pc386 seal evidence failed validation', damaged.stderr)
        self.assertNotIn('fixture must never launch a compiler', damaged.stderr)

    def test_rejects_symlinks_and_oversized_evidence_before_reading(self):
        self.ev.chmod(0o755)
        target = self.ev / 'top.fit.rpt'
        target.unlink()
        target.symlink_to(self.record_path)
        self.ev.chmod(0o555)
        with self.assertRaises(OSError):
            evidence.validate(self.root, self.package, self.record_path, self.ev)
        self.ev.chmod(0o755)
        target.unlink()
        with target.open('wb') as stream:
            stream.truncate(64 * 1024 * 1024 + 1)
        target.chmod(0o444)
        self.ev.chmod(0o555)
        with self.assertRaisesRegex(ValueError, 'bounded sealed'):
            evidence.validate(self.root, self.package, self.record_path, self.ev)

    def test_private_export_evidence_can_be_published_idempotently(self):
        output = Path(self.temp_case.name) / 'quartus-output'
        (output / 'project').mkdir(parents=True)
        for name, data in self.files.items():
            target = output / ('project/top.qsf' if name == 'top.qsf' else name)
            target.write_bytes(data)
        with tempfile.TemporaryDirectory(dir=self.temp_case.name) as private:
            private = Path(private)
            package = private / self.package_id
            shutil.copytree(self.package, package)
            package.chmod(0o555)
            record = private / 'record.json'
            record.write_bytes(self.record)
            record.chmod(0o444)
            first = evidence.publish(self.root, package, record, output)
            self.assertEqual(evidence.publish(self.root, package, record, output), first)
            result = evidence.publish_local(self.root, Path(self.temp_case.name) / 'store', package, record, first)
            self.assertEqual(result.name, self.package_id)
        self.assertTrue(result.is_dir())

    def test_local_store_rejects_symlinked_ancestor_without_external_writes(self):
        outside = Path(self.temp_case.name) / 'outside'
        outside.mkdir()
        linked = Path(self.temp_case.name) / 'linked'
        linked.symlink_to(outside, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'traverse a link'):
            evidence.publish_local(self.root, linked / 'store', self.package, self.record_path, self.ev)
        self.assertEqual(list(outside.iterdir()), [])

    def test_local_publication_is_atomic_and_can_retry_after_interruption(self):
        store = Path(self.temp_case.name) / 'store'
        original = Path.rename
        def fail_publish(path, target):
            if path.name.startswith('.publish-'):
                self.assertEqual({p.name for p in path.iterdir()}, {self.package_id, 'build-inputs.json', 'evidence'})
                raise OSError('simulated interruption')
            return original(path, target)
        with mock.patch.object(Path, 'rename', fail_publish), self.assertRaisesRegex(OSError, 'interruption'):
            evidence.publish_local(self.root, store, self.package, self.record_path, self.ev)
        entries = store / '.pc386-entries'
        self.assertEqual(list(entries.iterdir()), [])
        result = evidence.publish_local(self.root, store, self.package, self.record_path, self.ev)
        self.assertEqual(result.name, self.package_id)
        self.assertEqual(evidence.publish_local(self.root, store, self.package, self.record_path, self.ev), result)
