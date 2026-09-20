import subprocess
import tempfile
from pathlib import Path
import unittest

from scripts.affected import changed_paths, plan, LANES, CORES, MODULE_ROOTS


class AffectedTests(unittest.TestCase):
    def test_contracts_and_unknown_changes_close_over_every_lane(self):
        for path in ('sources/mister-packages', 'sources/mister-packages/packages/abi/new.yaml',
                     'image/Makefile', '.github/workflows/check.yml', 'unknown/new.bin'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertTrue(all(result['lanes'].values()))
                self.assertEqual(set(result['cores']), set(CORES))

    def test_module_roots_and_gitlinks(self):
        for module in ('host', 'runtime'):
            for path in (MODULE_ROOTS[module], MODULE_ROOTS[module] + '/src/main.cpp'):
                result = plan([path])
                self.assertEqual({k for k,v in result['lanes'].items() if v}, {'parent', module})

    def test_shared_and_core_rtl_are_conservative(self):
        for path in ('sources/misteross/cores/fes-common/rtl/fes_application_gp.v',
                     'sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv'):
            result = plan([path])
            self.assertTrue(result['lanes']['fpga'])
            self.assertEqual(set(result['cores']), set(CORES))

    def test_docs_only_reports_explicit_skips_and_metadata(self):
        result = plan(['README.md', 'docs/development.md', 'sources/FogCast/docs/ARCHITECTURE.md'])
        self.assertEqual(result['skipped'], list(LANES))
        self.assertTrue(result['always'])
        self.assertIn('hardware acceptance', result['not_run'])
        self.assertTrue(all(plan(['AGENTS.md'])['lanes'].values()))

    def test_real_git_merge_base_deletes_and_renames(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()
            git('init', '-q'); git('config', 'user.name', 'Test'); git('config', 'user.email', 'test@example.invalid')
            path = root/'sources/FogCast/main.go'
            path.parent.mkdir(parents=True); path.write_text('old')
            git('add', '.'); git('commit', '-qm', 'base')
            base = git('rev-parse', 'HEAD')
            git('checkout', '-qb', 'feature')
            path.unlink()
            moved = root/'sources/libmister-runtime/new.cpp'
            moved.parent.mkdir(parents=True); moved.write_text('old')
            git('add', '-A'); git('commit', '-qm', 'move between modules')
            head = git('rev-parse', 'HEAD')
            git('checkout', '-qb', 'base-advanced', base)
            (root/'unrelated.txt').write_text('base only')
            git('add', '.'); git('commit', '-qm', 'base advances')
            paths = changed_paths(root, 'HEAD', head)
            self.assertEqual(paths, ['sources/FogCast/main.go', 'sources/libmister-runtime/new.cpp'])
            result = plan(paths)
            self.assertTrue(result['lanes']['host'] and result['lanes']['runtime'])
            self.assertFalse(result['lanes']['fpga'])


if __name__ == '__main__':
    unittest.main()
