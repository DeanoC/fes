"""Real traced child reads and versioned closure regression; no FPGA compiler."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts import compiler_read_audit as audit, export_core_package as exporter
from tests import test_export_core_package as fixtures


class ClosurePolicyTests(unittest.TestCase):
    setUp = fixtures.ExportCorePackageTests.setUp
    tearDown = fixtures.ExportCorePackageTests.tearDown
    functional_fields = fixtures.ExportCorePackageTests.functional_fields

    def git(self, *args):
        return fixtures._run('git', *args, cwd=self.repo)

    def current(self, pinned=None):
        fields = dict(self.record_fields, dependencies={}, revision=self.git('rev-parse', 'HEAD'))
        return exporter.functional_record_fields(self.repo, fields, ['abi', 'scripts'], {'gpu_device': 0},
                 pinned_inputs=pinned or ['scripts/build.py', 'abi/fes-gp.json'])

    def commit(self):
        self.git('add', '-A'); self.git('commit', '-qm', 'edit')

    def test_markdown_add_edit_delete_stable_data_and_mode_changes_invalidate(self):
        before = exporter.build_identity(exporter.encode_build_record(self.current()))
        doc = self.repo / 'scripts/README.md'
        for operation in ('add', 'edit', 'delete'):
            if operation == 'delete': doc.unlink()
            else: doc.write_text(operation)
            self.commit()
            fields = self.current()
            self.assertEqual(before, exporter.build_identity(exporter.encode_build_record(fields)))
            exporter.verify_record_source_at_revision(self.repo, exporter.encode_build_record(fields))
        for relative in ('scripts/docs/data.hex', 'scripts/new-helper.py', 'abi/top.v', 'scripts/name.md/actual.v'):
            path = self.repo / relative; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('data')
            self.commit()
            self.assertNotEqual(before, exporter.build_identity(exporter.encode_build_record(self.current())))
        doc.write_text('executable data'); self.commit()
        noexec = exporter.build_identity(exporter.encode_build_record(self.current()))
        doc.chmod(0o755)
        self.assertNotEqual(noexec, exporter.build_identity(exporter.encode_build_record(self.current())))
        self.commit()
        exporter.verify_record_source_at_revision(self.repo, exporter.encode_build_record(self.current()))

    def test_legacy_v2_history_retains_documentation_and_unknown_policy_rejected(self):
        doc = self.repo / 'scripts/README.md'; doc.write_text('original'); self.commit()
        legacy = self.functional_fields(); legacy['dependencies'] = {}; legacy['revision'] = self.git('rev-parse', 'HEAD')
        record = exporter.encode_build_record(legacy)
        doc.write_text('new'); self.commit()
        exporter.verify_record_source_at_revision(self.repo, record)
        new = self.functional_fields(); new['revision'] = self.git('rev-parse', 'HEAD')
        self.assertNotEqual(exporter.build_identity(record), exporter.build_identity(exporter.encode_build_record(new)))
        new['parameters'] = dict(new['parameters'], source_closure_policy='unknown')
        with self.assertRaisesRegex(ValueError, 'unknown source closure policy'):
            exporter.encode_build_record(new)

    def test_explicit_markdown_input_and_symlink_rejected(self):
        doc = self.repo / 'scripts/README.md'; doc.write_text('input'); self.commit()
        with self.assertRaisesRegex(ValueError, 'explicit input excluded'):
            self.current(['scripts/README.md'])
        fields = self.current(); fields['recipe'] = 'scripts/README.md'
        with self.assertRaisesRegex(ValueError, 'recipe and ABI'):
            exporter.encode_build_record(fields)
        doc.unlink(); doc.symlink_to('build.py')
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.current()


@unittest.skipUnless(audit.TRACER.is_file(), 'strace required for actual read auditing')
class ReadAuditTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def run_child(self, code, *args):
        return audit.audited_run([sys.executable, '-c', code, *map(str, args)], source_root=self.root,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)

    def test_child_relative_dirfd_and_escaped_markdown_reads_rejected(self):
        for name in ('README.md', 'O_WRONLY.md', 'angle>quote"back\\slash.md', 'newline\n.markdown'):
            path = self.root / name; path.write_text('input')
            with self.subTest(name=name), self.assertRaises(audit.ReadAuditError):
                self.run_child('import os,sys; d=os.open(sys.argv[1],os.O_RDONLY); os.open(sys.argv[2],os.O_RDONLY,dir_fd=d)', self.root, name)
        with self.assertRaises(audit.ReadAuditError):
            self.run_child('import subprocess,sys; subprocess.run(["/bin/cat",sys.argv[1]],check=True)', self.root / 'README.md')

    def test_data_read_and_write_only_markdown_allowed(self):
        data = self.root / 'data.hex'; data.write_text('input')
        result = self.run_child('from pathlib import Path;import sys;Path(sys.argv[1]).read_bytes();Path(sys.argv[2]).write_text("output")', data, self.root / 'output.md')
        self.assertEqual(result.returncode, 0)

    def test_python_helper_guard_scoped_and_fd_read_checked(self):
        doc = self.root / 'README.md'; doc.write_text('input')
        with audit.python_source_guard(self.root, ['scripts']):
            with self.assertRaises(audit.ReadAuditError): doc.read_text()
            with self.assertRaises(audit.ReadAuditError):
                with doc.open('r'): pass
            doc.write_text('permitted write')
        self.assertEqual(doc.read_text(), 'permitted write')
        fd = os.open(doc, os.O_RDONLY)
        try:
            with audit.python_source_guard(self.root, ['scripts']):
                with self.assertRaises(audit.ReadAuditError): os.fdopen(fd, closefd=False)
        finally: os.close(fd)

    def test_trace_malformed_missing_size_and_bypass_fail_closed(self):
        with self.assertRaises(audit.ReadAuditError): audit.verify_traces(self.root, self.root)
        trace = self.root / 'open.1'
        for line in ('openat( <unfinished ...>', 'openat(AT_FDCWD, "x", O_RDONLY) = 3',
                     'io_uring_setup(1, {}) = 3<anon_inode>',
                     'open_by_handle_at(1, {}, O_RDONLY) = 3</tmp/x>'):
            trace.write_text(line)
            with self.assertRaises(audit.ReadAuditError): audit.verify_traces(self.root, self.root)
        trace.write_text('x' * 20)
        with patch.object(audit, 'MAX_TRACE_FILE', 10):
            with self.assertRaises(audit.ReadAuditError): audit.verify_traces(self.root, self.root)

    def test_placement_search_does_not_swallow_forbidden_read(self):
        from scripts.search_placer_qor import _run_nextpnr
        doc = self.root / 'README.md'; doc.write_text('input')
        tool = self.root / 'fake-nextpnr'
        tool.write_text('#!/bin/sh\ncat "' + str(doc) + '"\n'); tool.chmod(0o755)
        with self.assertRaises(audit.ReadAuditError):
            _run_nextpnr(tool, self.root / 'fixture', self.root / 'out', device='fixture',
                         qsf=self.root / 'qsf', sdc=None, freq=None, seed=1, weight=1,
                         critexp=1, extra=(), timeout=10, audit_source_root=self.root)

    def test_timeout_kills_tracees_and_preserves_only_benign_timeout(self):
        pidfile = self.root / 'pid'
        doc = self.root / 'README.md'; doc.write_text('input')
        for forbidden in (False, True):
            code = ('import os,time;from pathlib import Path;Path(' + repr(str(pidfile)) + ').write_text(str(os.getpid()));' +
                    ('Path(' + repr(str(doc)) + ').read_text();' if forbidden else '') + 'time.sleep(30)')
            with self.assertRaises(audit.ReadAuditError if forbidden else subprocess.TimeoutExpired):
                audit.audited_run([sys.executable, '-c', code], source_root=self.root,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=0.3, check=False)
            pid = int(pidfile.read_text())
            with self.assertRaises(ProcessLookupError): os.kill(pid, 0)

    def test_python_guard_propagates_to_parallel_search_workers(self):
        from scripts.search_placer_qor import evaluate_pairs
        doc = self.root / 'README.md'; doc.write_text('input')
        with audit.python_source_guard(self.root, ['scripts']):
            with self.assertRaises(audit.ReadAuditError):
                evaluate_pairs([(1, 1), (2, 1)], lambda seed, weight: doc.read_text(), 2)
        self.assertEqual(doc.read_text(), 'input')

    def test_trace_completion_and_deleted_markdown_fail_closed(self):
        trace = self.root / 'open.1'
        for data in ('', 'openat(AT_FDCWD, "x", O_RDONLY) = 3</tmp/x>\n',
                     'openat(AT_FDCWD, "x", O_RDONLY) = 3<' + str(self.root / 'gone.md') + ' (deleted)>\n+++ exited with 0 +++\n'):
            trace.write_text(data)
            with self.assertRaises(audit.ReadAuditError):
                audit.verify_traces(self.root, self.root)
