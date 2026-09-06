import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import build as cold_build
from media_inputs import MediaLock, Payloads

try:
    import media
except ImportError:
    media = None


class FakeRunner:
    def __init__(self):
        self.fail = False
        self.assemblies = 0
        self.checks = 0
        self.snapshot = None
        self.on_assemble = lambda: None

    def assemble(self, output, inputs, lock):
        self.assemblies += 1
        self.on_assemble()
        config = inputs.agent_config.read_bytes() if inputs.agent_config else b''
        self.snapshot = config
        image = output / 'fes.img'
        image.write_bytes(inputs.rootfs.read_bytes() + config)
        sha = cold_build.digest(image)
        (output / 'fes-media.toml').write_text('image_sha256 = "' + sha + '"\n')
        return [sha, sha]

    def verify(self, generation, inputs, lock, provision_sha):
        self.checks += 1
        if self.fail:
            raise ValueError('verification failed')

    def extract(self, generation, destination):
        destination.write_bytes(b'rootfs')

    def child_verify(self, fogcast, staged, env):
        self.asserted_env = env
        (staged / 'manifest.tsv').write_text('manifest')
        (fogcast / 'build/output/target-image/native-dev/qemu-smoke.log').write_text('new smoke')
        if self.fail:
            raise ValueError('verification failed')


class MediaTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(media, 'scripts/media.py is required')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.output = self.root / 'out/native-integration-dev'
        self.output.mkdir(parents=True)
        self.fogcast = self.root / 'fogcast'
        (self.fogcast / 'build/output/target-image/native-dev').mkdir(parents=True)
        self.log = self.fogcast / 'build/output/target-image/native-dev/qemu-smoke.log'
        self.log.write_text('original smoke')
        self.log.chmod(0o640)
        (self.output / 'linux.img').write_bytes(b'rootfs')
        (self.output / 'idle.rbf').write_bytes(b'idle')
        (self.output / 'qemu-smoke.log').write_text('smoke')
        sha = cold_build.digest(self.output / 'linux.img')
        (self.output / 'reproducibility.txt').write_text(f'run_1_sha256={sha}\nrun_2_sha256={sha}\n')
        cold_build.write_receipt(self.output, 'image', 'cold-fp', ['linux.img'])
        (self.output / 'verification.json').write_text(json.dumps(cold_build.verification_record(self.output, sha, False)))
        shutil.copyfile(ROOT / 'boot-media.lock.toml', self.root / 'boot-media.lock.toml')
        (self.root / 'kernel').write_bytes(b'kernel')
        (self.root / 'uboot').write_bytes(b'uboot')
        self.runner = FakeRunner()
        self.addCleanup(patch.stopall)
        patch.object(media, 'select', return_value=('cold-fp', self.fogcast, ('megadrive', 'pong', 'snes'), {})).start()
        patch.object(media, 'resolve_payloads', return_value=Payloads(self.root / 'uboot', self.root / 'kernel')).start()
        patch.object(media, 'recipe_fingerprint', return_value={'scripts/media.py': 'recipe'}).start()
        # The pinned idle cache is separate from cold output publication.
        (self.fogcast / 'build/cache/target-image/native').mkdir(parents=True)
        (self.fogcast / 'build/cache/target-image/native/idle.rbf').write_bytes(b'idle')

    def build(self, config=None):
        return media.build(self.root, 'native-integration-dev', config, self.runner)

    def test_rejects_development_and_missing_verification(self):
        (self.output / 'image.json').unlink()
        (self.output / 'development.json').write_text('{}')
        with self.assertRaisesRegex(ValueError, 'run make build and make verify'):
            self.build()
        self.assertFalse((self.output / 'media/current').exists())

    def test_receipt_and_atomic_relative_generation(self):
        result = self.build()
        self.assertEqual(result.generation.name, cold_build.digest(result.image))
        self.assertEqual(os.readlink(self.output / 'media/current'), 'generations/' + result.generation.name)
        receipt = json.loads((result.generation / 'media.json').read_text())
        self.assertEqual(receipt['assembly_sha256'], [result.generation.name] * 2)
        self.assertEqual(receipt['hardware'], 'not-run')
        self.assertEqual(receipt['checks'], dict.fromkeys(('structural_media', 'rootfs_structural', 'rootfs_qemu', 'reproducibility'), 'pass'))
        self.assertIn('reproducibility_sha256', receipt['inputs']['cold'])
        self.assertEqual(media.verify(self.root, 'native-integration-dev', self.runner).generation, result.generation)
        self.assertEqual(self.log.read_text(), 'original smoke')
        self.assertEqual(self.runner.asserted_env['NATIVE_RUNTIME_SYSTEMS'], 'megadrive pong snes')
        self.assertEqual(self.runner.asserted_env['TARGET_IMAGE_OUTPUT_VOLUME'], cold_build.output_volume(self.root, 'native-integration-dev'))
        self.assertEqual(stat.S_IMODE(self.log.stat().st_mode), 0o640)
        self.assertFalse((self.fogcast / 'build/output/target-image/media-verify').exists())

    def test_private_snapshot_survives_original_mutation(self):
        config = self.root / 'config'
        secret = b'agent_token = "do-not-print"\n'
        config.write_bytes(secret)
        config.chmod(0o600)
        self.runner.on_assemble = lambda: config.write_bytes(b'changed')
        result = self.build(config)
        self.assertEqual(self.runner.snapshot, secret)
        self.assertNotIn('do-not-print', (result.generation / 'media.json').read_text())
        self.assertNotIn('do-not-print', result.manifest_text + result.log)
        for path in result.generation.iterdir():
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(result.generation.stat().st_mode), 0o700)
        self.assertEqual(sorted(p.name for p in result.generation.iterdir()), ['fes-media.toml', 'fes.img', 'media.json'])
        self.assertEqual(media.verify(self.root, 'native-integration-dev', self.runner).generation, result.generation)

    def test_bad_config_inputs(self):
        config = self.root / 'config'
        config.write_bytes(b'secret')
        for mode in (0o644, 0o610, 0o601):
            config.chmod(mode)
            with self.assertRaisesRegex(ValueError, 'agent config'):
                self.build(config)
        config.chmod(0o600)
        link = self.root / 'link'
        link.symlink_to(config)
        for path in (Path('relative'), link, self.root, self.root / 'missing'):
            with self.assertRaisesRegex(ValueError, 'agent config'):
                self.build(path)
        config.write_bytes(b'x' * 65537)
        with self.assertRaisesRegex(ValueError, 'agent config'):
            self.build(config)

    def test_config_opened_once_with_nofollow(self):
        config = self.root / 'config'
        config.write_bytes(b'secret')
        config.chmod(0o600)
        real_open = os.open
        calls = []
        def recording(path, flags, *args, **kwargs):
            if Path(path) == config:
                calls.append(flags)
            return real_open(path, flags, *args, **kwargs)
        with patch.object(media.os, 'open', side_effect=recording):
            self.build(config)
        self.assertEqual(len(calls), 1)
        self.assertTrue(calls[0] & os.O_NOFOLLOW)

    def test_failed_republish_and_tampering_preserve_current(self):
        first = self.build()
        self.runner.fail = True
        with self.assertRaisesRegex(ValueError, 'verification failed'):
            self.build()
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)
        self.runner.fail = False
        first.image.write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError, 'receipt|digest'):
            self.build()
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)
        self.assertEqual(list((self.output / 'media').glob('.staging-*')), [])

    def test_republish_revalidates_existing(self):
        first = self.build()
        before = self.runner.checks
        second = self.build()
        self.assertEqual(first.generation, second.generation)
        self.assertGreaterEqual(self.runner.checks - before, 2)

    def test_stale_receipt_and_cold_evidence_rejected(self):
        result = self.build()
        receipt = result.generation / 'media.json'
        data = json.loads(receipt.read_text())
        data['checks']['rootfs_qemu'] = 'fail'
        receipt.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, 'receipt'):
            media.verify(self.root, 'native-integration-dev', self.runner)
        (self.output / 'qemu-smoke.log').write_text('changed')
        with self.assertRaisesRegex(ValueError, 'run make verify'):
            self.build()

    def test_concurrent_lease_preserves_metadata_and_dead_owner_releases(self):
        with media.lease(self.root, 'native-integration-dev'):
            path = self.root / 'out/locks/media-native-integration-dev.json'
            previous = path.read_bytes()
            with self.assertRaisesRegex(ValueError, 'another media'):
                self.build()
            self.assertEqual(path.read_bytes(), previous)
        script = 'import sys,time;sys.path.insert(0,sys.argv[1]);import media;from pathlib import Path\nwith media.lease(Path(sys.argv[2]),"native-integration-dev"):\n print("ready",flush=True);time.sleep(60)'
        process = subprocess.Popen([sys.executable, '-c', script, str(ROOT / 'scripts'), str(self.root)], stdout=subprocess.PIPE, text=True)
        try:
            self.assertEqual(process.stdout.readline().strip(), 'ready')
            process.kill()
            process.wait()
            with media.lease(self.root, 'native-integration-dev'):
                self.assertTrue(path.exists())
        finally:
            process.kill() if process.poll() is None else None
            process.wait()
            process.stdout.close()

    def test_wrong_profile_and_current_escape_rejected(self):
        with self.assertRaisesRegex(ValueError, 'native-integration-dev'):
            media.build(self.root, 'native-dev', None, self.runner)
        self.build()
        current = self.output / 'media/current'
        current.unlink()
        current.symlink_to(self.root)
        with self.assertRaisesRegex(ValueError, 'current'):
            media.verify(self.root, 'native-integration-dev', self.runner)

    def test_child_failure_restores_existing_scratch_and_log(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir()
        (staged / 'keep').write_text('preserved')
        def fail(fogcast, scratch, env):
            self.log.write_text('failed smoke')
            raise ValueError('child failed')
        self.runner.child_verify = fail
        with self.assertRaisesRegex(ValueError, 'child failed'):
            self.build()
        self.assertEqual((staged / 'keep').read_text(), 'preserved')
        self.assertEqual(self.log.read_text(), 'original smoke')
        self.assertFalse((self.output / 'media/current').exists())

    def test_child_setup_failure_restores_existing_scratch(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir()
        (staged / 'keep').write_text('preserved')
        mkdir = Path.mkdir
        def failed_mkdir(path, *args, **kwargs):
            if path == staged:
                raise OSError('scratch creation failed')
            return mkdir(path, *args, **kwargs)
        with patch.object(Path, 'mkdir', new=failed_mkdir):
            with self.assertRaisesRegex(OSError, 'scratch creation failed'):
                self.build()
        self.assertTrue((staged / 'keep').exists(), 'existing child scratch must survive setup failure')
        self.assertEqual((staged / 'keep').read_text(), 'preserved')
        self.assertEqual(self.log.read_text(), 'original smoke')

    def test_extracted_rootfs_identity_required(self):
        self.runner.extract = lambda generation, destination: destination.write_bytes(b'wrong rootfs')
        with self.assertRaisesRegex(ValueError, 'embedded rootfs'):
            self.build()
        self.assertFalse((self.output / 'media/current').exists())

    def test_publication_failure_preserves_current(self):
        first = self.build()
        config = self.root / 'config'
        config.write_bytes(b'another generation')
        config.chmod(0o600)
        replace = os.replace
        def fail_current(source, destination):
            if Path(destination) == self.output / 'media/current':
                raise OSError('publication interrupted')
            return replace(source, destination)
        with patch.object(media.os, 'replace', side_effect=fail_current):
            with self.assertRaisesRegex(OSError, 'publication interrupted'):
                self.build(config)
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)
        self.assertEqual(list((self.output / 'media').glob('.staging-*')), [])

    def test_publication_sync_failure_rolls_back_current(self):
        first = self.build()
        config = self.root / 'config'
        config.write_bytes(b'another generation')
        config.chmod(0o600)
        sync = media.fsync_dir
        def fail_after_replace(path):
            current = self.output / 'media/current'
            if path == current.parent and current.resolve() != first.generation:
                raise OSError('publication sync failed')
            return sync(path)
        with patch.object(media, 'fsync_dir', side_effect=fail_after_replace):
            with self.assertRaisesRegex(OSError, 'publication sync failed'):
                self.build(config)
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)

    def test_verify_reads_current_once(self):
        result = self.build()
        readlink = os.readlink
        reads = []
        def once(path, *args, **kwargs):
            if Path(path) == self.output / 'media/current':
                reads.append(path)
            return readlink(path, *args, **kwargs)
        with patch.object(media.os, 'readlink', side_effect=once):
            self.assertEqual(media.verify(self.root, runner=self.runner).generation, result.generation)
        self.assertEqual(len(reads), 1)

    def test_symlink_generation_parent_rejected(self):
        first = self.build()
        generations = self.output / 'media/generations'
        actual = self.root / 'elsewhere'
        generations.rename(actual)
        generations.symlink_to(actual)
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.build()

    def test_inode_replacement_during_open_rejected(self):
        config = self.root / 'config'
        config.write_bytes(b'secret')
        config.chmod(0o600)
        replacement = self.root / 'replacement'
        replacement.write_bytes(b'different')
        replacement.chmod(0o600)
        original = os.open
        def swap(path, flags, *args, **kwargs):
            if Path(path) == config:
                replacement.replace(config)
            return original(path, flags, *args, **kwargs)
        with patch.object(media.os, 'open', side_effect=swap):
            with self.assertRaisesRegex(ValueError, 'agent config'):
                self.build(config)


class KernelPreflightTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(media, 'scripts/media.py is required')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.work = self.root / 'work'
        self.volume = self.root / 'volume'
        self.scratch = self.work / 'build/output/target-image/media-verify'
        self.scratch.mkdir(parents=True)
        (self.scratch / 'bin').mkdir()
        guard = self.scratch / 'bin/make'
        guard.write_text('#!/bin/sh\necho "run make verify" >&2\nexit 1\n')
        guard.chmod(0o700)
        (self.work / 'scripts').mkdir()
        self.marker = self.root / 'calls'
        self.write_script('verify-target-image-source-cache.sh', 'exit 0')
        self.write_script('verify-target-image.sh', 'echo structural >> "$CALLS"')
        self.write_script('qemu-smoke-target-image.sh', r'''if [ "$1" = --verify-kernel-cache ]; then
  test "$2" = "$(sed -n 's/^base_key=//p' "$3/provenance.txt")" || exit 1
  test "$(sha256sum "$3/arch/arm/boot/zImage" | cut -d ' ' -f 1)" = "$(sed -n 's/^zimage_sha256=//p' "$3/provenance.txt")" || exit 1
  test "$(sha256sum "$3/arch/arm/boot/dts/vexpress-v2p-ca9.dtb" | cut -d ' ' -f 1)" = "$(sed -n 's/^dtb_sha256=//p' "$3/provenance.txt")" || exit 1
else
  if [ "${ATTEMPT_BUILD:-0}" = 1 ]; then make; fi
  echo "qemu:$*" >> "$CALLS"
fi''')
        self.source = self.volume / 'qemu-vexpress-source'
        self.source.mkdir(parents=True)
        self.git('init', '-q', self.source)
        self.git('-C', self.source, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.com', 'commit', '-qm', 'fixture', '--allow-empty')
        self.head = self.git('-C', self.source, 'rev-parse', 'HEAD').strip()
        (self.source / '.target-image-commit').write_text(self.head + '\n')
        bare = self.work / 'build/cache/target-image/linux-kernel.git'
        self.git('clone', '-q', '--bare', self.source, bare)
        self.git('--git-dir=' + str(bare), 'update-ref', 'refs/target-image/pinned', self.head)
        self.toolchain = self.volume / 'work-2-native-dev/host'
        (self.toolchain / 'bin').mkdir(parents=True)
        compiler = self.toolchain / 'bin/arm-buildroot-linux-gnueabihf-gcc'
        compiler.write_text('#!/bin/sh\nexit 99\n')
        compiler.chmod(0o700)
        (self.toolchain / 'bin/gcc-link').symlink_to(compiler.name)
        self.kernel = self.volume / 'qemu-vexpress-kernel'
        (self.kernel / 'arch/arm/boot/dts').mkdir(parents=True)
        (self.kernel / 'arch/arm/boot/zImage').write_bytes(b'kernel')
        (self.kernel / 'arch/arm/boot/dts/vexpress-v2p-ca9.dtb').write_bytes(b'dtb')
        records = []
        for path in sorted(self.toolchain.rglob('*')):
            if path.is_symlink():
                records.append(f'./{path.relative_to(self.toolchain)}\tsymlink\t{os.readlink(path)}\n')
            elif path.is_file():
                records.append(f'./{path.relative_to(self.toolchain)}\tfile\t{cold_build.digest(path)}\n')
        tool_sha = hashlib.sha256(''.join(records).encode()).hexdigest()
        script_sha = cold_build.digest(self.work / 'scripts/qemu-smoke-target-image.sh')
        key = hashlib.sha256(f'{self.head}\n{tool_sha}\n{script_sha}\n'.encode()).hexdigest()
        (self.kernel / 'provenance.txt').write_text(f'format=1\nbase_key={key}\nzimage_sha256={cold_build.digest(self.kernel / "arch/arm/boot/zImage")}\ndtb_sha256={cold_build.digest(self.kernel / "arch/arm/boot/dts/vexpress-v2p-ca9.dtb")}\n')

    def git(self, *args):
        return subprocess.check_output(['git', *map(str, args)], text=True, stderr=subprocess.PIPE)

    def write_script(self, name, text):
        path = self.work / 'scripts' / name
        path.write_text('#!/bin/sh\nset -eu\n' + text + '\n')
        path.chmod(0o700)

    def execute(self, **environment):
        script = media.CHILD_VERIFY.replace('/target-image-output', str(self.volume)).replace('/work/', str(self.work) + '/')
        return subprocess.run(['sh', '-c', script], env=dict(os.environ, CALLS=str(self.marker), **environment), capture_output=True, text=True)

    def test_matching_cache_runs_both_child_verifiers(self):
        result = self.execute()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.marker.read_text().splitlines(), ['structural', 'qemu:--inside native-dev ' + str(self.scratch / 'linux.img')])

    def test_missing_cache_fails_without_building(self):
        shutil.rmtree(self.kernel)
        result = self.execute()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('run make verify', result.stderr)
        self.assertFalse(self.kernel.exists())
        self.assertFalse(self.marker.exists())

    def test_stale_toolchain_and_kernel_rejected(self):
        (self.toolchain / 'bin/extra').write_bytes(b'changed toolchain')
        result = self.execute()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('run make verify', result.stderr)
        (self.toolchain / 'bin/extra').unlink()
        (self.kernel / 'arch/arm/boot/zImage').write_bytes(b'tampered kernel')
        result = self.execute()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.marker.exists())

    def test_fallback_make_is_blocked(self):
        result = self.execute(ATTEMPT_BUILD='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('run make verify', result.stderr)
        self.assertEqual(self.marker.read_text(), 'structural\n')


class InterfaceTests(unittest.TestCase):
    def test_recipe_split(self):
        cold = set(cold_build.BUILD_RECIPE_FILES)
        media_files = set(cold_build.MEDIA_RECIPE_FILES)
        for name in ('media.py', 'media_inputs.py', 'media_container.py', 'media_inside.py'):
            self.assertNotIn(ROOT / 'scripts' / name, cold)
            self.assertIn(ROOT / 'scripts' / name, media_files)
        self.assertIn(ROOT / 'boot-media.lock.toml', media_files)
        self.assertTrue(set((ROOT / 'containers/boot-media').iterdir()) <= media_files)

    def test_make_interface(self):
        command = subprocess.run(['make', '-n', 'media', 'AGENT_CONFIG=/tmp/private'], cwd=ROOT, capture_output=True, text=True)
        self.assertEqual(command.returncode, 0, command.stderr)
        self.assertIn('--agent-config "/tmp/private"', command.stdout)
        command = subprocess.run(['make', '-n', 'verify-media', 'AGENT_CONFIG=/tmp/private'], cwd=ROOT, capture_output=True, text=True)
        self.assertNotEqual(command.returncode, 0)
        command = subprocess.run([sys.executable, str(ROOT / 'scripts/media.py'), '--help'], capture_output=True, text=True)
        self.assertEqual(command.returncode, 0, command.stderr)


if __name__ == '__main__':
    unittest.main()
