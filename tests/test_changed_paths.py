"""Large CI selections cross file boundaries without changing test selection."""
import argparse
import contextlib
import errno
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from scripts import changed_paths, runtime_tests

ROOT = Path(__file__).resolve().parents[1]


class ChangedPathsTests(unittest.TestCase):
    def parser(self):
        parser = argparse.ArgumentParser()
        changed_paths.add_arguments(parser, help='test paths')
        return parser

    def test_inline_file_and_default_selections_are_identical(self):
        paths = ['sources/libmister-runtime/src/runtime.cpp', 'docs/odd "name".md', 'docs/line\nbreak.md']
        parser = self.parser()
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'paths.json'
            path.write_text(json.dumps(paths))
            self.assertIsNone(changed_paths.read(parser.parse_args([])))
            self.assertEqual(changed_paths.read(parser.parse_args(['--paths-json', json.dumps(paths)])), paths)
            self.assertEqual(changed_paths.read(parser.parse_args(['--paths-json-file', str(path)])), paths)

    def test_malformed_or_missing_file_never_becomes_a_default_selection(self):
        parser = self.parser()
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'paths.json'
            for value in ('null', '{}', '[1]', '["unfinished"'):
                path.write_text(value)
                for flag, payload in (('--paths-json', value), ('--paths-json-file', str(path))):
                    with self.subTest(value=value, flag=flag), self.assertRaises(ValueError):
                        changed_paths.read(parser.parse_args([flag, payload]))
            with self.assertRaises(OSError):
                changed_paths.read(parser.parse_args(['--paths-json-file', str(path) + '-missing']))
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                parser.parse_args(['--paths-json', '[]', '--paths-json-file', str(path)])

    @unittest.skipUnless(sys.platform.startswith('linux'), 'Linux exec string limit regression')
    def test_selection_larger_than_exec_limit_runs_from_file(self):
        paths = ['sources/libmister-runtime/tests/unit/' + str(n) + '-' + 'x' * 90 + '.cpp'
                 for n in range(2500)]
        payload = json.dumps(paths)
        self.assertGreater(len(payload.encode()), 131072)
        # Reproduce the Actions failure before bash can execute any test.
        with self.assertRaises(OSError) as failure:
            subprocess.run(['bash', '-c', 'true'], env={**os.environ, 'CHANGED_PATHS': payload})
        self.assertEqual(failure.exception.errno, errno.E2BIG)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'paths.json'; path.write_text(payload)
            result = subprocess.run([sys.executable, 'scripts/runtime_tests.py', '--plan-only',
                '--paths-json-file', str(path)], cwd=ROOT, capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)['command'], runtime_tests.command(paths))

    def test_workflow_transports_the_exact_selection_as_an_attempt_bound_artifact(self):
        workflow = (ROOT / '.github/workflows/check.yml').read_text()
        self.assertNotIn('CHANGED_PATHS:', workflow)
        self.assertNotIn('needs.plan.outputs.paths }}', workflow)
        self.assertIn("json.dump(plan['paths'], output)", workflow)
        self.assertIn('            /tmp/changed-paths.json', workflow)
        self.assertIn('paths_artifact: ${{ steps.plan.outputs.paths_artifact }}', workflow)
        self.assertEqual(workflow.count('name: ${{ needs.plan.outputs.paths_artifact }}'), 4)
        for job, following in (('parent-tests', 'consistency'), ('consistency', 'host'),
                               ('host', 'runtime'), ('runtime', 'contracts')):
            section = workflow.split('  ' + job + ':\n', 1)[1].split('  ' + following + ':\n', 1)[0]
            self.assertIn('uses: actions/download-artifact@', section)
            self.assertIn('--paths-json-file "$RUNNER_TEMP/fes-ci-plan/changed-paths.json"', section)
