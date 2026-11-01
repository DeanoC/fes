import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch, Mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('kit_software', ROOT / 'scripts/kit_software.py')
kit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(kit)


class SoftwareWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.base = self.root / 'base.img'
        self.base.write_bytes(b'authenticated image fixture')

    def test_standalone_producer_bytes_do_not_invalidate_cold_recipe(self):
        # Load the real cold-build recipe selector in a private filesystem,
        # retaining its registry input. Change actual file bytes, not a mocked
        # hash, and use an ordinary recipe script as the positive control.
        scripts = self.root / 'scripts'; scripts.mkdir()
        shutil.copyfile(ROOT / 'scripts/build.py', scripts / 'build.py')
        producer = scripts / 'kit_software.py'; producer.write_text('producer version one')
        consumed = scripts / 'consumed.py'; consumed.write_text('image recipe one')
        registry = self.root / 'config/core-recipes.toml'
        registry.parent.mkdir(); registry.write_text('format = 1')
        with patch('recipes.REGISTRY_PATH', registry):
            cold = kit.load(scripts / 'build.py', 'isolated_cold_build')
        before = cold.recipe_fingerprint(cold.BUILD_RECIPE_FILES)
        self.assertNotIn('scripts/kit_software.py', before)
        self.assertIn('config/core-recipes.toml', before)
        producer.write_text('producer version two with different build logic')
        self.assertEqual(before, cold.recipe_fingerprint(cold.BUILD_RECIPE_FILES))
        consumed.write_text('image recipe two')
        self.assertNotEqual(before, cold.recipe_fingerprint(cold.BUILD_RECIPE_FILES))

    def test_dirty_tracked_and_untracked_selected_sources_fail_before_build(self):
        for status in (' M sources/FogCast/main.go', '?? sources/libmister-runtime/new.cpp'):
            with self.subTest(status=status):
                def git(args, **kwargs):
                    if 'status' in args:
                        self.assertIn('--untracked-files=all', args)
                        self.assertEqual(args[-3:], ['sources/FogCast', 'sources/libmister-runtime', 'image'])
                        return status
                    return 'b' * 40
                with patch.object(kit, 'run', side_effect=git), \
                     patch.object(kit, 'build_go') as go, \
                     patch.object(kit, 'build_runtime') as runtime, \
                     patch.object(kit, 'load') as sdk:
                    with self.assertRaisesRegex(ValueError, 'uncommitted changes'):
                        kit.publish(self.root, self.base, kit.sha(self.base), 'v1', self.root / 'release')
                go.assert_not_called()
                runtime.assert_not_called()
                sdk.assert_not_called()
                self.assertFalse((self.root / 'release').exists())

    def test_image_digest_rejected_before_build_or_publication(self):
        with patch.object(kit, 'build_go') as build:
            with self.assertRaisesRegex(ValueError, 'reviewed SHA'):
                kit.publish(self.root, self.base, 'a' * 64, 'v1', self.root / 'release')
            build.assert_not_called()
        self.assertEqual(self.base.read_bytes(), b'authenticated image fixture')
        self.assertFalse((self.root / 'release').exists())

    def test_nonregular_base_rejected_without_opening_fifo(self):
        fifo = self.root / 'fifo'
        os.mkfifo(fifo)
        with self.assertRaisesRegex(ValueError, 'regular file'):
            kit.publish(self.root, fifo, 'a' * 64, 'v1', self.root / 'release')

    def test_existing_output_untouched(self):
        out = self.root / 'release'
        out.mkdir()
        (out / 'bundle.tar').write_bytes(b'accepted previous release')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            kit.publish(self.root, self.base, kit.sha(self.base), 'v1', out)
        self.assertEqual((out / 'bundle.tar').read_bytes(), b'accepted previous release')

    def test_publication_cannot_replace_concurrent_empty_or_nonempty_directory(self):
        for nonempty in (False, True):
            source = self.root / ('staged' + str(nonempty))
            source.mkdir()
            (source / 'bundle.tar').write_bytes(b'new')
            destination = self.root / ('winner' + str(nonempty))
            destination.mkdir()
            if nonempty:
                (destination / 'bundle.tar').write_bytes(b'winner')
            with self.assertRaises(FileExistsError):
                kit.publish_directory(source, destination)
            self.assertEqual((source / 'bundle.tar').read_bytes(), b'new')
            if nonempty:
                self.assertEqual((destination / 'bundle.tar').read_bytes(), b'winner')
            else:
                self.assertEqual(list(destination.iterdir()), [])

    def test_publication_moves_complete_pair(self):
        source = self.root / 'staged'; source.mkdir()
        (source / 'bundle.tar').write_bytes(b'bundle')
        (source / 'receipt.json').write_text('{"deployed": false}')
        destination = self.root / 'published'
        kit.publish_directory(source, destination)
        self.assertEqual(set(p.name for p in destination.iterdir()), {'bundle.tar', 'receipt.json'})
        self.assertFalse(source.exists())

    def test_go_builds_only_three_target_applications_and_host_helper(self):
        (self.root / 'sources/FogCast').mkdir(parents=True)
        calls = []
        goroot = self.root / 'go'
        for name in ('bin/go', 'pkg/tool/linux_amd64/compile'):
            path = goroot / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(b'compiler')
        def builder(args, **kw):
            calls.append((args, kw))
            if args[:2] == ['go', 'build']:
                Path(args[args.index('-o') + 1]).write_bytes(kw['env']['GOARCH'].encode())
            if args == ['go', 'env', 'GOROOT']:
                return str(goroot)
            return 'go version go1.26.5 linux/amd64'
        with patch.object(kit, 'run', side_effect=builder):
            kit.build_go(self.root, 'v1', 'b' * 40, {})
        self.assertEqual([c[0][-1] for c in calls[:4]],
                         ['./cmd/mister-agent', './cmd/fogcast-kit', './cmd/fogcast-tenfoot', './cmd/fes-kit-bundle'])
        self.assertEqual([c[1]['env']['GOARCH'] for c in calls[:4]], ['arm', 'arm', 'arm', 'amd64'])
        for args, kw in calls[:4]:
            self.assertIn('-trimpath', args)
            self.assertIn('-buildvcs=false', args)
            self.assertEqual(kw['env']['CGO_ENABLED'], '0')
            self.assertIn('Revision=' + 'b' * 40, args[args.index('-ldflags') + 1])
        self.assertEqual((self.root / 'sources/FogCast/bin/kit-software.revision').read_text(), 'b'*40+'\n')

    DRIVER = {'kernel_release': '5.15.1-MiSTer'}

    def test_runtime_script_uses_private_sdk_and_no_image_build(self):
        script = kit.runtime_script('b' * 40, self.DRIVER)
        self.assertIn('extract /work/build/software/sdk', script)
        self.assertIn('/work/build/software/sdk/host/relocate-sdk.sh', script)
        self.assertIn('make -C /work/build/software/runtime', script)
        self.assertIn('CXX="$prefix-g++" AR="$prefix-ar" NM="$prefix-nm"', script)
        self.assertIn('MISTER_RUNTIME_VERSION=git-' + 'b' * 40, script)
        self.assertNotIn('build-target-image', script)
        self.assertNotIn('zImage', script)
        self.assertNotIn('/target-image-output/external-toolchain', script)
        with self.assertRaises(ValueError):
            kit.runtime_script('$(touch injected)', self.DRIVER)

    def test_runtime_script_builds_window_driver_for_pinned_kernel(self):
        script = kit.runtime_script('b' * 40, self.DRIVER)
        build = ('/work/scripts/build-fes-window.sh /work/build/software/kernel-source /work/build/software/fes-window-build \\\n'
                 ' /work/build/software/fes-window "$prefix-" /work/build/software/sdk/host 5.15.1-MiSTer\n')
        self.assertIn(build, script)
        # Relocation rewrites GCC's plugin configargs.h, so the kernel plugins build first.
        relocate = script.index('\n/work/build/software/sdk/host/relocate-sdk.sh\n')
        self.assertLess(script.index(build), relocate)
        self.assertLess(relocate, script.index('make -C /work/build/software/runtime'))
        with self.assertRaises(ValueError):
            kit.runtime_script('b' * 40, {'kernel_release': '5.15; touch x'})

    BOOT_KERNEL = 'a' * 64

    def kernel_snapshot(self, name, commit, paired=BOOT_KERNEL, boot=BOOT_KERNEL):
        snapshot = self.root / name
        image = snapshot / 'image'
        (image / 'build').mkdir(parents=True)
        (image / 'build/target-image.sources.lock.toml').write_text(
            f'[image_creator]\nkernel_sha256 = "{paired}"\n\n'
            f'[kernel]\ncommit = "{commit}"\nrelease = "5.15.1-MiSTer"\n')
        (snapshot / 'boot-media.lock.toml').write_text(f'[kernel]\npath = "zImage_dtb"\nsha256 = "{boot}"\n')
        return snapshot

    def test_kernel_module_stages_pinned_source_from_shared_or_worktree_cache(self):
        git_env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@t', GIT_COMMITTER_NAME='t',
                       GIT_COMMITTER_EMAIL='t@t')
        work = self.root / 'kernel-work'
        work.mkdir()
        subprocess.run(['git', 'init', '-q', str(work)], check=True, env=git_env)
        (work / 'Makefile').write_text('kernel')
        subprocess.run(['git', '-C', str(work), 'add', 'Makefile'], check=True, env=git_env)
        subprocess.run(['git', '-C', str(work), 'commit', '-qm', 'k'], check=True, env=git_env)
        commit = subprocess.run(['git', '-C', str(work), 'rev-parse', 'HEAD'], check=True,
                                capture_output=True, text=True).stdout.strip()
        cache = self.root / 'shared-cache'
        bare = cache / 'linux-kernel.git'
        subprocess.run(['git', 'clone', '-q', '--bare', str(work), str(bare)], check=True, env=git_env)
        pin = bare / 'refs/target-image/pinned'
        pin.parent.mkdir(parents=True)
        # (locked commit, fetch pin, expected refusal)
        for index, (locked, pinned, expected) in enumerate((
                (commit, commit, None),
                ('f' * 40, commit, f'pin is {commit}, expected f+'),
                (commit, None, 'pin is missing'),
                ('f' * 40, 'f' * 40, 'pinned kernel commit f+ missing'))):
            with self.subTest(locked=locked, pinned=pinned):
                if pinned:
                    pin.write_text(pinned + '\n')
                else:
                    pin.unlink()
                snapshot = self.kernel_snapshot(f'snapshot-{index}', locked)
                image = snapshot / 'image'
                env = dict(os.environ, FES_TARGET_IMAGE_SHARED_CACHE=str(cache))
                if expected:
                    with self.assertRaisesRegex(ValueError, expected):
                        kit.prepare_kernel_module(self.root, snapshot, env)
                    self.assertFalse((image / 'build/software/kernel-source').exists())
                    continue
                driver = kit.prepare_kernel_module(self.root, snapshot, env)
                self.assertEqual(driver, {'kernel_commit': commit, 'kernel_release': '5.15.1-MiSTer',
                                          'kernel_sha256': self.BOOT_KERNEL})
                self.assertEqual((image / 'build/software/kernel-source/Makefile').read_text(), 'kernel')
        # Without a shared cache the checkout's own target-image source cache is used.
        pin.write_text(commit + '\n')
        local = self.root / 'image/build/cache/target-image'
        local.mkdir(parents=True)
        shutil.copytree(bare, local / 'linux-kernel.git')
        snapshot = self.kernel_snapshot('snapshot-local', commit)
        env = {k: v for k, v in os.environ.items() if k != 'FES_TARGET_IMAGE_SHARED_CACHE'}
        self.assertEqual(kit.prepare_kernel_module(self.root, snapshot, env)['kernel_commit'], commit)

        # A cold shared cache has no kernel repo; kit-sdk leaves the pinned repo
        # in the worktree cache, which kit-software must use after checking it.
        shared = self.root / 'empty-shared-cache'
        shared.mkdir()
        snapshot = self.kernel_snapshot('snapshot-cold-shared', commit)
        env = dict(os.environ, FES_TARGET_IMAGE_SHARED_CACHE=str(shared))
        driver = kit.prepare_kernel_module(self.root, snapshot, env)
        self.assertEqual(driver['kernel_commit'], commit)
        self.assertEqual((snapshot / 'image/build/software/kernel-source/Makefile').read_text(), 'kernel')
        self.assertFalse((shared / 'linux-kernel.git').exists())

    def test_kernel_module_targets_only_the_boot_media_kernel(self):
        # A base image holds no kernel: the module must target the kernel every
        # card boots, so a source pin paired with another image is refused first.
        for index, (paired, boot) in enumerate((('b' * 64, self.BOOT_KERNEL), ('A' * 64, 'A' * 64))):
            with self.subTest(paired=paired, boot=boot):
                snapshot = self.kernel_snapshot(f'snapshot-{index}', 'c' * 40, paired=paired, boot=boot)
                with self.assertRaisesRegex(ValueError, 'not paired with the boot-media kernel'):
                    kit.prepare_kernel_module(self.root, snapshot, dict(os.environ))
                self.assertFalse((snapshot / 'image/build/software').exists())

    def test_repository_kernel_source_is_paired_with_boot_media_kernel(self):
        boot = kit.tomllib.loads((ROOT / 'boot-media.lock.toml').read_text())['kernel']['sha256']
        source = kit.tomllib.loads((ROOT / 'image/build/target-image.sources.lock.toml').read_text())['kernel']
        self.assertEqual(kit.pinned_kernel(ROOT), {'kernel_commit': source['commit'],
                                                   'kernel_release': source['release'], 'kernel_sha256': boot})

    def runtime_fixture(self):
        runtime = self.root / 'sources/libmister-runtime'
        runtime.mkdir(parents=True)
        (runtime / 'Makefile').write_text('all:')
        image = self.root / 'image'
        for name in ('containers/target-image/Dockerfile',
                     'containers/target-image/create-builder-user.sh',
                     'build/target-image-container-packages.sha256'):
            p = image / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text('fixture')
        (image / 'build/target-image.sources.lock.toml').write_text(
            '[container]\nimage="debian"\nplatform="linux/amd64"\ndigest="sha256:' + 'a' * 64 + '"\n')
        cache = self.root / 'cache'; cache.mkdir()
        (cache / 'host.tar').write_bytes(b'SDK')
        (cache / 'receipt.json').write_text('{}')
        sdk = Mock(); sdk.key.return_value = 'key'; sdk.validated_sha.return_value = 'a' * 64
        return cache, sdk

    def test_runtime_invokes_only_cached_builder_and_private_sdk(self):
        cache, sdk = self.runtime_fixture()
        calls = []
        def command(args, **kwargs):
            calls.append((args, kwargs))
            return 'sha256:' + 'c' * 64 if 'inspect' in args else ''
        with patch.object(kit, 'load', return_value=sdk), \
             patch.object(kit, 'run', side_effect=command), \
             patch.object(kit.subprocess, 'run') as cleanup:
            result = kit.build_runtime(self.root, cache, 'b' * 40, {'FES_TARGET_IMAGE_SHARED_CACHE':'/shared'},
                                       self.DRIVER)
        self.assertIn('inspect', calls[0][0])
        self.assertEqual(str(calls[1][0][0]), str(self.root / 'image/scripts/target-image-container.sh'))
        self.assertEqual(calls[1][0][1:4], ['run', 'sh', '-c'])
        self.assertNotIn('FES_TARGET_IMAGE_SHARED_CACHE', calls[1][1]['env'])
        self.assertNotIn('LIBMISTER_RUNTIME_DIR', calls[1][1]['env'])
        self.assertEqual(result['archive_sha256'], 'a' * 64)
        self.assertEqual(sdk.validated_sha.call_count, 2)
        self.assertEqual(cleanup.call_count, 2)
        self.assertEqual((self.root / 'image/build/cache/target-image/toolchains/key/host.tar').read_bytes(), b'SDK')

    def test_runtime_does_not_claim_or_remove_existing_volume(self):
        cache, sdk = self.runtime_fixture()
        with patch.object(kit, 'load', return_value=sdk), \
             patch.object(kit, 'run', return_value='sha256:' + 'c' * 64) as command, \
             patch.object(kit.subprocess, 'run', return_value=Mock(returncode=0)) as docker:
            with self.assertRaisesRegex(ValueError, 'already exists'):
                kit.build_runtime(self.root, cache, 'b' * 40, {}, self.DRIVER)
        command.assert_called_once()
        docker.assert_called_once()
        self.assertIn('inspect', docker.call_args.args[0])
        self.assertNotIn('rm', docker.call_args.args[0])

    def test_runtime_rejects_cache_changed_during_copy_before_container(self):
        cache, sdk = self.runtime_fixture()
        sdk.validated_sha.side_effect = ['a' * 64, 'b' * 64]
        with patch.object(kit, 'load', return_value=sdk), patch.object(kit, 'run') as command:
            with self.assertRaisesRegex(ValueError, 'changed while copying'):
                kit.build_runtime(self.root, cache, 'b' * 40, {}, self.DRIVER)
            command.assert_not_called()

    def test_pack_failure_never_publishes_partial_output(self):
        # Exercise snapshot -> builders -> staging -> pack failure, preserving
        # both the reviewed base and an unrelated previously published release.
        previous = self.root / 'previous'; previous.mkdir()
        (previous / 'bundle.tar').write_bytes(b'old bundle')
        sdk = Mock(); sdk.key.return_value = 'k'; sdk.validated_sha.return_value = 's'
        sdk.cache_dir.return_value = self.root / 'cache'
        def clone(args, **kw):
            if 'status' in args:
                return ''
            if args[:2] == ['git', 'clone']:
                snapshot = Path(args[-1])
                (snapshot / 'sources/FogCast/bin').mkdir(parents=True)
                for name in ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot'):
                    (snapshot / ('sources/FogCast/bin/' + name + '-linux-armv7')).write_bytes(b'executable')
                assets = snapshot / 'image/buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/fogcast'
                assets.mkdir(parents=True); (assets / 'config.toml').write_text('defaults')
            return 'b' * 40
        def runtime(snapshot, *unused):
            binary = snapshot / 'image/build/software/runtime/build/mister-runtime'
            binary.parent.mkdir(parents=True); binary.write_bytes(b'runtime')
            module = snapshot / 'image/build/software/fes-window/fes_window.ko'
            module.parent.mkdir(parents=True); module.write_bytes(b'module')
            (module.parent / 'version').write_text('0123456789abcdef\n')
            return {}
        def fail(base, payload, output, *args):
            self.assertEqual((payload / 'bin/mister-runtime').read_bytes(), b'runtime')
            self.assertEqual((payload / 'share/fogcast/config.toml').read_text(), 'defaults')
            self.assertEqual((payload / 'share/fes-window/fes_window.ko').read_bytes(), b'module')
            self.assertEqual((payload / 'share/fes-window/version').read_text(), '0123456789abcdef\n')
            self.assertEqual((payload / 'share/fes-window/version').stat().st_mode & 0o777, 0o444)
            self.assertEqual((payload / 'share/fogcast/firmware/test-rom/manifest.json').read_text(), '{}')
            output.write_bytes(b'partial failed pack')
            raise ValueError('incompatible base')
        def stage(output, cache):
            self.assertEqual(cache, self.root / 'out/cache/firmware')
            (output / 'test-rom').mkdir(parents=True)
            (output / 'test-rom/manifest.json').write_text('{}')
            return [{'id': 'test-rom', 'sha256': 'f' * 64, 'size': 1}]
        with patch.object(kit, 'load', return_value=sdk), \
             patch.object(kit.bundled_firmware, 'stage', side_effect=stage), \
             patch.object(kit, 'package', side_effect=fail), \
             patch.object(kit, 'run', side_effect=clone), \
             patch.object(kit, 'build_go', return_value='go test'), \
             patch.object(kit, 'prepare_kernel_module', return_value=dict(self.DRIVER)), \
             patch.object(kit, 'build_runtime', side_effect=runtime), \
             patch.object(kit, 'shared_cache_root', return_value=self.root / 'out/cache'):
            with self.assertRaisesRegex(ValueError, 'incompatible base'):
                kit.publish(self.root, self.base, kit.sha(self.base), 'v1', self.root / 'new')
        self.assertFalse((self.root / 'new').exists())
        self.assertFalse(list(self.root.glob('.kit-software-*')))
        self.assertEqual(self.base.read_bytes(), b'authenticated image fixture')
        self.assertEqual((previous / 'bundle.tar').read_bytes(), b'old bundle')


if __name__ == '__main__':
    unittest.main()
