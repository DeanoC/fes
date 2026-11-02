"""Development caches must survive app edits but reject base changes."""
import hashlib
import fcntl
from contextlib import ExitStack
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
import native_dev


class NativeDevTest(unittest.TestCase):
    def test_verification_record_binds_qemu_log_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / 'qemu-smoke.log').write_bytes(b'qemu passed\n')
            (output / 'reproducibility.txt').write_text(
                f"toolchain_key={'a' * 64}\ntoolchain_sha256={'b' * 64}\n")
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
                'licenses/fes.ramtest/COPYING': 'license',
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
                         'scripts/build-target-image.sh', 'licenses/fes.ramtest/COPYING', 'Makefile'):
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
            (output / 'reproducibility.txt').write_text(f'image_passes=1\nrun_1_sha256={sha}\n')
            build.write_receipt(output, 'image', hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest(),
                                ['linux.img', 'reproducibility.txt'])
            self.assertEqual(native_dev.seed_digest(output, info), sha,
                             'one-pass cold output should seed the incremental base too')

            changed = dict(info, sources={'FogCast': 'different'})
            self.assertIsNone(native_dev.seed_digest(output, changed))
            (output / 'inputs.json').write_text(json.dumps(changed))
            self.assertIsNone(native_dev.seed_digest(output, changed),
                              'a host-only update must not relabel an old image seed')
            (output / 'inputs.json').write_text(json.dumps(info))
            (output / 'linux.img').write_bytes(b'changed')
            self.assertIsNone(native_dev.seed_digest(output, info))

    def test_seed_copies_only_the_selected_pass_tree(self):
        # Buildroot host tools embed their output path; never relocate work-1 to work-2.
        sha = hashlib.sha256(b'cold').hexdigest()
        for trees, passes, origin in (
                ({1: b'cold', 2: b'cold'}, 2, 'work-2-native-dev'),
                ({1: b'cold', 2: b'cold'}, 1, 'work-1-native-dev'),
                ({1: b'cold'}, 1, 'work-1-native-dev'),
                ({1: b'cold'}, 2, None),
                ({1: b'cold', 2: b'stale'}, 2, None)):
            with self.subTest(trees=sorted(trees), passes=passes), tempfile.TemporaryDirectory() as tmp:
                seed, dest = Path(tmp) / 'seed', Path(tmp) / 'dest'
                dest.mkdir()
                for run_number, rootfs in trees.items():
                    work = seed / Path(native_dev.WORK.format(passes=run_number)).name
                    for name in ('host', 'build', 'target', 'images'):
                        (work / name).mkdir(parents=True)
                    (work / '.config').write_text('config')
                    (work / 'images/rootfs.ext4').write_bytes(rootfs)
                    (work / 'host/origin').write_text(work.name)
                absent = {'dev': 1, 'cold': 0}
                with patch.object(native_dev.subprocess, 'run', side_effect=lambda args, **_:
                                  subprocess.CompletedProcess(args, absent[args[-1]])), \
                     patch.object(native_dev, 'run') as run:
                    native_dev.seed_base('docker', {'platform': 'linux/amd64', 'image': 'base',
                                                    'digest': 'sha256:abc'}, 'cold', 'dev', sha,
                                         native_dev.WORK.format(passes=passes))
                args = [str(arg) for arg in run.call_args.args[0]]
                script = args[args.index('-c') + 1].replace('/dest', str(dest))
                script = script.replace('/seed/', f'{seed}/')
                subprocess.run(['sh', '-c', script, *args[args.index('-c') + 2:]], check=True,
                               capture_output=True)
                selected = Path(native_dev.WORK.format(passes=passes)).name
                if origin is None:
                    self.assertEqual(list(dest.iterdir()), [])
                else:
                    self.assertEqual([path.name for path in dest.iterdir()], [selected])
                    self.assertEqual((dest / selected / 'host/origin').read_text(), origin)

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
            shared_cache_envs = []
            locked_fetches = []

            def record_run(args, **kwargs):
                nonlocal shared_env
                arg_text = [str(argument) for argument in args]
                if 'target-image-native-fetch' in arg_text:
                    shared_env = kwargs['env']
                is_native_container = (len(args) >= 2 and
                                       str(args[0]).endswith('target-image-container.sh') and
                                       args[1] == 'run')
                is_source_fetch = (len(args) >= 3 and
                                   str(args[0]).endswith('target-image-container.sh') and
                                   args[1:3] == ['fetch', '/work/scripts/fetch-target-image-sources.sh'])
                if is_source_fetch:
                    lock_path = image / 'build/cache/.kit-sdk-sources.lock'
                    with lock_path.open('a') as competing_lock:
                        with self.assertRaises(BlockingIOError):
                            fcntl.flock(competing_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    locked_fetches.append(lock_path)
                is_native_verifier = str(args[0]).endswith('verify-target-image.sh')
                if is_native_container or is_native_verifier:
                    native_boundary_envs.append(
                        (kwargs['env'] is not shared_env,
                         kwargs['env'].get('NATIVE_RUNTIME_MODE')))
                if is_native_container or '--ensure-toolchain' in arg_text:
                    shared_cache_envs.append(kwargs['env'].get('FES_TARGET_IMAGE_SHARED_CACHE'))

            with patch.object(native_dev, 'base_key', return_value='base'), \
                 patch.object(native_dev, 'seed_base'), \
                 patch.object(native_dev.platform, 'build_kit_software'), \
                 patch.object(native_dev, 'git', return_value='a' * 40), \
                 patch.object(native_dev.subprocess, 'run',
                              return_value=subprocess.CompletedProcess([], 0)), \
                 patch.object(native_dev, 'run', side_effect=record_run) as run:
                native_dev.build_development(
                    root, image, fogcast, root / 'runtime', 'native-integration-dev',
                    profile, {}, 'candidate',
                    {'TARGET_IMAGE_CONTAINER_RUNTIME': 'docker',
                     'FES_TARGET_IMAGE_SHARED_CACHE': '/external/shared-cache'}, ['make'], ['make'],
                    package)

            calls = [call.args[0] for call in run.call_args_list]
            self.assertEqual(native_boundary_envs,
                             [(True, 'package-only'), (True, 'package-only')])
            self.assertEqual(shared_cache_envs, ['/external/shared-cache', '/external/shared-cache'])
            self.assertEqual(locked_fetches, [image / 'build/cache/.kit-sdk-sources.lock'])
            self.assertTrue(any('target-image-native-fetch' in call for call in calls))
            self.assertTrue(any('NATIVE_RUNTIME_MODE=package-only' in call for call in calls))
            self.assertTrue(any('FES_PONG_PACKAGE_DIR=' + str(package_source) in call
                                for call in calls))
            # make dev must use the generated external config and the cached SDK, never
            # the bare defconfig (the toolchain options live only in the shared fragment).
            ensure = [index for index, call in enumerate(calls)
                      if [str(part) for part in call[1:]] == ['--ensure-toolchain', 'native-dev']]
            container = [index for index, call in enumerate(calls)
                         if str(call[0]).endswith('target-image-container.sh') and call[1] == 'run']
            self.assertEqual(len(ensure), 1)
            self.assertLess(ensure[0], container[0])
            script = calls[container[0]][-1]
            self.assertNotIn('fogcast_target_native_dev_defconfig', script)
            self.assertIn('toolchain_cache.py config external "$WORK/fogcast.generated.defconfig"', script)
            self.assertIn('BR2_DEFCONFIG="$WORK/fogcast.generated.defconfig" defconfig', script)
            self.assertIn('toolchain_cache.py validate-config external "$WORK/.config"', script)
            self.assertLess(script.index('relocate-sdk.sh'), script.index('defconfig\n'))
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
                 patch.object(native_dev.platform, 'build_kit_software'), \
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
                    'base_rootfs_sha256': build.digest(self.base_image / 'rootfs.ext4')}
        self.package = {'inputs': {'selection': {'core_id': 'fes.menu', 'package_id': 'a' * 64}},
                        'directory': inputs / 'package', 'selection_path': inputs / 'selection.toml'}
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
        self.host = self.patch(build, 'build_host_outputs')
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
            sdk_lock=lambda: {'sdk_sha256': build.digest(owner_sdk)})
        self.patch(de25_appliance, 'modules', return_value=(base,
            SimpleNamespace(recipe_key=lambda root: 'e' * 64), None, None))
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
        return {'fixture': 'Go; no CPU execution'}

    def build_runtime(self, snapshot, cache, revision, env, driver, board, **kwargs):
        self.assertEqual((driver, board, kwargs['base_factory'], kwargs['provision']),
                         (None, 'de25-nano', False, True))
        self.assertNotIn('CXXFLAGS', env)
        self.assertNotIn('GOARM', env)
        destination = snapshot / 'image/build/software/runtime/build'
        destination.mkdir(parents=True)
        (destination / 'mister-runtime').write_bytes(('fixture runtime ' + revision).encode())
        return {'fixture': 'runtime; no compiler execution'}

    def pack(self, binary, package, selection, fogcast, runtime, output, *, board):
        self.assertEqual(board, 'de25-nano')
        output.mkdir()
        data = {'provenance': {'fogcast': fogcast, 'runtime': runtime},
                'packages': [self.package['inputs']]}
        (output / 'artifacts.json').write_text(json.dumps(data))

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
        output = self.run_source()
        previous = (output / 'development.json').read_bytes()
        first_image = (output / 'linux.img').read_bytes()
        self.revisions['libmister-runtime'] = '2' * 40
        with patch.object(self.runtime, 'side_effect', RuntimeError('compile failure')):
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


if __name__ == '__main__':
    unittest.main()
