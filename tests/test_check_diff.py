import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import check_diff


class ImportWhitespaceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@example.invalid')
        (self.root / 'original').write_text('preserved upstream bytes  \n')
        self.git('add', '.')
        self.git('commit', '-qm', 'component')
        self.original = self.git('rev-parse', 'HEAD')
        self.tree = self.git('rev-parse', 'HEAD^{tree}')
        self.git('read-tree', '--empty')
        (self.root / 'original').unlink()
        self.git('update-index', '--add', '--cacheinfo', '160000,' + self.original + ',sources/misteross')
        self.git('commit', '-qm', 'parent gitlink')
        self.base = self.git('rev-parse', 'HEAD')
        self.git('update-index', '--force-remove', 'sources/misteross')
        self.git('read-tree', '--prefix=sources/misteross/', '-u', self.original)
        (self.root / 'config').mkdir()
        self.manifest = self.root / 'config/source-imports.toml'
        self.manifest.write_text('[imports.misteross]\n' + '\n'.join(
            key + ' = ' + json.dumps(value) for key, value in {
                'path': 'sources/misteross', 'imported_commit': self.original,
                'prior_gitlink': self.original, 'tree': self.tree}.items()) + '\n')
        self.commit('import')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.DEVNULL).decode().strip()

    def commit(self, message):
        self.git('add', '.')
        self.git('commit', '-qm', message)

    def test_preserved_import_bytes_and_new_branch_pass(self):
        check_diff.check(self.root, self.base)
        check_diff.check(self.root, '0' * 40)

    def test_new_branch_preserves_parent_whitespace_but_checks_new_changes(self):
        (self.root / 'historical.patch').write_text('existing patch bytes  \n')
        self.commit('existing parent patch')
        baseline = self.git('rev-parse', 'HEAD')
        (self.root / 'clean').write_text('new clean content\n')
        self.commit('new branch')
        check_diff.check(self.root, '0' * 40, fallback_base=baseline)
        (self.root / 'new').write_text('new error  \n')
        self.commit('bad new branch content')
        with self.assertRaisesRegex(ValueError, 'trailing whitespace'):
            check_diff.check(self.root, '0' * 40, fallback_base=baseline)

    def test_new_branch_missing_fallback_fails_closed(self):
        with self.assertRaises(subprocess.CalledProcessError):
            check_diff.check(self.root, '0' * 40, fallback_base='refs/remotes/origin/missing')

    def test_new_module_whitespace_fails(self):
        (self.root / 'sources/misteross/added').write_text('new error  \n')
        self.commit('edit module')
        with self.assertRaisesRegex(ValueError, 'trailing whitespace'):
            check_diff.check(self.root, self.base)

    def test_root_whitespace_fails(self):
        (self.root / 'new').write_text('new error  \n')
        self.commit('edit parent')
        with self.assertRaisesRegex(ValueError, 'trailing whitespace'):
            check_diff.check(self.root, self.base)

    def test_tampered_import_tree_fails(self):
        self.manifest.write_text(self.manifest.read_text().replace(self.tree, '0' * 40))
        self.commit('tamper')
        with self.assertRaisesRegex(ValueError, 'import tree'):
            check_diff.check(self.root, self.base)
