"""Release validation and real pinned-tool bootstrap reproducibility tests."""
import dataclasses
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest

from scripts import appliance, appliance_inside

ROOT = Path(__file__).resolve().parents[1]
INSIDE = os.environ.get('FES_APPLIANCE_TEST_INSIDE') == '1'


def arm_elf():
    data = bytearray(256)
    data[:16] = b'\x7fELF\x01\x01\x01' + bytes(9)
    struct.pack_into('<HHIIIIIHHHHHH', data, 16, 2, 40, 1, 0x10054, 52, 0, 0x05000000, 52, 32, 1, 0, 0, 0)
    struct.pack_into('<IIIIIIII', data, 52, 1, 0, 0x10000, 0x10000, len(data), len(data), 5, 4096)
    return data


def provenance(rootfs, kernel):
    return appliance.ReleaseProvenance('a'*40, 'b'*40, 'c'*40, appliance.digest(rootfs), appliance.digest(kernel), 'd'*64, 'e'*64, 'f'*64)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.kernel = self.root/'kernel'; self.kernel.write_bytes(b'locked-kernel')
        self.rootfs = self.root/'system.img'
        data = bytearray(4096)
        struct.pack_into('<I', data, 1028, 4)
        struct.pack_into('<H', data, 1080, 0xef53)
        struct.pack_into('<I', data, 1120, 0x40)
        self.rootfs.write_bytes(data)

    def test_export_binds_inputs_and_is_immutable(self):
        result = appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v1', provenance=provenance(self.rootfs, self.kernel))
        manifest = appliance.load_manifest(result.manifest)
        self.assertEqual(manifest['image_sha256'], appliance.digest(self.rootfs))
        self.assertEqual(manifest['image_size'], 4096)
        self.assertEqual(result.image.read_bytes(), self.rootfs.read_bytes())
        inode = result.image.stat().st_ino
        again = appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v1', provenance=provenance(self.rootfs, self.kernel))
        self.assertEqual(again, result)
        self.assertEqual(result.image.stat().st_ino, inode)
        with self.assertRaises(ValueError):
            appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v2', provenance=provenance(self.rootfs, self.kernel))

    def test_reject_mismatched_provenance_and_unknown_manifest(self):
        p = dataclasses.replace(provenance(self.rootfs,self.kernel),rootfs_sha256='0'*64)
        with self.assertRaisesRegex(ValueError,'rootfs'):
            appliance.export_release(self.root/'bad',self.rootfs,self.kernel,version='v1',provenance=p)
        self.assertFalse((self.root/'bad').exists())
        result=appliance.export_release(self.root/'release',self.rootfs,self.kernel,version='v1',provenance=provenance(self.rootfs,self.kernel))
        manifest=json.loads(result.manifest.read_text());manifest['unknown']=1
        malformed=self.root/'malformed';malformed.write_text(json.dumps(manifest))
        with self.assertRaises(ValueError):appliance.load_manifest(malformed)
        malformed.write_text(result.manifest.read_text().rstrip()[:-1]+',"format":1}')
        with self.assertRaises(ValueError):appliance.load_manifest(malformed)

    def test_arm_static_validation_rejects_dynamic_and_wrong_machine(self):
        binary=self.root/'fes-boot';binary.write_bytes(arm_elf());appliance.validate_static_arm(binary)
        for offset,value in ((18,62),(52,3),(52,2)):
            data=arm_elf();struct.pack_into('<H' if offset==18 else '<I',data,offset,value);binary.write_bytes(data)
            with self.assertRaises(ValueError):appliance.validate_static_arm(binary)


@unittest.skipIf(INSIDE,'host container driver')
class ContainerTests(unittest.TestCase):
    def test_real_pinned_bootstrap_suite(self):
        if not shutil.which('docker') or subprocess.run(['docker','info'],capture_output=True).returncode:
            self.skipTest('Docker unavailable')
        from scripts.media_inputs import MediaLock
        image=appliance.cached_media_container(ROOT,'docker',MediaLock.load(ROOT/'boot-media.lock.toml'))
        subprocess.run(['docker','run','--rm','--network=none','--cap-drop=ALL','--security-opt=no-new-privileges','-e','FES_APPLIANCE_TEST_INSIDE=1','-v',f'{ROOT}:/work:ro','--entrypoint','python3',image,'-m','unittest','tests.test_appliance.RealBootstrapTests','-v'],check=True)


@unittest.skipUnless(INSIDE,'executed in pinned media container')
class RealBootstrapTests(unittest.TestCase):
    def test_two_pass_contents_devices_timestamps_and_features(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);binary=root/'fes-boot';binary.write_bytes(arm_elf())
            factory=root/'factory.json'
            factory.write_text(json.dumps({'format':1,'board':'de10-nano','boot_abi':'fes-bootstrap-v1','version':'v1','kernel_sha256':'a'*64,'image_sha256':'b'*64,'image_size':4096,'fes_revision':'c'*40,'fogcast_revision':'d'*40,'runtime_revision':'e'*40}))
            first=appliance_inside.assemble(root/'first.img',binary,factory)
            # Delay source inode ctime to catch unpatched mkfs -d copying it.
            import time
            time.sleep(1.1)
            binary.chmod(0o700)
            second=appliance_inside.assemble(root/'second.img',binary,factory)
            self.assertEqual(first['image_sha256'],second['image_sha256'])
            for path in ('/sbin/init','/etc/fes/factory.json','/dev/console','/dev/null','/.fes-bootstrap','/run','/tmp','/proc','/sys','/media/fat'):
                text=appliance_inside.debugfs(root/'first.img',f'stat {path}')
                self.assertIn('Inode:',text,path)
                self.assertIn('User:     0',text,path)
            self.assertIn('character special',appliance_inside.debugfs(root/'first.img','stat /dev/console'))
            extracted=root/'extracted'
            appliance_inside.debugfs(root/'first.img',f'dump /sbin/init {extracted}')
            self.assertEqual(extracted.read_bytes(),binary.read_bytes())
            appliance.validate_ext4(root/'first.img')

class VerificationBoundaryTests(unittest.TestCase):
    def test_cli_input_boundary_rejects_missing_cold_verification(self):
        import sys
        from unittest import mock
        sys.path.insert(0,str(ROOT/'scripts'))
        import media
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            with mock.patch.object(media.cold_build,'git',return_value=''), mock.patch.object(media,'select',return_value=('fingerprint',root/'fogcast',[],{})):
                with self.assertRaisesRegex(ValueError,'cold image receipt'):
                    appliance.verified_inputs(root,'native-integration-dev')

    def test_ext4_rejects_new_kernel_features_and_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);path=root/'image';data=bytearray(4096)
            struct.pack_into('<I',data,1028,4);struct.pack_into('<H',data,1080,0xef53)
            struct.pack_into('<I',data,1120,0x40);path.write_bytes(data)
            appliance.validate_ext4(path)
            struct.pack_into('<I',data,1116,0x1000);path.write_bytes(data)
            with self.assertRaises(ValueError):appliance.validate_ext4(path)
            link=root/'link';link.symlink_to(path)
            with self.assertRaises(ValueError):appliance.validate_ext4(link)
