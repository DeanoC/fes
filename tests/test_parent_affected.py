"""Parent integration boundaries and fail-safe suite filtering."""
import contextlib
import ast
import io
import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

from scripts import parent_affected as policy, parent_tests
from scripts.affected import documentation

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

    def test_target_image_selector_library_keeps_media_regressions(self):
        path = 'sources/FogCast/internal/targetimage/lock.go'
        groups = policy.select_groups([path])
        self.assertEqual(groups, {'policy', 'build', 'media', 'packages', 'acceptance'})
        modules = policy.selected_modules(groups, [])
        self.assertIn('test_media', modules)

    def test_boot_consumers_and_extended_modes_restore_their_groups(self):
        self.assertEqual(policy.select_groups(['sources/FogCast/appliance/release.go']),
                         set(policy.GROUPS))
        self.assertEqual(policy.select_groups([ROOM], video=True, media=True),
                         {'policy', 'build', 'packages', 'media'})

    def test_unknown_shared_parent_inputs_and_empty_plans_keep_all(self):
        for path in ('<new-branch>', 'scripts/build.py', 'scripts/target_acceptance.py',
                     'tests/test_parent_affected.py', 'image/Makefile', 'platform/boot.go',
                     'profiles/native-integration-dev.toml', 'sources/mister-packages/abi.yaml',
                     'sources/FogCast/go.mod', 'sources/FogCast/go.sum',
                     'sources/FogCast/new-area/source.go', 'sources/FogCast-other/api.go',
                     'sources/FogCast/ui/../../go.mod', 'new-root/file'):
            with self.subTest(path=path):
                self.assertIsNone(policy.select_groups([ROOM, path]))
        self.assertIsNone(policy.select_groups(None))
        self.assertIsNone(policy.select_groups([]))

    def test_mixed_documentation_keeps_code_groups_and_doc_readers(self):
        for code in (ROOM, 'tests/test_receipt.py', 'scripts/appliance.py'):
            for doc in ('README.md', 'AGENTS.md', 'docs/development.md',
                        'sources/FogCast/docs/ARCHITECTURE.md'):
                with self.subTest(code=code, doc=doc):
                    paths = [code, doc]
                    groups = policy.select_groups(paths)
                    self.assertEqual(groups, policy.select_groups([code]))
                    selected = policy.selected_modules(groups, [], paths)
                    self.assertTrue(policy.DOCUMENTATION_MODULES <= selected)
                    self.assertNotIn('test_atari_st_rendered_capture', selected)
        self.assertIsNone(policy.select_groups([ROOM, 'docs/guide.md', 'new-root/code']))
        self.assertIsNone(policy.select_groups([ROOM, 'docs/../unknown.md']))

    def test_documentation_classification_matches_outer_planner(self):
        for path in ('README.md', 'docs/example.json', 'sources/FogCast/docs/a.md',
                     'sources/FogCast/a.go', 'docs-other/a.py', 'scripts/file.py'):
            self.assertEqual(policy.documentation(path), documentation(path), path)

    def test_doc_readers_execute_while_unrelated_suite_setup_is_omitted(self):
        paths = ['tests/test_receipt.py', 'docs/core-packages.md']
        modules = policy.selected_modules(policy.select_groups(paths), [], paths)
        suite = unittest.TestSuite([case(module) for module in policy.DOCUMENTATION_MODULES] +
                                  [case('test_atari_st_rendered_capture', fail=True)])
        kept, selected, omitted = parent_tests.filter_suite(suite, video=False, media=False, modules=modules)
        result = unittest.TestResult()
        kept.run(result)
        self.assertTrue(result.wasSuccessful())
        self.assertEqual(result.testsRun, len(policy.DOCUMENTATION_MODULES))
        self.assertEqual(omitted, ['test_atari_st_rendered_capture.Case.test_case'])

    def test_documentation_modules_exist_and_belong_to_reviewed_groups(self):
        assigned = set().union(*policy.GROUPS.values())
        self.assertTrue(policy.DOCUMENTATION_MODULES <= assigned)
        for module in policy.DOCUMENTATION_MODULES:
            self.assertTrue((ROOT / 'tests' / (module + '.py')).is_file())

    def test_broken_guide_still_fails_real_doc_consumer_in_mixed_plan(self):
        paths = ['tests/test_receipt.py', 'docs/core-packages.md']
        modules = policy.selected_modules(policy.select_groups(paths), [], paths)
        suite = unittest.defaultTestLoader.loadTestsFromName(
            'tests.test_core_build.CoreBuildTest.test_native_image_mode_is_package_only')
        kept, _, omitted = parent_tests.filter_suite(suite, video=False, media=False, modules=modules)
        original = Path.read_text
        def read(path, *args, **kwargs):
            return '' if path == ROOT / 'docs/core-packages.md' else original(path, *args, **kwargs)
        result = unittest.TestResult()
        with patch.object(Path, 'read_text', read):
            kept.run(result)
        self.assertEqual(omitted, [])
        self.assertEqual(result.testsRun, 1)
        self.assertEqual(len(result.failures), 1)
        self.assertIn('image route is package-only', result.failures[0][1])

    def test_reviewed_parent_inputs_keep_owners_and_shared_test_consumers(self):
        for path, expected in (
                ('scripts/appliance.py', {'policy', 'build', 'media'}),
                ('scripts/appliance_inside.py', {'policy', 'build', 'media'}),
                ('tests/test_receipt.py', {'policy', 'build'}),
                ('tests/test_core_build.py', {'policy', 'build', 'packages'}),
                ('tests/test_package_acceptance.py', {'policy', 'build', 'packages', 'acceptance'})):
            with self.subTest(path=path):
                self.assertEqual(policy.select_groups([path]), expected)

    def test_reviewed_planner_and_gate_inputs_keep_policy_and_build(self):
        for path in ('scripts/affected.py', 'scripts/ci_gate.py', 'scripts/check_diff.py',
                     'tests/test_affected.py', 'tests/test_ci_gate.py',
                     'tests/test_check_diff.py', 'tests/test_test_changed.py'):
            with self.subTest(path=path):
                paths = [path, 'docs/test-changed.md']
                groups = policy.select_groups(paths)
                self.assertEqual(groups, {'policy', 'build'})
                modules = policy.selected_modules(groups, [], paths)
                self.assertTrue({'test_ci_gate', 'test_affected', 'test_test_changed',
                                 'test_ci_workflow', 'test_hil_plan', 'test_status'} <= modules)
                self.assertTrue(policy.DOCUMENTATION_MODULES <= modules)
                self.assertNotIn('test_media', modules)
                self.assertNotIn('test_package_acceptance', modules)

    def test_planner_inputs_union_consumers_and_keep_selector_edits_full(self):
        self.assertEqual(policy.select_groups(['scripts/affected.py', 'scripts/appliance.py']),
                         {'policy', 'build', 'media'})
        self.assertEqual(policy.select_groups(['scripts/ci_gate.py', ROOM], video=True),
                         {'policy', 'build', 'packages'})
        for path in ('scripts/parent_affected.py', 'scripts/parent_tests.py',
                     'tests/test_parent_affected.py', 'scripts/test_policy.py',
                     'scripts/test_changed.py', 'scripts/affected_other.py',
                     'tests/test_new_policy.py', '<new-branch>'):
            with self.subTest(path=path):
                self.assertIsNone(policy.select_groups(['scripts/affected.py', path]))

    def test_planner_group_filter_keeps_gate_failures_and_omits_unrelated_setup(self):
        modules = policy.selected_modules(policy.select_groups(['scripts/ci_gate.py']), [])
        suite = unittest.TestSuite([case('test_ci_gate', fail=True),
                                   case('test_media', fail=True), case('test_status')])
        kept, _, omitted = parent_tests.filter_suite(suite, video=False, media=False, modules=modules)
        result = unittest.TestResult()
        kept.run(result)
        self.assertEqual(result.testsRun, 1)
        self.assertEqual(len(result.errors), 1)
        self.assertIn('test_ci_gate', str(result.errors))
        self.assertEqual(omitted, ['test_media.Case.test_case'])

    def test_parent_mixed_paths_and_modes_union_coverage(self):
        paths = ['scripts/appliance_media.py', 'tests/test_core_build.py']
        self.assertEqual(policy.select_groups(paths), {'policy', 'build', 'media', 'packages'})
        self.assertEqual(policy.select_groups(['tests/test_receipt.py'], video=True, media=True),
                         {'policy', 'build', 'packages', 'media'})
        for unknown in ('scripts/new.py', 'tests/test_new.py', 'tests/helpers.py',
                        'scripts/appliance_extra.py', 'scripts/parent_affected.py',
                        'scripts/parent_tests.py', 'scripts/test_policy.py'):
            with self.subTest(path=unknown):
                self.assertIsNone(policy.select_groups(paths + [unknown]))

    def test_reviewed_script_paths_exist(self):
        for path in policy.PARENT_INPUT_GROUPS:
            self.assertTrue((ROOT / path).is_file(), path)

    def test_cross_test_import_consumers_remain_selected(self):
        # Guard fixture sharing when an existing test starts importing another
        # group's helpers. Dynamic file/subprocess consumers still need review.
        owners = {module: group for group, modules in policy.GROUPS.items()
                  for module in modules}
        for consumer, group in owners.items():
            tree = ast.parse((ROOT / 'tests' / (consumer + '.py')).read_text())
            dependencies = set()
            for node in ast.walk(tree):
                if isinstance(node, ast.Import):
                    dependencies.update(alias.name.rsplit('.', 1)[-1] for alias in node.names)
                elif isinstance(node, ast.ImportFrom):
                    dependencies.add((node.module or '').rsplit('.', 1)[-1])
                    dependencies.update(alias.name for alias in node.names)
            for dependency in dependencies & owners.keys():
                selected = policy.select_groups(['tests/' + dependency + '.py'])
                if selected is not None:
                    self.assertIn(group, selected, f'{consumer} imports {dependency}')

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

    def test_unassigned_discovered_suite_runs_even_outside_root_file_inventory(self):
        suite = unittest.TestSuite([case('nested.test_new_integration')])
        kept, selected, omitted = parent_tests.filter_suite(
            suite, video=False, media=False, modules=policy.GROUPS['policy'])
        self.assertEqual(omitted, [])
        result = unittest.TestResult()
        kept.run(result)
        self.assertTrue(result.wasSuccessful())
        self.assertEqual(result.testsRun, 1)

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
        receipt = plan(['tests/test_receipt.py'])
        self.assertEqual(receipt['selected'], room['selected'])
        appliance = plan(['scripts/appliance.py'])
        self.assertTrue(any('test_appliance.' in test for test in appliance['selected']))
        self.assertFalse(any('test_core_build.' in test for test in appliance['selected']))
        self.assertEqual(sorted(appliance['selected'] + appliance['omitted']),
                         sorted(full['selected'] + full['omitted']))
        mixed = plan(['tests/test_receipt.py', 'docs/development.md'])
        self.assertEqual(mixed['groups'], receipt['groups'])
        self.assertEqual(set(mixed['documentation_modules']), policy.DOCUMENTATION_MODULES)
        extra = set(mixed['selected']) - set(receipt['selected'])
        self.assertTrue(extra)
        self.assertTrue(all(test.split('.')[0] in policy.DOCUMENTATION_MODULES for test in extra))
        self.assertEqual(sorted(mixed['selected'] + mixed['omitted']),
                         sorted(full['selected'] + full['omitted']))

    def test_malformed_path_payload_is_rejected(self):
        for payload in ('null', '{}', '[1]', 'broken'):
            with self.subTest(payload=payload), patch('sys.stderr'), self.assertRaises(SystemExit):
                parent_tests.main(['--paths-json', payload, '--plan-only'])
