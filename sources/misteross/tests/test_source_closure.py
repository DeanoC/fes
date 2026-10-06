"""Host-only closure, key, and read-audit checks."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from scripts.compiler_read_audit import ReadAuditError, python_source_guard, verify_traces
from scripts.functional_execution import AuditedRoots, source_roots_for_inputs, source_roots_for_producer
from scripts.source_closure import ROOT, check, derive, imports, load_manifest, minimize, static, main
from scripts import export_core_package as exporter
from tests import test_export_core_package as fixtures


class ManifestTests(unittest.TestCase):
    def test_parse_and_static(self):
        manifest = load_manifest()
        self.assertIn('build_fes_pong', manifest)
        self.assertEqual(manifest['build_fes_pong'], sorted(set(manifest['build_fes_pong'])))
        roots = static('build_fes_pong')
        for expected in ('scripts/build_fes_pong.py', 'scripts/functional_execution.py',
                         'toolchain.lock', 'cores/fes-pong/generated'):
            self.assertTrue(any(expected == root or expected.startswith(root + '/') for root in roots))
        self.assertFalse(any('/sim/' in root or root.endswith('/sim') for root in roots))
        self.assertFalse(any(root.startswith('cores/fes-common/sim') for root in roots))

    def test_manifest_rejects_unsorted_or_escaping_roots(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'manifest.json'
            for roots in (['scripts/z.py', 'scripts/a.py'], ['../outside']):
                path.write_text(json.dumps({'build_fes_pong':roots}))
                with self.assertRaises(ValueError): load_manifest(path)

    def test_record_command_passes_explicit_record_mode(self):
        with tempfile.TemporaryDirectory() as temp, patch('sys.argv', [
            'source_closure.py', 'record', '--producer', 'build_fes_pong',
            '--record', str(Path(temp)/'reads.jsonl'), '--record-only'
        ]), patch('scripts.source_closure.subprocess.run') as launched:
            main()
            environment = launched.call_args.kwargs['env']
            self.assertEqual(environment['FES_SOURCE_CLOSURE_RECORD_ONLY'], '1')
            self.assertEqual(environment['FES_SOURCE_READ_RECORD'], str(Path(temp)/'reads.jsonl'))

    def test_fallback_and_manifest_mode(self):
        pins = ['scripts/build_fes_pong.py', 'cores/fes-pong/rtl/top.v']
        self.assertEqual(source_roots_for_producer('missing_producer', pins), source_roots_for_inputs(pins))
        self.assertIs(type(source_roots_for_producer('missing_producer', pins)), list)
        self.assertIsInstance(source_roots_for_producer('build_fes_pong', pins), AuditedRoots)
        with patch.dict(os.environ, {'FES_SOURCE_CLOSURE_BROAD': '1'}):
            self.assertEqual(source_roots_for_producer('build_fes_pong', pins), source_roots_for_inputs(pins))

    def test_declared_inputs_stay_in_audited_roots(self):
        from scripts.build_fes_coleco_socket_v2 import VIDEO_INPUTS, NATIVE_VIDEO_INPUTS
        for inputs in (VIDEO_INPUTS, NATIVE_VIDEO_INPUTS):
            roots = source_roots_for_producer('build_fes_coleco_socket_v2', inputs)
            self.assertIsInstance(roots, AuditedRoots)
            for path in inputs:
                self.assertTrue(any(path == r or path.startswith(r + '/') for r in roots), path)
        roots = source_roots_for_producer('build_fes_coleco_socket_v2', VIDEO_INPUTS)
        self.assertIn('cores/fes-common/generated/fes_video_part.vh', roots)
        self.assertEqual(roots, sorted(roots))

    def test_minimize_and_derive(self):
        files = {'scripts/a.py', 'scripts/b.py', 'scripts/other/c.py'}
        self.assertEqual(minimize({'scripts/a.py'}, set(), files), ['scripts/a.py'])
        self.assertEqual(minimize({'scripts/a.py', 'scripts/b.py'}, set(), files), ['scripts/a.py', 'scripts/b.py'])
        self.assertEqual(minimize({'scripts/a.py', 'scripts/b.py'}, {'scripts'}, files), ['scripts'])
        with tempfile.TemporaryDirectory() as temp:
            record = Path(temp) / 'reads.jsonl'
            record.write_text(json.dumps({'event':'read','path':'scripts/build_fes_pong.py'})+'\n')
            roots = derive('build_fes_pong', [record])
            self.assertIn('toolchain.lock', roots)

    def test_check_rejects_missing_import_and_sim(self):
        roots = static('build_fes_pong')
        missing = [root for root in roots if root != 'scripts/functional_execution.py']
        with self.assertRaisesRegex(ValueError, 'missing required source'):
            check(manifest={'build_fes_pong': missing})
        with self.assertRaisesRegex(ValueError, 'forbidden simulation'):
            check(manifest={'build_fes_pong': sorted(roots + ['cores/fes-common/sim'])})


class GuardTests(unittest.TestCase):
    def test_python_read_and_listing(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); (root/'in').mkdir(); (root/'out').mkdir(); (root/'build').mkdir()
            (root/'in/a.v').write_text('a'); (root/'out/b.v').write_text('b')
            (root/'out/__pycache__').mkdir()
            (root/'out/b.py').write_text('pass')
            (root/'out/__pycache__/b.cpython-314.pyc').write_bytes(b'bytecode')
            with python_source_guard(root, AuditedRoots(['in'])):
                self.assertEqual((root/'in/a.v').read_text(), 'a')
                self.assertEqual(os.listdir(root/'in'), ['a.v'])
                with self.assertRaises(ReadAuditError): (root/'out/b.v').read_text()
                with self.assertRaises(ReadAuditError): os.listdir(root/'out')
                with self.assertRaises(ReadAuditError):
                    (root/'out/__pycache__/b.cpython-314.pyc').read_bytes()
                (root/'build/output').write_text('x')
                self.assertEqual((root/'build/output').read_text(), 'x')

    def test_compiler_trace_open(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp); (root/'in').mkdir(); (root/'out').mkdir(); (root/'trace').mkdir()
            (root/'in/a.v').write_text('a'); (root/'out/b.v').write_text('b')
            trace=root/'trace/open.1'
            def write(path):
                trace.write_text(f'openat(AT_FDCWD, "x", O_RDONLY) = 3<{path}>\n+++ exited with 0 +++\n')
            write(root/'in/a.v'); verify_traces(root/'trace', root, ['in'])
            write(root/'out/b.v')
            with self.assertRaisesRegex(ReadAuditError, 'outside audited closure'):
                verify_traces(root/'trace', root, ['in'])

    def test_compiler_listing_and_record_only(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); (root/'out').mkdir(); (root/'trace').mkdir()
            trace = root/'trace/open.1'
            trace.write_text(f'getdents64(3<{root}/out>, /* 2 entries */, 32768) = 48\n+++ exited with 0 +++\n')
            with self.assertRaisesRegex(ReadAuditError, 'outside audited closure'):
                verify_traces(root/'trace', root, ['in'])
            record = root/'reads.jsonl'
            with patch.dict(os.environ, {'FES_SOURCE_READ_RECORD':str(record),
                                          'FES_SOURCE_CLOSURE_RECORD_ONLY':'1'}):
                verify_traces(root/'trace', root, ['in'])
            self.assertEqual(json.loads(record.read_text().splitlines()[0]),
                             {'event':'list', 'path':'out'})


class RecordTests(unittest.TestCase):
    setUp = fixtures.ExportCorePackageTests.setUp
    tearDown = fixtures.ExportCorePackageTests.tearDown

    def git(self, *args):
        return fixtures._run('git', *args, cwd=self.repo)

    def test_audited_fields_and_narrowed_bytes(self):
        fields = dict(self.record_fields, dependencies={}, revision=self.revision)
        broad = exporter.functional_record_fields(self.repo, fields, ['abi', 'scripts'], {'gpu_device':0},
                                                    pinned_inputs=['scripts/build.py','abi/fes-gp.json'])
        fallback = exporter.functional_record_fields(self.repo, fields,
            source_roots_for_producer('missing_producer', ['scripts/build.py','abi/fes-gp.json']),
            {'gpu_device':0}, pinned_inputs=['scripts/build.py','abi/fes-gp.json'])
        self.assertEqual(exporter.encode_build_record(broad), exporter.encode_build_record(fallback))
        narrow = exporter.functional_record_fields(self.repo, fields,
            AuditedRoots(['abi/fes-gp.json', 'scripts/build.py']), {'gpu_device':0},
            pinned_inputs=['scripts/build.py','abi/fes-gp.json'])
        self.assertEqual(narrow['parameters']['source_closure_mode'], 'audited-v1')
        before = narrow['source_inputs']
        (self.repo/'scripts/unrelated.py').write_text('x')
        self.git('add','-A'); self.git('commit','-qm','unrelated')
        later = exporter.functional_record_fields(self.repo, fields,
            AuditedRoots(['abi/fes-gp.json', 'scripts/build.py']), {'gpu_device':0},
            pinned_inputs=['scripts/build.py','abi/fes-gp.json'])
        self.assertEqual(before, later['source_inputs'])
        (self.repo/'scripts/build.py').write_text('changed')
        self.git('add','-A'); self.git('commit','-qm','input')
        changed = exporter.functional_record_fields(self.repo, fields,
            AuditedRoots(['abi/fes-gp.json', 'scripts/build.py']), {'gpu_device':0},
            pinned_inputs=['scripts/build.py','abi/fes-gp.json'])
        self.assertNotEqual(before, changed['source_inputs'])

    def test_helper_and_lock_invalidate_but_unrelated_shared_sim_does_not(self):
        (self.repo/'scripts/helper.py').write_text('one')
        (self.repo/'toolchain.lock').write_text('one')
        sim = self.repo/'cores/fes-common/sim/unused.cpp'
        sim.parent.mkdir(parents=True); sim.write_text('one')
        self.git('add','-A'); self.git('commit','-qm','closure fixture')
        fields = dict(self.record_fields, dependencies={}, revision=self.git('rev-parse','HEAD'))
        roots = AuditedRoots(['abi/fes-gp.json', 'scripts/build.py', 'scripts/helper.py', 'toolchain.lock'])
        def inputs():
            return exporter.functional_record_fields(self.repo, fields, roots, {'gpu_device':0},
                       pinned_inputs=['scripts/build.py','abi/fes-gp.json'])['source_inputs']
        before = inputs()
        sim.write_text('two'); self.git('add','-A'); self.git('commit','-qm','sim')
        self.assertEqual(before, inputs())
        for relative in ('scripts/helper.py', 'toolchain.lock'):
            path = self.repo/relative; path.write_text('two')
            self.git('add','-A'); self.git('commit','-qm','edit')
            changed = inputs()
            self.assertNotEqual(before, changed)
            before = changed
