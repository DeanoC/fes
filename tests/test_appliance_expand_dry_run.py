"""Host-side leftover-capacity expand dry-run; never writes a block device."""
import contextlib
from io import StringIO
import json
from pathlib import Path
import os
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from scripts import appliance_expand_dry_run as expand
from scripts.appliance_media_inside import appliance_layout
from scripts.media_inputs import MediaLock

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'boot-media.lock.toml'
# Sizes the kernel reports for cards in the admitted reader, and the p3 each
# gets: Kit B's 32 GB card (lsblk 28.8G) and the 64 GB spare from the
# 2026-09-10 HIL (lsblk 58.2G).
CARDS = {
    '32GB': SimpleNamespace(bytes=30953963520, sectors=60456960, p3_sectors=58355712),
    '64GB': SimpleNamespace(bytes=62534975488, sectors=122138624, p3_sectors=120037376),
}


def other_card(name):
    return CARDS['64GB' if name == '32GB' else '32GB']


def populated(mbr):
    return [part for part in mbr['partitions'] if part['sector_count']]


class ApplianceExpandDryRunTests(unittest.TestCase):
    def setUp(self):
        self.layout = appliance_layout(MediaLock.load(LOCK))
        self.temporary = tempfile.TemporaryDirectory(prefix='fes-expand-')
        self.root = Path(self.temporary.name)

    def tearDown(self):
        self.temporary.cleanup()

    @contextlib.contextmanager
    def fake_usb(self, card_bytes):
        """Admit a sparse appliance file as the exact USB by-id.

        The by-id link, sysfs and udev answers all live in the temporary
        directory, so nothing here opens, probes or writes a real device.
        """
        root = Path(tempfile.mkdtemp(dir=self.root))
        disk = root / 'dev' / 'sdz'
        disk.parent.mkdir()
        expand.synthesize_image(disk, self.layout, card_bytes)
        by_id = root / 'usb-Generic_STORAGE_DEVICE_000000000819-0:0'
        by_id.symlink_to(disk)
        sysfs = root / 'sys' / 'sdz'
        sysfs.mkdir(parents=True)
        (sysfs / 'size').write_text(f'{card_bytes // 512}\n')
        (sysfs / 'removable').write_text('1\n')
        udev = {
            'DEVNAME': os.path.realpath(disk),
            'ID_BUS': 'usb',
            'ID_TYPE': 'disk',
            'ID_VENDOR': expand.USB_VENDOR,
            'ID_MODEL': expand.USB_MODEL,
            'ID_SERIAL_SHORT': expand.USB_SERIAL,
        }
        with mock.patch.object(expand, 'USB_BY_ID', str(by_id)), \
                mock.patch.object(expand, 'SYS_CLASS_BLOCK', root / 'sys'), \
                mock.patch.object(expand, 'is_block_device', return_value=True), \
                mock.patch.object(expand, '_udev_properties', return_value=udev):
            yield SimpleNamespace(by_id=str(by_id), disk=disk, resolved=os.path.realpath(disk),
                                  sysfs=sysfs, udev=udev)

    @contextlib.contextmanager
    def kernel_steps(self):
        """Stub the mount check, partition rescan and mkfs of a physical apply."""
        with mock.patch.object(expand, 'require_unmounted') as unmounted, \
                mock.patch.object(expand, 'rescan_partition_table'), \
                mock.patch.object(expand, 'require_partition_device',
                                  side_effect=lambda device, number, _: f'{device}{number}') as partition, \
                mock.patch.object(expand, 'format_ext4') as mkfs, \
                mock.patch.object(expand, 'read_ext4_superblock', return_value={'label': expand.EXT4_LABEL}), \
                mock.patch.object(expand.os, 'sync'):
            yield SimpleNamespace(unmounted=unmounted, partition=partition, mkfs=mkfs)

    def run_cli(self, *argv):
        stdout, stderr = StringIO(), StringIO()
        with mock.patch('sys.stdout', new=stdout), mock.patch('sys.stderr', new=stderr):
            try:
                expand.main([*argv, '--lock', str(LOCK)])
            except SystemExit as stop:
                return stop.code, stdout.getvalue(), stderr.getvalue()
        return 0, stdout.getvalue(), stderr.getvalue()

    def test_synthetic_inspect_matches_locked_1g_layout(self):
        image = self.root / 'card.img'
        expand.synthesize_image(image, self.layout, expand.SYNTHETIC_CARD_BYTES)
        result = expand.inspect_image(image, self.layout)
        self.assertEqual(result['layout_id'], 'de10-nano-appliance-1g-v1')
        self.assertEqual(result['image_bytes'], 1075838976)
        self.assertEqual(result['partitions'][0]['start_sector'], 2048)
        self.assertEqual(result['partitions'][0]['sector_count'], 2097152)
        self.assertEqual(result['partitions'][0]['type'], 0x0c)
        self.assertEqual(result['partitions'][1]['start_sector'], 2099200)
        self.assertEqual(result['partitions'][1]['sector_count'], 2048)
        self.assertEqual(result['partitions'][1]['type'], 0xa2)
        self.assertEqual(len(result['partitions']), 2)
        self.assertEqual(result['unpartitioned_if_written_to_synthetic_card'],
                         expand.SYNTHETIC_CARD_BYTES - 1075838976)
        self.assertGreater(result['fat32_table_bytes_512_cluster_card'], 400 << 20)
        self.assertLess(result['fat32_table_bytes_64k_cluster_card'], 8 << 20)

    def test_extra_partition_plan_leaves_fat_and_a2_untouched(self):
        plan = expand.plan_extra_partition(self.layout)
        self.assertTrue(plan['recommended'])
        self.assertFalse(plan['touches_fat'])
        self.assertFalse(plan['touches_a2'])
        self.assertTrue(plan['safe_while_fat_mounted'])
        p3 = plan['partition_3']
        self.assertEqual(p3['type'], 0x83)
        self.assertEqual(p3['start_sector'], 2101248)
        self.assertEqual(p3['start_sector'] % 2048, 0)
        self.assertEqual(p3['sector_count'] % 2048, 0)
        self.assertGreater(p3['bytes'], 20 << 30)
        self.assertIn('/fogcast/releases/', plan['keep_on_fat'])
        self.assertIn('/media/fat/fogcast/cache', plan['move_or_bind_to_p3'])

    def test_grow_fat_plan_moves_a2_and_is_not_safe_mounted(self):
        plan = expand.plan_grow_fat(self.layout)
        self.assertFalse(plan['recommended'])
        self.assertTrue(plan['touches_a2'])
        self.assertFalse(plan['safe_while_fat_mounted'])
        self.assertGreater(plan['moved_a2']['start_sector'], 2099200)
        self.assertEqual(plan['grown_fat']['start_sector'], 2048)
        self.assertEqual(plan['grown_fat']['start_sector'] + plan['grown_fat']['sector_count'],
                         plan['moved_a2']['start_sector'])
        self.assertGreater(plan['fat32_table_bytes_if_cluster_stays_512'], 400 << 20)

    def test_apply_extra_partition_updates_mbr_on_sparse_file_only(self):
        image = self.root / 'card.img'
        expand.synthesize_image(image, self.layout)
        result = expand.apply_plan(image, self.layout, 'extra-partition', env={})
        self.assertEqual(result['applied'], 'mbr-only')
        mbr = expand.read_mbr(image)
        populated = [part for part in mbr['partitions'] if part['sector_count']]
        self.assertEqual(len(populated), 3)
        self.assertEqual(populated[0]['start_sector'], 2048)
        self.assertEqual(populated[1]['start_sector'], 2099200)
        self.assertEqual(populated[1]['type'], 0xa2)
        self.assertEqual(populated[2]['type'], 0x83)
        self.assertEqual(populated[2]['start_sector'], 2101248)
        with image.open('rb') as stream:
            stream.seek(2099200 * 512)
            self.assertEqual(stream.read(12), b'A2-SPL-UBOOT')

    def test_apply_grow_fat_copies_a2_marker_and_does_not_resize_filesystem(self):
        image = self.root / 'card.img'
        expand.synthesize_image(image, self.layout)
        result = expand.apply_plan(image, self.layout, 'grow-fat', env={})
        self.assertEqual(result['applied'], 'mbr-and-a2-copy')
        self.assertFalse(result['filesystem_grown'])
        plan = result['plan']
        mbr = expand.read_mbr(image)
        populated = [part for part in mbr['partitions'] if part['sector_count']]
        self.assertEqual(len(populated), 2)
        self.assertEqual(populated[0]['sector_count'], plan['grown_fat']['sector_count'])
        self.assertEqual(populated[1]['start_sector'], plan['moved_a2']['start_sector'])
        with image.open('rb') as stream:
            stream.seek(plan['moved_a2']['start_sector'] * 512)
            self.assertEqual(stream.read(12), b'A2-SPL-UBOOT')
            stream.seek(2099200 * 512)
            self.assertEqual(stream.read(12), b'A2-SPL-UBOOT')

    def test_refuses_system_disks_without_write_go(self):
        for node in ('/dev/disk0', '/dev/nvme0n1', '/dev/sda', '/dev/mmcblk0'):
            with self.subTest(node=node):
                with self.assertRaisesRegex(expand.ExpandError, 'raw block|system disk'):
                    expand.require_regular_or_gated_usb(node, {}, write=False)

    def test_write_go_still_refuses_raw_nodes_and_admits_exact_usb(self):
        with self.assertRaisesRegex(expand.ExpandError, 'raw block'):
            expand.require_regular_or_gated_usb('/dev/disk0', {'WRITE_GO': '1'}, write=True)
        usb = expand.USB_BY_ID
        identity = {
            'by_id': usb,
            'resolved_path': '/dev/sda',
            'size_sectors': CARDS['64GB'].sectors,
            'size_bytes': CARDS['64GB'].bytes,
            'serial': expand.USB_SERIAL,
            'verified': True,
        }
        with mock.patch.object(expand, 'is_block_device', return_value=True), \
                mock.patch.object(expand, 'is_system_disk', return_value=False), \
                mock.patch.object(expand, 'verify_usb_device', return_value=identity) as verify:
            self.assertEqual(expand.require_regular_or_gated_usb(usb, {'WRITE_GO': '1'}, write=True), identity)
            verify.assert_called_once_with(usb, write=True, card_bytes=None)
        with mock.patch.object(expand, 'is_block_device', return_value=True):
            with self.assertRaisesRegex(expand.ExpandError, 'WRITE_GO=1'):
                expand.require_regular_or_gated_usb(usb, {}, write=True)

    def test_physical_apply_dispatches_only_recommended_mode(self):
        identity = {
            'by_id': expand.USB_BY_ID,
            'resolved_path': '/dev/sda',
            'size_bytes': CARDS['64GB'].bytes,
            'verified': True,
        }
        with mock.patch.object(expand, 'require_regular_or_gated_usb', return_value=identity), \
                mock.patch.object(expand, 'apply_physical_extra_partition', return_value={'verified': True}) as apply:
            result = expand.apply_plan(expand.USB_BY_ID, self.layout, 'extra-partition',
                                       env={'WRITE_GO': '1'})
        self.assertEqual(result, {'verified': True})
        apply.assert_called_once_with(expand.USB_BY_ID, self.layout, card_bytes=CARDS['64GB'].bytes,
                                      env={'WRITE_GO': '1'})
        with self.assertRaisesRegex(expand.ExpandError, 'recommended extra-partition'):
            expand.apply_plan(expand.USB_BY_ID, self.layout, 'grow-fat', env={'WRITE_GO': '1'})

    def test_expanded_table_fits_each_card(self):
        for name, card in CARDS.items():
            with self.subTest(card=name):
                image = self.root / f'{name}.img'
                expand.synthesize_image(image, self.layout, card.bytes)
                plan = expand.apply_plan(image, self.layout, 'extra-partition', card.bytes, env={})['plan']
                p3 = plan['partition_3']
                self.assertEqual((p3['start_sector'], p3['sector_count']), (2101248, card.p3_sectors))
                self.assertLessEqual(p3['start_sector'] + p3['sector_count'], card.sectors)
                mbr = expand.read_mbr(image)
                self.assertEqual(expand.require_expanded_table(mbr, self.layout, card.bytes), plan)
                with self.assertRaisesRegex(expand.ExpandError, 'expanded MBR does not match'):
                    expand.require_expanded_table(mbr, self.layout, other_card(name).bytes)

    def test_admitted_usb_takes_its_size_from_the_card(self):
        for name, card in CARDS.items():
            with self.subTest(card=name), self.fake_usb(card.bytes) as usb:
                for card_bytes in (None, card.bytes):
                    identity = expand.verify_usb_device(usb.by_id, card_bytes=card_bytes)
                    self.assertEqual((identity['size_sectors'], identity['size_bytes']),
                                     (card.sectors, card.bytes))
                    self.assertEqual(identity['serial'], expand.USB_SERIAL)
                with self.assertRaisesRegex(expand.ExpandError, 'USB size mismatch'):
                    expand.verify_usb_device(usb.by_id, card_bytes=other_card(name).bytes)

    def test_card_size_does_not_replace_the_usb_identity_checks(self):
        card = CARDS['32GB']
        with self.fake_usb(card.bytes) as usb:
            with self.assertRaisesRegex(expand.ExpandError, 'exact spare USB by-id'):
                expand.verify_usb_device(str(usb.disk), card_bytes=card.bytes)
            usb.udev['ID_SERIAL_SHORT'] = '000000000820'
            with self.assertRaisesRegex(expand.ExpandError, 'ID_SERIAL_SHORT'):
                expand.verify_usb_device(usb.by_id, card_bytes=card.bytes)
            usb.udev['ID_SERIAL_SHORT'] = expand.USB_SERIAL
            (usb.sysfs / 'removable').write_text('0\n')
            with self.assertRaisesRegex(expand.ExpandError, 'not marked removable'):
                expand.verify_usb_device(usb.by_id, card_bytes=card.bytes)
            (usb.sysfs / 'removable').write_text('1\n')
            (usb.sysfs / 'partition').write_text('1\n')
            with self.assertRaisesRegex(expand.ExpandError, 'is a partition'):
                expand.verify_usb_device(usb.by_id, card_bytes=card.bytes)

    def test_cli_inspect_reports_the_card_in_the_reader(self):
        for name, card in CARDS.items():
            with self.subTest(card=name), self.fake_usb(card.bytes) as usb:
                for given in ([], ['--card-bytes', str(card.bytes)]):
                    code, out, err = self.run_cli('inspect', '--image', usb.by_id, *given)
                    self.assertEqual(code, 0, err)
                    body = json.loads(out)
                    self.assertEqual(body['target']['size_sectors'], card.sectors)
                    self.assertEqual(body['synthetic_card_bytes'], card.bytes)
                    self.assertEqual(body['unpartitioned_if_written_to_synthetic_card'],
                                     card.bytes - 1075838976)
                code, _, err = self.run_cli('inspect', '--image', usb.by_id,
                                            '--card-bytes', str(other_card(name).bytes))
                self.assertEqual(code, 1)
                self.assertIn('USB size mismatch', err)

    def test_cli_apply_accepts_card_bytes_that_match_the_card(self):
        for name, card in CARDS.items():
            with self.subTest(card=name), self.fake_usb(card.bytes) as usb, self.kernel_steps(), \
                    mock.patch.dict(os.environ, {'WRITE_GO': '1'}):
                code, out, err = self.run_cli('apply', '--image', usb.by_id, '--card-bytes', str(card.bytes))
                self.assertEqual(code, 0, err)
                body = json.loads(out)
                self.assertEqual(body['target']['size_sectors'], card.sectors)
                self.assertEqual(body['plan']['partition_3']['sector_count'], card.p3_sectors)

    def test_physical_apply_sizes_p3_to_the_card_in_the_reader(self):
        for name, card in CARDS.items():
            with self.subTest(card=name), self.fake_usb(card.bytes) as usb, self.kernel_steps() as kernel:
                before = populated(expand.read_mbr(usb.disk))
                result = expand.apply_plan(usb.by_id, self.layout, 'extra-partition', env={'WRITE_GO': '1'})
                self.assertEqual(result['target']['size_bytes'], card.bytes)
                p3 = result['plan']['partition_3']
                self.assertEqual((p3['start_sector'], p3['sector_count']), (2101248, card.p3_sectors))
                after = populated(expand.read_mbr(usb.disk))
                self.assertEqual(after[:2], before)
                self.assertEqual((after[2]['start_sector'], after[2]['sector_count']),
                                 (p3['start_sector'], p3['sector_count']))
                self.assertGreaterEqual(p3['start_sector'], 2099200 + 2048)
                self.assertLessEqual(p3['start_sector'] + p3['sector_count'], card.sectors)
                with usb.disk.open('rb') as stream:
                    stream.seek(2099200 * 512)
                    self.assertEqual(stream.read(12), b'A2-SPL-UBOOT')
                kernel.unmounted.assert_called_with(usb.resolved)
                kernel.partition.assert_called_once_with(usb.resolved, 3, card.p3_sectors)
                kernel.mkfs.assert_called_once_with(usb.resolved + '3')

    def test_physical_apply_refuses_card_bytes_that_disagree_before_writing(self):
        for name, card in CARDS.items():
            with self.subTest(card=name), self.fake_usb(card.bytes) as usb, self.kernel_steps() as kernel:
                mbr = expand.read_mbr(usb.disk)['raw']
                with self.assertRaisesRegex(expand.ExpandError, 'WRITE_GO=1'):
                    expand.apply_plan(usb.by_id, self.layout, 'extra-partition', card_bytes=card.bytes, env={})
                with self.assertRaisesRegex(expand.ExpandError, 'USB size mismatch'):
                    expand.apply_plan(usb.by_id, self.layout, 'extra-partition',
                                      card_bytes=other_card(name).bytes, env={'WRITE_GO': '1'})
                self.assertEqual(expand.read_mbr(usb.disk)['raw'], mbr)
                kernel.mkfs.assert_not_called()

    def test_physical_apply_refuses_a_card_swapped_mid_run(self):
        with self.fake_usb(CARDS['32GB'].bytes) as usb, self.kernel_steps() as kernel:
            mbr = expand.read_mbr(usb.disk)['raw']
            hash_region = expand.sha256_region

            def swap_card(*args, **kwargs):
                (usb.sysfs / 'size').write_text(f'{CARDS["64GB"].sectors}\n')
                return hash_region(*args, **kwargs)

            with mock.patch.object(expand, 'sha256_region', side_effect=swap_card), \
                    self.assertRaisesRegex(expand.ExpandError, 'USB size mismatch'):
                expand.apply_plan(usb.by_id, self.layout, 'extra-partition', env={'WRITE_GO': '1'})
            self.assertEqual(expand.read_mbr(usb.disk)['raw'], mbr)
            kernel.mkfs.assert_not_called()

    def test_write_base_refuses_a_card_of_the_wrong_size_before_writing(self):
        base = self.root / 'base.img'
        expand.synthesize_image(base, self.layout, self.layout.total_sectors * 512, a2_marker=b'NEW-BASE')
        with self.fake_usb(CARDS['32GB'].bytes) as usb, mock.patch.object(expand, 'require_unmounted'):
            with self.assertRaisesRegex(expand.ExpandError, 'USB size mismatch'):
                expand.write_base_image(usb.by_id, self.layout, base, env={'WRITE_GO': '1'},
                                        card_bytes=CARDS['64GB'].bytes)
            (usb.sysfs / 'size').write_text(f'{(512 << 20) // 512}\n')
            with self.assertRaisesRegex(expand.ExpandError, 'appliance image needs'):
                expand.write_base_image(usb.by_id, self.layout, base, env={'WRITE_GO': '1'})
            with usb.disk.open('rb') as stream:
                stream.seek(2099200 * 512)
                self.assertEqual(stream.read(12), b'A2-SPL-UBOOT')

    def test_write_go_usb_that_resolves_to_nvme_is_denied(self):
        usb = '/dev/disk/by-id/usb-forged'
        with mock.patch.object(expand, 'is_block_device', return_value=True), \
                mock.patch.object(os.path, 'realpath', return_value='/dev/nvme0n1'):
            with self.assertRaisesRegex(expand.ExpandError, 'system disk'):
                expand.require_regular_or_gated_usb(usb, {'WRITE_GO': '1'}, write=True)

    def test_apply_refuses_image_smaller_than_modeled_card(self):
        image = self.root / 'tiny.img'
        expand.synthesize_image(image, self.layout, card_bytes=self.layout.total_sectors * 512)
        with self.assertRaisesRegex(expand.ExpandError, 'smaller than the modeled card'):
            expand.apply_plan(image, self.layout, 'extra-partition', env={})

    def test_cli_plan_json(self):
        from io import StringIO
        with mock.patch('sys.stdout', new=StringIO()) as stdout:
            expand.main(['plan', '--mode', 'extra-partition', '--lock', str(LOCK)])
            body = stdout.getvalue()
        self.assertIn('"mode": "extra-partition"', body)
        self.assertIn('"recommended": true', body)


if __name__ == '__main__':
    unittest.main()
