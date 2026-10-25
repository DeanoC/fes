import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import textwrap
import unittest

from scripts import runtime_tests

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'sources/libmister-runtime/tests/incremental_build_test.sh'
HEADERS = ('include/libmister-runtime/runtime.h', 'src/native/artifacts.hpp',
           'src/native/hardware.hpp', 'src/native/linux/mmio.hpp',
           'src/daemon/protocol.hpp', 'src/linux/production_hardware.hpp',
           'tests/support/fake_hardware.hpp')


class RuntimeChecks(unittest.TestCase):
    def test_selection_keeps_build_and_uncertain_inputs_exhaustive(self):
        source = 'sources/libmister-runtime/src/native/hardware.cpp'
        for paths, full, target in (
            ([source], False, 'test-routine'),
            ([source, 'docs/development.md'], False, 'test-routine'),
            (['sources/libmister-runtime/tests/unit/input_test.cpp'], False, 'test-routine'),
            ([source], True, 'test'), (None, False, 'test'), ([], False, 'test'),
            (['<new-branch>'], False, 'test'),
            ([source, 'sources/libmister-runtime/Makefile'], False, 'test'),
            ([source, 'sources/libmister-runtime/tests/incremental_build_test.sh'], False, 'test'),
            ([source, 'scripts/runtime_tests.py'], False, 'test'),
            ([source, 'third_party/vendor.hpp'], False, 'test'),
            (['sources/libmister-runtime/src/../Makefile'], False, 'test'),
            ([source, 'unknown'], False, 'test')):
            with self.subTest(paths=paths, full=full):
                self.assertEqual(runtime_tests.command(paths, 3, full)['argv'],
                                 ['make', '-j3', target])

    def test_incremental_build_target_requires_the_functional_suite(self):
        makefile = (ROOT / 'sources/libmister-runtime/Makefile').read_text()
        target = re.search(r'(?m)^incremental-build-test:\s*(.*)$', makefile)
        self.assertIsNotNone(target)
        self.assertIn('run-tests', target.group(1).split())

    def fixture(self, root, dependent=True, stale=False):
        for header in HEADERS:
            path = root / header
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()
            os.utime(path, (100, 100))
        binary = root / 'binary'
        binary.write_text('#!/bin/sh\nexit 0\n')
        binary.chmod(0o755)
        os.utime(binary, (50 if stale else 200, 50 if stale else 200))
        (root / 'Makefile').write_text('binary: ' + (' '.join(HEADERS) if dependent else '') +
                                      '\n\t@echo rebuilt >> mutations\n')

    def test_dependency_query_detects_missing_edges_and_stale_baseline_without_mutation(self):
        for dependent, stale, success in ((True, False, True), (False, False, False),
                                         (True, True, False)):
            with self.subTest(dependent=dependent, stale=stale), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self.fixture(root, dependent, stale)
                before = {p: p.stat().st_mtime_ns for p in root.rglob('*') if p.is_file()}
                result = subprocess.run(['bash', str(SCRIPT), '--check-only', str(root), 'binary'],
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
                self.assertEqual(before, {p: p.stat().st_mtime_ns for p in before})
                self.assertFalse((root / 'mutations').exists())

    def test_real_header_rebuild_preserves_edges_without_rerunning_functional_cases(self):
        for dependent in (True, False):
            with self.subTest(dependent=dependent), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self.fixture(root, dependent)
                executable = '#!/bin/sh\necho ran >> executions\n'
                (root / 'binary').write_text(executable)
                (root / 'binary-source').write_text(executable)
                os.utime(root / 'binary', (200, 200))
                (root / 'Makefile').write_text(
                    'binary: ' + (' '.join(HEADERS) if dependent else '') +
                    '\n\tcp binary-source binary\n\tchmod +x binary\n' +
                    '\t@echo rebuilt >> builds\n' +
                    'run-tests: binary\n\t./binary\n')
                subprocess.run(['make', 'run-tests'], cwd=root, check=True, capture_output=True)
                before = {header: (root / header).stat().st_mtime_ns for header in HEADERS}
                result = subprocess.run(['bash', str(SCRIPT), str(root), 'binary'],
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, dependent, result.stdout + result.stderr)
                self.assertEqual((root / 'executions').read_text().splitlines(), ['ran'])
                self.assertEqual(before, {header: (root / header).stat().st_mtime_ns for header in HEADERS})
                if dependent:
                    self.assertEqual((root / 'builds').read_text().splitlines(), ['rebuilt'])
                else:
                    self.assertIn('did not rebuild test binary', result.stderr)
                    self.assertFalse((root / 'builds').exists())

    def test_workflow_executes_shared_selection_and_propagates_make_failure(self):
        workflow = (ROOT / '.github/workflows/check.yml').read_text()
        runtime = workflow.split('  runtime:\n', 1)[1].split('  contracts:\n', 1)[0]
        script = textwrap.dedent(re.search(r'        run: \|\n(.*)', runtime, re.S).group(1))
        for paths, target in ((['sources/libmister-runtime/src/runtime.cpp'], 'test-routine'),
                              (['sources/libmister-runtime/Makefile'], 'test'),
                              (['<new-branch>'], 'test')):
            for status in (0, 7):
                with self.subTest(paths=paths, status=status), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    make = root / 'make'
                    make.write_text('#!/bin/sh\nprintf "%s\\n" "$*" > "$COMMAND_LOG"\nexit "$MAKE_STATUS"\n')
                    make.chmod(0o755)
                    result = subprocess.run(['bash', '-e', '-c', script], cwd=ROOT,
                        capture_output=True, text=True, env={**os.environ,
                        'PATH': str(root) + os.pathsep + os.environ['PATH'],
                        'CHANGED_PATHS': json.dumps(paths), 'MAKE_STATUS': str(status),
                        'COMMAND_LOG': str(root / 'commands')})
                    self.assertEqual(result.returncode, status)
                    self.assertEqual((root / 'commands').read_text().strip(), '-j2 ' + target)
                    self.assertEqual(json.loads(result.stdout)['status'],
                                     'passed' if status == 0 else 'failed')
