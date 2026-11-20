"""Synthetic pc386 receipt exercises the real historic-source and seal gates.

EvidenceReuseTests (#1984 S3) seals a design-v2 entry, the window S4 opens,
and builds each counterexample to evidence-only reuse against it.
"""
import contextlib
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
from scripts import input_identity, source_closure
from scripts.core_package import package_identity
from scripts.export_core_package import build_identity
from tests.test_build_fes_pc386_quartus import CLOCK_TABLE, MET, UNCONSTRAINED, FIT_SUMMARY

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'https://github.com/DeanoC/fes.git'
# The CPU netlist the synthetic seal was built from. A test regenerates it by
# writing these bytes where the seal's Yosys would.
NETLIST = b'// synthetic fes_i386_cpu netlist\n'
PRODUCER = 'cores/fes-pc386/tools/build_pc386_product.py'


def sha(data): return hashlib.sha256(data).hexdigest()


def to_design_v2(record, root):
    """The record S4's producer would write for the same tree: design-v2, with
    the ROM hex digest firmware.lock pins in its firmware parameter."""
    fields = json.loads(record)
    fields.pop('identities')
    parameters = fields['parameters']
    for key in ('identity_policy', 'toolchain_pins'):
        parameters.pop(key)
    parameters['firmware'] = json.dumps({**json.loads(parameters['firmware']),
                                         'rom_hex': seal.pinned_rom_digest(root)},
                                        sort_keys=True, separators=(',', ':'))
    return seal.encode_build_record(input_identity.with_identities(fields, root=root, policy=input_identity.DESIGN_V2))


