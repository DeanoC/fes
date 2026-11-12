import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
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
        sdk = scripts / 'kit_sdk.py'; sdk.write_text('SDK selector version one')
        consumed = scripts / 'consumed.py'; consumed.write_text('image recipe one')
        registry = self.root / 'config/core-recipes.toml'
        registry.parent.mkdir(); registry.write_text('format = 1')
        with patch('recipes.REGISTRY_PATH', registry):
            cold = kit.load(scripts / 'build.py', 'isolated_cold_build')
        before = cold.recipe_fingerprint(cold.BUILD_RECIPE_FILES)
        self.assertNotIn('scripts/kit_software.py', before)
        self.assertNotIn('scripts/kit_sdk.py', before)
        self.assertIn('config/core-recipes.toml', before)
        producer.write_text('producer version two with different build logic')
        sdk.write_text('SDK selector version two')
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
                         ['./cmd/mister-agent', './cmd/fogcast-api', './cmd/fogcast-tenfoot', './cmd/fes-kit-bundle'])
        self.assertEqual([c[1]['env']['GOARCH'] for c in calls[:4]], ['arm', 'arm', 'arm', 'amd64'])
        for args, kw in calls[:4]:
            self.assertIn('-trimpath', args)
            self.assertIn('-buildvcs=false', args)
            self.assertEqual(kw['env']['CGO_ENABLED'], '0')
            self.assertIn('Revision=' + 'b' * 40, args[args.index('-ldflags') + 1])
        self.assertEqual((self.root / 'sources/FogCast/bin/kit-software.revision').read_text(), 'b'*40+'\n')

    def test_de25_builds_arm64_targets_and_amd64_helper_without_ambient_cpu_flags(self):
        (self.root / 'sources/FogCast').mkdir(parents=True)
        goroot = self.root / 'go'
        for name in ('bin/go', 'pkg/tool/linux_amd64/compile'):
            path = goroot / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(b'compiler')
        calls = []
        def builder(args, **kw):
            if args[:2] == ['go', 'build']:
                calls.append((args, kw['env']))
                Path(args[args.index('-o') + 1]).write_bytes(kw['env']['GOARCH'].encode())
            return str(goroot) if args == ['go', 'env', 'GOROOT'] else 'go fixture'
        with patch.object(kit, 'run', side_effect=builder):
            result = kit.build_go(self.root, 'v1', 'b' * 40,
                                  {'GOARM': '5', 'GOARM64': 'v9.5'}, 'de25-nano')
        self.assertEqual([env['GOARCH'] for _, env in calls], ['arm64'] * 3 + ['amd64'])
        for args, env in calls[:3]:
            self.assertEqual(env['GOARM64'], 'v8.0')
            self.assertNotIn('GOARM', env)
            self.assertTrue(str(args[args.index('-o') + 1]).endswith('-linux-aarch64'))
        self.assertNotIn('GOARM', calls[-1][1]); self.assertNotIn('GOARM64', calls[-1][1])
        self.assertEqual(result['target'], 'linux/arm64 GOARM64=v8.0 CGO_ENABLED=0')

    def de25_sdk_fixture(self, extra=None, producer_root=ROOT):
        directory = self.root / 'de25-image'; directory.mkdir(exist_ok=True)
        archive = directory / 'aarch64-sdk.tar.gz'
        with tarfile.open(archive, 'w:gz') as sdk:
            item = tarfile.TarInfo('sdk/bin/aarch64-buildroot-linux-gnu-g++')
            item.size = 8; sdk.addfile(item, io.BytesIO(b'compiler'))
            if extra is not None:
                sdk.addfile(extra, io.BytesIO(b''))
        config = directory / 'buildroot.config'
        config.write_text('BR2_aarch64=y\nBR2_cortex_a53=y\nBR2_TOOLCHAIN_BUILDROOT_GLIBC=y\nBR2_TOOLCHAIN_BUILDROOT_CXX=y\n')
        lane = kit.load(ROOT / 'image/de25-nano/build.py', 'test_de25_sdk_recipe')
        producer = subprocess.check_output(['git', '-C', producer_root, 'rev-parse', 'HEAD'], text=True).strip()
        record = {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64', 'fes_revision': producer,
                  'recipe_sha256': lane.recipe_key(producer_root), 'rootfs_key': lane.rootfs_key(producer_root),
                  'builder_image': 'sha256:' + 'c' * 64,
                  'files': {path.name: kit.sha(path) for path in (archive, config)}}
        (directory / 'image.json').write_text(json.dumps(record))
        return directory, record

    def test_de25_sdk_checks_receipt_configuration_digest_and_archive_containment(self):
        directory, record = self.de25_sdk_fixture()
        self.assertEqual(kit.de25_sdk(ROOT, directory)['archive_sha256'], record['files']['aarch64-sdk.tar.gz'])
        receipt = directory / 'image.json'
        for field, value in [('board', 'de10-nano'), ('architecture', 'armv7'),
                             ('rootfs_key', 'd' * 64), ('recipe_sha256', 'd' * 64)]:
            receipt.write_text(json.dumps({**record, field: value}))
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, 'receipt differs'):
                kit.de25_sdk(ROOT, directory)
        receipt.write_text(json.dumps(record))
        (directory / 'aarch64-sdk.tar.gz').write_bytes(b'corrupt')
        with self.assertRaisesRegex(ValueError, 'input differs'):
            kit.de25_sdk(ROOT, directory)
        for name, target in [('sdk/../escape', None), ('sdk/link', '../../escape')]:
            extra = tarfile.TarInfo(name)
            if target:
                extra.type = tarfile.SYMTYPE; extra.linkname = target
            directory, record = self.de25_sdk_fixture(extra)
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'unsafe DE25 SDK'):
                kit.de25_sdk(ROOT, directory)

    def managed_sdk_fixture(self):
        repository = self.root / 'producer'; repository.mkdir()
        names = subprocess.check_output(['git', '-C', ROOT, 'ls-files', '-z', '--', 'image', 'scripts']).decode().split('\0')
        for name in filter(None, names):
            source, destination = ROOT / name, repository / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            if source.is_symlink():
                destination.symlink_to(os.readlink(source))
            else:
                shutil.copy2(source, destination)
        subprocess.run(['git', 'init', '-q', repository], check=True)
        subprocess.run(['git', '-C', repository, 'add', 'image', 'scripts'], check=True)
        subprocess.run(['git', '-C', repository, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid',
                        'commit', '-qm', 'managed base producer fixture'], check=True)
        producer = subprocess.check_output(['git', '-C', repository, 'rev-parse', 'HEAD'], text=True).strip()
        managed = kit.load(ROOT / 'image/de25-nano/managed_base.py', 'managed_sdk_fixture_recipe')
        directory = self.root / 'managed-image'; directory.mkdir()
        for name in managed.artifact_names():
            path = directory / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(b'fixture only')
        with tarfile.open(directory / 'aarch64-sdk.tar.gz', 'w:gz') as archive:
            item = tarfile.TarInfo('sdk/bin/aarch64-buildroot-linux-gnu-g++'); item.size = 8
            archive.addfile(item, io.BytesIO(b'compiler'))
        (directory / 'buildroot.config').write_text('BR2_aarch64=y\nBR2_cortex_a53=y\nBR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y\nBR2_TOOLCHAIN_EXTERNAL_CXX=y\n')
        record = {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64',
                  'classification': 'managed-base-intermediate', 'fes_revision': producer,
                  'recipe_sha256': managed.recipe_key(repository), 'sdk_sha256': managed.sdk_lock()['sdk_sha256'],
                  'builder_image': managed.sdk_lock()['builder_image'], 'normal_runtime_admission': 'closed'}
        inputs = {key: record[key] for key in ('fes_revision', 'recipe_sha256', 'sdk_sha256')}
        (directory / 'base-inputs.json').write_text(json.dumps(inputs))
        record['files'] = {name: kit.sha(directory / name) for name in managed.artifact_names()}
        (directory / 'base.json').write_text(json.dumps(record))
        return repository, directory, record, managed.owner

    def test_managed_sdk_authenticates_producer_and_survives_later_consumer_changes(self):
        repository, directory, record, owner = self.managed_sdk_fixture()
        consumer = repository / 'scripts/kit_software.py'
        consumer.write_text(consumer.read_text() + '\n# A later software-only consumer change.\n')
        subprocess.run(['git', '-C', repository, 'add', 'scripts/kit_software.py'], check=True)
        subprocess.run(['git', '-C', repository, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid',
                        'commit', '-qm', 'later consumer fixture'], check=True)
        with patch.object(owner, 'selected_kernel', return_value={}):
            selected = kit.de25_sdk(repository, directory)
            self.assertEqual(selected['producer_revision'], record['fes_revision'])
            self.assertEqual(selected['key'], 'managed-' + record['recipe_sha256'])
            self.assertEqual(selected['base_rootfs_sha256'], record['files']['rootfs.ext4'])
            (directory / 'rootfs.ext4').write_bytes(b'tampered base')
            with self.assertRaisesRegex(ValueError, 'artifact differs'):
                kit.de25_sdk(repository, directory)

    def test_headless_sdk_retains_producer_across_factory_edits_but_refuses_compiler_policy_changes(self):
        repository, _, _, _ = self.managed_sdk_fixture()
        directory, record = self.de25_sdk_fixture(producer_root=repository)
        original_receipt = (directory / 'image.json').read_bytes()
        factory = repository / 'image/de25-nano/managed_base.py'
        factory.write_text(factory.read_text() + '\n# A later factory-only assembly edit.\n')
        subprocess.run(['git', '-C', repository, 'add', 'image'], check=True)
        subprocess.run(['git', '-C', repository, '-c', 'user.name=Fixture', '-c',
                        'user.email=fixture@example.invalid', 'commit', '-qm', 'factory consumer fixture'], check=True)
        selected = kit.de25_sdk(repository, directory)
        self.assertEqual(selected['producer_revision'], record['fes_revision'])
        self.assertEqual((directory / 'image.json').read_bytes(), original_receipt)
        lane = kit.load(ROOT / 'image/de25-nano/build.py', 'headless_sdk_policy_fixture')
        self.assertNotEqual(record['recipe_sha256'], lane.recipe_key(repository))
        # A receipt cannot be relabelled with the later consumer's recipe.
        (directory / 'image.json').write_text(json.dumps({**record, 'recipe_sha256': lane.recipe_key(repository)}))
        with self.assertRaisesRegex(ValueError, 'recorded image producer'):
            kit.de25_sdk(repository, directory)
        (directory / 'image.json').write_bytes(original_receipt)
        # The same actual SDK is refused if the current compiler/container or
        # rootfs policy changes, even though the producer is still authentic.
        dockerfile = repository / 'image/containers/target-image/Dockerfile'
        before = dockerfile.read_bytes()
        dockerfile.write_bytes(before + b'\n# Changed current compiler environment.\n')
        with self.assertRaisesRegex(ValueError, 'selected image recipe'):
            kit.de25_sdk(repository, directory)
        dockerfile.write_bytes(before)
        (directory / 'image.json').write_text(json.dumps({**record, 'fes_revision': 'f' * 40}))
        with self.assertRaisesRegex(ValueError, 'producer commit is unavailable'):
            kit.de25_sdk(repository, directory)

    def test_managed_sdk_never_falls_back_from_a_bad_or_ambiguous_receipt(self):
        repository, directory, record, owner = self.managed_sdk_fixture()
        with patch.object(owner, 'selected_kernel', return_value={}):
            (directory / 'image.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'ambiguous'):
                kit.de25_sdk(repository, directory)
            (directory / 'image.json').unlink()
            for key, value in (('board', 'de10-nano'), ('recipe_sha256', '0' * 64),
                               ('fes_revision', '0' * 40)):
                (directory / 'base.json').write_text(json.dumps({**record, key: value}))
                with self.subTest(key=key), self.assertRaises(ValueError):
                    kit.de25_sdk(repository, directory)

    def test_factory_sdk_uses_original_git_producer_and_current_factory_verifier(self):
        from scripts import de25_appliance
        from tests.test_de25_appliance_workflow import FactoryReceiptTests
        base, factory, boot, _ = de25_appliance.modules(ROOT)
        directory = self.root / 'factory'; directory.mkdir()
        receipt, manifest, packages = FactoryReceiptTests().fixture(directory, factory)
        producer = subprocess.check_output(['git', '-C', ROOT, 'rev-parse', 'HEAD'], text=True).strip()
        receipt['fes_revision'] = producer
        inputs = json.loads((directory / factory.INPUTS).read_text()); inputs['fes_revision'] = producer
        (directory / factory.INPUTS).write_text(json.dumps(inputs))
        with tarfile.open(directory / 'aarch64-sdk.tar.gz', 'w:gz') as archive:
            item = tarfile.TarInfo('sdk/bin/aarch64-buildroot-linux-gnu-g++'); item.size = 8
            archive.addfile(item, io.BytesIO(b'compiler'))
        (directory / 'buildroot.config').write_text('BR2_aarch64=y\nBR2_cortex_a53=y\nBR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y\nBR2_TOOLCHAIN_EXTERNAL_CXX=y\n')
        for name in receipt['files']:
            receipt['files'][name] = kit.sha(directory / name)
        (directory / 'factory.json').write_text(json.dumps(receipt))
        load = kit.load
        def current(path, name):
            return factory if Path(path).name == 'managed_factory.py' else load(path, name)
        with patch.object(kit, 'load', side_effect=current), \
                patch.object(factory, 'selected', return_value=(manifest, packages)), \
                patch.object(base.owner, 'selected_kernel'), patch.object(boot, 'verify_report'):
            selected = kit.de25_sdk(ROOT, directory)
            self.assertEqual(selected['profile'], 'managed-factory')
            self.assertEqual(selected['key'], 'factory-' + receipt['recipe_sha256'])
            self.assertEqual(selected['producer_revision'], producer)
            self.assertEqual(selected['archive_root'], 'sdk')
            (directory / 'base.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'ambiguous'):
                kit.de25_sdk(ROOT, directory)

    def test_board_and_sdk_selection_fail_before_build(self):
        for board, sdk in [('unknown', None), ('de25-nano', None), ('de10-nano', self.root)]:
            with self.subTest(board=board), patch.object(kit, 'build_go') as go:
                with self.assertRaisesRegex(ValueError, 'requires --board'):
                    kit.publish(self.root, self.base, kit.sha(self.base), 'v1', self.root / 'release',
                                board=board, de25_image=sdk)
                go.assert_not_called()
                self.assertFalse((self.root / 'release').exists())

    def test_static_selection_rejects_wrong_board_and_nonbooleans_before_build(self):
        for board, value in [('de10-nano', True), ('de25-nano', 1),
                             ('de25-nano', '1'), ('de25-nano', None)]:
            with self.subTest(board=board, value=value), patch.object(kit, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'static expansion'):
                    kit.publish(self.root, self.base, kit.sha(self.base), 'v1', self.root / 'release',
                                board=board, de25_image=self.root if board == 'de25-nano' else None,
                                de25_static_expansion=value)
                run.assert_not_called()
                self.assertFalse((self.root / 'release').exists())

    def test_static_build_choice_reaches_make_even_with_hostile_environment(self):
        # Execute the generated shell with private SDK/tool stubs. Observe the
        # actual make arguments, rather than just inspecting a script substring.
        work = self.root / 'work'
        host = work / 'build/software/sdk/host'
        tools = host / 'bin'; tools.mkdir(parents=True)
        stubs = self.root / 'stubs'; stubs.mkdir()
        log = self.root / 'make-arguments'
        for name in ('tar', 'debugfs', 'sha256sum'):
            path = stubs / name
            path.write_text('#!/bin/sh\ncat >/dev/null\n' if name == 'sha256sum' else '#!/bin/sh\nexit 0\n')
            path.chmod(0o755)
        make = stubs / 'make'
        make.write_text('#!/bin/sh\nprintf "%s\\n" "$@" > "$MAKE_TEST_LOG"\n')
        make.chmod(0o755)
        for name, body in [('relocate-sdk.sh', 'exit 0'),
                           ('bin/aarch64-buildroot-linux-gnu-g++', 'echo aarch64-buildroot-linux-gnu'),
                           ('bin/aarch64-buildroot-linux-gnu-strip', 'exit 0')]:
            path = host / name; path.write_text('#!/bin/sh\n' + body + '\n'); path.chmod(0o755)
        for enabled in (False, True):
            with self.subTest(enabled=enabled), patch.dict(os.environ, {'ENABLE_DE25_STATIC': '$(invalid)'}):
                env = kit.software_environment()
                self.assertNotIn('ENABLE_DE25_STATIC', env)
                env.update(PATH=str(stubs) + os.pathsep + env['PATH'], MAKE_TEST_LOG=str(log))
                script = kit.runtime_script('b'*40, None, 'de25-nano', 'c'*64,
                                            de25_static_expansion=enabled)
                subprocess.run(['sh', '-c', script.replace('/work/', str(work) + '/')],
                               env=env, check=True, capture_output=True, text=True)
                arguments = log.read_text().splitlines()
                self.assertEqual([a for a in arguments if a.startswith('ENABLE_DE25_STATIC=')],
                                 ['ENABLE_DE25_STATIC=' + str(int(enabled))])
        self.assertIn('ENABLE_DE25_STATIC=0', kit.runtime_script('b'*40, self.DRIVER))

    def test_make_forwards_static_choice_and_cli_rejects_invalid_values(self):
        command = ['make', '--no-print-directory', '-n', 'kit-software',
                   'KIT_SOFTWARE_BASE=/reviewed.img', 'KIT_SOFTWARE_BASE_SHA256=' + 'a'*64,
                   'KIT_SOFTWARE_VERSION=trial', 'BOARD=de25-nano', 'DE25_IMAGE=/sdk',
                   'KIT_SOFTWARE_DE25_STATIC_EXPANSION=1']
        result = subprocess.run(command, cwd=ROOT, check=True, capture_output=True, text=True)
        self.assertIn('--de25-static-expansion "1"', result.stdout)
        for value in ('2', 'true', '0 1'):
            result = subprocess.run([sys.executable, ROOT / 'scripts/kit_software.py',
                                     '--base', self.base, '--base-sha256', kit.sha(self.base),
                                     '--version', 'trial', '--output', self.root / 'release',
                                     '--de25-static-expansion', value], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('invalid', result.stderr)
            self.assertFalse((self.root / 'release').exists())

    def test_de25_script_extracts_private_sdk_without_building_de10_driver(self):
        script = kit.runtime_script('b' * 40, None, 'de25-nano', 'c' * 64)
        subprocess.run(['sh', '-n'], input=script, text=True, check=True)
        self.assertIn('aarch64-buildroot-linux-gnu', script)
        self.assertIn('sha256sum -c -', script)
        self.assertIn('dump /etc/fes-board', script)
        self.assertNotIn('fes-window', script)
        self.assertNotIn('toolchain_cache.py', script)
        abi_only = kit.runtime_script('b' * 40, None, 'de25-nano', 'c' * 64, base_factory=False)
        subprocess.run(['sh', '-n'], input=abi_only, text=True, check=True)
        self.assertIn("rdump /lib ", abi_only)
        self.assertNotIn('rdump /usr/share/fes/software ', abi_only)
        for driver, digest in [(self.DRIVER, 'c' * 64), (None, None), (None, '$(bad)')]:
            with self.assertRaises(ValueError):
                kit.runtime_script('b' * 40, driver, 'de25-nano', digest)

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
        driver = image / 'kernel/de25-dma'
        driver.mkdir(parents=True)
        shutil.copyfile(ROOT / 'image/kernel/de25-dma/linux.lock.toml', driver / 'linux.lock.toml')
        execution = image / 'de25-nano/sdk-builder'
        execution.mkdir(parents=True)
        for name in ('identity.lock.toml', 'manifest.json'):
            shutil.copyfile(ROOT / 'image/de25-nano/sdk-builder' / name, execution / name)
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

    def test_de25_runtime_uses_validated_cache_bytes_and_keeps_original_source_identity(self):
        import kit_sdk
        cache, _ = self.runtime_fixture()
        sdk = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'runtime_cache_fixture')
        raw = cache / 'host.tar'; digest = kit.sha(raw); key = 'd' * 64
        (cache / 'receipt.json').write_text(json.dumps({'key': key, 'sha256': digest}))
        selected = {'key': 'managed-' + 'e'*64, 'archive_sha256': digest, 'builder_image': 'sha256:' + 'c'*64}
        cached = {'source': selected, 'key': key, 'directory': str(cache)}
        calls = []
        def command(args, **kwargs):
            calls.append((args, kwargs)); return 'sha256:' + 'c'*64 if 'inspect' in args else ''
        with patch.object(kit_sdk, 'ensure_de25', return_value=cached) as ensure, \
                patch.object(kit, 'load', return_value=sdk), patch.object(kit, 'run', side_effect=command), \
                patch.object(kit.de25_builder, 'resolve', return_value=selected['builder_image']) as resolve, \
                patch.object(kit.subprocess, 'run', return_value=Mock(returncode=1)):
            result = kit.build_runtime(self.root, self.root / 'recorded-image', 'b'*40,
                                       {'ENABLE_DE25_STATIC': 'hostile'}, None, 'de25-nano',
                                       de25_static_expansion=True)
        ensure.assert_called_once_with(self.root, self.root / 'recorded-image')
        self.assertEqual((self.root / 'image/build/software/aarch64-sdk.tar.gz').read_bytes(), b'SDK')
        self.assertEqual(result['de25_image_sdk'], selected)
        self.assertEqual(result['sdk_archive_cache'], {'key': key, 'archive_sha256': digest})
        self.assertNotIn('fes-window', calls[1][0][-1])
        self.assertIn('ENABLE_DE25_STATIC=1', calls[1][0][-1])
        self.assertNotIn('ENABLE_DE25_STATIC', calls[1][1]['env'])
        self.assertEqual(result['runtime_make_variables'], {'ENABLE_DE25_STATIC': 1})
        resolve.assert_called_once_with('docker', selected['builder_image'], root=self.root,
                                        env=calls[1][1]['env'])
        self.assertEqual(calls[1][0][0:3], ['docker', 'run', '--rm'])
        self.assertEqual(calls[0][0][calls[0][0].index('--user') + 1], '0:0')
        self.assertIn(selected['builder_image'], calls[1][0])
        self.assertEqual(calls[1][0][calls[1][0].index('--network') + 1], 'none')
        self.assertEqual(calls[1][0][calls[1][0].index('--user') + 1],
                         f'{os.getuid()}:{os.getgid()}')
        self.assertIn(str(self.root / 'image') + ':/work', calls[1][0])
        self.assertNotIn('container_context_sha256', result)
        self.assertNotIn('container_base', result)

    def test_de25_runtime_rejects_cache_receipt_poison_before_container(self):
        import kit_sdk
        cache, _ = self.runtime_fixture()
        sdk = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'runtime_bad_cache_fixture')
        key = 'd'*64; (cache / 'receipt.json').write_text(json.dumps({'key': key, 'sha256': kit.sha(cache / 'host.tar')}))
        selected = {'key': 'managed-' + 'e'*64, 'archive_sha256': 'f'*64, 'builder_image': 'sha256:' + 'c'*64}
        with patch.object(kit_sdk, 'ensure_de25', return_value={'source': selected, 'key': key, 'directory': str(cache)}), \
                patch.object(kit, 'load', return_value=sdk), patch.object(kit, 'run') as command:
            with self.assertRaisesRegex(ValueError, 'SDK changed while copying'):
                kit.build_runtime(self.root, self.root / 'recorded-image', 'b'*40, {}, None, 'de25-nano')
            command.assert_not_called()

    def test_de25_runtime_keeps_config_provenance_when_executing_oci_manifest(self):
        import kit_sdk
        cache, _ = self.runtime_fixture()
        sdk = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'runtime_oci_cache_fixture')
        key = 'd'*64; digest = kit.sha(cache / 'host.tar')
        (cache / 'receipt.json').write_text(json.dumps({'key': key, 'sha256': digest}))
        lock, manifest, size = kit.de25_builder.locked_manifest(self.root)
        selected = {'key': 'managed-' + 'e'*64, 'archive_sha256': digest,
                    'builder_image': lock['builder_image']}
        inspect = {'Id': lock['builder_manifest'], 'Os': 'linux', 'Architecture': 'amd64',
                   'Descriptor': {'digest': lock['builder_manifest'],
                                  'mediaType': kit.de25_builder.MANIFEST_TYPE, 'size': size},
                   'RootFS': {'Type': 'layers', 'Layers': [x['digest'] for x in manifest['layers']]}}
        calls = []
        def engine(args, **kwargs):
            calls.append(args)
            if args[:3] == ['docker', 'image', 'inspect']:
                return Mock(returncode=int(args[3] == lock['builder_image']), stdout=json.dumps([inspect]))
            self.assertEqual(args[:2], ['docker', 'volume'])
            return Mock(returncode=1 if 'inspect' in args else 0)
        with patch.object(kit_sdk, 'ensure_de25', return_value={'source': selected, 'key': key, 'directory': str(cache)}), \
                patch.object(kit, 'load', return_value=sdk), \
                patch.object(kit.subprocess, 'run', side_effect=engine), patch.object(kit, 'run', return_value='') as compile:
            result = kit.build_runtime(self.root, self.root / 'recorded-image', 'b'*40,
                                       {}, None, 'de25-nano')
        self.assertEqual(result['container_id'], lock['builder_image'])
        self.assertEqual(result['container_execution_reference'], lock['builder_manifest'])
        self.assertEqual(len(compile.call_args_list), 2)
        self.assertIn(lock['builder_manifest'], compile.call_args.args[0])
        self.assertNotIn(lock['builder_image'], compile.call_args.args[0])
        self.assertEqual([c[:3] for c in calls], [['docker', 'image', 'inspect'],
                         ['docker', 'image', 'inspect'], ['docker', 'volume', 'inspect'],
                         ['docker', 'volume', 'rm']])

    def test_de25_runtime_rejects_substituted_builder_before_volume_or_compile(self):
        import kit_sdk
        cache, _ = self.runtime_fixture()
        sdk = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'runtime_provision_fixture')
        digest = kit.sha(cache / 'host.tar')
        key = 'd' * 64
        (cache / 'receipt.json').write_text(json.dumps({'key': key, 'sha256': digest}))
        selected = {'key': 'managed-' + 'e' * 64, 'archive_sha256': digest,
                    'builder_image': 'sha256:' + 'c' * 64}
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            self.assertEqual(args[:3], ['docker', 'image', 'inspect'])
            return Mock(returncode=0, stdout=json.dumps([{'Id': 'sha256:' + 'f'*64}]))
        with patch.object(kit_sdk, 'ensure_de25', return_value={'source': selected, 'key': key, 'directory': str(cache)}), \
                patch.object(kit, 'load', return_value=sdk), patch.object(kit, 'run') as compile, \
                patch.object(kit.subprocess, 'run', side_effect=command) as inspect:
            with self.assertRaisesRegex(ValueError, 'SDK and runtime builder identities differ'):
                kit.build_runtime(self.root, self.root / 'recorded-image', 'b' * 40, {},
                                  None, 'de25-nano', base_factory=False)
        self.assertEqual(calls[0][3], selected['builder_image'])
        self.assertEqual(len(calls), 1)
        compile.assert_not_called()
        self.assertEqual(inspect.call_count, 1)

    def test_de25_runtime_keeps_existing_volume_and_cleans_only_its_failed_build(self):
        import kit_sdk
        for existing in (True, False):
            with self.subTest(existing=existing):
                root = self.root / str(existing); root.mkdir()
                # Each attempt has a separate private snapshot and volume.
                with patch.object(self, 'root', root):
                    cache, _ = self.runtime_fixture()
                sdk = kit.load(ROOT / 'image/scripts/toolchain_cache.py', 'runtime_volume_fixture')
                key = 'd' * 64; digest = kit.sha(cache / 'host.tar')
                (cache / 'receipt.json').write_text(json.dumps({'key': key, 'sha256': digest}))
                selected = {'key': 'managed-' + 'e' * 64, 'archive_sha256': digest,
                            'builder_image': 'sha256:' + 'c' * 64}
                def command(args, **kwargs):
                    if 'fes-sdk-workspace' in args:
                        return ''
                    raise subprocess.CalledProcessError(17, args)
                with patch.object(kit_sdk, 'ensure_de25', return_value={
                        'source': selected, 'key': key, 'directory': str(cache)}), \
                        patch.object(kit, 'load', return_value=sdk), \
                        patch.object(kit.de25_builder, 'resolve', return_value=selected['builder_image']), \
                        patch.object(kit, 'run', side_effect=command) as run, \
                        patch.object(kit.subprocess, 'run', return_value=Mock(returncode=0 if existing else 1)) as volume:
                    with self.assertRaises(ValueError if existing else subprocess.CalledProcessError):
                        kit.build_runtime(root, root / 'recorded-image', 'b' * 40, {}, None, 'de25-nano')
                self.assertEqual(run.call_count, 0 if existing else 2)
                self.assertEqual(volume.call_count, 1 if existing else 2)
                if not existing:
                    self.assertEqual(volume.call_args.args[0][-2:],
                                     ['rm', 'fes-kit-software-' + root.parent.name])

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
        def runtime(snapshot, *unused, **kwargs):
            self.assertIs(kwargs['de25_static_expansion'], False)
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

    def test_de25_pack_failure_never_builds_or_carries_de10_window_driver(self):
        def clone(args, **kw):
            if 'status' in args:
                return ''
            if args[:2] == ['git', 'clone']:
                snapshot = Path(args[-1])
                (snapshot / 'sources/FogCast/bin').mkdir(parents=True)
                for name in ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot'):
                    (snapshot / ('sources/FogCast/bin/' + name + '-linux-aarch64')).write_bytes(b'arm64')
                (snapshot / 'image/buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/fogcast').mkdir(parents=True)
            return 'b' * 40
        def runtime(snapshot, cache, revision, env, driver, board, **kwargs):
            self.assertIsNone(driver); self.assertEqual(board, 'de25-nano')
            self.assertIs(kwargs['de25_static_expansion'], True)
            binary = snapshot / 'image/build/software/runtime/build/mister-runtime'
            binary.parent.mkdir(parents=True); binary.write_bytes(b'arm64 runtime')
            return {}
        def fail(image, payload, output, *args):
            self.assertFalse((payload / 'share/fes-window').exists())
            self.assertEqual((payload / 'bin/mister-agent').read_bytes(), b'arm64')
            output.write_bytes(b'partial failed pack')
            raise ValueError('incompatible base')
        with patch.object(kit, 'de25_sdk', return_value={}), \
             patch.object(kit, 'package', side_effect=fail), \
             patch.object(kit, 'run', side_effect=clone), \
             patch.object(kit, 'build_go'), \
             patch.object(kit, 'prepare_kernel_module') as driver, \
             patch.object(kit, 'build_runtime', side_effect=runtime):
            with self.assertRaisesRegex(ValueError, 'incompatible base'):
                kit.publish(self.root, self.base, kit.sha(self.base), 'v1', self.root / 'new',
                            board='de25-nano', de25_image=self.root, de25_static_expansion=True)
            driver.assert_not_called()
        self.assertFalse((self.root / 'new').exists())
        self.assertFalse(list(self.root.glob('.kit-software-*')))
        self.assertEqual(self.base.read_bytes(), b'authenticated image fixture')


if __name__ == '__main__':
    unittest.main()
