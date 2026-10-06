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

    def test_resolver_result_survives_producer_stdout(self):
        # Producers inherit the resolver's stdout; only the result may reach it.
        from scripts.core_key_nightly import _resolve
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp) / 'repo'; (repo / 'scripts').mkdir(parents=True)
            (repo / 'scripts/module_sources.py').write_text(
                'def materialize(repo, name, revision, destination):\n    return destination\n')
            (repo / 'scripts/recipes.py').write_text('def recipe_for(core):\n    return core\n')
            (repo / 'scripts/bundle.py').write_text(
                'import subprocess, sys\n'
                'def resolve_core_package(source, revision, selection, force=False, recipe=None):\n'
                '    print("python producer noise")\n'
                '    subprocess.run(["echo", "child producer noise"], check=True)\n'
                '    return {"inputs": {"core_rbf_sha256": "a" * 64,\n'
                '            "source_selection": {"functional_inputs_sha256": "b" * 64}}}\n')
            subprocess.run(['git', 'init', '-q', str(repo)], check=True)
            subprocess.run(['git', '-C', str(repo), '-c', 'user.name=t', '-c', 'user.email=t@t',
                            'commit', '-q', '--allow-empty', '-m', 'x'], check=True)
            env = dict(os.environ, PYTHONPATH=str(repo))
            self.assertEqual(_resolve(repo, 'fes.pong', Path(temp) / 'sel.toml', env, False),
                             {'sha256': 'a' * 64, 'key': 'b' * 64})
