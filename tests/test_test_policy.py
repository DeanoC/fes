"""Routine/extended test-mode policy shared by CI and the affected runner."""
import contextlib
import io
import json
import os
import re
import subprocess
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import host_tests
import parent_tests
import test_policy

ROOT = Path(__file__).resolve().parents[1]
ALL_FALSE = {'video': False, 'media': False, 'full_race': False}
ALL_TRUE = {'video': True, 'media': True, 'full_race': True}
RACE = ['go', 'test', '-race', '-short', '-timeout', '30m']


class SelectTestModesTests(unittest.TestCase):
    def test_ordinary_and_policy_only_changes_select_no_extended_mode(self):
        for path in ('sources/FogCast/internal/hostapi/ui_app.js',
                     'sources/FogCast/catalog/query_test.go',
                     'sources/FogCast/cmd/fogcast/main.go',
                     'scripts/test_policy.py', 'scripts/ci_gate.py',
                     'sources/libmister-runtime/src/protocol.cpp',
                     '.github/workflows/check.yml', 'Makefile'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]), ALL_FALSE)

    def test_video_relevant_inputs_select_video_only(self):
        for path in ('scripts/factory_video_parts.py', 'scripts/core_catalog.py',
                     'scripts/core_dev_accept.py', 'scripts/recipes.py', 'scripts/bundle.py',
                     'scripts/artifact_cache.py', 'config/core-recipes.toml',
                     'tests/test_factory_video_parts.py',
                     'tests/test_factory_video_publication.py',
                     'sources/misteross/cores/fes-coleco/rtl/machine.sv',
                     'sources/misteross/expansion/rbf.go',
                     'sources/FogCast/corepackage/install.go',
                     'sources/FogCast/corecatalog/entries.go',
                     'sources/FogCast/catalog/query.go', 'sources/FogCast/fogcast/service.go'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]),
                                 {'video': True, 'media': False, 'full_race': False})

    def test_shared_script_and_dependency_inputs_select_video_and_media(self):
        for path in ('scripts/build.py', 'scripts/inputs.py', 'scripts/native_dev.py',
                     'scripts/environment.py',
                     'sources/FogCast/go.mod', 'sources/FogCast/go.sum'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]),
                                 {'video': True, 'media': True, 'full_race': False})

    def test_media_relevant_inputs_select_media_only(self):
        for path in ('image/Makefile', 'image/scripts/build-target-image.sh',
                     'platform/internal/applianceboot/boot.go', 'containers/build/Dockerfile',
                     'profiles/native-integration-dev.toml', 'boot-media.lock.toml',
                     'scripts/media.py', 'scripts/media_container.py',
                     'scripts/appliance.py', 'scripts/appliance_media.py',
                     'scripts/platform.py', 'scripts/image_toolchain.py',
                     'tests/test_media_image.py', 'tests/test_appliance.py',
                     'tests/test_appliance_media.py',
                     'sources/FogCast/appliance/update.go',
                     'sources/FogCast/cmd/target-image-lock/main.go'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]),
                                 {'video': False, 'media': True, 'full_race': False})

    def test_contracts_agents_and_unknown_inputs_select_everything(self):
        for path in ('sources/mister-packages/packages/abi/new.yaml',
                     'AGENTS.md', 'unknown/new.bin', '<new-branch>'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]), ALL_TRUE)

    def test_documentation_is_ignored_and_prefix_siblings_stay_conservative(self):
        self.assertEqual(test_policy.select_test_modes(['docs/guide.md']), ALL_FALSE)
        for path in ('scripts-other/new.py', 'sources/FogCast-catalog/x.go'):
            with self.subTest(path=path):
                self.assertEqual(test_policy.select_test_modes([path]), ALL_TRUE)
        # Siblings inside a recognized root are recognized, not relevant.
        self.assertEqual(test_policy.select_test_modes(['sources/FogCast/catalog2/x.go']),
                         ALL_FALSE)

    def test_mixed_and_renamed_changes_union_modes(self):
        self.assertEqual(test_policy.select_test_modes(
            ['image/old.sh', 'image/new.sh', 'sources/FogCast/internal/hostapi/ui.js']),
            {'video': False, 'media': True, 'full_race': False})
        self.assertEqual(test_policy.select_test_modes(
            ['scripts/bundle.py', 'image/Makefile']),
            {'video': True, 'media': True, 'full_race': False})

    def test_validate_rejects_malformed_and_lane_inconsistent_modes(self):
        for bad in (None, [], {}, {'video': True}, 'true',
                    {'video': 1, 'media': False, 'full_race': False},
                    {'video': 'true', 'media': False, 'full_race': False},
                    {'video': True, 'media': False, 'full_race': False, 'x': False}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                test_policy.validate_test_modes(bad)
        test_policy.validate_test_modes(ALL_TRUE)
        test_policy.validate_test_modes(ALL_TRUE, {'parent': True, 'host': True})
        for modes, lanes in [(dict(ALL_TRUE), {'parent': False, 'host': True}),
                             ({'video': False, 'media': True, 'full_race': False},
                              {'parent': False, 'host': True}),
                             ({'video': False, 'media': False, 'full_race': True},
                              {'parent': True, 'host': False})]:
            with self.subTest(modes=modes, lanes=lanes), self.assertRaises(ValueError):
                test_policy.validate_test_modes(modes, lanes)


class HostCommandTests(unittest.TestCase):
    PACKAGES = ('github.com/DeanoC/FogCast/catalog', 'github.com/DeanoC/FogCast/fogcast',
                'github.com/DeanoC/FogCast/corepackage', 'github.com/DeanoC/FogCast/internal/x')

    def test_routine_command_shape_and_race_exclusions(self):
        root, others = [], []
        for command in test_policy.host_test_commands(packages=self.PACKAGES):
            (root if command['cwd'] == 'sources/FogCast' else others).append(command)
        self.assertEqual(root[0]['argv'], ['go', 'test', '-short', '-timeout', '30m',
                                         './fogcast', './corepackage'])
        self.assertEqual(root[0]['label'], 'host artifact functional suites')
        self.assertEqual(root[1]['argv'],
                         [*RACE, 'github.com/DeanoC/FogCast/catalog',
                          'github.com/DeanoC/FogCast/internal/x'])
        self.assertEqual(root[2]['argv'],
                         [*RACE, '-run', test_policy.HOST_RACE_FOCUS, './fogcast', './corepackage'])
        by_dir = {c['cwd']: c['argv'] for c in others}
        self.assertEqual(by_dir['sources/FogCast/appliance'], [*RACE, './...'])
        expansion = [c['argv'] for c in others if c['cwd'] == 'sources/misteross/expansion']
        self.assertEqual(expansion,
                         [['go', 'test', '-short', '-timeout', '30m', './...'],
                          [*RACE, '-run', test_policy.HOST_RACE_FOCUS, './...']])
        # Functional coverage is partitioned exactly once: the two expensive
        # packages non-race, every other listed package under race.
        covered = ({'github.com/DeanoC/FogCast/' + arg.removeprefix('./')
                    for arg in root[0]['argv'][5:]} |
                   set(root[1]['argv'][len(RACE):]))
        self.assertEqual(covered, set(self.PACKAGES))

    def test_full_mode_races_everything_and_mode_must_be_boolean(self):
        commands = test_policy.host_test_commands(full=True)
        self.assertEqual(len(commands), 3)
        for command in commands:
            self.assertEqual(command['argv'], ['go', 'test', '-race', '-timeout', '30m', './...'])
        with self.assertRaises(ValueError):
            test_policy.host_test_commands(full='yes')
        for empty in (None, []):
            with self.subTest(empty=empty), self.assertRaises(ValueError):
                test_policy.host_test_commands(packages=empty)
        # A listing of only the focused packages must not yield a bare `go test`.
        with self.assertRaises(ValueError):
            test_policy.host_test_commands(
                packages=list(test_policy.EXPENSIVE_HOST_PACKAGES))

    def test_race_focus_matches_concurrency_names_only(self):
        pattern = re.compile(test_policy.HOST_RACE_FOCUS)
        for name in ('TestConcurrentTransitionReturnsBusy',
                     'TestCoreMediaLifecycleQueuedLaunchCannotRebindTarget',
                     'TestStoreConcurrentIdenticalImportPublishesOnce'):
            self.assertTrue(pattern.search(name), name)
        for name in ('TestCoreCatalogVideoInstallFeedsOrdinaryPlay',
                     'TestLibraryVideoPreferenceLaunchesExactPartsWithCPUAndMedia'):
            self.assertFalse(pattern.search(name), name)

    def test_runner_go_lists_then_stops_on_failure_with_argv_arrays(self):
        calls = []
        def run(argv, cwd=None, env=None, stdout=None, stderr=None):
            calls.append((argv, cwd, env, stdout, stderr))
            return subprocess.CompletedProcess(argv, 9 if len(calls) == 2 else 0)
        packages = 'github.com/DeanoC/FogCast/catalog\ngithub.com/DeanoC/FogCast/fogcast\n'
        with patch.object(host_tests.subprocess, 'check_output', return_value=packages), \
                patch.object(host_tests.subprocess, 'run', side_effect=run), \
                contextlib.redirect_stdout(io.StringIO()) as out:
            code = host_tests.main([])
        self.assertEqual(code, 1)
        self.assertEqual(len(calls), 2)
        self.assertTrue(all(isinstance(argv, list) for argv, *_ in calls))
        for _, _, _, stdout, stderr in calls:
            self.assertIs(stdout, sys.stderr)
            self.assertIs(stderr, sys.stderr)
        json.loads(out.getvalue())  # stdout stays a standalone JSON report
        with patch.object(host_tests.subprocess, 'check_output', return_value=packages), \
                contextlib.redirect_stdout(io.StringIO()) as out:
            self.assertEqual(host_tests.main(['--plan-only']), 0)
        self.assertEqual(json.loads(out.getvalue())['mode'], 'routine')

    def test_runner_sanitizes_cross_vars_before_listing_and_running(self):
        seen = {}
        def listing(argv, cwd=None, env=None, text=None):
            seen['env'] = env
            return ('github.com/DeanoC/FogCast/catalog\n'
                    'github.com/DeanoC/FogCast/fogcast\n')
        runs = []
        def run(argv, cwd=None, env=None, stdout=None, stderr=None):
            runs.append(env)
            return subprocess.CompletedProcess(argv, 0)
        cross = ('GOOS', 'GOARCH', 'GOARM')
        with patch.dict(os.environ, dict.fromkeys(cross, 'x')), \
                patch.object(host_tests.subprocess, 'check_output', side_effect=listing), \
                patch.object(host_tests.subprocess, 'run', side_effect=run), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(host_tests.main([]), 0)
        for env in [seen['env'], *runs]:
            self.assertTrue(all(name not in env for name in cross))


def _synthetic(module, name, heavy=False):
    def boom(cls):
        raise AssertionError('heavy class setup ran')
    namespace = {'test_marker': lambda self: None}
    if heavy:
        namespace['setUpClass'] = classmethod(boom)
    cls = type(name, (unittest.TestCase,), namespace)
    cls.__module__ = module
    return cls


def _synthetic_suite():
    suite = unittest.TestSuite()
    loader = unittest.defaultTestLoader
    for cls in (_synthetic('test_factory_video_parts', 'RealProducerEvidenceTests', True),
                _synthetic('test_factory_video_parts', 'VideoIndexTests'),
                _synthetic('test_factory_video_publication', 'FactoryVideoPublicationTests', True),
                _synthetic('test_media_image', 'ContainerImageTests', True),
                _synthetic('test_appliance', 'RealBootstrapTests', True),
                _synthetic('test_appliance_media', 'RealCardTests', True),
                _synthetic('test_recipes', 'RecipeRegistryTest')):
        suite.addTests(loader.loadTestsFromTestCase(cls))
    return suite


class ParentFilterTests(unittest.TestCase):
    def test_fast_modes_omit_heavy_classes_before_any_setup(self):
        suite, selected, omitted = parent_tests.filter_suite(
            _synthetic_suite(), video=False, media=False)
        result = unittest.TestResult()
        suite.run(result)
        self.assertTrue(result.wasSuccessful(), result.errors)
        self.assertEqual(len(omitted), 5)
        self.assertTrue(all('VideoIndexTests' in i or 'RecipeRegistryTest' in i
                            for i in selected))

    def test_video_and_media_modes_restore_their_classes_independently(self):
        _, selected, omitted = parent_tests.filter_suite(
            _synthetic_suite(), video=True, media=False)
        self.assertEqual(len(omitted), 3)
        self.assertTrue(all('test_media_image' in i or 'test_appliance' in i
                            for i in omitted))
        _, _, omitted = parent_tests.filter_suite(_synthetic_suite(), video=False, media=True)
        self.assertEqual(len(omitted), 2)
        self.assertTrue(all('test_factory_video' in i for i in omitted))
        _, selected, omitted = parent_tests.filter_suite(_synthetic_suite())
        self.assertEqual(omitted, [])
        self.assertEqual(len(selected), 7)

    def test_qualified_modules_filter_and_failed_imports_survive(self):
        suite = unittest.TestSuite()
        qualified = _synthetic('tests.test_media_image', 'ContainerImageTests', True)
        suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(qualified))
        suite.addTest(unittest.loader._FailedTest('broken', ImportError('boom')))
        _, selected, omitted = parent_tests.filter_suite(suite, media=False)
        self.assertEqual(len(omitted), 1)
        self.assertIn('ContainerImageTests', omitted[0])
        self.assertTrue(any('_FailedTest' in i for i in selected))

    def test_main_report_stdout_stays_json_when_tests_print(self):
        class ChattyTests(unittest.TestCase):
            def test_marker(self):
                print('incidental stdout from a test case')
        suite = unittest.TestSuite([ChattyTests('test_marker')])
        with patch.object(parent_tests.unittest.defaultTestLoader,
                          'discover', return_value=suite), \
                contextlib.redirect_stdout(io.StringIO()) as out:
            code = parent_tests.main(['--video', 'false', '--media', 'false'])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out.getvalue())['status'], 'passed')

    def test_real_plan_only_reports_known_heavy_omissions(self):
        script = str(ROOT / 'scripts/parent_tests.py')
        routine = subprocess.run(
            [sys.executable, script, '--video', 'false', '--media', 'false', '--plan-only'],
            capture_output=True, text=True)
        self.assertEqual(routine.returncode, 0, routine.stderr)
        report = json.loads(routine.stdout)
        heavy = {module + '.' + name
                 for module, name in parent_tests.VIDEO_CLASSES | parent_tests.MEDIA_CLASSES}
        self.assertEqual({i.rsplit('.', 1)[0] for i in report['omitted']}, heavy)
        self.assertTrue(report['selected'])
        full = subprocess.run([sys.executable, script, '--plan-only'],
                              capture_output=True, text=True)
        self.assertEqual(full.returncode, 0, full.stderr)
        self.assertEqual(json.loads(full.stdout)['omitted'], [])


if __name__ == '__main__':
    unittest.main()
