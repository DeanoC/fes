"""DE25 layout/construction fixtures; no qualified factory or native boot proof."""
from dataclasses import replace
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import appliance, appliance_inside, appliance_media, appliance_media_inside
from scripts import media_container
from tests.test_appliance import aarch64_elf
from tests import test_de25_boot_contract as boot_fixtures

ROOT = Path(__file__).resolve().parents[1]
MEDIA = appliance_media_inside.de25_media()
INSIDE = os.environ.get('FES_DE25_MEDIA_TEST_INSIDE') == '1'


class LayoutTests(unittest.TestCase):
    def test_explicit_de25_layout_keeps_partition_numbers_and_reserves_rollback_capacity(self):
        vendor = MEDIA.boot.load_lock()
        original = copy.deepcopy(vendor)
        layout = MEDIA.appliance_layout(vendor)
        self.assertEqual(layout['partition_1'], {'type': 0x0b, 'start_sector': 2048, 'sectors': 4194304})
        self.assertEqual(layout['partition_2'], {'type': 0x83, 'start_sector': 4196352, 'sectors': 65536})
        self.assertEqual(layout['total_sectors'], 4261888)
        self.assertEqual(vendor, original)
        MEDIA.check_capacity(256 << 20, 64 << 20)
        with self.assertRaisesRegex(ValueError, 'rollback capacity'):
            MEDIA.check_capacity(512 << 20, 64 << 20)

    def test_foreign_or_changed_vendor_geometry_is_refused(self):
        vendor = MEDIA.boot.load_lock()
        for key, value in [('board', 'de10_nano'), ('sector_size', 4096),
                           ('root_device', '/dev/mmcblk0p1'), ('console', 'other')]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                MEDIA.appliance_layout(dict(vendor, **{key: value}))
        changed = copy.deepcopy(vendor);changed['partitions'][1]['type'] = 0xa2
        with self.assertRaises(ValueError):
            MEDIA.appliance_layout(changed)

    def test_bad_capacity_and_foreign_metadata_do_not_start_construction(self):
        for size in (False, 0, -1, '256M'):
            with self.assertRaises(ValueError):
                MEDIA.check_capacity(size, 1)
        with mock.patch.object(appliance_media, 'compare_card') as compare:
            with self.assertRaisesRegex(ValueError, 'selected board'):
                appliance_media.reconstruct(Path('/unused'), None, {'board': 'de10-nano'},
                                             None, board='de25-nano')
            compare.assert_not_called()

    def test_de10_inputs_cannot_be_interpreted_as_de25(self):
        with self.assertRaises(TypeError):
            MEDIA.Inputs(factory=Path('/factory'), bootstrap=Path('/bootstrap'), manifest=Path('/manifest'),
                         idle=Path('/idle'), kernel=Path('/kernel'), uboot=Path('/de10-boot'))


@unittest.skipIf(INSIDE, 'host container driver')
class ContainerTests(unittest.TestCase):
    def test_real_fat_ext4_construction_and_refusals(self):
        if not shutil.which('docker') or subprocess.run(['docker', 'info'], capture_output=True).returncode:
            self.skipTest('Docker unavailable')
        image = media_container.ensure_media_tools(ROOT, 'docker')
        subprocess.run(['docker', 'run', '--rm', '--pull=never', '--network=none', '--cap-drop=ALL',
                        '--security-opt=no-new-privileges', '-e', 'FES_DE25_MEDIA_TEST_INSIDE=1',
                        '-v', str(ROOT) + ':/work:ro', '--entrypoint', 'python3', image,
                        '-m', 'unittest', 'tests.test_de25_managed_media.RealCardTests', '-v'], check=True)


