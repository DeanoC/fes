import subprocess
import contextlib
import io
import json
import unittest
from unittest.mock import patch
from pathlib import Path

from scripts.check_deadcode import MODULES, U1000
from scripts import check_deadcode

ROOT = Path(__file__).resolve().parent.parent


class CheckDeadcodeTests(unittest.TestCase):
    def test_non_go_parent_inputs_skip_before_tools_or_graph_loading(self):
        for paths in ([], ['scripts/appliance.py'], ['tests/test_receipt.py'],
                      ['image/Makefile', 'profiles/native-integration-dev.toml'],
                      ['sources/misteross/cores/fes-zx81/rtl/zx81.v']):
            with self.subTest(paths=paths), patch.object(check_deadcode, 'install_tools') as install, \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(check_deadcode.main(['--paths-json', json.dumps(paths)]), 0)
                install.assert_not_called()

    def test_go_modules_checker_and_unknown_inputs_run(self):
        for path in ('sources/FogCast/internal/agent/agent.go', 'platform/go.mod',
                     'sources/FogCast/go.sum', 'sources/mister-packages/packages/x.yaml',
                     'sources/misteross/expansion/layout.json', 'sources/FogCast/assets/icon.png',
                     'scripts/new.go', 'Makefile', '.github/workflows/check.yml',
                     'scripts/check_deadcode.py', 'tests/test_check_deadcode.py',
                     'tests/test_ci_workflow.py', '<new-branch>', 'new-root/file',
                     'scripts-other/file', 'scripts/../go.mod'):
            with self.subTest(path=path):
                self.assertTrue(check_deadcode.required(['scripts/appliance.py', path]))
        self.assertTrue(check_deadcode.required(None))

    def test_invalid_paths_fail_before_analysis(self):
        for payload in ('null', '{}', '[1]', '[""]', 'broken'):
            with self.subTest(payload=payload), contextlib.redirect_stderr(io.StringIO()), \
                    self.assertRaises(SystemExit) as error:
                check_deadcode.main(['--paths-json', payload])
            self.assertEqual(error.exception.code, 2)

    def test_default_and_selected_runs_keep_both_architectures_and_findings(self):
        finding = {('file.go', 1, 2, 'unused')}
        for args in ([], ['--paths-json', '["<new-branch>"]'],
                     ['--paths-json', '["platform/main.go"]']):
            with self.subTest(args=args), patch.object(check_deadcode, 'install_tools'), \
                    patch.object(check_deadcode, 'findings', return_value=finding) as find, \
                    contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(check_deadcode.main(args), 1)
                self.assertEqual([call.args[-1] for call in find.call_args_list],
                                 list(check_deadcode.LANES))

    def test_every_tracked_go_module_is_checked(self):
        tracked = subprocess.run(['git', 'ls-files', '*go.mod'], cwd=ROOT, check=True,
                                 capture_output=True, text=True).stdout.split()
        self.assertEqual(sorted(str(Path(p).parent) for p in tracked), sorted(MODULES))

    def test_u1000_line_parse(self):
        match = U1000.match('ui/tenfoot/stamp.go:75:6: type flightMemory is unused (U1000)')
        self.assertEqual(match.groups(), ('ui/tenfoot/stamp.go', '75', '6', 'type flightMemory is unused'))
        self.assertIsNone(U1000.match('-: module requires at least go1.26.5 (compile)'))


if __name__ == '__main__':
    unittest.main()
