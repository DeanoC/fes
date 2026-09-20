"""Development caches must survive app edits but reject base changes."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
import native_dev


class NativeDevTest(unittest.TestCase):
    def test_verification_record_binds_qemu_log_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'qemu-smoke.log').write_bytes(b'qemu passed\n')
            image_sha256 = hashlib.sha256(b'cold').hexdigest()
            record = build.verification_record(output, image_sha256, None)
            self.assertEqual(record['image_sha256'], image_sha256)
            self.assertEqual(record['qemu_log_sha256'],
                             hashlib.sha256(b'qemu passed\n').hexdigest())

    def test_base_key_tracks_recipes_not_application_revision(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            image = root / 'image'
            fogcast = root / 'fogcast'
            subprocess.run(['git', 'init', '-q', str(image)], check=True)
            files = {
                'buildroot/configs/native': 'base',
                'containers/target-image/Dockerfile': 'compiler',
                'build/target-image.sources.lock.toml': 'sources',
                'build/target-image-container-packages.sha256': 'packages',
                'scripts/build-target-image.sh': 'recipe',
                'Makefile': 'make',
            }
            for name, data in files.items():
                path = image / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(data)
            (fogcast / 'build').mkdir(parents=True)
            (fogcast / 'cmd/mister-agent').mkdir(parents=True)
            (fogcast / 'cmd/mister-agent/main.go').write_text('agent')
            (image / 'build/native-inputs.toml').write_text(
                '[mister_runtime]\ncommit="old"\nrepository="repo"\n')
            subprocess.run(['git', '-C', str(image), 'add', '.'], check=True)
            before = native_dev.base_key(image, fogcast)
            (fogcast / 'cmd/mister-agent/main.go').write_text('changed')
            (image / 'build/native-inputs.toml').write_text(
                '[mister_runtime]\ncommit="new"\nrepository="repo"\n')
            self.assertEqual(before, native_dev.base_key(image, fogcast))
            for name in ('buildroot/configs/native', 'containers/target-image/Dockerfile',
                         'build/target-image.sources.lock.toml',
                         'build/target-image-container-packages.sha256',
                         'scripts/build-target-image.sh', 'Makefile'):
                with self.subTest(name=name):
                    original = (image / name).read_bytes()
                    (image / name).write_bytes(b'changed base')
                    self.assertNotEqual(before, native_dev.base_key(image, fogcast))
                    (image / name).write_bytes(original)
            path = image / 'buildroot/new-patch'
            path.write_text('new')
            subprocess.run(['git', '-C', str(image), 'add', str(path)], check=True)
            self.assertNotEqual(before, native_dev.base_key(image, fogcast))

            non_build_test = image / 'scripts/tests/diagnostic_test.sh'
            non_build_test.parent.mkdir(parents=True)
            non_build_test.write_text('test')
            subprocess.run(['git', '-C', str(image), 'add', str(non_build_test)], check=True)
            filtered = native_dev.base_key(image, fogcast)
            non_build_test.write_text('changed test')
            self.assertEqual(filtered, native_dev.base_key(image, fogcast))

    def test_seed_requires_matching_sources_and_intact_cold_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            info = {'sources': {'FogCast': 'abc'}, 'profile': {'version': '1'}, 'go': 'go1'}
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'cold')
            sha = build.digest(output / 'linux.img')
            (output / 'reproducibility.txt').write_text(
                f'run_1_sha256={sha}\nrun_2_sha256={sha}\n')
            build.write_receipt(output, 'image', hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest(),
                                ['linux.img', 'reproducibility.txt'])
            self.assertEqual(native_dev.seed_digest(output, info), sha)
            changed = dict(info, sources={'FogCast': 'different'})
            self.assertIsNone(native_dev.seed_digest(output, changed))
            (output / 'inputs.json').write_text(json.dumps(changed))
            self.assertIsNone(native_dev.seed_digest(output, changed),
                              'a host-only update must not relabel an old image seed')
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'changed')
            self.assertIsNone(native_dev.seed_digest(output, info))

    def test_seed_accepts_bound_derived_package_fingerprint(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            base_info = {'sources': {'FogCast': 'abc'}, 'profile': {'version': '1'}, 'go': 'go1'}
            package = {'inputs': {'selection': {'package_id': 'a' * 64},
                                  'selection_sha256': 'b' * 64,
                                  'manifest_sha256': 'c' * 64,
                                  'core_rbf_sha256': 'd' * 64}}
            fingerprint, info = build.image_fingerprint('base', base_info, package)
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'cold')
            sha = build.digest(output / 'linux.img')
            (output / 'reproducibility.txt').write_text(
                f'run_1_sha256={sha}\nrun_2_sha256={sha}\n')
            build.write_receipt(output, 'image', fingerprint,
                                ['linux.img', 'reproducibility.txt', 'inputs.json'])
            self.assertEqual(native_dev.seed_digest(output, info), sha)

    def test_unchanged_development_output_skips_build_tools(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / 'out/native-integration-dev/development'
            output.mkdir(parents=True)
            (output / 'linux.img').write_bytes(b'dev')
            build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
            with patch.object(native_dev, 'run', side_effect=AssertionError('unexpected rebuild')), \
                 patch.object(build, 'digest', wraps=build.digest) as digest:
                native_dev.build_development(root, None, None, None, 'native-integration-dev',
                    {'bundle_interface': 'selection'}, {}, 'same-inputs', {}, [], [], None)
            self.assertEqual(digest.call_count, 1, 'cached image must be hashed only once')

    def test_development_miss_preserves_reason_before_rebuild_setup(self):
        for changed, fingerprint, reason in (
                (False, 'new-inputs', 'selected inputs changed'),
                (True, 'same-inputs', 'output missing or digest changed')):
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                output = root / 'out/native-integration-dev/development'
                output.mkdir(parents=True)
                (output / 'linux.img').write_bytes(b'dev')
                build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
                if changed:
                    (output / 'linux.img').write_bytes(b'corrupted')
                # Stop before external build setup; receipt checking and reporting stay real.
                package = {
                    'directory': '/package',
                    'selection_path': '/selection',
                    'inputs': {
                        'selection': {'package_id': 'a' * 64, 'core_id': 'fes.pong'},
                    },
                }
                with patch.object(native_dev, 'base_key', side_effect=RuntimeError('stop setup')), \
                     self.assertRaisesRegex(RuntimeError, 'stop setup'):
                    native_dev.build_development(root, None, None, None,
                        'native-integration-dev', {'bundle_interface': 'selection'}, {},
                        fingerprint, {}, [], [], package)
                report = json.loads((output / 'build-diagnostics.json').read_text())
                self.assertIn({'name': 'development', 'status': 'miss', 'reason': reason},
                              report['stages'])

    def test_package_only_development_passes_no_legacy_inputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            image = root / 'image'
            fogcast = root / 'FogCast'
            (image / 'build').mkdir(parents=True)
            (image / 'scripts').mkdir()
            (image / 'scripts/build-target-image.sh').write_text('epoch=1234567890\n')
            (image / 'build/target-image.sources.lock.toml').write_text(
                '[container]\nimage="base"\ndigest="sha256:abc"\nplatform="linux/amd64"\n')
            built = image / 'build/output/target-image/fes-development'
            built.mkdir(parents=True)
            for name in ('linux.img', 'manifest.tsv', 'library-report.tsv'):
                (built / name).write_text(name)
            package_id = 'a' * 64
            package_source = root / 'package'
            package_source.mkdir()
            (package_source / 'manifest.toml').write_bytes(b'manifest')
            (package_source / 'core.rbf').write_bytes(b'package-rbf')
            package_selection = root / 'fes-pong.package-selection.toml'
            package_selection.write_bytes(b'format = 2\n')
            (built / 'fes-pong.package-selection.toml').write_bytes(
                package_selection.read_bytes())
            package = {
                'directory': package_source,
                'selection_path': package_selection,
                'inputs': {
                    'selection': {'package_id': package_id, 'core_id': 'fes.pong'},
                    'selection_sha256': build.digest(package_selection),
                    'manifest_sha256': build.digest(package_source / 'manifest.toml'),
                    'core_rbf_sha256': build.digest(package_source / 'core.rbf'),
                },
            }
            profile = {'native_image_mode': 'package-only'}
            shared_env = None
            native_boundary_envs = []

            def record_run(args, **kwargs):
                nonlocal shared_env
                arg_text = [str(argument) for argument in args]
                if 'target-image-native-fetch' in arg_text:
                    shared_env = kwargs['env']
                is_native_container = (len(args) >= 2 and
                                       str(args[0]).endswith('target-image-container.sh') and
                                       args[1] == 'run')
                is_native_verifier = str(args[0]).endswith('verify-target-image.sh')
                if is_native_container or is_native_verifier:
                    native_boundary_envs.append(
                        (kwargs['env'] is not shared_env,
                         kwargs['env'].get('NATIVE_RUNTIME_MODE')))

            with patch.object(native_dev, 'base_key', return_value='base'), \
                 patch.object(native_dev, 'seed_base'), \
                 patch.object(native_dev, 'git', return_value='a' * 40), \
                 patch.object(native_dev.subprocess, 'run',
                              return_value=subprocess.CompletedProcess([], 0)), \
                 patch.object(native_dev, 'run', side_effect=record_run) as run:
                native_dev.build_development(
                    root, image, fogcast, root / 'runtime', 'native-integration-dev',
                    profile, {}, 'candidate',
                    {'TARGET_IMAGE_CONTAINER_RUNTIME': 'docker'}, ['make'], ['make'],
                    package)

            calls = [call.args[0] for call in run.call_args_list]
            self.assertEqual(native_boundary_envs,
                             [(True, 'package-only'), (True, 'package-only')])
            self.assertTrue(any('target-image-native-fetch' in call for call in calls))
            self.assertTrue(any('NATIVE_RUNTIME_MODE=package-only' in call for call in calls))
            self.assertTrue(any('FES_PONG_PACKAGE_DIR=' + str(package_source) in call
                                for call in calls))
            self.assertTrue(any('FES_PONG_PACKAGE_SELECTION=' + str(package_selection) in call
                                for call in calls))
            output = root / 'out/native-integration-dev/development'
            self.assertTrue((output / 'fes-pong.package-selection.toml').is_file())

    def test_package_only_development_publishes_the_complete_package_tuple(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            image = root / 'image'
            fogcast = root / 'FogCast'
            (image / 'build').mkdir(parents=True)
            (image / 'scripts').mkdir()
            (image / 'scripts/build-target-image.sh').write_text('epoch=1234567890\n')
            (image / 'build/target-image.sources.lock.toml').write_text(
                '[container]\nimage="base"\ndigest="sha256:abc"\nplatform="linux/amd64"\n')
            built = image / 'build/output/target-image/fes-development'
            built.mkdir(parents=True)
            for name in ('linux.img', 'manifest.tsv', 'library-report.tsv'):
                (built / name).write_text(name)
            packages = []
            for index, (core_id, selection_name, package_id) in enumerate((
                    ('fes.pong', 'fes-pong.package-selection.toml', 'a' * 64),
                    ('fes.zx81', 'fes-zx81.package-selection.toml', 'b' * 64))):
                package_source = root / core_id
                package_source.mkdir()
                (package_source / 'manifest.toml').write_bytes(f'manifest-{index}'.encode())
                (package_source / 'core.rbf').write_bytes(f'payload-{index}'.encode())
                package_selection = root / selection_name
                package_selection.write_bytes(f'format = 2\ncore = "{core_id}"\n'.encode())
                (built / selection_name).write_bytes(package_selection.read_bytes())
                packages.append({
                    'directory': package_source,
                    'selection_path': package_selection,
                    'inputs': {
                        'selection': {'package_id': package_id, 'core_id': core_id},
                        'selection_sha256': build.digest(package_selection),
                        'manifest_sha256': build.digest(package_source / 'manifest.toml'),
                        'core_rbf_sha256': build.digest(package_source / 'core.rbf'),
                    },
                })
            packages = tuple(packages)
            profile = {'native_image_mode': 'package-only'}
            calls = []
            native_container_envs = []

            def record_run(args, **kwargs):
                if (len(args) >= 2 and
                        str(args[0]).endswith('target-image-container.sh') and
                        args[1] in ('fetch', 'run')):
                    native_container_envs.append(dict(kwargs['env']))
                calls.append((args, kwargs))

            with patch.object(native_dev, 'base_key', return_value='base'), \
                 patch.object(native_dev, 'seed_base'), \
                 patch.object(native_dev, 'git', return_value='a' * 40), \
                 patch.object(native_dev.subprocess, 'run',
                              return_value=subprocess.CompletedProcess([], 0)), \
                 patch.object(native_dev, 'run', side_effect=record_run):
                native_dev.build_development(
                    root, image, fogcast, root / 'runtime', 'native-integration-dev',
                    profile, {}, 'candidate',
                    {'TARGET_IMAGE_CONTAINER_RUNTIME': 'docker'}, ['make'], ['make'],
                    packages)

            flattened = [' '.join(str(value) for value in args) for args, _ in calls]
            self.assertTrue(any('FES_PACKAGE_IDS=fes.pong,fes.zx81' in call for call in flattened))
            self.assertTrue(any('FES_PONG_PACKAGE_DIR=' + str(root / 'fes.pong') in call
                                for call in flattened))
            self.assertTrue(any('FES_ZX81_PACKAGE_DIR=' + str(root / 'fes.zx81') in call
                                for call in flattened))
            self.assertGreaterEqual(len(native_container_envs), 2)
            for env in native_container_envs:
                self.assertEqual(env['FES_PACKAGE_IDS'], 'fes.pong,fes.zx81')
                self.assertEqual(env['FES_PONG_PACKAGE_DIR'], str(root / 'fes.pong'))
                self.assertEqual(env['FES_ZX81_PACKAGE_DIR'], str(root / 'fes.zx81'))
            output = root / 'out/native-integration-dev/development'
            self.assertEqual(build.verify_package_outputs(output, packages), [
                'fes-pong.package-selection.toml',
                'core-packages/' + 'a' * 64 + '/manifest.toml',
                'core-packages/' + 'a' * 64 + '/core.rbf',
                'fes-zx81.package-selection.toml',
                'core-packages/' + 'b' * 64 + '/manifest.toml',
                'core-packages/' + 'b' * 64 + '/core.rbf',
            ])

    def test_development_receipt_cannot_satisfy_release_reuse(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'linux.img').write_bytes(b'dev')
            build.write_receipt(output, 'development', 'same-inputs', ['linux.img'])
            self.assertFalse(build.reusable(output, 'image', 'same-inputs'))


if __name__ == '__main__':
    unittest.main()
