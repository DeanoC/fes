import contextlib
import io
import json
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import check_deadcode, check_govet


class CheckGoVetTests(unittest.TestCase):
    def test_non_go_parent_inputs_skip_before_running_vet(self):
        for paths in ([], ['scripts/appliance.py'], ['tests/test_receipt.py'],
                      ['image/Makefile', 'profiles/native-integration-dev.toml'],
                      ['sources/misteross/cores/fes-zx81/rtl/zx81.v']):
            with self.subTest(paths=paths), patch.object(check_govet, 'run_all') as run_all, \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(check_govet.main(['--paths-json', json.dumps(paths)]), 0)
                run_all.assert_not_called()

    def test_go_modules_checker_and_unknown_inputs_run(self):
        for path in ('sources/FogCast/internal/agent/agent.go', 'platform/go.mod',
                     'sources/FogCast/go.sum', 'sources/mister-packages/packages/x.yaml',
                     'sources/misteross/expansion/layout.json', 'scripts/new.go',
                     'Makefile', '.github/workflows/check.yml',
                     'scripts/check_govet.py', 'tests/test_check_govet.py',
                     'scripts/check_deadcode.py', 'tests/test_ci_workflow.py',
                     '<new-branch>', 'new-root/file'):
            with self.subTest(path=path):
                self.assertTrue(check_govet.required(['scripts/appliance.py', path]))
        self.assertTrue(check_govet.required(None))

    def test_invalid_paths_fail_before_analysis(self):
        for payload in ('null', '{}', '[1]', '[""]', 'broken'):
            with self.subTest(payload=payload), contextlib.redirect_stderr(io.StringIO()), \
                    self.assertRaises(SystemExit) as error:
                check_govet.main(['--paths-json', payload])
            self.assertEqual(error.exception.code, 2)

    def test_plan_only_reports_selection_without_running_vet(self):
        out = io.StringIO()
        with patch.object(check_govet, 'run_all') as run_all, contextlib.redirect_stdout(out):
            rc = check_govet.main(['--plan-only', '--paths-json', '["scripts/check_govet.py"]'])
        self.assertEqual(rc, 0)
        run_all.assert_not_called()
        self.assertEqual(json.loads(out.getvalue()), {
            'selected': True,
            'paths': ['scripts/check_govet.py'],
        })

    def test_run_all_vets_every_module_on_every_lane_and_collects_failures(self):
        root = Path(__file__).resolve().parent.parent
        calls = []

        def fake_vet(repo_root, module, work, lane):
            self.assertEqual(repo_root, root)
            self.assertTrue(work.is_file())
            calls.append((lane['GOARCH'], module))
            if module == 'sources/FogCast' and lane['GOARCH'] == 'arm':
                return 'internal/agent/x_test.go:10:2: copylocks'
            return ''

        with patch.object(check_govet, 'vet_module', side_effect=fake_vet):
            failures = check_govet.run_all(root)
        expected_calls = [
            (lane['GOARCH'], module)
            for lane in check_deadcode.LANES
            for module in check_deadcode.MODULES
        ]
        self.assertEqual(calls, expected_calls)
        self.assertEqual(failures, [('linux/arm', 'sources/FogCast', 'internal/agent/x_test.go:10:2: copylocks')])

    def test_main_returns_nonzero_and_prints_failures(self):
        err = io.StringIO()
        with patch.object(check_govet, 'run_all',
                          return_value=[('linux/amd64', 'sources/FogCast', 'bad.go:1:1: problem')]), \
                contextlib.redirect_stderr(err):
            rc = check_govet.main(['--paths-json', '["sources/FogCast/a.go"]'])
        self.assertEqual(rc, 1)
        self.assertIn('[linux/amd64] sources/FogCast:', err.getvalue())
        self.assertIn('bad.go:1:1: problem', err.getvalue())


if __name__ == '__main__':
    unittest.main()
