"""Parent integration boundaries and fail-safe suite filtering."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

from scripts import parent_affected as policy, parent_tests

ROOT = Path(__file__).resolve().parents[1]
ROOM = 'sources/FogCast/ui/rooms/examples/fes-cores/main.lua'


def case(module, name='Case', fail=False):
    def setup(cls):
        if fail:
            raise AssertionError('omitted suite setup was executed')
    cls = type(name, (unittest.TestCase,), {
        '__module__': module, 'setUpClass': classmethod(setup),
        'test_case': lambda self: None})
    return cls('test_case')


class ParentAffectedTests(unittest.TestCase):
    def test_rooms_keep_build_and_policy_without_media_or_core_diagnostics(self):
        groups = policy.select_groups([ROOM])
        self.assertEqual(groups, {'policy', 'build'})
        modules = policy.selected_modules(groups, [])
        self.assertIn('test_kit_software', modules)
        self.assertIn('test_image_assembly', modules)
        self.assertNotIn('test_media', modules)
        self.assertNotIn('test_atari_st_rendered_capture', modules)

    def test_package_and_runtime_consumers_keep_acceptance_regressions(self):
        for path in ('sources/FogCast/corepackage/admit.go',
                     'sources/FogCast/protocol/api.go',
                     'sources/libmister-runtime/src/runtime.cpp',
                     'sources/misteross/cores/fes-atari-st/rtl/st.v'):
            with self.subTest(path=path):
                self.assertEqual(policy.select_groups([path]),
                                 {'policy', 'build', 'packages', 'acceptance'})

    def test_boot_consumers_and_extended_modes_restore_their_groups(self):
        self.assertEqual(policy.select_groups(['sources/FogCast/appliance/release.go']),
                         set(policy.GROUPS))
        self.assertEqual(policy.select_groups([ROOM], video=True, media=True),
                         {'policy', 'build', 'packages', 'media'})

    def test_unknown_shared_parent_inputs_and_empty_plans_keep_all(self):
        for path in ('<new-branch>', 'scripts/build.py', 'scripts/target_acceptance.py',
                     'tests/test_core_build.py', 'image/Makefile', 'platform/boot.go',
                     'profiles/native-integration-dev.toml', 'sources/mister-packages/abi.yaml',
                     'sources/FogCast/go.mod', 'sources/FogCast/go.sum',
                     'sources/FogCast/new-area/source.go', 'sources/FogCast-other/api.go',
                     'sources/FogCast/ui/../../go.mod', 'new-root/file', 'README.md'):
            with self.subTest(path=path):
                self.assertIsNone(policy.select_groups([ROOM, path]))
        self.assertIsNone(policy.select_groups(None))
        self.assertIsNone(policy.select_groups([]))

    def test_renames_union_both_owners(self):
        self.assertEqual(policy.select_groups([ROOM, 'sources/FogCast/appliance/assets/room.lua']),
                         set(policy.GROUPS))

    def test_new_unassigned_modules_are_never_dropped(self):
        selected = policy.selected_modules({'policy', 'build'}, ['test_new_integration'])
        self.assertIn('test_new_integration', selected)

    def test_group_names_are_unique_and_refer_to_existing_tests(self):
        seen = set()
        for group, modules in policy.GROUPS.items():
            self.assertFalse(seen & modules, group)
            for module in modules:
                self.assertTrue((ROOT / 'tests' / (module + '.py')).is_file(), module)
            seen.update(modules)

    def test_group_filter_runs_before_setup_and_retains_import_failures(self):
        suite = unittest.TestSuite([
            case('test_inputs'), case('test_media', fail=True),
            unittest.loader._FailedTest('broken', ImportError('broken suite'))])
        kept, selected, omitted = parent_tests.filter_suite(
            suite, video=False, media=False, modules={'test_inputs'})
        self.assertEqual(omitted, ['test_media.Case.test_case'])
        result = unittest.TestResult()
        kept.run(result)
        self.assertEqual(result.testsRun, 2)
        self.assertEqual(len(result.errors), 1)
        self.assertIn('broken suite', result.errors[0][1])

    def test_modes_and_groups_are_both_applied(self):
        suite = unittest.TestSuite([
            case('test_factory_video_parts', 'RealProducerEvidenceTests', fail=True),
            case('test_core_catalog'), case('test_media', fail=True)])
        kept, selected, omitted = parent_tests.filter_suite(
            suite, video=False, media=False, modules=policy.GROUPS['packages'])
        result = unittest.TestResult()
        kept.run(result)
        self.assertTrue(result.wasSuccessful())
        self.assertEqual(result.testsRun, 1)
        self.assertEqual(len(omitted), 2)

    def test_full_flag_restores_groups_and_extended_modes(self):
        suite = unittest.TestSuite([case('test_media'),
            case('test_factory_video_parts', 'RealProducerEvidenceTests')])
        with patch.object(parent_tests.unittest.defaultTestLoader, 'discover', return_value=suite), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            result = parent_tests.main(['--plan-only', '--full', '--video', 'false',
                                        '--media', 'false', '--paths-json', json.dumps([ROOM])])
        report = json.loads(output.getvalue())
        self.assertEqual(result, 0)
        self.assertEqual(report['groups'], 'all')
        self.assertTrue(report['video'] and report['media'])
        self.assertEqual(report['omitted'], [])

    def test_real_plan_partitions_all_discovered_tests(self):
        def plan(paths=None):
            argv = [sys.executable, 'scripts/parent_tests.py', '--plan-only',
                    '--video', 'false', '--media', 'false']
            if paths is not None:
                argv.extend(['--paths-json', json.dumps(paths)])
            return json.loads(subprocess.check_output(argv, cwd=ROOT, text=True))
        full, room = plan(), plan([ROOM])
        self.assertEqual(sorted(full['selected'] + full['omitted']),
                         sorted(room['selected'] + room['omitted']))
        self.assertLess(len(room['selected']), len(full['selected']) // 2)
        self.assertTrue(any('test_parent_affected.' in test for test in room['selected']))
        self.assertEqual(plan(['<new-branch>'])['selected'], full['selected'])

    def test_malformed_path_payload_is_rejected(self):
        for payload in ('null', '{}', '[1]', 'broken'):
            with self.subTest(payload=payload), patch('sys.stderr'), self.assertRaises(SystemExit):
                parent_tests.main(['--paths-json', payload, '--plan-only'])
