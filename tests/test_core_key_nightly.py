import unittest
from scripts.core_key_nightly import decide, run
from pathlib import Path
import json
import subprocess
import tempfile
import os
import sys


class NightlyDecisionTests(unittest.TestCase):
    def test_private_artifact_cache_keeps_shared_toolchain(self):
        with tempfile.TemporaryDirectory() as temp:
            environment = dict(os.environ, FES_ARTIFACT_CACHE_ROOT=str(Path(temp)/'artifacts'))
            output = subprocess.check_output([sys.executable, '-c',
                'from scripts.recipes import ARTIFACT_CACHE_ROOT, TOOLCHAIN_CACHE_ROOT; '
                'print(ARTIFACT_CACHE_ROOT); print(TOOLCHAIN_CACHE_ROOT)'],
                env=environment, text=True).splitlines()
            self.assertEqual(output[0], str(Path(temp)/'artifacts'))
            self.assertNotIn(temp, output[1])

    def test_same_payload_and_covered_reads_pass(self):
        self.assertTrue(decide({'sha256':'a'}, {'sha256':'a'},
                               [{'event':'read','path':'scripts/a.py'}], ['scripts/a.py'])['pass'])

    def test_different_payload_fails(self):
        self.assertFalse(decide({'sha256':'a'}, {'sha256':'b'}, [], ['scripts/a.py'])['pass'])

    def test_uncovered_read_fails(self):
        result = decide({'sha256':'a'}, {'sha256':'a'},
                        [{'event':'read','path':'scripts/b.py'}], ['scripts/a.py'])
        self.assertFalse(result['pass'])
        self.assertEqual(result['uncovered_paths'], ['scripts/b.py'])

    def test_run_with_fake_resolver(self):
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp) / 'repo'
            subprocess.run(['git', 'init', '-q', str(repo)], check=True)
            for broad_sha, read_path, expected in (
                ('a', 'scripts/a.py', True), ('b', 'scripts/a.py', False),
                ('a', 'scripts/other.py', False)):
                def fake(_repo, _core, _selection, env, force):
                    if force:
                        Path(env['FES_SOURCE_READ_RECORD']).write_text(
                            json.dumps({'event':'read', 'path':read_path}) + '\n')
                    return {'sha256': broad_sha if force else 'a', 'key': 'broad' if force else 'narrow'}
                report = run(repo, Path(temp) / 'reports', [('fes.pong', 'build_fes_pong')],
                             resolver=fake, manifest={'build_fes_pong':['scripts/a.py']})
                self.assertEqual(report['pass'], expected)
