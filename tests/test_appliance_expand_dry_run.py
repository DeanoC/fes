"""Host-side leftover-capacity expand dry-run; never writes a block device."""
from pathlib import Path
import os
import tempfile
import unittest
from unittest import mock

from scripts import appliance_expand_dry_run as expand
from scripts.appliance_media_inside import appliance_layout
from scripts.media_inputs import MediaLock

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'boot-media.lock.toml'


class ApplianceExpandDryRunTests(unittest.TestCase):
    def setUp(self):
        self.layout = appliance_layout(MediaLock.load(LOCK))
        self.temporary = tempfile.TemporaryDirectory(prefix='fes-expand-')
        self.root = Path(self.temporary.name)

    def tearDown(self):
        self.temporary.cleanup()

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
            'size_sectors': expand.USB_SIZE_SECTORS,
            'size_bytes': expand.USB_SIZE_BYTES,
            'serial': expand.USB_SERIAL,
            'verified': True,
        }
        with mock.patch.object(expand, 'is_block_device', return_value=True), \
                mock.patch.object(expand, 'is_system_disk', return_value=False), \
                mock.patch.object(expand, 'verify_usb_device', return_value=identity) as verify:
            self.assertEqual(expand.require_regular_or_gated_usb(usb, {'WRITE_GO': '1'}, write=True), identity)
            verify.assert_called_once_with(usb, write=True)
        with mock.patch.object(expand, 'is_block_device', return_value=True):
            with self.assertRaisesRegex(expand.ExpandError, 'WRITE_GO=1'):
                expand.require_regular_or_gated_usb(usb, {}, write=True)

    def test_physical_apply_dispatches_only_recommended_mode(self):
        identity = {
            'by_id': expand.USB_BY_ID,
            'resolved_path': '/dev/sda',
            'size_bytes': expand.USB_SIZE_BYTES,
            'verified': True,
        }
        with mock.patch.object(expand, 'require_regular_or_gated_usb', return_value=identity), \
                mock.patch.object(expand, 'apply_physical_extra_partition', return_value={'verified': True}) as apply:
            result = expand.apply_plan(expand.USB_BY_ID, self.layout, 'extra-partition',
                                       env={'WRITE_GO': '1'})
        self.assertEqual(result, {'verified': True})
        apply.assert_called_once_with(expand.USB_BY_ID, self.layout, env={'WRITE_GO': '1'})
        with self.assertRaisesRegex(expand.ExpandError, 'recommended extra-partition'):
            expand.apply_plan(expand.USB_BY_ID, self.layout, 'grow-fat', env={'WRITE_GO': '1'})

    def test_expanded_table_uses_verified_58g_card_capacity(self):
        image = self.root / 'card.img'
        expand.synthesize_image(image, self.layout, expand.USB_SIZE_BYTES)
        plan = expand.plan_extra_partition(self.layout, expand.USB_SIZE_BYTES)
        expand.write_mbr(image, self.layout.disk_id,
                          expand.expected_image_partitions(self.layout) + [plan['partition_3']])
        self.assertEqual(expand.require_expanded_table(expand.read_mbr(image), self.layout,
                                                        expand.USB_SIZE_BYTES), plan)
        self.assertGreater(plan['partition_3']['bytes'], 50 << 30)

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
