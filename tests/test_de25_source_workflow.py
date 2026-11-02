"""DE25 SDK/kernel/source/factory integration; fixtures grant no package authority."""
from contextlib import ExitStack
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
import native_dev


class DE25SourceWorkflowTests(unittest.TestCase):
    """Filesystem orchestration fixtures; package/SDK/factory authority is mocked."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.revisions = {name: '1' * 40 for name in ('FogCast', 'libmister-runtime', 'misteross', 'mister-packages')}
        for name in self.revisions:
            (self.root / ('sources/' + name)).mkdir(parents=True)
        (self.root / 'profiles').mkdir()
        shutil.copyfile(Path(__file__).resolve().parents[1] / 'profiles/de25-nano.toml',
                        self.root / 'profiles/de25-nano.toml')
        for name in ('build', 'native_dev', 'de25_appliance', 'kit_software', 'kit_sdk',
                     'environment', 'image_artifacts', 'recipes'):
            path = self.root / ('scripts/' + name + '.py')
            path.parent.mkdir(exist_ok=True)
            path.write_text('# fixture recipe\n')
        (self.root / 'config').mkdir()
        (self.root / 'config/core-recipes.toml').write_text('# fixture registry\n')
        inputs = self.root / 'inputs'
        self.base_image = inputs / 'base'
        self.base_image.mkdir(parents=True)
        (self.base_image / 'rootfs.ext4').write_bytes(b'fixture base ABI')
        kernel = inputs / 'kernel'
        kernel.mkdir()
        for name in ('Image', 'de25-owner.dtb'):
            (kernel / name).write_bytes(('fixture ' + name).encode())
        owner_sdk = inputs / 'owner-sdk.tar.gz'
        owner_sdk.write_bytes(b'fixture compiler')
        self.args = SimpleNamespace(action='dev', profile='de25-nano', output=None,
                                    kernel=kernel, sdk=owner_sdk, de25_image=self.base_image)
        self.sdk = {'profile': 'managed-base', 'kernel_image_sha256': build.digest(kernel / 'Image'),
                    'base_rootfs_sha256': build.digest(self.base_image / 'rootfs.ext4'),
                    'builder_image': 'sha256:' + 'b' * 64, 'archive_sha256': 'd' * 64, 'key': 'fixture-sdk'}
        package = inputs / 'package'; package.mkdir()
        (package / 'manifest.toml').write_text('# unqualified DE25 publication fixture\n')
        (package / 'core.rbf').write_bytes(b'unqualified RBF fixture')
        selection = inputs / 'fes-menu.package-selection.toml'
        selection.write_text('core_id="fes.menu"\npackage_id="' + 'a' * 64 + '"\n')
        self.package = {'inputs': {'selection': {'core_id': 'fes.menu', 'package_id': 'a' * 64},
                                  'selection_sha256': build.digest(selection),
                                  'manifest_sha256': build.digest(package / 'manifest.toml'),
                                  'core_rbf_sha256': build.digest(package / 'core.rbf')},
                        'directory': package, 'selection_path': selection}
        self.compiler = inputs / 'compiler'
        for name in ('bin/go', 'pkg/tool/linux_amd64/compile'):
            path = self.compiler / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'fixture Go compiler')
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.patch(build, 'validate', side_effect=lambda *a, **kw: dict(self.revisions))
        self.patch(build, 'git', side_effect=lambda root, command, *a: '' if command == 'status' else '1' * 40)
        self.patch(native_dev.platform, 'system', return_value='Linux')
        self.patch(native_dev.platform, 'machine', return_value='x86_64')
        self.patch(native_dev.shutil, 'which', return_value='/fixture/tool')
        self.disk = self.patch(native_dev.shutil, 'disk_usage', return_value=SimpleNamespace(free=40 * 1024**3))
        self.patch(build, 'run')
        self.resolve = self.patch(build, 'resolve_packages_for_action', return_value=((self.package,), ()))
        self.checkout = self.patch(build, 'source_checkout', return_value=self.root / 'sources/FogCast')
        self.patch(build, 'host_fingerprint', return_value=('host-key', {}))
        self.host = self.patch(build, 'build_host_outputs', side_effect=self.build_host)
        self.patch(native_dev.subprocess, 'check_output', side_effect=lambda args, **kw:
                   str(self.compiler) + '\n' if args[:3] == ['go', 'env', 'GOROOT'] else 'go fixture\n')
        import kit_software
        import module_sources
        from scripts import de25_appliance
        import consistency
        import image_artifacts
        self.patch(kit_software, 'de25_sdk', side_effect=lambda *a: dict(self.sdk))
        self.go = self.patch(kit_software, 'build_go', side_effect=self.build_go)
        self.runtime = self.patch(kit_software, 'build_runtime', side_effect=self.build_runtime)
        self.patch(module_sources, 'materialize', side_effect=self.materialize)
        self.consistency = self.patch(consistency, 'check')
        base = SimpleNamespace(KERNEL_FILES=('Image', 'de25-owner.dtb'),
            owner=SimpleNamespace(selected_kernel=lambda p: None),
            sdk_lock=lambda: {'sdk_sha256': build.digest(owner_sdk), 'builder_image': self.sdk['builder_image']})
        self.patch(de25_appliance, 'modules', return_value=(base,
            SimpleNamespace(recipe_key=lambda root: 'e' * 64, selected=lambda path: (
                json.loads((path / 'artifacts.json').read_text()), image_artifacts.packages_at(
                    path, [self.package['inputs']], board='de25-nano'))), None, None))
        self.patch(image_artifacts, 'pack', side_effect=self.pack)
        self.factory = self.patch(de25_appliance, 'image_action', side_effect=self.build_factory)
        self.verify = self.patch(de25_appliance, 'verified_inputs', side_effect=self.verify_factory)
        self.factory_count = 0

    def patch(self, obj, name, **kwargs):
        return self.stack.enter_context(patch.object(obj, name, **kwargs))

    def materialize(self, root, name, revision, destination):
        for part in self.revisions:
            (destination / ('sources/' + part)).mkdir(parents=True, exist_ok=True)
        return destination / ('sources/' + name)

    def build_go(self, snapshot, version, revision, env, board, *, fogcast_source):
        self.assertEqual(board, 'de25-nano')
        destination = fogcast_source / 'bin'
        destination.mkdir()
        for name in ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot'):
            (destination / (name + '-linux-aarch64')).write_bytes(('fixture ' + name).encode())
        return {'version': 'go fixture', 'go_sha256': build.digest(self.compiler / 'bin/go'),
                'compile_sha256': build.digest(self.compiler / 'pkg/tool/linux_amd64/compile'),
                'target': 'linux/arm64 GOARM64=v8.0 CGO_ENABLED=0'}

    def build_host(self, output, profile, revisions, fingerprint, info, command, env, **kwargs):
        output.mkdir(parents=True, exist_ok=True)
        (output / 'host-fixture').write_bytes(b'unqualified host fixture')
        build.write_receipt(output, 'host', fingerprint, ['host-fixture'])

    def build_runtime(self, snapshot, cache, revision, env, driver, board, **kwargs):
        self.assertEqual((driver, board, kwargs['base_factory'], kwargs['provision']),
                         (None, 'de25-nano', False, True))
        self.assertNotIn('CXXFLAGS', env)
        self.assertNotIn('GOARM', env)
        destination = snapshot / 'image/build/software/runtime/build'
        destination.mkdir(parents=True)
        (destination / 'mister-runtime').write_bytes(('fixture runtime ' + revision).encode())
        return {'key': self.sdk['key'], 'archive_sha256': self.sdk['archive_sha256'],
                'container_id': self.sdk['builder_image'], 'de25_image_sdk': dict(self.sdk)}

    def pack(self, binary, package, selection, fogcast, runtime, output, *, board):
        self.assertEqual(board, 'de25-nano')
        output.mkdir()
        data = {'provenance': {'fogcast': fogcast, 'runtime': runtime},
                'packages': [self.package['inputs']]}
        (output / 'artifacts.json').write_text(json.dumps(data))
        shutil.copytree(package, output / 'core-packages' / ('a' * 64))
        shutil.copyfile(selection, output / 'fes-menu.package-selection.toml')

    def build_factory(self, root, args):
        self.factory_count += 1
        args.output.mkdir()
        (args.output / 'rootfs.ext4').write_bytes(('fixture generation ' + str(self.factory_count)).encode())
        (args.output / 'kernel').mkdir()
        for name in ('Image', 'de25-owner.dtb'):
            shutil.copyfile(args.kernel / name, args.output / 'kernel' / name)
        shutil.copytree(args.artifacts, args.output / 'artifacts')
        names = ('rootfs.ext4', 'kernel/Image', 'kernel/de25-owner.dtb')
        (args.output / 'factory.json').write_text(json.dumps({
            'fes_revision': '1' * 40,
            'files': {name: build.digest(args.output / name) for name in names}}))

    def verify_factory(self, root, profile, directory):
        from scripts.de25_appliance import Factory
        record = json.loads((directory / 'factory.json').read_text())
        for name, expected in record['files'].items():
            self.assertEqual(build.digest(directory / name), expected)
        artifacts = json.loads((directory / 'artifacts/artifacts.json').read_text())
        return (None, None, None, None, None, Factory(directory, record, artifacts, None))

    def run_source(self):
        native_dev.build_de25_development(self.root, self.args)
        return self.root / 'out/de25-nano' / ('development' if self.args.action == 'dev' else '')

    def test_missing_menu_stops_before_sdk_target_sources_and_compilers(self):
        import kit_software
        self.resolve.side_effect = ValueError('no admitted DE25 MENU')
        with patch.object(kit_software, 'de25_sdk') as sdk:
            with self.assertRaisesRegex(ValueError, 'no admitted DE25 MENU'):
                self.run_source()
            sdk.assert_not_called()
        self.checkout.assert_not_called()
        self.go.assert_not_called()
        self.runtime.assert_not_called()
        self.factory.assert_not_called()

    def test_source_build_and_hit_revalidate_without_recompiling(self):
        with patch.dict(os.environ, {'CXXFLAGS': '-DUNREVIEWED', 'GOARM': '5'}):
            output = self.run_source()
            original = (output / 'linux.img').read_bytes()
            self.run_source()
        self.assertEqual((output / 'linux.img').read_bytes(), original)
        self.go.assert_called_once()
        self.runtime.assert_called_once()
        self.factory.assert_called_once()
        self.consistency.assert_called_once()
        self.assertEqual(self.resolve.call_count, 2)
        self.assertTrue(build.reusable(output, 'development', json.loads((output / 'development.json').read_text())['inputs']))
        self.assertFalse((output / 'image.json').exists())

    def test_new_source_keeps_previous_generation_and_failed_build_keeps_receipt(self):
        import kit_software
        output = self.run_source()
        previous = (output / 'development.json').read_bytes()
        first_image = (output / 'linux.img').read_bytes()
        self.revisions['libmister-runtime'] = '2' * 40
        with patch.object(kit_software, 'build_runtime', side_effect=RuntimeError('compile failure')):
            with self.assertRaisesRegex(RuntimeError, 'compile failure'):
                self.run_source()
        self.assertEqual((output / 'development.json').read_bytes(), previous)
        self.assertEqual((output / 'linux.img').read_bytes(), first_image)
        self.run_source()
        self.assertNotEqual((output / 'linux.img').read_bytes(), first_image)
        self.assertEqual(len(list((output / 'generations').glob('*/factory/factory.json'))), 2)

    def test_rebuild_preserves_both_generations_and_hit_uses_the_recorded_one(self):
        self.args.action = 'build'
        output = self.run_source()
        first = (output / 'linux.img').read_bytes()
        self.args.action = 'rebuild'
        self.run_source()
        second = (output / 'linux.img').read_bytes()
        self.assertNotEqual(first, second)
        self.args.action = 'build'
        self.run_source()
        self.assertEqual((output / 'linux.img').read_bytes(), second)
        self.assertEqual(self.factory.call_count, 2)
        self.assertEqual(len(list((output / 'generations').glob('*/factory/factory.json'))), 2)
        self.assertEqual(self.host.call_count, 3)

    def test_resealed_flat_image_still_must_match_verified_factory(self):
        output = self.run_source()
        (output / 'linux.img').write_bytes(b'foreign rootfs')
        path = output / 'development.json'
        receipt = json.loads(path.read_text())
        receipt['files']['linux.img'] = build.digest(output / 'linux.img')
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, 'differs from its verified factory'):
            self.run_source()
        self.factory.assert_called_once()

    def test_kernel_mismatch_and_busy_workspace_stop_before_build(self):
        self.sdk['kernel_image_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'SDK and selected kernel differ'):
            self.run_source()
        self.go.assert_not_called()
        self.runtime.assert_not_called()
        self.sdk['kernel_image_sha256'] = build.digest(self.args.kernel / 'Image')
        with (self.root / 'out/build.lock').open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            before = self.resolve.call_count
            with self.assertRaisesRegex(ValueError, 'another parent build'):
                self.run_source()
            self.assertEqual(self.resolve.call_count, before)

    def test_low_disk_stops_before_package_or_source_work(self):
        self.disk.return_value = SimpleNamespace(free=29 * 1024**3)
        with self.assertRaisesRegex(ValueError, 'at least 30 GiB free'):
            self.run_source()
        self.resolve.assert_not_called()
        self.checkout.assert_not_called()
        self.go.assert_not_called()

    def test_verification_and_acceptance_read_the_published_selection_without_production(self):
        from scripts import target_acceptance
        output = self.run_source()
        before = {p.relative_to(output): (p.read_bytes(), p.stat().st_ino, p.stat().st_mtime_ns)
                  for p in output.rglob('*') if p.is_file()}
        with patch.object(build, 'resolve_packages_for_action', side_effect=AssertionError('must not resolve')), \
                patch.object(build, 'source_checkout', side_effect=AssertionError('must not stage')):
            native_dev.verify_de25_development(self.root, output)
        runner = object.__new__(target_acceptance.Runner)
        runner.selection_dir = output
        declared = runner.declared_package_ids()
        self.assertEqual(declared, {'fes.menu': 'a' * 64})
        runner.validate_declared_selections(declared)
        self.assertEqual(before, {p.relative_to(output): (p.read_bytes(), p.stat().st_ino, p.stat().st_mtime_ns)
                                 for p in output.rglob('*') if p.is_file()})
        self.go.assert_called_once(); self.runtime.assert_called_once(); self.factory.assert_called_once()

    def test_stale_source_recipe_and_forged_base_key_refuse_before_factory_work(self):
        output = self.run_source()
        original = (output / 'inputs.json').read_bytes()
        checks = self.verify.call_count
        for field, value, reason in (('board', 'de10-nano', 'selected source or recipe'),
                                    ('sources', {}, 'selected source or recipe'),
                                    ('profile', {}, 'selected source or recipe'),
                                    ('recipe', {}, 'selected source or recipe'),
                                    ('image_base_fingerprint', 'f' * 64, 'source fingerprint')):
            with self.subTest(field=field):
                info = json.loads(original); info[field] = value
                (output / 'inputs.json').write_text(json.dumps(info))
                with self.assertRaisesRegex(ValueError, reason):
                    native_dev.verify_de25_development(self.root, output)
        self.assertEqual(checks, self.verify.call_count)

    def test_verification_rejects_ambiguous_receipts_and_escaping_or_linked_generations(self):
        output = self.run_source()
        original = (output / 'inputs.json').read_bytes()
        (output / 'image.json').write_text('{}')
        with self.assertRaisesRegex(ValueError, 'one image or development receipt'):
            native_dev.verify_de25_development(self.root, output)
        (output / 'image.json').unlink()
        for path in ('../factory', '/foreign/factory', 'generations/foreign/factory', 3):
            with self.subTest(path=path):
                info = json.loads(original); info['factory'] = path
                (output / 'inputs.json').write_text(json.dumps(info))
                with self.assertRaisesRegex(ValueError, 'factory path'):
                    native_dev.verify_de25_development(self.root, output)
        (output / 'inputs.json').write_bytes(original)
        generation = output / Path(json.loads(original)['factory']).parent
        moved = output / 'moved'; generation.rename(moved)
        generation.symlink_to(moved, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'factory path'):
            native_dev.verify_de25_development(self.root, output)

    def test_omitted_package_receipt_and_resealed_foreign_package_are_rejected(self):
        output = self.run_source()
        path = output / 'development.json'; original = path.read_bytes()
        record = json.loads(original)
        del record['files']['fes-menu.package-selection.toml']
        path.write_text(json.dumps(record))
        with self.assertRaisesRegex(ValueError, 'complete output selection'):
            native_dev.verify_de25_development(self.root, output)
        foreign = output / 'core-packages' / ('a' * 64) / 'core.rbf'
        foreign.chmod(0o644); foreign.write_bytes(b'foreign FPGA bytes')
        record = json.loads(original)
        record['files'][str(foreign.relative_to(output))] = build.digest(foreign)
        path.write_text(json.dumps(record))
        with self.assertRaisesRegex(ValueError, 'published FES package changed'):
            native_dev.verify_de25_development(self.root, output)

    def test_cold_verification_requires_matching_host_and_package_failure_keeps_old_outputs(self):
        self.args.action = 'build'
        output = self.run_source()
        native_dev.verify_de25_development(self.root, output)
        (output / 'host-fixture').write_bytes(b'foreign host')
        with self.assertRaisesRegex(ValueError, 'matching verified host'):
            native_dev.verify_de25_development(self.root, output)
        old = {name: (output / name).read_bytes() for name in ('linux.img', 'image.json', 'inputs.json')}
        self.revisions['libmister-runtime'] = '2' * 40
        with patch.object(build, 'publish_package_state', side_effect=OSError('package publication failed')):
            with self.assertRaisesRegex(OSError, 'package publication failed'):
                self.run_source()
        self.assertEqual(old, {name: (output / name).read_bytes() for name in old})


if __name__ == '__main__':
    unittest.main()
