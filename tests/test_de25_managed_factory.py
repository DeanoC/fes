"""Factory configuration and refusal boundaries; fixtures do not qualify MENU."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tomllib
from types import SimpleNamespace
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'image/de25-nano'))
SPEC = importlib.util.spec_from_file_location('de25_managed_factory', ROOT / 'image/de25-nano/managed_factory.py')
FACTORY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FACTORY)


class ManagedFactoryTests(unittest.TestCase):
    def fixture(self, root):
        directory = root / 'artifacts'
        package = directory / 'core-packages' / ('a' * 64)
        package.mkdir(parents=True)
        (package / 'core.rbf').write_bytes(b'unqualified fixture')
        (package / 'manifest.toml').write_text('[build]\nrepository="https://example.invalid/source"\nrevision="' + 'b' * 40 + '"\n')
        manifest = {'provenance': {'runtime': 'c' * 40, 'fogcast': 'd' * 40},
                    'binaries': {'mister-runtime': 'e' * 64}}
        return directory, (manifest, [{'directory': package,
                                      'selection_path': directory / 'fes-menu.package-selection.toml'}])

    def test_factory_enables_selected_runtime_without_changing_base_policy(self):
        key = 'factory-' + '1' * 64
        configured = FACTORY.base.configuration(factory_key=key).splitlines()
        self.assertIn('BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y', configured)
        self.assertNotIn('# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set', configured)
        self.assertIn('# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set', configured)
        self.assertIn('BR2_aarch64=y', configured)
        self.assertIn(str(FACTORY.base.toolchain_path(key)), '\n'.join(configured))
        self.assertIn('# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set', FACTORY.base.configuration())

    def test_factory_key_binds_selected_binaries_kernel_and_source(self):
        fields = {'binaries': {'runtime': 'a' * 64}, 'kernel_image_sha256': 'b' * 64, 'fes_revision': 'c' * 40}
        original = FACTORY.build_key(fields)
        for name in fields:
            changed = dict(fields, **{name: 'changed'})
            self.assertNotEqual(FACTORY.build_key(changed), original)

    def test_runtime_lock_uses_fpga_producer_and_selected_payload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); directory, selected = self.fixture(root)
            with mock.patch.object(FACTORY, 'selected', return_value=selected):
                record = tomllib.loads(FACTORY.runtime_lock(directory))
            self.assertEqual(record['mister_runtime']['commit'], 'c' * 40)
            self.assertEqual(record['idle_rbf']['commit'], 'b' * 40)
            self.assertEqual(record['idle_rbf']['sha256'], FACTORY.image.digest(selected[1][0]['directory'] / 'core.rbf'))
            self.assertEqual(record['idle_rbf']['sha256'], record['splash_rbf']['sha256'])
            (selected[1][0]['directory'] / 'manifest.toml').write_text('[build]\nrevision="missing"\n')
            with mock.patch.object(FACTORY, 'selected', return_value=selected), self.assertRaisesRegex(ValueError, 'sealed build'):
                FACTORY.runtime_lock(directory)

    def test_factory_environment_does_not_inherit_other_package_or_board_modules(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); directory, selected = self.fixture(root)
            with mock.patch.object(FACTORY, 'selected', return_value=selected), mock.patch.dict(os.environ, {
                'FES_PONG_PACKAGE_DIR': '/unrelated', 'FES_BLUETOOTH_MODULES_REQUIRED': '1',
                'FES_WINDOW_DRIVER_DIR': '/de10', 'NATIVE_RUNTIME_MODE': 'legacy',
            }):
                env = FACTORY.environment(directory, root / 'FogCast', root / 'cache')
            for name in ('FES_PONG_PACKAGE_DIR', 'FES_BLUETOOTH_MODULES_REQUIRED', 'FES_WINDOW_DRIVER_DIR'):
                self.assertNotIn(name, env)
            self.assertEqual(env['FES_TARGET_BOARD'], 'de25-nano')
            self.assertEqual(env['FES_PACKAGE_IDS'], 'fes.menu')
            self.assertEqual(env['NATIVE_RUNTIME_MODE'], 'package-only')
            self.assertEqual(env['FES_PREBUILT_RUNTIME_SHA256'], 'e' * 64)

    def test_selector_failure_prevents_subsequent_runtime_validation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); directory, selected = self.fixture(root)
            with mock.patch.object(FACTORY, 'selected', return_value=selected), \
                    mock.patch.object(FACTORY.image, 'run', side_effect=subprocess.CalledProcessError(1, ['selector'])) as run:
                with self.assertRaises(subprocess.CalledProcessError):
                    FACTORY.prepare_packages(directory, root / 'FogCast', root / 'cache')
                self.assertEqual(run.call_count, 1)
                self.assertEqual(run.call_args.args[0][:2], [FACTORY.EXTRA, 'fetch'])

    def test_package_cache_reuse_keeps_matching_idle_and_refuses_changed_payload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); directory, selected = self.fixture(root); cache = root / 'cache'
            with mock.patch.object(FACTORY, 'selected', return_value=selected), mock.patch.object(FACTORY.image, 'run'):
                FACTORY.prepare_packages(directory, root / 'FogCast', cache)
                FACTORY.prepare_packages(directory, root / 'FogCast', cache)
                old = (cache / 'idle.rbf').read_bytes()
                (selected[1][0]['directory'] / 'core.rbf').write_bytes(b'changed')
                with self.assertRaisesRegex(ValueError, 'cache idle differs'):
                    FACTORY.prepare_packages(directory, root / 'FogCast', cache)
                self.assertEqual((cache / 'idle.rbf').read_bytes(), old)

    def test_postbuild_does_not_write_success_metadata_after_shared_assembly_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary)
            with mock.patch.object(FACTORY.image, 'run', side_effect=subprocess.CalledProcessError(1, ['factory'])):
                with self.assertRaises(subprocess.CalledProcessError):
                    FACTORY.post_build(target, {'recipe_sha256': FACTORY.recipe_key()})
            self.assertFalse((target / 'usr/share/fes/managed-factory.json').exists())

    def test_missing_factory_inputs_and_verify_overrides_fail_before_any_build(self):
        args = SimpleNamespace(action='managed-factory', kernel=None, sdk=None, artifacts=None)
        with mock.patch.object(FACTORY.image, 'snapshot') as snapshot:
            with self.assertRaisesRegex(ValueError, 'requires --kernel'):
                FACTORY.publish(args)
            args.action, args.artifacts = 'verify-factory', Path('/unselected')
            with self.assertRaisesRegex(ValueError, 'recorded inputs'):
                FACTORY.publish(args)
            snapshot.assert_not_called()

    def test_fetch_only_factory_stage_has_network(self):
        with mock.patch.object(FACTORY.image, 'run') as run:
            for action in ('factory-fetch', 'factory-build', 'factory-verify'):
                FACTORY.image.container('/frozen', 'docker', 'builder', 'own-volume',
                                        '/cache', '/output', action, 2, inputs='/inputs')
                command = run.call_args.args[0]
                self.assertEqual(command[command.index('--network') + 1],
                                 'bridge' if action == 'factory-fetch' else 'none')
                self.assertIn('/frozen:/src:ro', command)
                self.assertIn('/inputs:/inputs:ro', command)
                self.assertNotIn('--privileged', command)


if __name__ == '__main__':
    unittest.main()
