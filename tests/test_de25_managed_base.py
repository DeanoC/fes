"""Managed-base provenance and staging refusal checks; no hardware or Docker."""
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tarfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'image/de25-nano'))
SPEC = importlib.util.spec_from_file_location('de25_managed_base', ROOT / 'image/de25-nano/managed_base.py')
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)


class ManagedBaseTests(unittest.TestCase):
    def test_sdk_export_keeps_host_and_sysroot_aliases_inside_the_archive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); source = root / 'sdk.tar.gz'; output = root / 'export.tar.gz'
            with tarfile.open(source, 'w:gz') as archive:
                for name in ('sdk/share/gettext-tiny',):
                    item = tarfile.TarInfo(name); item.type = tarfile.DIRTYPE; archive.addfile(item)
                for name in ('sdk/share/gettext-tiny/ABOUT-NLS', 'sdk/target/sysroot/usr/bin/ntfs-3g'):
                    item = tarfile.TarInfo(name); item.size = 7; archive.addfile(item, io.BytesIO(b'payload'))
                for name, target in (
                    ('sdk/share/gettext', '/build/key/host/usr/share/gettext-tiny'),
                    ('sdk/target/sysroot/usr/sbin/mount.ntfs-3g', '/usr/bin/ntfs-3g'),
                    ('sdk/bin/alias', '/build/key/host/share/gettext-tiny/ABOUT-NLS'),
                ):
                    item = tarfile.TarInfo(name); item.type = tarfile.SYMTYPE; item.linkname = target; archive.addfile(item)
            changes = BASE.export_sdk(source, output, Path('/build/key/host'))
            self.assertEqual(len(changes), 3)
            with tarfile.open(output) as archive:
                self.assertEqual(archive.getmember('sdk/share/gettext').linkname, 'gettext-tiny')
                self.assertEqual(archive.getmember('sdk/target/sysroot/usr/sbin/mount.ntfs-3g').linkname, '../bin/ntfs-3g')
                self.assertEqual(archive.extractfile('sdk/share/gettext-tiny/ABOUT-NLS').read(), b'payload')

    def test_sdk_export_refuses_unknown_absolute_and_escaping_links(self):
        for target in ('/etc/passwd', '../../outside'):
            with self.subTest(target=target), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary); source = root / 'sdk.tar.gz'
                with tarfile.open(source, 'w:gz') as archive:
                    item = tarfile.TarInfo('sdk/bin/unsafe'); item.type = tarfile.SYMTYPE; item.linkname = target; archive.addfile(item)
                with self.assertRaises(ValueError):
                    BASE.export_sdk(source, root / 'export.tar.gz', Path('/build/key/host'))

    def test_shared_managed_policy_and_matching_external_toolchain(self):
        configured = BASE.configuration().splitlines()
        for line in ('BR2_aarch64=y', 'BR2_cortex_a53=y', 'BR2_ROOTFS_MERGED_USR=y',
                     'BR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y', 'BR2_TOOLCHAIN_EXTERNAL_CXX=y',
                     'BR2_PACKAGE_BLUEZ5_UTILS=y', 'BR2_PACKAGE_IMLIB2=y',
                     '# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set',
                     '# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set'):
            self.assertIn(line, configured)
        self.assertNotIn('BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y', configured)
        self.assertIn(BASE.sdk_lock()['sdk_sha256'], '\n'.join(configured))
        self.assertNotIn('armv7', '\n'.join(configured))
        self.assertTrue(BASE.toolchain_path().is_relative_to(Path('/build') / BASE.recipe_key() / 'host'))

    def test_postbuild_uses_the_pinned_python_when_buildroot_shadows_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            impostor = root / 'python3'
            impostor.write_text('#!/bin/sh\nexit 97\n')
            impostor.chmod(0o755)
            probe = root / 'probe.py'
            probe.write_text('import tomllib\nprint("PINNED_PYTHON_PASS")\n')
            wrapper = root / 'post-build.sh'
            wrapper.write_text((ROOT / 'image/de25-nano/managed-base-post-build.sh').read_text()
                               .replace('/src/image/de25-nano/managed_base.py', str(probe)))
            result = subprocess.run(['/bin/sh', wrapper, '/unused-target'],
                                    env={**os.environ, 'PATH': str(root) + ':' + os.environ['PATH']},
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), 'PINNED_PYTHON_PASS')

    def test_receipt_rejects_target_builder_input_and_artifact_changes(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            record = {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64',
                      'classification': 'managed-base-intermediate', 'fes_revision': 'a' * 40,
                      'recipe_sha256': BASE.recipe_key(), 'sdk_sha256': BASE.sdk_lock()['sdk_sha256'],
                      'builder_image': BASE.sdk_lock()['builder_image'], 'normal_runtime_admission': 'closed'}
            inputs = {key: record[key] for key in ('fes_revision', 'recipe_sha256', 'sdk_sha256')}
            for name in BASE.artifact_names():
                path = output / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'fixture only, not a real image')
            (output / 'base-inputs.json').write_text(json.dumps(inputs))
            record['files'] = {name: BASE.image.digest(output / name) for name in BASE.artifact_names()}
            def select(value):
                (output / 'base.json').write_text(json.dumps(value))
            with mock.patch.object(BASE.owner, 'selected_kernel', return_value={}):
                select(record)
                BASE.verify_receipt(output)
                for key, value in (('board', 'de10-nano'), ('architecture', 'armv7'),
                                   ('builder_image', 'sha256:' + '0' * 64),
                                   ('normal_runtime_admission', 'open'), ('fes_revision', 'not-a-commit')):
                    changed = copy.deepcopy(record); changed[key] = value; select(changed)
                    with self.subTest(key=key), self.assertRaises(ValueError):
                        BASE.verify_receipt(output)
                changed = copy.deepcopy(record); changed['fes_revision'] = 'b' * 40; select(changed)
                with self.assertRaisesRegex(ValueError, 'producer inputs'):
                    BASE.verify_receipt(output)
                select(record)
                (output / 'rootfs.tar').write_bytes(b'tampered')
                with self.assertRaisesRegex(ValueError, 'artifact differs'):
                    BASE.verify_receipt(output)

    def test_container_network_and_inputs_are_scoped_to_the_stage(self):
        with mock.patch.object(BASE.image, 'run') as run:
            for action, network in (('base-fetch', 'bridge'), ('base-build', 'none'), ('base-verify', 'none')):
                BASE.image.container('/frozen', 'docker', 'sha256:builder', 'own-volume',
                                     '/cache', '/output', action, 2, inputs='/inputs-host')
                command = run.call_args.args[0]
                self.assertEqual(command[command.index('--network') + 1], network)
                self.assertIn('/frozen:/src:ro', command)
                self.assertIn('/inputs-host:/inputs:ro', command)
                self.assertNotIn('--privileged', command)
                self.assertNotIn('--device', command)


if __name__ == '__main__':
    unittest.main()