class SealedEntryFixture(unittest.TestCase):
    """A synthetic Git checkout holding a sealed pc386 entry under POLICY."""
    POLICY = input_identity.DESIGN_V1

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
        if cls.POLICY == input_identity.DESIGN_V2:
            # Stand-in for S4: this checkout's producer writes design-v2, so the
            # real resolver selects with a v2 record too.
            producer = cls.root / PRODUCER
            text = producer.read_text()
            for old, new in (('with_identities(fields, root=root)',
                              'with_identities(fields, root=root, policy="design-v2")'),
                             ('json.dumps(firmware_digests(root), sort_keys',
                              'json.dumps({**firmware_digests(root), "rom_hex": pinned_rom_digest(root)}, sort_keys')):
                if text.count(old) != 1:
                    raise AssertionError('producer fixture patch no longer applies: ' + old)
                text = text.replace(old, new)
            producer.write_text(text)
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
        cls.record = cls.record_at(cls.revision)
        cls.fields = json.loads(cls.record)
        cls.build_id = build_identity(cls.record)
        tool = seal.board.AuthenticatedTool(Path('/unused'), cls.tools['yosys'])
        verilator = seal.lockfile.load_lock(cls.root / seal.I386_LOCK)['verilator'].commit
        cls.report = {
            'run': {'passed': True, 'default_suite': True, 'complete_corpus': True, 'arguments': {}},
            'oracle': {**seal.oracle_identity(cls.root),
                       'linked_sha256': {n: '1' * 64 for n in seal.LINKED_NAMES},
                       'testbench_sha256': '2' * 64},
            'verilator': {'lock': seal.I386_LOCK, 'commit': verilator, 'sha256': '3' * 64},
            'netlist': {'program_sha256': seal.yosys_netlist.program_sha256(seal.yosys_netlist.SEAL),
                        'cpu_source_sha256': seal.yosys_netlist.cpu_source_sha256(cls.root),
                        'netlist_sha256': sha(NETLIST),
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
            'firmware': {**seal.firmware_digests(cls.root), 'rom_hex': seal.pinned_rom_digest(cls.root)},
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
    def record_at(cls, revision):
        """The producer's record of the synthetic checkout at revision, under POLICY."""
        # The synthetic checkout's product source matches this one's, apart
        # from the v2 stand-in. This fixture executes no compiler from it.
        with mock.patch.object(seal, 'ROOT', cls.root):
            record = seal.create_build_record(cls.root, REPOSITORY, revision, cls.tools,
                                              execution={'toolchain': 'quartus', 'gpu_device': 'lease'})
        return to_design_v2(record, cls.root) if cls.POLICY == input_identity.DESIGN_V2 else record

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


class EvidenceTests(SealedEntryFixture):
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

    def test_tampered_retained_oracle_evidence_is_rejected(self):
        files = dict(self.files)
        report = copy.deepcopy(self.report)
        report['oracle']['provisioner_sha256'] = '0' * 64
        files['lockstep-report.json'] = json.dumps(report).encode()
        self.write(files)
        with self.assertRaises((ValueError, seal.board.BuildError)):
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


def _remove(path):
    """Remove a tree of sealed (read-only) entries."""
    path = Path(path)
    if not path.exists():
        return
    for directory, _subdirectories, _files in os.walk(path):
        os.chmod(directory, 0o755)
    shutil.rmtree(path)


RESOLVE = r'''
import json, sys
from pathlib import Path
from scripts import bundle
from scripts.recipes import recipe_for
def refuse_build(*args, **kwargs):
    raise RuntimeError('fixture must never launch a compiler')
bundle._build_package = refuse_build
selection = bundle.canonical_package_record
def cache_miss(*args, selection_only=False, **kwargs):
    # A miss authenticates the producer and then builds: stop at the first step.
    if not selection_only:
        raise RuntimeError('cache miss: the resolver would rebuild')
    return selection(*args, selection_only=True, **kwargs)
bundle.canonical_package_record = cache_miss
if sys.argv[3] == 'lockstep-fails':
    checked = bundle._pc386_reuse
    def lockstep_fails(*args, run_lockstep=False, **kwargs):
        if run_lockstep:
            raise ValueError('pc386 evidence-only reuse refused: fresh full lockstep failed: fixture lockstep exit 1')
        return checked(*args, **kwargs)
    bundle._pc386_reuse = lockstep_fails
result = bundle.resolve_core_package(Path(sys.argv[1]), 'f' * 40, Path(sys.argv[2]),
                                     recipe=recipe_for('fes.pc386'))
print(json.dumps({'package': result['directory'].name, 'source': result['inputs']['source_selection']}))
'''


class DesignV1ReuseTests(SealedEntryFixture):
    """Under design-v1 every evidence file is a design input: S3 changes nothing there."""

    def test_design_v1_evidence_change_moves_the_design_id_so_reuse_is_refused(self):
        tb = 'cores/fes-common/sim/i386/lockstep_tb.cpp'
        selected = json.loads(self.record)
        selected['source_inputs'] = dict(selected['source_inputs'], **{tb: '0' * 64})
        selected = input_identity.with_identities(
            {k: v for k, v in selected.items() if k != 'identities'}, policy=input_identity.DESIGN_V1)
        self.assertNotEqual(input_identity.functional_key(selected), input_identity.functional_key(self.fields))
        with self.assertRaisesRegex(ValueError, 'design identity differs'):
            evidence.evidence_drift(self.fields, selected)


class EvidenceReuseTests(SealedEntryFixture):
    """#1984 S3: an evidence-only change reuses a sealed package only after a
    fresh full lockstep of the selected tree passes. Each test builds one
    counterexample against a sealed design-v2 entry."""
    POLICY = input_identity.DESIGN_V2
    TESTBENCH = 'cores/fes-common/sim/i386/lockstep_tb.cpp'
    SUITE = 'scripts/test_fes_i386_lockstep.py'

    def setUp(self):
        super().setUp()
        self.base = self.git('rev-parse', 'HEAD')
        self.store = self.root / 'build/packages'
        self.addCleanup(self.reset)
        self.tool = seal.board.AuthenticatedTool(Path('/unused'), self.tools['yosys'])

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], text=True).strip()

    def reset(self):
        self.git('reset', '-q', '--hard', self.base)
        _remove(self.root / 'build')

    def change(self, *paths):
        """Commit a comment appended to each path; return the selected record there."""
        for path in paths:
            comment = b'\n// synthetic edit\n' if path.endswith(('.cpp', '.h', '.sv')) else b'\n# synthetic edit\n'
            with (self.root / path).open('ab') as stream:
                stream.write(comment)
        self.git('add', '--', *('sources/misteross/' + path for path in paths))
        self.git('commit', '-qm', 'synthetic edit: ' + ', '.join(paths))
        return self.record_at(self.git('rev-parse', 'HEAD'))

    def fresh_report(self, **run):
        """A full default lockstep report of the checkout as it is now."""
        report = copy.deepcopy(self.report)
        report['oracle'] = {**seal.oracle_identity(self.root),
                            'linked_sha256': {n: '1' * 64 for n in seal.LINKED_NAMES}, 'testbench_sha256': '5' * 64}
        report['run'].update(run)
        return json.dumps(report).encode()

    def regenerate(self, netlist=NETLIST):
        def seal_netlist(yosys, output, root=None, **kwargs):
            output.mkdir(parents=True, exist_ok=True)
            (output / 'fes_i386_cpu.netlist.v').write_bytes(netlist)
            return output / 'fes_i386_cpu.netlist.v'
        return mock.patch.object(seal.yosys_netlist, 'seal_netlist', seal_netlist)

    def no_quartus(self):
        """Every path that would compile the design raises."""
        stack = contextlib.ExitStack()
        for name in ('run_quartus', 'elaborate_machine', 'write_project'):
            stack.enter_context(mock.patch.object(seal, name, side_effect=AssertionError('Quartus path reached: ' + name)))
        return stack

    def gate(self, selected):
        return evidence.reuse_gate(self.root, self.package, self.record_path, self.ev, selected, self.store)

    def attest(self, selected, report):
        with self.regenerate(), self.no_quartus():
            return evidence.attest_fresh_lockstep(self.root, self.package, self.record_path, self.ev,
                                                  selected, report, self.tool, self.store)

    def resolve(self, mode='real'):
        cache = Path(self.temp_case.name) / 'cache'
        selection = Path(self.temp_case.name) / 'selection.toml'
        env = dict(os.environ, FES_ARTIFACT_CACHE_ROOT=str(cache), PYTHONDONTWRITEBYTECODE='1')
        return subprocess.run([os.sys.executable, '-c', RESOLVE, str(self.root), str(selection), mode],
                              cwd=ROOT.parents[1], env=env, text=True, capture_output=True)

    def assertNoFreshLockstepRecorded(self):
        self.assertFalse(any((self.store / evidence.FRESH_LOCKSTEP_STORE).glob('*')),
                         'a refused fresh lockstep must leave no record')

    def test_an_evidence_only_change_keeps_the_design_id_and_moves_only_the_evidence_id(self):
        selected = json.loads(self.change(self.TESTBENCH))
        self.assertEqual(selected['identities']['design'], self.fields['identities']['design'])
        self.assertNotEqual(selected['identities']['evidence'], self.fields['identities']['evidence'])
        self.assertEqual(evidence.evidence_drift(self.fields, selected), [self.TESTBENCH])

    def test_oracle_source_list_drift_reaches_fresh_lockstep_gate(self):
        selected = self.change('cores/fes-common/sim/i386/oracle_sources.toml')
        status = self.gate(selected)
        self.assertEqual(status['reuse'], 'needs-fresh-lockstep')
        self.assertIn('cores/fes-common/sim/i386/oracle_sources.toml', status['evidence_drift'])

    def test_provisioner_drift_reaches_fresh_lockstep_gate(self):
        selected = self.change('scripts/fes_i386_oracle.py')
        status = self.gate(selected)
        self.assertEqual(status['reuse'], 'needs-fresh-lockstep')
        self.assertIn('scripts/fes_i386_oracle.py', status['evidence_drift'])

    def test_a_design_input_change_refuses_reuse_and_the_resolver_rebuilds(self):
        evidence.publish_local(self.root, self.store, self.package, self.record_path, self.ev)
        selected = self.change(self.TESTBENCH, 'cores/fes-common/rtl/i386/fes_i386_cpu.sv')
        status = self.gate(selected)
        self.assertEqual(status['reuse'], 'refused')
        self.assertIn('design identity differs', status['reason'])
        result = self.resolve()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cache miss: the resolver would rebuild', result.stderr,
                      'a design change is a cache miss: the resolver builds instead of reusing')

    def test_a_firmware_generator_change_refuses_reuse_and_the_resolver_rebuilds(self):
        evidence.publish_local(self.root, self.store, self.package, self.record_path, self.ev)
        selected = self.change(self.TESTBENCH, 'scripts/build_pc386_firmware.py')
        self.assertEqual(json.loads(selected)['identities']['design'], self.fields['identities']['design'])
        status = self.gate(selected)
        self.assertEqual(status['reuse'], 'refused')
        self.assertIn('scripts/build_pc386_firmware.py (firmware-generator)', status['reason'])
        result = self.resolve()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('not reused', result.stderr)
        self.assertIn('cache miss: the resolver would rebuild', result.stderr)

    def test_an_evidence_only_change_without_a_fresh_lockstep_refuses_reuse(self):
        evidence.publish_local(self.root, self.store, self.package, self.record_path, self.ev)
        selected = self.change(self.TESTBENCH)
        self.assertEqual(self.gate(selected), {'reuse': 'needs-fresh-lockstep', 'evidence_drift': [self.TESTBENCH]})
        result = self.resolve('lockstep-fails')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('fresh full lockstep failed', result.stderr)
        self.assertNotIn('cache miss: the resolver would rebuild', result.stderr,
                         'a failed lockstep refuses outright: no fallback to the old package or a rebuild')
        self.assertNoFreshLockstepRecorded()

    def test_a_failed_or_partial_fresh_lockstep_refuses_reuse(self):
        selected = self.change(self.TESTBENCH)
        report = self.fresh_report()
        stale = copy.deepcopy(json.loads(report))
        stale['oracle']['suite_sha256'] = self.report['oracle']['suite_sha256']
        for case, data, refusal in (
                ('failed run', self.fresh_report(passed=False), 'did not pass'),
                ('incomplete corpus', self.fresh_report(complete_corpus=False), 'complete corpus'),
                ('not the default suite', self.fresh_report(default_suite=False), 'not the default suite'),
                ('the sealed suite, not the current one', json.dumps(stale).encode(), 'oracle identity')):
            with self.subTest(case=case):
                with self.assertRaisesRegex(ValueError, refusal):
                    self.attest(selected, data)
                self.assertNoFreshLockstepRecorded()
                self.assertEqual(self.gate(selected)['reuse'], 'needs-fresh-lockstep')

    def test_a_lockstep_runner_that_fails_records_nothing(self):
        selected = self.change(self.TESTBENCH)
        with mock.patch.object(seal, 'authenticate_product_tools', return_value={'yosys': self.tool}), \
                mock.patch.object(seal.pc386_owned_job, 'run', return_value=1) as run, self.no_quartus():
            with self.assertRaisesRegex(ValueError, r'fresh full lockstep failed \(exit 1\)'):
                evidence.run_fresh_lockstep(self.root, self.package, self.record_path, self.ev, selected,
                                            self.store, cache_root=None)
        command = run.call_args.args[0]
        self.assertEqual(command[1], str(self.root / evidence.LOCKSTEP_RUNNER))
        self.assertIn('--netlist', command)
        self.assertNoFreshLockstepRecorded()

    def test_a_fresh_lockstep_of_another_netlist_refuses_reuse(self):
        selected = self.change(self.TESTBENCH)
        other = b'// another netlist\n'
        report = json.loads(self.fresh_report())
        report['netlist']['netlist_sha256'] = sha(other)
        with self.regenerate(other), self.assertRaisesRegex(ValueError, 'did not test the sealed CPU netlist'):
            evidence.attest_fresh_lockstep(self.root, self.package, self.record_path, self.ev, selected,
                                           json.dumps(report).encode(), self.tool, self.store)
        self.assertNoFreshLockstepRecorded()

    def test_an_evidence_only_change_with_a_passing_fresh_lockstep_reuses_without_quartus(self):
        evidence.publish_local(self.root, self.store, self.package, self.record_path, self.ev)
        selected = self.change(self.TESTBENCH)
        status = self.attest(selected, self.fresh_report())
        self.assertEqual(status['reuse'], 'fresh-lockstep')
        self.assertEqual(self.gate(selected), status)
        result = self.resolve()
        self.assertEqual(result.returncode, 0, result.stderr)
        chosen = json.loads(result.stdout)
        self.assertEqual(chosen['package'], self.package_id, 'the sealed package bytes are reused unchanged')
        self.assertEqual(chosen['source']['identity_policy'], input_identity.DESIGN_V2)
        self.assertEqual(chosen['source']['pc386_evidence_drift'], [self.TESTBENCH])
        self.assertEqual(chosen['source']['pc386_fresh_lockstep_sha256'], status['fresh_lockstep_sha256'])
        self.assertNotEqual(chosen['source']['selected_revision'], chosen['source']['original_revision'])

    def test_a_fresh_lockstep_from_an_earlier_evidence_state_does_not_carry_over(self):
        evidence.publish_local(self.root, self.store, self.package, self.record_path, self.ev)
        first = self.change(self.TESTBENCH)
        recorded = self.store / evidence.FRESH_LOCKSTEP_STORE / evidence._fresh_key(self.fields, json.loads(first))
        self.attest(first, self.fresh_report())
        later = self.change(self.SUITE)
        self.assertEqual(self.gate(later)['reuse'], 'needs-fresh-lockstep')
        # The earlier record moved under the later evidence state's name.
        moved = self.store / evidence.FRESH_LOCKSTEP_STORE / evidence._fresh_key(self.fields, json.loads(later))
        shutil.copytree(recorded, moved)
        with self.assertRaisesRegex(ValueError, 'not valid for the selected tree|does not bind'):
            self.gate(later)
        result = self.resolve('lockstep-fails')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('pc386 evidence-only reuse refused', result.stderr)
        self.assertNotIn('cache miss: the resolver would rebuild', result.stderr)

    def test_a_fresh_lockstep_record_for_another_package_refuses(self):
        selected = self.change(self.TESTBENCH)
        self.attest(selected, self.fresh_report())
        recorded = self.store / evidence.FRESH_LOCKSTEP_STORE / evidence._fresh_key(self.fields, json.loads(selected))
        receipt = json.loads((recorded / 'receipt.json').read_bytes())
        receipt['package_id'] = 'f' * 64
        recorded.chmod(0o755)
        (recorded / 'receipt.json').chmod(0o644)
        (recorded / 'receipt.json').write_bytes(json.dumps(receipt, sort_keys=True, separators=(',', ':')).encode() + b'\n')
        (recorded / 'receipt.json').chmod(0o444)
        recorded.chmod(0o555)
        with self.assertRaisesRegex(ValueError, 'does not bind this package'):
            self.gate(selected)

    def test_historical_sources_follow_the_original_records_list_not_todays(self):
        added = ('scripts/added_after_the_seal.py',)
        with mock.patch.object(seal, 'SUITE_SOURCES', seal.SUITE_SOURCES + added), \
                mock.patch.object(seal.yosys_netlist, 'CPU_SOURCES', seal.yosys_netlist.CPU_SOURCES + added), \
                tempfile.TemporaryDirectory() as written:
            evidence._historical_sources(self.root, self.fields, Path(written))
            files = {p.relative_to(written).as_posix() for p in Path(written).rglob('*') if p.is_file()}
        self.assertEqual(files, set(self.fields['source_inputs']))
        fields = copy.deepcopy(self.fields)
        fields['source_inputs'][self.TESTBENCH] = '0' * 64
        with tempfile.TemporaryDirectory() as written, \
                self.assertRaisesRegex(ValueError, 'unavailable or changed: ' + self.TESTBENCH):
            evidence._historical_sources(self.root, fields, Path(written))
