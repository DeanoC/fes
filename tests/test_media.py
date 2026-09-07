import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import time
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
from media_inside import load_manifest, manifest_data, write_manifest

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
        config_sha = hashlib.sha256(config).hexdigest() if inputs.agent_config else None
        write_manifest(output / 'fes-media.toml', manifest_data(image, inputs, lock, config_sha, [sha, sha]))
        return [sha, sha]

    def verify(self, generation, inputs, lock, provision_sha, *, rootfs_verified=True):
        self.checks += 1
        if self.fail:
            raise ValueError('verification failed')
        data = load_manifest(generation / 'fes-media.toml')
        expected = manifest_data(generation / 'fes.img', inputs, lock, provision_sha,
                                 data['assembly']['sha256'], rootfs_verified=rootfs_verified)
        if data != expected:
            raise ValueError('manifest provenance or checks differ')

    def extract(self, generation, destination):
        destination.write_bytes(b'rootfs')

    def extract_config(self, generation, destination):
        destination.write_bytes((generation / 'fes.img').read_bytes()[len(b'rootfs'):])
        destination.chmod(0o600)

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
        self.addCleanup(patch.stopall)
        patch.object(cold_build, 'git', return_value='f' * 40).start()
        self.output = self.root / 'out/native-integration-dev'
        self.output.mkdir(parents=True)
        self.fogcast = self.root / 'fogcast'
        (self.fogcast / 'build/output/target-image/native-dev').mkdir(parents=True)
        self.log = self.fogcast / 'build/output/target-image/native-dev/qemu-smoke.log'
        self.log.write_text('original smoke')
        self.log.chmod(0o640)
        (self.output / 'linux.img').write_bytes(b'rootfs')
        (self.output / 'idle.rbf').write_bytes(b'idle')
        (self.output / 'fogcast').write_bytes(b'host cli')
        (self.output / 'fogcast-api').write_bytes(b'host api')
        cold_build.write_receipt(self.output, 'host', 'cold-fp', ['fogcast', 'fogcast-api'])
        (self.output / 'manifest.tsv').write_text('verified child manifest')
        (self.output / 'qemu-smoke.log').write_text('smoke')
        sha = cold_build.digest(self.output / 'linux.img')
        (self.output / 'reproducibility.txt').write_text(f'run_1_sha256={sha}\nrun_2_sha256={sha}\n')
        cold_build.write_receipt(self.output, 'image', 'cold-fp', ['linux.img', 'manifest.tsv'])
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
        (self.fogcast / 'build/native-runtime.inputs.lock.toml').write_text(
            '[idle_rbf]\nrepository="https://github.com/MiSTer-devel/Distribution_MiSTer"\n'
            'commit="' + 'd' * 40 + '"\npath="menu.rbf"\nsize=4\nsha256="' + hashlib.sha256(b'idle').hexdigest()
            + '"\ninstall_path="/usr/share/mister-runtime/idle.rbf"\n')

    def build(self, config=None):
        return media.build(self.root, 'native-integration-dev', config, self.runner)

    def test_refreshed_qemu_and_recipe_evidence_preserve_identical_disk_history(self):
        for change in ('qemu', 'recipe'):
            for action in ('build', 'verify'):
                with self.subTest(change=change, action=action):
                    shutil.rmtree(self.output / 'media', ignore_errors=True)
                    first = self.build()
                    retained = {path.name: path.read_bytes() for path in first.generation.iterdir()}
                    if change == 'qemu':
                        (self.output / 'qemu-smoke.log').write_text('refreshed smoke ' + action)
                        sha = cold_build.digest(self.output / 'linux.img')
                        (self.output / 'verification.json').write_text(json.dumps(cold_build.verification_record(self.output, sha, False)))
                    recipe = {'scripts/media.py': 'new recipe ' + action} if change == 'recipe' else {'scripts/media.py': 'recipe'}
                    with patch.object(media, 'recipe_fingerprint', return_value=recipe):
                        try:
                            result = self.build() if action == 'build' else media.verify(self.root, runner=self.runner)
                        except ValueError as error:
                            self.fail('valid refreshed evidence must support unchanged disk bytes: ' + str(error))
                        self.assertEqual(cold_build.digest(result.image), cold_build.digest(first.image))
                        self.assertNotEqual(result.generation, first.generation)
                        self.assertEqual(result.generation.parent.name, cold_build.digest(result.image))
                        self.assertEqual(result.generation.name, cold_build.digest(result.generation / 'media.json'))
                        self.assertEqual(media.verify(self.root, runner=self.runner).generation, result.generation)
                    self.assertEqual({path.name: path.read_bytes() for path in first.generation.iterdir()}, retained)

    def test_provisioned_evidence_refresh_uses_private_embedded_snapshot(self):
        config = self.root / 'config'
        config.write_bytes(b'private embedded configuration')
        config.chmod(0o600)
        first = self.build(config)
        config.unlink()
        (self.output / 'qemu-smoke.log').write_text('refreshed valid smoke')
        (self.output / 'verification.json').write_text(json.dumps(cold_build.verification_record(self.output, cold_build.digest(self.output / 'linux.img'), False)))
        try:
            refreshed = media.verify(self.root, runner=self.runner)
        except ValueError as error:
            self.fail('provisioned evidence must refresh from the verified embedded config: ' + str(error))
        self.assertEqual(refreshed.image.read_bytes(), first.image.read_bytes())
        self.assertNotEqual(refreshed.generation, first.generation)
        self.assertEqual(sorted(path.name for path in refreshed.generation.iterdir()), ['fes-media.toml', 'fes.img', 'media.json'])

    def test_auto_provisioned_build_embeds_derived_config_and_only_records_digest(self):
        host = self.root / 'host-config'
        host.write_text('base_url = "http://192.0.2.10:8182"\ntoken = "auto-token"\n')
        host.chmod(0o600)
        expected = (
            'listen_address = "0.0.0.0:8182"\n'
            'token = "auto-token"\n'
            'mister_process_comm = "MiSTer"\n'
            'command_pipe = "/dev/MiSTer_cmd"\n'
            'core_name_file = "/tmp/CORENAME"\n'
            'menu_rbf = "/media/fat/menu.rbf"\n'
            'mgl_directory = "/tmp/fogcast"\n'
        ).encode()
        with patch.dict(os.environ, {'FES_HOST_CONFIG': str(host), 'CI': '', 'FES_UNPROVISIONED': ''}, clear=False):
            result = media.build(self.root, 'native-integration-dev', None, self.runner,
                                 auto_agent_config=True)
        self.assertEqual(self.runner.snapshot, expected)
        self.assertIn('provisioned = true', result.manifest_text)
        self.assertIn(hashlib.sha256(expected).hexdigest(), result.manifest_text)
        evidence = (result.generation / 'media.json').read_text()
        self.assertIn(hashlib.sha256(expected).hexdigest(), evidence)
        self.assertNotIn('auto-token', result.manifest_text + evidence)

    def test_termination_after_current_replace_restores_previous_selection(self):
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            with self.subTest(signal=signum):
                first = self.build()
                config = self.root / 'config'
                config.write_bytes(str(signum).encode())
                config.chmod(0o600)
                sync = media.fsync_dir
                fired = False
                def terminate_after_replace(path):
                    nonlocal fired
                    current = self.output / 'media/current'
                    if path == current.parent and current.resolve() != first.generation and not fired:
                        fired = True
                        signal.raise_signal(signum)
                    return sync(path)
                with media.termination_handling(), patch.object(media, 'fsync_dir', side_effect=terminate_after_replace):
                    with self.assertRaises(media.TerminationRequested):
                        self.build(config)
                self.assertEqual((self.output / 'media/current').resolve(), first.generation)

    def test_rollback_holds_lease_through_validation_and_selection(self):
        self.assertTrue(hasattr(media, 'rollback'), 'leased rollback entrypoint is required')
        first = self.build()
        config = self.root / 'config'
        config.write_bytes(b'another provisioned disk')
        config.chmod(0o600)
        second = self.build(config)
        target = str(first.generation.relative_to(self.output / 'media/generations'))
        with media.lease(self.root, 'native-integration-dev'):
            with self.assertRaisesRegex(ValueError, 'another media'):
                media.rollback(self.root, target, runner=self.runner)
        self.assertEqual((self.output / 'media/current').resolve(), second.generation)
        self.runner.fail = True
        with self.assertRaisesRegex(ValueError, 'verification failed'):
            media.rollback(self.root, target, runner=self.runner)
        self.assertEqual((self.output / 'media/current').resolve(), second.generation)
        self.runner.fail = False
        original = self.runner.child_verify
        def concurrent(fogcast, staged, env):
            code = 'import sys;sys.path.insert(0,sys.argv[1]);from pathlib import Path;import media\nwith media.lease(Path(sys.argv[2]),"native-integration-dev"): pass'
            competitor = subprocess.run([sys.executable, '-c', code, str(ROOT / 'scripts'), str(self.root)], capture_output=True, text=True)
            self.assertNotEqual(competitor.returncode, 0)
            self.assertIn('another media', competitor.stderr)
            self.assertEqual((self.output / 'media/current').resolve(), second.generation)
            original(fogcast, staged, env)
        self.runner.child_verify = concurrent
        selected = media.rollback(self.root, target, runner=self.runner)
        self.assertEqual(selected.generation, first.generation)
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)

    def test_legacy_flat_evidence_refreshes_without_overwriting_original_files(self):
        first = self.build()
        outer = first.generation.parent
        for path in first.generation.iterdir():
            shutil.copyfile(path, outer / path.name)
            (outer / path.name).chmod(0o600)
        original = {name: (outer / name).read_bytes() for name in ('fes.img', 'fes-media.toml', 'media.json')}
        current = self.output / 'media/current'
        current.unlink()
        current.symlink_to('generations/' + outer.name)
        try:
            selected = media.verify(self.root, runner=self.runner)
        except ValueError as error:
            self.fail('legacy immutable files must remain usable as refresh candidates: ' + str(error))
        self.assertEqual(selected.generation, first.generation)
        self.assertEqual(current.resolve(), first.generation)
        self.assertEqual(media.rollback(self.root, outer.name, runner=self.runner).generation, first.generation)
        self.assertEqual({name: (outer / name).read_bytes() for name in original}, original)

    def test_rehashed_evidence_cannot_change_its_outer_disk_identity(self):
        first = self.build()
        first.image.write_bytes(b'different disk')
        receipt_path = first.generation / 'media.json'
        receipt = json.loads(receipt_path.read_text())
        receipt['image_sha256'] = cold_build.digest(first.image)
        receipt_path.write_text(json.dumps(receipt))
        renamed = first.generation.with_name(cold_build.digest(receipt_path))
        first.generation.rename(renamed)
        with self.assertRaisesRegex(ValueError, 'receipt|digest'):
            media.read_receipt(renamed)

    def test_current_recipe_cannot_refresh_to_different_disk_bytes(self):
        first = self.build()
        original = self.runner.assemble
        def different(output, inputs, lock):
            original(output, inputs, lock)
            image = output / 'fes.img'
            image.write_bytes(image.read_bytes() + b'new recipe bytes')
            sha = cold_build.digest(image)
            return [sha, sha]
        self.runner.assemble = different
        with patch.object(media, 'recipe_fingerprint', return_value={'scripts/media.py': 'new policy'}):
            with self.assertRaisesRegex(ValueError, 'retained disk differs'):
                media.verify(self.root, runner=self.runner)
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)

    def test_rollback_termination_during_sync_restores_previous_selection(self):
        first = self.build()
        config = self.root / 'config'
        config.write_bytes(b'current provisioned disk')
        config.chmod(0o600)
        second = self.build(config)
        target = str(first.generation.relative_to(self.output / 'media/generations'))
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            with self.subTest(signal=signum):
                sync = media.fsync_dir
                fired = False
                def terminate(path):
                    nonlocal fired
                    current = self.output / 'media/current'
                    if path == current.parent and current.resolve() == first.generation and not fired:
                        fired = True
                        signal.raise_signal(signum)
                    return sync(path)
                with media.termination_handling(), patch.object(media, 'fsync_dir', side_effect=terminate):
                    with self.assertRaises(media.TerminationRequested):
                        media.rollback(self.root, target, runner=self.runner)
                self.assertEqual((self.output / 'media/current').resolve(), second.generation)

    def test_refresh_resolves_current_only_once(self):
        self.build()
        reads = []
        readlink = os.readlink
        def record(path, *args, **kwargs):
            if Path(path) == self.output / 'media/current':
                reads.append(path)
            return readlink(path, *args, **kwargs)
        with patch.object(media, 'recipe_fingerprint', return_value={'scripts/media.py': 'refreshed recipe'}), \
             patch.object(media.os, 'readlink', side_effect=record):
            media.verify(self.root, runner=self.runner)
        self.assertEqual(len(reads), 1)

    def test_docs_only_head_change_reuses_identical_media_generation(self):
        first = self.build()
        original_receipt = (first.generation / 'media.json').read_bytes()
        original_manifest = (first.generation / 'fes-media.toml').read_bytes()
        # The old implementation consults media.current_revision; the fixed
        # implementation must not consult repository HEAD at all here.
        with patch.object(media, 'current_revision', return_value='0' * 40, create=True), \
             patch.object(cold_build, 'git', return_value='0' * 40):
            try:
                second = self.build()
                verified = media.verify(self.root, runner=self.runner)
            except ValueError as error:
                self.fail('docs-only HEAD change must preserve artifact provenance: ' + str(error))
        self.assertEqual(second.generation, first.generation)
        self.assertEqual(verified.generation, first.generation)
        self.assertEqual((first.generation / 'media.json').read_bytes(), original_receipt)
        self.assertEqual((first.generation / 'fes-media.toml').read_bytes(), original_manifest)

    def test_mismatched_artifact_revisions_rejected_before_media_assembly(self):
        path = self.output / 'image.json'
        receipt = json.loads(path.read_text())
        receipt['fes_revision'] = '0' * 40
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, 'revision'):
            self.build()
        self.assertEqual(self.runner.assemblies, 0)

    def test_changed_valid_artifact_revision_preserves_existing_evidence(self):
        first = self.build()
        for name in ('host', 'image'):
            path = self.output / (name + '.json')
            receipt = json.loads(path.read_text())
            receipt['fes_revision'] = '0' * 40
            path.write_text(json.dumps(receipt))
        original = (first.generation / 'media.json').read_bytes()
        refreshed = self.build()
        self.assertEqual(refreshed.image.read_bytes(), first.image.read_bytes())
        self.assertNotEqual(refreshed.generation, first.generation)
        self.assertEqual((first.generation / 'media.json').read_bytes(), original)

    def test_host_outputs_are_prerequisites_and_bound_to_media_receipt(self):
        first = self.build()
        receipt = json.loads((first.generation / 'media.json').read_text())
        for key, path in (('host_receipt_sha256', 'host.json'), ('fogcast_sha256', 'fogcast'),
                          ('fogcast_api_sha256', 'fogcast-api')):
            self.assertEqual(receipt['inputs']['cold'][key], cold_build.digest(self.output / path))
        for name in ('host.json', 'fogcast', 'fogcast-api'):
            path = self.output / name
            original = path.read_bytes()
            path.unlink()
            with self.assertRaisesRegex(ValueError, 'host'):
                self.build()
            with self.assertRaisesRegex(ValueError, 'host'):
                media.verify(self.root, runner=self.runner)
            path.write_bytes(original)
        (self.output / 'fogcast-api').write_bytes(b'changed host')
        cold_build.write_receipt(self.output, 'host', 'cold-fp', ['fogcast', 'fogcast-api'])
        refreshed = media.verify(self.root, runner=self.runner)
        self.assertEqual(refreshed.image.read_bytes(), first.image.read_bytes())
        self.assertNotEqual(refreshed.generation, first.generation)

    def test_published_manifest_promotes_only_successful_child_checks(self):
        before_child = []
        original = self.runner.child_verify
        def child(fogcast, staged, env):
            manifest, = (self.output / 'media').glob('.staging-*/generation/fes-media.toml')
            data = load_manifest(manifest)
            before_child.append(data['checks'].copy())
            original(fogcast, staged, env)
        self.runner.child_verify = child
        result = self.build()
        data = load_manifest(result.generation / 'fes-media.toml')
        self.assertEqual(before_child[0]['rootfs_structural'], 'not-run')
        self.assertEqual(before_child[0]['rootfs_qemu'], 'not-run')
        self.assertEqual(set(data['checks'].values()), {'pass'})
        self.assertEqual(data['fes']['revision'], 'f' * 40)
        self.assertEqual(data['rootfs']['child_manifest_sha256'], cold_build.digest(self.output / 'manifest.tsv'))
        self.assertEqual(data['rootfs']['image_receipt_sha256'], cold_build.digest(self.output / 'image.json'))
        receipt = json.loads((result.generation / 'media.json').read_text())
        self.assertEqual(receipt['assembly_sha256'], data['assembly']['sha256'])

    def test_unequal_reported_assembly_passes_cannot_publish(self):
        first = self.build()
        original = self.runner.assemble
        def unequal(output, inputs, lock):
            hashes = original(output, inputs, lock)
            return [hashes[0], '0' * 64]
        self.runner.assemble = unequal
        with self.assertRaisesRegex(ValueError, 'independent media assembly hashes'):
            self.build()
        self.assertEqual((self.output / 'media/current').resolve(), first.generation)

    def test_rejects_development_and_missing_verification(self):
        (self.output / 'image.json').unlink()
        (self.output / 'development.json').write_text('{}')
        with self.assertRaisesRegex(ValueError, 'run make build and make verify'):
            self.build()
        self.assertFalse((self.output / 'media/current').exists())

    def test_receipt_and_atomic_relative_generation(self):
        result = self.build()
        self.assertEqual(result.generation.parent.name, cold_build.digest(result.image))
        self.assertEqual(os.readlink(self.output / 'media/current'), 'generations/' + result.generation.parent.name + '/' + result.generation.name)
        receipt = json.loads((result.generation / 'media.json').read_text())
        self.assertEqual(receipt['assembly_sha256'], [result.generation.parent.name] * 2)
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

    def test_dangling_child_log_symlink_is_preserved(self):
        self.log.unlink()
        self.log.symlink_to('missing-original-log')
        with self.assertRaisesRegex(ValueError, 'symlinks'):
            self.build()
        self.assertTrue(self.log.is_symlink(), 'rejected original log entry must survive')
        self.assertEqual(os.readlink(self.log), 'missing-original-log')

    def test_failed_restoration_retains_recoverable_backup(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir(mode=0o750)
        (staged / 'keep').write_text('preserved')
        replace = os.replace
        def fail_restore(source, destination):
            if Path(source).name == 'scratch' and Path(destination) == staged:
                raise OSError('restore denied')
            return replace(source, destination)
        with patch.object(media.os, 'replace', side_effect=fail_restore):
            with self.assertRaises(Exception) as caught:
                self.build()
        self.assertIn('backup retained at', str(caught.exception))
        backups = list(staged.parent.glob('.media-restore-*'))
        self.assertEqual(len(backups), 1)
        self.assertIn(str(backups[0]), str(caught.exception))
        self.assertEqual((backups[0] / 'scratch/keep').read_text(), 'preserved')
        self.assertEqual(stat.S_IMODE((backups[0] / 'scratch').stat().st_mode), 0o750)
        self.assertEqual(self.log.read_text(), 'original smoke')

    def test_cancelled_container_is_removed_before_scratch_restoration(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir()
        (staged / 'keep').write_text('preserved')
        runtime = self.root / 'runtime'
        removed = self.root / 'removed'
        runtime.write_text('#!/bin/sh\nif [ "$1" = inspect ]; then echo "No such container" >&2; exit 1; fi\ntest "$1" = rm && test "$2" = --force || exit 9\ntest ! -e "' + str(staged / 'keep') + '" || exit 8\nprintf "%s" "$3" > "' + str(removed) + '"\n')
        runtime.chmod(0o700)
        runner = object.__new__(media.Runner)
        runner.runtime = str(runtime)
        with self.assertRaises(Exception) as caught:
            with media.termination_handling(), media.child_scratch(self.fogcast):
                runner.run([sys.executable, '-c', 'import os,signal,time; os.kill(os.getppid(),signal.SIGTERM); time.sleep(60)'], container_id='a' * 64)
        self.assertIsInstance(caught.exception, media.TerminationRequested)
        self.assertEqual(removed.read_text(), 'a' * 64)
        self.assertEqual((staged / 'keep').read_text(), 'preserved')

    def test_failed_container_client_removes_owned_container(self):
        runtime = self.root / 'runtime'
        removed = self.root / 'removed'
        runtime.write_text('#!/bin/sh\nif [ "$1" = inspect ]; then echo "No such container" >&2; exit 1; fi\ntest "$1" = rm && test "$2" = --force || exit 9\nprintf "%s" "$3" > "' + str(removed) + '"\n')
        runtime.chmod(0o700)
        runner = object.__new__(media.Runner)
        runner.runtime = str(runtime)
        with self.assertRaisesRegex(ValueError, 'child output withheld'):
            runner.run([sys.executable, '-c', 'raise SystemExit(7)'], container_id='b' * 64)
        self.assertTrue(removed.exists(), 'client failure must stop its owned container')
        self.assertEqual(removed.read_text(), 'b' * 64)

    def test_signal_before_container_identity_cannot_leave_daemon_writer(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir(mode=0o750)
        (staged / 'keep').write_text('preserved')
        scripts = self.fogcast / 'scripts'
        scripts.mkdir()
        wrapper = scripts / 'target-image-container.sh'
        wrapper.write_text('#!/bin/sh\nexec "$TARGET_IMAGE_CONTAINER_RUNTIME" run --rm fixture-image true\n')
        wrapper.chmod(0o700)
        runtime = self.root / 'runtime'
        fixture = self.root / 'daemon'
        fixture.mkdir()
        runtime_code = r'''import json,os,subprocess,sys,time
from pathlib import Path
state=Path(FIXTURE)
args=sys.argv[1:]
command=args[0]
identity='c'*64
with (state/'calls').open('a') as stream: stream.write(json.dumps(args)+'\n')
if command in ('run','create'):
    if command == 'run':
        (state/'container').write_text(identity)
        child_code="import time; from pathlib import Path; state=Path("+repr(str(state))+"); scratch=Path("+repr(SCRATCH)+"); log=Path("+repr(LOG)+");\nwhile (state/'container').exists():\n if (scratch/'keep').exists(): (state/'post-restore-write').write_text('late write'); log.write_text('daemon changed restored log')\n time.sleep(0.01)"
        child=subprocess.Popen([sys.executable,'-c',child_code],start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        (state/'pid').write_text(str(child.pid))
    (state/'ready').write_text(command)
    while not (state/'release').exists(): time.sleep(0.01)
    if command == 'create':
        (state/'container').write_text(identity)
        (state/'name').write_text(args[args.index('--name')+1])
    else:
        Path(args[args.index('--cidfile')+1]).write_text(identity)
    print(identity,flush=True)
    if command == 'run':
        while (state/'container').exists(): time.sleep(0.01)
elif command == 'start':
    (state/'started').write_text(args[-1])
elif command == 'rm':
    assert args[-1] == identity or args[-1] == (state/'name').read_text()
    (state/'container').unlink(missing_ok=True)
elif command == 'inspect':
    if (state/'container').exists(): print(identity)
    else: print('No such container',file=sys.stderr); sys.exit(1)
else:
    raise SystemExit(9)
'''
        runtime.write_text('#!' + sys.executable + '\n' + runtime_code.replace('FIXTURE', repr(str(fixture))).replace('SCRATCH', repr(str(staged))).replace('LOG', repr(str(self.log))))
        runtime.chmod(0o700)
        parent_code = r'''import sys
from pathlib import Path
sys.path.insert(0,sys.argv[1])
import media
root,fogcast,runtime=map(Path,sys.argv[2:5])
mode=sys.argv[5]
def build(*args):
    with media.child_scratch(fogcast) as scratch:
        runner=object.__new__(media.Runner)
        runner.root=root; runner.runtime=str(runtime); runner.container='fixture-image'
        if mode == 'disk': runner.disk(['true'])
        else: runner.child_verify(fogcast,scratch,{})
media.build=build
sys.argv=['media.py','build']
media.main()
'''
        for mode in ('disk', 'child'):
            with self.subTest(mode=mode):
                for path in fixture.iterdir(): path.unlink()
                process = subprocess.Popen([sys.executable, '-c', parent_code, str(ROOT / 'scripts'), str(self.root), str(self.fogcast), str(runtime), mode], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
                try:
                    deadline = time.monotonic() + 5
                    while not (fixture / 'ready').exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue((fixture / 'ready').exists(), 'runtime did not reach pre-identity window')
                    process.send_signal(signal.SIGTERM)
                    # Let ordinary termination happen before the runtime returns ID.
                    time.sleep(0.05)
                    (fixture / 'release').touch()
                    stdout, stderr = process.communicate(timeout=10)
                    self.assertNotEqual(process.returncode, 0)
                    time.sleep(0.05)
                    self.assertFalse((fixture / 'container').exists(), 'owned daemon container survived startup termination')
                    self.assertFalse((fixture / 'post-restore-write').exists(), 'daemon wrote after original paths were restored')
                    self.assertFalse((fixture / 'started').exists(), 'signal during create must prevent start')
                    self.assertEqual((staged / 'keep').read_text(), 'preserved')
                    self.assertEqual(self.log.read_text(), 'original smoke')
                    self.assertEqual(stat.S_IMODE(staged.stat().st_mode), 0o750)
                    self.assertEqual(stat.S_IMODE(self.log.stat().st_mode), 0o640)
                    self.assertEqual(list(staged.parent.glob('.media-restore-*')), [])
                finally:
                    (fixture / 'container').unlink(missing_ok=True)
                    if process.poll() is None: process.kill()
                    process.wait()
                    process.stdout.close(); process.stderr.close()
                    if (fixture / 'pid').exists():
                        try: os.kill(int((fixture / 'pid').read_text()), signal.SIGKILL)
                        except ProcessLookupError: pass
                    self.log.write_text('original smoke')

    def test_both_container_paths_start_only_known_identity_and_confirm_removal(self):
        runtime = self.root / 'runtime'
        calls = self.root / 'runtime-calls'
        runtime.write_text('#!' + sys.executable + '\nimport json,sys\nfrom pathlib import Path\n'
            + 'with Path(' + repr(str(calls)) + ').open("a") as stream: stream.write(json.dumps(sys.argv[1:])+"\\n")\n'
            + 'if sys.argv[1] == "create": print("d"*64)\n'
            + 'elif sys.argv[1] == "start": print("payload output")\n'
            + 'elif sys.argv[1] == "inspect": print("No such container",file=sys.stderr); sys.exit(1)\n')
        runtime.chmod(0o700)
        scripts = self.fogcast / 'scripts'
        scripts.mkdir()
        wrapper = scripts / 'target-image-container.sh'
        wrapper.write_text('#!/bin/sh\nexec "$TARGET_IMAGE_CONTAINER_RUNTIME" run --rm fixture-image true\n')
        wrapper.chmod(0o700)
        runner = object.__new__(media.Runner)
        runner.root = self.root
        runner.runtime = str(runtime)
        runner.container = 'fixture-image'
        for mode in ('disk', 'child'):
            with self.subTest(mode=mode):
                calls.unlink(missing_ok=True)
                with media.child_scratch(self.fogcast) as staged:
                    if mode == 'disk':
                        self.assertEqual(runner.disk(['true']), 'payload output\n')
                    else:
                        runner.child_verify(self.fogcast, staged, {})
                commands = [json.loads(line) for line in calls.read_text().splitlines()]
                self.assertEqual([command[0] for command in commands], ['create', 'start', 'rm', 'inspect'])
                name = commands[0][commands[0].index('--name') + 1]
                self.assertRegex(name, '^fes-media-[0-9a-f]{32}$')
                self.assertIn('org.fes.media.operation=' + name, commands[0])
                self.assertEqual(commands[1], ['start', '--attach', 'd' * 64])
                self.assertEqual(commands[2], ['rm', '--force', 'd' * 64])
                self.assertEqual(commands[3][-1], 'd' * 64)

    def test_cli_signals_stop_live_child_before_restoring_scratch(self):
        staged = self.fogcast / 'build/output/target-image/media-verify'
        staged.mkdir(mode=0o750)
        (staged / 'keep').write_text('preserved')
        (staged / 'keep').chmod(0o640)
        ready, stopped = self.root / 'ready', self.root / 'stopped'
        child_code = r'''import os,signal,sys,time
from pathlib import Path
ready,stopped,scratch,log = map(Path,sys.argv[1:])
def stop(signum, frame):
    stopped.write_text('stopped-before-restore' if not (scratch/'keep').exists() and log.read_text() == 'live child' else 'restored-too-early')
    sys.exit(0)
signal.signal(signal.SIGTERM,stop)
log.write_text('live child')
ready.write_text(str(os.getpid()))
while True: time.sleep(1)
'''
        parent_code = r'''import sys
from pathlib import Path
sys.path.insert(0,sys.argv[1])
import media
fogcast,ready,stopped=map(Path,sys.argv[2:5])
child_code=sys.argv[5]
def build(*args):
    with media.child_scratch(fogcast) as scratch:
        runner=object.__new__(media.Runner)
        runner.run([sys.executable,'-c',child_code,ready,stopped,scratch,fogcast/'build/output/target-image/native-dev/qemu-smoke.log'])
media.build=build
sys.argv=['media.py','build']
media.main()
'''
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            with self.subTest(signal=signum):
                ready.unlink(missing_ok=True)
                stopped.unlink(missing_ok=True)
                process = subprocess.Popen([sys.executable, '-c', parent_code, str(ROOT / 'scripts'), str(self.fogcast), str(ready), str(stopped), child_code], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
                child_pid = None
                try:
                    deadline = time.monotonic() + 5
                    while not ready.exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue(ready.exists(), 'live child did not become ready')
                    child_pid = int(ready.read_text())
                    process.send_signal(signum)
                    stdout, stderr = process.communicate(timeout=10)
                    self.assertEqual(process.returncode, 128 + signum, stderr)
                    self.assertIn("cleanup completed", stderr)
                    self.assertTrue(stopped.exists(), 'termination must stop and wait for the active child')
                    self.assertEqual(stopped.read_text(), 'stopped-before-restore')
                    with self.assertRaises(ProcessLookupError):
                        os.kill(child_pid, 0)
                    self.assertEqual((staged / 'keep').read_text(), 'preserved')
                    self.assertEqual(stat.S_IMODE(staged.stat().st_mode), 0o750)
                    self.assertEqual(stat.S_IMODE((staged / 'keep').stat().st_mode), 0o640)
                    self.assertEqual(self.log.read_text(), 'original smoke')
                    self.assertEqual(stat.S_IMODE(self.log.stat().st_mode), 0o640)
                    self.assertEqual(list(staged.parent.glob('.media-restore-*')), [])
                finally:
                    if process.poll() is None:
                        process.kill()
                    process.wait()
                    process.stdout.close()
                    process.stderr.close()
                    if child_pid:
                        try:
                            os.kill(child_pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass

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
        command = subprocess.run(['make', '-n', 'media'], cwd=ROOT, capture_output=True, text=True)
        self.assertEqual(command.returncode, 0, command.stderr)
        self.assertIn('--auto-agent-config', command.stdout)
        command = subprocess.run(['make', '-n', 'media', 'FES_UNPROVISIONED=1'], cwd=ROOT, capture_output=True, text=True)
        self.assertEqual(command.returncode, 0, command.stderr)
        self.assertIn('--unprovisioned', command.stdout)
        command = subprocess.run(['make', '-n', 'verify-media', 'AGENT_CONFIG=/tmp/private'], cwd=ROOT, capture_output=True, text=True)
        self.assertNotEqual(command.returncode, 0)
        command = subprocess.run([sys.executable, str(ROOT / 'scripts/media.py'), '--help'], capture_output=True, text=True)
        self.assertEqual(command.returncode, 0, command.stderr)



class AgentConfigTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_generate_agent_config_from_private_legacy_host_config(self):
        host = self.root / 'host.toml'
        host.write_text('base_url = "http://192.0.2.10:8182"\ntoken = "host-token"\n')
        host.chmod(0o600)
        scratch = self.root / 'scratch'
        scratch.mkdir()
        config, config_sha = media.generate_agent_config(host, scratch)
        expected = (
            'listen_address = "0.0.0.0:8182"\n'
            'token = "host-token"\n'
            'mister_process_comm = "MiSTer"\n'
            'command_pipe = "/dev/MiSTer_cmd"\n'
            'core_name_file = "/tmp/CORENAME"\n'
            'menu_rbf = "/media/fat/menu.rbf"\n'
            'mgl_directory = "/tmp/fogcast"\n'
        ).encode()
        self.assertEqual(config.read_bytes(), expected)
        self.assertEqual(config_sha, hashlib.sha256(expected).hexdigest())
        self.assertEqual(stat.S_IMODE(config.stat().st_mode), 0o600)

    def test_generate_agent_config_uses_selected_enabled_target(self):
        host = self.root / 'host-targets.toml'
        host.write_text(
            'selected_target = " kit "\n'
            '[[targets]]\n'
            'name = "kit"\n'
            'enabled = true\n'
            'address = "http://192.0.2.10:8182"\n'
            'agent = "selected-token"\n'
        )
        host.chmod(0o600)
        scratch = self.root / 'scratch-targets'
        scratch.mkdir()
        config, _ = media.generate_agent_config(host, scratch)
        self.assertIn(b'token = "selected-token"\n', config.read_bytes())

    def test_generate_agent_config_rejects_duplicate_normalized_target_names(self):
        host = self.root / 'duplicate-targets.toml'
        host.write_text(
            'selected_target = "kit"\n'
            '[[targets]]\n'
            'name = "kit"\n'
            'enabled = true\n'
            'agent = "first-token"\n'
            '[[targets]]\n'
            'name = " kit "\n'
            'enabled = true\n'
            'agent = "second-token"\n'
        )
        host.chmod(0o600)
        scratch = self.root / 'duplicate-scratch'
        scratch.mkdir()
        with self.assertRaisesRegex(ValueError, 'could not be converted'):
            media.generate_agent_config(host, scratch)

    def test_generate_agent_config_rejects_token_outside_target_bearer_grammar(self):
        host = self.root / 'invalid-token.toml'
        host.write_text('token = "bad!token"\n')
        host.chmod(0o600)
        scratch = self.root / 'invalid-token-scratch'
        scratch.mkdir()
        with self.assertRaisesRegex(ValueError, 'could not be converted'):
            media.generate_agent_config(host, scratch)

    def test_generate_agent_config_rejects_oversized_host_config(self):
        host = self.root / 'oversized-host.toml'
        host.write_bytes(b'token = "host-token"\n' + b'#' * media.MAX_CONFIG_BYTES)
        host.chmod(0o600)
        scratch = self.root / 'oversized-scratch'
        scratch.mkdir()
        with self.assertRaisesRegex(ValueError, 'could not be converted'):
            media.generate_agent_config(host, scratch)

    def test_generate_agent_config_rejects_generated_config_over_size_limit(self):
        token = 'a' * (media.MAX_CONFIG_BYTES - 32)
        host = self.root / 'oversized-generated-host.toml'
        host.write_text('token = "' + token + '"\n')
        host.chmod(0o600)
        self.assertLessEqual(host.stat().st_size, media.MAX_CONFIG_BYTES)
        scratch = self.root / 'oversized-generated-scratch'
        scratch.mkdir()
        with self.assertRaisesRegex(ValueError, 'could not be converted'):
            media.generate_agent_config(host, scratch)

    def test_auto_agent_config_requires_private_host_config_and_allows_explicit_unprovisioned(self):
        scratch = self.root / 'auto-scratch'
        scratch.mkdir()
        with patch.dict(os.environ, {'FES_HOST_CONFIG': str(self.root / 'missing.toml')}, clear=True):
            with self.assertRaisesRegex(ValueError, 'automatic media provisioning requires'):
                media.resolve_agent_config(None, scratch, auto=True)
            self.assertEqual(media.resolve_agent_config(None, scratch, auto=False), (None, None))

    def test_auto_agent_config_suppressed_in_ci(self):
        host = self.root / 'host-ci.toml'
        host.write_text('base_url = "http://192.0.2.10:8182"\ntoken = "host-token"\n')
        host.chmod(0o600)
        scratch = self.root / 'ci-scratch'
        scratch.mkdir()
        with patch.dict(os.environ, {'FES_HOST_CONFIG': str(host), 'CI': 'true'}, clear=True):
            self.assertEqual(media.resolve_agent_config(None, scratch, auto=True), (None, None))


if __name__ == '__main__':
    unittest.main()
