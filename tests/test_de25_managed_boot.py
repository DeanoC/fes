"""Packaging-proof refusal and child-lifecycle tests, not an emulated board boot."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'image/de25-nano'))
SPEC = importlib.util.spec_from_file_location('de25_managed_boot', ROOT / 'image/de25-nano/managed_boot.py')
BOOT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BOOT)
GOOD_LOG = ('TARGET_IMAGE_SMOKE_ROOT options=relatime,ro\n'
            'TARGET_IMAGE_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs\n'
            'TARGET_IMAGE_SMOKE_DBUS machine-id and system bus ready\n'
            'TARGET_IMAGE_SMOKE_READY\n')


class ManagedBootTests(unittest.TestCase):
    def fixture(self, root, profile='managed-base'):
        directory, output = root / 'image', root / 'proof'
        (directory / 'kernel').mkdir(parents=True)
        output.mkdir()
        inputs = {'fes_revision': 'a' * 40, 'recipe_sha256': 'b' * 64}
        if profile == 'managed-factory':
            inputs['profile'] = profile
        (directory / 'base-inputs.json').write_text(json.dumps(inputs))
        (directory / 'rootfs.ext4').write_bytes(b'unit fixture; not a filesystem')
        (directory / 'kernel/Image').write_bytes(b'unit fixture; not a kernel')
        return directory, output

    def unit_report(self, directory, output, profile='managed-base'):
        def observe(argv, log):
            # This stand-in exercises report binding only. Actual QEMU evidence
            # is produced separately using the retained managed base/kernel.
            Path(log).write_text(GOOD_LOG)
            Path(argv[argv.index('-drive') + 1].split(',')[0].removeprefix('file=')).write_bytes(b'guest changed disposable disk')
            return {'reason': 'observation-complete', 'returncode': 0}
        with mock.patch.object(BOOT, 'observe', side_effect=observe), mock.patch.object(BOOT, 'qemu_provenance', return_value={
                'version': 'QEMU emulator version unit fixture', 'executable_sha256': 'c' * 64}):
            return BOOT.boot(directory, output, profile, 'd' * 40)

    def test_log_requires_exact_sentinels_and_readonly_mount_token(self):
        with tempfile.TemporaryDirectory() as temporary:
            log = Path(temporary) / 'log'
            log.write_text(GOOD_LOG)
            self.assertTrue(BOOT.verify_log(log)['readonly_root'])
            bad = [GOOD_LOG.replace(',ro', ',root'), GOOD_LOG.replace(',ro', ',rw,ro'),
                   GOOD_LOG.replace('TARGET_IMAGE_SMOKE_READY', 'echo TARGET_IMAGE_SMOKE_READY'),
                   GOOD_LOG.replace('writable tmpfs', 'writable ext4'),
                   GOOD_LOG.replace('TARGET_IMAGE_SMOKE_DBUS machine-id and system bus ready\n', ''),
                   GOOD_LOG + 'TARGET_IMAGE_SMOKE_FAILED mount=/run\n',
                   GOOD_LOG + 'Kernel panic - not syncing: late failure\n',
                   GOOD_LOG + 'Internal error: Oops\n',
                   GOOD_LOG + 'mister-main: waiting for /media/fat payloads\n',
                   GOOD_LOG + 'TARGET_IMAGE_SMOKE_ROOT options=rw\n']
            for contents in bad:
                with self.subTest(contents=contents):
                    log.write_text(contents)
                    with self.assertRaises(ValueError):
                        BOOT.verify_log(log)

    def test_shared_de10_log_gate_also_requires_the_live_bus_probe(self):
        with tempfile.TemporaryDirectory() as temporary:
            log = Path(temporary) / 'log'
            command = ['sh', str(ROOT / 'image/scripts/qemu-smoke-target-image.sh'),
                       '--verify-log', 'native-dev', str(log)]
            env = {**os.environ, 'TARGET_IMAGE_TEST_MODE': '1'}
            log.write_text(GOOD_LOG)
            subprocess.run(command, env=env, check=True, capture_output=True)
            log.write_text(GOOD_LOG.replace('TARGET_IMAGE_SMOKE_DBUS machine-id and system bus ready\n', ''))
            self.assertNotEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)

    def test_actual_observer_stops_and_reaps_its_own_child(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            script = 'import os,time; print(os.getpid(),flush=True); time.sleep(60)'
            result = BOOT.observe([sys.executable, '-c', script], root / 'log', observation_seconds=.2)
            self.assertEqual(result, {'reason': 'observation-complete', 'returncode': -signal.SIGTERM})
            pid = int((root / 'log').read_text().strip())
            with self.assertRaises(ProcessLookupError):
                os.kill(pid, 0)

    def test_nonzero_child_and_ignored_termination_never_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            script = 'import os,sys; print(os.getpid(),flush=True); sys.exit(7)'
            with self.assertRaisesRegex(ValueError, 'exited unsuccessfully'):
                BOOT.observe([sys.executable, '-c', script], root / 'early', observation_seconds=.2)
            script = ('import os,signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); '
                      'print(os.getpid(),flush=True); time.sleep(60)')
            with self.assertRaisesRegex(ValueError, 'did not stop'):
                BOOT.observe([sys.executable, '-c', script], root / 'stubborn',
                             observation_seconds=.2, terminate_seconds=.2)
            for name in ('early', 'stubborn'):
                pid = int((root / name).read_text().strip())
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)

    def test_report_binds_actual_bytes_and_keeps_profiles_separate(self):
        for profile in BOOT.PROFILES:
            with self.subTest(profile=profile), tempfile.TemporaryDirectory() as temporary:
                directory, output = self.fixture(Path(temporary), profile)
                original = (directory / 'rootfs.ext4').read_bytes()
                report = self.unit_report(directory, output, profile)
                self.assertEqual(BOOT.verify_report(directory, output, profile), report)
                self.assertEqual((directory / 'rootfs.ext4').read_bytes(), original)
                self.assertFalse(list(output.glob('.boot-smoke-*')))
                self.assertEqual(report['image']['profile'], profile)
                self.assertEqual(report['hardware_acceptance'], 'not-run')
                self.assertEqual(report['normal_runtime_admission'], 'closed')
                other = 'managed-factory' if profile == 'managed-base' else 'managed-base'
                with self.assertRaises(ValueError):
                    BOOT.verify_report(directory, output, other)
                for name in ('rootfs.ext4', 'kernel/Image', 'base-inputs.json'):
                    path = directory / name; before = path.read_bytes()
                    path.write_bytes(before + b' ')
                    with self.assertRaises(ValueError):
                        BOOT.verify_report(directory, output, profile)
                    path.write_bytes(before)

    def test_altered_log_receipt_recipe_and_command_are_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, output = self.fixture(Path(temporary))
            report = self.unit_report(directory, output)
            for field, value in (('producer_revision', 'invalid'), ('builder_image', 'different'),
                                 ('recipe_sha256', '0' * 64), ('hardware_acceptance', 'passed'),
                                 ('observation_seconds', 1), ('termination', {'reason': 'guest-exited', 'returncode': 7}),
                                 ('argv', ['invalid']), ('checks', {})):
                with self.subTest(field=field):
                    changed = copy.deepcopy(report); changed[field] = value
                    BOOT.save(output / 'boot.json', changed)
                    with self.assertRaises(ValueError):
                        BOOT.verify_report(directory, output, 'managed-base')
            BOOT.save(output / 'boot.json', report)
            with self.assertRaises(ValueError):
                BOOT.verify_report(directory, output, 'managed-base', revision='e' * 40)
            (output / 'qemu-smoke.log').write_text(GOOD_LOG.replace(',ro', ',rw'))
            report['qemu_log_sha256'] = BOOT.image.digest(output / 'qemu-smoke.log')
            BOOT.save(output / 'boot.json', report)
            with self.assertRaises(ValueError):
                BOOT.verify_report(directory, output, 'managed-base')

    def test_changed_input_retains_failure_and_does_not_publish_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, output = self.fixture(Path(temporary))
            def changing_input(argv, log):
                Path(log).write_text(GOOD_LOG)
                (directory / 'rootfs.ext4').write_bytes(b'changed')
                return {'reason': 'observation-complete', 'returncode': 0}
            with mock.patch.object(BOOT, 'observe', side_effect=changing_input), \
                    mock.patch.object(BOOT, 'qemu_provenance', return_value={}):
                with self.assertRaisesRegex(ValueError, 'input changed'):
                    BOOT.boot(directory, output, 'managed-base', 'd' * 40)
            self.assertFalse((output / 'boot.json').exists())
            self.assertEqual(json.loads((output / 'boot-run.json').read_text())['state'], 'failed')
            self.assertEqual((output / 'qemu-smoke.log').read_text(), GOOD_LOG)
            before = (output / 'boot-run.json').read_bytes()
            with self.assertRaisesRegex(ValueError, 'preserves existing evidence'):
                BOOT.boot(directory, output, 'managed-base', 'd' * 40)
            self.assertEqual((output / 'boot-run.json').read_bytes(), before)

    def test_ambiguous_or_unselected_image_fails_before_producer_execution(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, output = self.fixture(Path(temporary))
            for name in ('base.json', 'factory.json'):
                (directory / name).write_text('{}')
            with mock.patch.object(BOOT.image, 'run') as run, mock.patch.object(BOOT.subprocess, 'check_output') as check:
                for profile in ('managed-base', 'managed-factory', 'headless', None):
                    with self.assertRaises(ValueError):
                        BOOT.authenticated_image(directory, profile)
                run.assert_not_called(); check.assert_not_called()

    def test_host_requires_explicit_profile_and_new_disjoint_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, output = self.fixture(Path(temporary))
            args = SimpleNamespace(kernel=None, sdk=None, artifacts=None, image_profile=None,
                                   boot_output=None, output=directory)
            with mock.patch.object(BOOT.image, 'snapshot') as snapshot:
                with self.assertRaisesRegex(ValueError, 'requires --image-profile'):
                    BOOT.publish(args)
                args.image_profile, args.boot_output = 'managed-base', directory / 'nested'
                with self.assertRaisesRegex(ValueError, 'overlaps'):
                    BOOT.publish(args)
                args.boot_output = output
                with self.assertRaisesRegex(ValueError, 'preserves existing evidence'):
                    BOOT.publish(args)
                snapshot.assert_not_called()


if __name__ == '__main__':
    unittest.main()
