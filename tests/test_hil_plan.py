import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/hil_plan.py'
FIXTURES = ROOT / 'tests/fixtures/hil_plan'


def run(*args, cwd=None):
    return subprocess.run([sys.executable, str(SCRIPT), *map(str, args)],
                          cwd=cwd, text=True, capture_output=True)


class HilPlanTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)

    def classify_paths(self, text):
        source = self.work / 'paths.txt'
        source.write_text(text)
        return run('classify', '--paths-file', source)

    def test_real_pr_file_lists_overlay(self):
        expected = {
            'pr569.files': ('mister-runtime', 'kit-go+host'),
            'pr567.files': ('mister-runtime', 'core:atari-st'),
            'pr565.files': ('core:ramtest',),
        }
        for filename, components in expected.items():
            with self.subTest(filename=filename):
                result = run('classify', '--paths-file', FIXTURES / filename, '--json')
                self.assertEqual(result.returncode, 0, result.stderr)
                data = json.loads(result.stdout)
                self.assertEqual(data['class'], 'overlay')
                for component in components:
                    self.assertIn(component, data['components'])

    def test_full_and_docs_only(self):
        for path in ('image/buildroot/board/x/etc/init.d/S42x', 'foo/bar'):
            result = self.classify_paths(path + '\n')
            self.assertIn('DECISION: FULL_IMAGE', result.stdout)
        result = self.classify_paths('docs/guide.md\n')
        self.assertIn('DECISION: NO_DEPLOY_CHANGE', result.stdout)

    def test_manifest_and_evidence_happy_path(self):
        repo = self.git_repo()
        base, head = self.commit_overlay(repo)
        payload = self.work / 'binary'
        payload.write_bytes(b'kit artifact')
        manifest_file = self.work / 'manifest.json'
        result = run('manifest', '--head', head, '--out', manifest_file,
                     f'{payload}=/usr/bin/mister-agent')
        self.assertEqual(result.returncode, 0, result.stderr)
        digest = hashlib.sha256(payload.read_bytes()).hexdigest()
        kit_file = self.work / 'kit.sha256'
        kit_file.write_text(f'{digest}  /usr/bin/mister-agent\n')
        evidence_file = self.work / 'evidence.md'
        result = run('evidence', '--repo', repo, '--base-image-commit', base,
                     '--head', head, '--base-image-sha256', 'a' * 64,
                     '--manifest', manifest_file, '--kit-sha256', kit_file,
                     '--out', evidence_file)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('MATCH', evidence_file.read_text())

    def test_mismatch_and_missing_do_not_write_passing_evidence(self):
        repo = self.git_repo()
        base, head = self.commit_overlay(repo)
        manifest_file, kit_file = self.make_artifact_files(head)
        for contents in ('0' * 64 + '  /usr/bin/mister-agent\n', ''):
            kit_file.write_text(contents)
            evidence_file = self.work / 'evidence.md'
            result = run('evidence', '--repo', repo, '--base-image-commit', base,
                         '--head', head, '--base-image-sha256', 'a' * 64,
                         '--manifest', manifest_file, '--kit-sha256', kit_file,
                         '--out', evidence_file)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(evidence_file.exists())

    def test_short_sha_is_exit_two(self):
        result = run('manifest', '--head', '1234', '--out', self.work / 'm.json',
                     'x=/x')
        self.assertEqual(result.returncode, 2)

    def test_two_tree_diff_counts_base_side_drift(self):
        repo = self.git_repo()
        (repo / 'base.txt').write_text('base\n')
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'base image')
        base = self.git(repo, 'rev-parse', 'HEAD')
        # Main moved after the kit's base image was built: an image input changed.
        (repo / 'image/buildroot').mkdir(parents=True)
        (repo / 'image/buildroot/defconfig').write_text('drift\n')
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'main drift')
        merge_base = self.git(repo, 'rev-parse', 'HEAD')
        (repo / 'sources/libmister-runtime').mkdir(parents=True)
        (repo / 'sources/libmister-runtime/change.cpp').write_text('change\n')
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'PR head')
        head = self.git(repo, 'rev-parse', 'HEAD')
        # merge-base..head alone would say overlay ...
        result = run('classify', '--repo', repo, '--base-image-commit', merge_base,
                     '--head', head, '--json')
        self.assertEqual(json.loads(result.stdout)['class'], 'overlay')
        # ... but the kit's base-image commit..head includes the drift and forces a full image.
        result = run('classify', '--repo', repo, '--base-image-commit', base,
                     '--head', head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'FULL_IMAGE')
        self.assertIn('image/buildroot/defconfig', [row['path'] for row in data['rows']])
        manifest_file, kit_file = self.make_artifact_files(head)
        result = run('evidence', '--repo', repo, '--base-image-commit', base,
                     '--head', head, '--base-image-sha256', 'a' * 64,
                     '--manifest', manifest_file, '--kit-sha256', kit_file)
        self.assertEqual(result.returncode, 1)
        self.assertIn('full image', result.stderr)

    def test_rename_out_of_image_reports_old_path(self):
        repo = self.git_repo()
        (repo / 'image').mkdir()
        (repo / 'image/S50agent').write_text('init script\n' * 20)
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'base image')
        base = self.git(repo, 'rev-parse', 'HEAD')
        (repo / 'sources/libmister-runtime').mkdir(parents=True)
        self.git(repo, 'mv', 'image/S50agent', 'sources/libmister-runtime/S50agent')
        self.git(repo, 'commit', '-m', 'move')
        head = self.git(repo, 'rev-parse', 'HEAD')
        result = run('classify', '--repo', repo, '--base-image-commit', base,
                     '--head', head, '--json')
        data = json.loads(result.stdout)
        self.assertIn('image/S50agent', [row['path'] for row in data['rows']])
        self.assertEqual(data['decision'], 'FULL_IMAGE')

    def test_empty_duplicate_and_extra_kit_entries_fail(self):
        repo = self.git_repo()
        base, head = self.commit_overlay(repo)
        manifest_file, kit_file = self.make_artifact_files(head)
        good = kit_file.read_text()
        evidence = lambda: run('evidence', '--repo', repo, '--base-image-commit', base,
                               '--head', head, '--base-image-sha256', 'a' * 64,
                               '--manifest', manifest_file, '--kit-sha256', kit_file)
        kit_file.write_text(good + 'b' * 64 + '  /usr/bin/unexpected\n')
        self.assertNotEqual(evidence().returncode, 0)
        kit_file.write_text(good)
        data = json.loads(manifest_file.read_text())
        manifest_file.write_text(json.dumps({**data, 'entries': data['entries'] * 2}))
        self.assertNotEqual(evidence().returncode, 0)
        manifest_file.write_text(json.dumps({**data, 'entries': []}))
        kit_file.write_text('')
        self.assertNotEqual(evidence().returncode, 0)
        manifest_file.write_text(json.dumps({**data, 'head': base}))
        kit_file.write_text(good)
        self.assertNotEqual(evidence().returncode, 0)

    def make_artifact_files(self, head):
        payload = self.work / 'binary'
        payload.write_bytes(b'kit artifact')
        manifest_file = self.work / 'manifest.json'
        result = run('manifest', '--head', head, '--out', manifest_file,
                     f'{payload}=/usr/bin/mister-agent')
        self.assertEqual(result.returncode, 0, result.stderr)
        kit_file = self.work / 'kit.sha256'
        kit_file.write_text('')
        return manifest_file, kit_file

    def git_repo(self):
        repo = self.work / 'repo'
        repo.mkdir()
        self.git(repo, 'init', '-q')
        self.git(repo, 'config', 'user.email', 'test@example.com')
        self.git(repo, 'config', 'user.name', 'Test')
        return repo

    @staticmethod
    def git(repo, *args):
        return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()

    def commit_overlay(self, repo):
        (repo / 'docs').mkdir()
        (repo / 'docs/base.md').write_text('base\n')
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'base')
        base = self.git(repo, 'rev-parse', 'HEAD')
        (repo / 'sources/libmister-runtime').mkdir(parents=True)
        (repo / 'sources/libmister-runtime/change.cpp').write_text('overlay\n')
        self.git(repo, 'add', '.')
        self.git(repo, 'commit', '-m', 'overlay')
        return base, self.git(repo, 'rev-parse', 'HEAD')


if __name__ == '__main__':
    unittest.main()