@unittest.skipUnless(INSIDE, 'executed in pinned media container')
class RealCardTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='de25-media-fixture-')
        cls.root = root = Path(cls.temporary.name)
        cls.vendor = vendor = MEDIA.boot.load_lock()
        cls.boot = boot = root / 'vendor';boot.mkdir()
        # Synthetic but structurally valid FIT, DTB and boot-script fixtures.
        # The real decoder, FAT parser and all payload-hash checks still run.
        boot_fixtures.BootSelectionTests().inspect_fixture(boot)
        (boot / 'Image').write_bytes(b'fixture Linux Image')
        factory = boot / 'factory-boot';factory.mkdir()
        for row in vendor['factory_boot']:
            path = factory / row['name'];path.write_bytes(('fixture ' + row['name']).encode())
            row.update(size=path.stat().st_size, sha256=MEDIA.boot.digest(path))
        for row in vendor['boot_files']:
            path = boot / row['name'];row.update(size=path.stat().st_size, sha256=MEDIA.boot.digest(path))
        prefix = boot / 'vendor-boot-prefix.bin'
        mbr = bytearray(512);mbr[510:] = b'\x55\xaa';mbr[440:444] = b'FES2'
        for offset, part in zip((446, 462), vendor['partitions']):
            mbr[offset+4] = part['type']
            struct.pack_into('<II', mbr, offset+8, part['start_sector'], part['sectors'])
        vendor['mbr_sha256'] = hashlib.sha256(mbr).hexdigest()
        with prefix.open('wb') as stream:
            stream.write(mbr);stream.truncate(vendor['prefix_size'])
        MEDIA.media.run('mkfs.fat', '--invariant', '-F', '32', '-S', '512', '-s', '1',
                        '--offset=2048', prefix, str(vendor['partitions'][0]['sectors'] // 2))
        device = str(prefix) + '@@1048576'
        for row in vendor['boot_files']:
            MEDIA.media.run('mcopy', '-i', device, boot / row['name'], '::/' + row['name'])
        vendor['prefix_sha256'] = MEDIA.boot.digest(prefix)
        MEDIA.boot._verify_prepared_boot(boot, vendor)
        cls.kernel = kernel = root / 'kernel';kernel.mkdir()
        (kernel / 'Image').write_bytes(b'fixture selected owner Image')
        shutil.copyfile(boot / 'socfpga_agilex5_de25_nano.dtb', kernel / 'de25-owner.dtb')
        cls.kernel_record = {'files': {'Image': {'size': (kernel/'Image').stat().st_size,
                                               'sha256': MEDIA.boot.digest(kernel/'Image')}}}
        cls.idle = root / 'idle.rbf';cls.idle.write_bytes(b'fixture idle; not FPGA configuration')
        tree = root / 'tree'
        for name in ('.fes-bootstrap', 'etc/fes', 'sbin', 'usr/share/mister-runtime'):
            (tree / name).mkdir(parents=True, exist_ok=True)
        (tree / 'etc/fes-board').write_bytes(b'de25-nano\n')
        (tree / 'etc/fes/software-base.json').write_text('{"board":"de25-nano","architecture":"aarch64"}\n')
        (tree / 'sbin/init').write_bytes(aarch64_elf());(tree / 'sbin/init').chmod(0o755)
        shutil.copyfile(cls.idle, tree / 'usr/share/mister-runtime/idle.rbf')
        cls.factory = root / 'factory.img'
        with cls.factory.open('wb') as stream:stream.truncate(8 << 20)
        MEDIA.media.run('mke2fs', '-q', '-F', '-t', 'ext4', '-d', tree, cls.factory)
        manifest = {'format':1, 'board':'de25-nano', 'boot_abi':'fes-bootstrap-v1', 'version':'construction-fixture',
                    'kernel_sha256':MEDIA.boot.digest(kernel/'Image'), 'image_sha256':MEDIA.boot.digest(cls.factory),
                    'image_size':cls.factory.stat().st_size, 'fes_revision':'a'*40, 'fogcast_revision':'b'*40,
                    'runtime_revision':'c'*40}
        cls.manifest = root / 'release.json';cls.manifest.write_bytes(appliance.canonical(manifest))
        cls.bootstrap = root / 'bootstrap.img'
        appliance_inside.assemble(cls.bootstrap, tree/'sbin/init', cls.manifest, board='de25-nano')
        cls.agent = root / 'agent.toml';cls.agent.write_text('token="fixture-agent-token"\n');cls.agent.chmod(0o600)
        cls.inputs = MEDIA.Inputs(cls.factory, cls.bootstrap, cls.manifest, cls.idle, cls.kernel, cls.boot, cls.agent)

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def setUp(self):
        # Only owner-kernel attestation is a stand-in. These tests do not build
        # an actual kernel, qualify firmware, execute CPUs or admit a factory.
        self.patch = mock.patch.object(MEDIA.owner, 'selected_kernel', return_value=self.kernel_record)
        self.patch.start();self.addCleanup(self.patch.stop)

    def test_two_independent_disks_match_and_keep_bootstrap_in_partition_two(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root = Path(temporary)
            first = MEDIA.assemble(root/'first.img', self.inputs, self.vendor)
            second = MEDIA.assemble(root/'second.img', self.inputs, self.vendor)
            self.assertEqual(first, second)
            self.assertEqual(first['image_size'], 2182086656)
            self.assertEqual(first['boot_contract']['root_device'], '/dev/mmcblk0p2')
            self.assertIn('/fogcast/agent.toml', first['paths'])
            self.assertNotIn('/linux/linux.img', first['paths'])
            self.assertEqual((root/'first.img').stat().st_mode & 0o777, 0o600)

    def test_foreign_board_and_changed_bootstrap_binding_leave_no_output(self):
        for field, value in [('board', 'de10-nano'), ('version', 'changed-binding')]:
            with self.subTest(field=field), tempfile.TemporaryDirectory(dir=self.root) as temporary:
                root = Path(temporary);manifest = json.loads(self.manifest.read_text());manifest[field] = value
                selected = root / 'release.json';selected.write_bytes(appliance.canonical(manifest))
                with self.assertRaises(ValueError):
                    MEDIA.assemble(root/'bad.img', replace(self.inputs, manifest=selected), self.vendor)
                self.assertFalse((root/'bad.img').exists())

    def test_extra_fat_file_and_changed_partition_two_are_refused(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root = Path(temporary);image = root / 'card.img'
            MEDIA.assemble(image, self.inputs, self.vendor)
            layout = MEDIA.appliance_layout(self.vendor)
            offset = layout['partition_2']['start_sector'] * 512
            with image.open('r+b') as stream:
                stream.seek(offset);byte = stream.read(1);stream.seek(offset);stream.write(bytes([byte[0] ^ 1]))
            with self.assertRaisesRegex(ValueError, 'partition-2'):
                MEDIA.verify(image, self.inputs, self.vendor)
            with image.open('r+b') as stream:
                stream.seek(offset);stream.write(byte)
            extra = root / 'extra';extra.write_bytes(b'extra')
            MEDIA.media.run('mcopy', '-i', str(image)+'@@1048576', extra, '::/unexpected')
            with self.assertRaisesRegex(ValueError, 'owned paths'):
                MEDIA.verify(image, self.inputs, self.vendor)

    def test_existing_file_is_preserved_and_no_payload_can_select_another_board(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root = Path(temporary);image = root / 'card.img';image.write_bytes(b'preserve')
            with self.assertRaisesRegex(ValueError, 'already exists'):
                MEDIA.assemble(image, self.inputs, self.vendor)
            self.assertEqual(image.read_bytes(), b'preserve')
            with self.assertRaisesRegex(ValueError, 'selected board'):
                appliance_media.reconstruct(root, self.inputs, {'board':'de10-nano'}, None, board='de25-nano')


if __name__ == '__main__':
    unittest.main()
