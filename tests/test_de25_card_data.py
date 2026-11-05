import argparse
import contextlib
import hashlib
import os
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock

from scripts import de25_card_data as data

CAPACITY = 31267487744


def base_mbr():
    mbr = bytearray(512)
    # Nonzero boot bytes prove this is not reconstructed as a DE10 MBR.
    mbr[:16] = b'TERASIC-PRESERVE!'
    struct.pack_into('<I', mbr, 440, 0x7082bc26)
    for offset, name in ((446, 'partition_1'), (462, 'partition_2')):
        part = data.layout()[name]
        start, count = part['start_sector'], part['sectors']
        mbr[offset:offset + 16] = struct.pack(
            '<B3sB3sII', 0, data.media_module().chs(start), part['type'],
            data.media_module().chs(start + count - 1), start, count)
    mbr[510:] = b'\x55\xaa'
    return bytes(mbr)


class De25CardDataTests(unittest.TestCase):
    @contextlib.contextmanager
    def modeled_reader(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            disk = root / 'disk'
            disk.touch()
            link = root / 'usb-Generic_STORAGE_DEVICE-0:0'
            link.symlink_to(disk)
            node = root / 'devices/usb1/block/disk'
            node.mkdir(parents=True)
            for name, value in {'size': str(CAPACITY // 512), 'removable': '1',
                                'diskseq': '54'}.items():
                (node / name).write_text(value)
            sysfs = root / 'sys'
            sysfs.mkdir()
            (sysfs / 'disk').symlink_to(node)
            properties = {'DEVNAME': str(disk), 'ID_BUS': 'usb',
                          'ID_MODEL': 'STORAGE_DEVICE', 'ID_SERIAL': data.USB_SERIAL}
            with mock.patch.object(data, 'USB_BY_ID', str(link)), \
                    mock.patch.object(data.stat, 'S_ISBLK', return_value=True), \
                    mock.patch.object(data.shared, 'SYS_CLASS_BLOCK', sysfs), \
                    mock.patch.object(data.shared, '_udev_properties', return_value=properties), \
                    mock.patch.object(data.shared, 'require_unmounted') as unmounted:
                yield link, node, properties, unmounted

    def test_reader_requires_live_usb_identity_capacity_and_no_mounts(self):
        with self.modeled_reader() as (link, node, properties, unmounted):
            result = data.reader(link, CAPACITY)
            self.assertEqual(result['diskseq'], 54)
            self.assertEqual(result['size_bytes'], CAPACITY)
            unmounted.assert_called_once()
            for field in ('DEVNAME', 'ID_BUS', 'ID_MODEL', 'ID_SERIAL'):
                original = properties[field]
                properties[field] = 'different'
                with self.subTest(field=field), self.assertRaises(ValueError):
                    data.reader(link, CAPACITY)
                properties[field] = original
            with self.assertRaises(ValueError):
                data.reader(link, CAPACITY + 512)
            (node / 'removable').write_text('0')
            with self.assertRaises(ValueError):
                data.reader(link, CAPACITY)
            (node / 'removable').write_text('1')
            (node / 'partition').write_text('1')
            with self.assertRaises(ValueError):
                data.reader(link, CAPACITY)

    def test_mounted_reader_is_refused(self):
        with self.modeled_reader() as (link, node, properties, unmounted):
            unmounted.side_effect = ValueError('mounted target')
            with self.assertRaisesRegex(ValueError, 'mounted target'):
                data.reader(link, CAPACITY)

    @contextlib.contextmanager
    def modeled_apply(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            disk = root / 'disk'
            disk.write_bytes(base_mbr())
            partition = root / 'disk3'
            node = root / 'sys/disk3'
            node.mkdir(parents=True)
            (node / 'start').write_text('4261888\n')
            target = {'resolved_path': str(disk), 'size_bytes': CAPACITY,
                      'diskseq': 54, 'device': 0}
            filesystem = {'label': 'FESDATA3', 'incompat_features': '0x40',
                          'block_size': 4096, 'block_count': 56807424 // 8}
            with mock.patch.object(data, 'reference', return_value=(base_mbr(), data.layout(), 'b' * 64)), \
                    mock.patch.object(data.os, 'geteuid', return_value=0), \
                    mock.patch.dict(os.environ, {'WRITE_GO': '1'}), \
                    mock.patch.object(data, 'reader', return_value=target) as reader, \
                    mock.patch.object(data, 'protected_hashes', return_value={
                        'partition_1': 'a' * 64, 'partition_2': 'b' * 64}) as protected, \
                    mock.patch.object(data.shared, 'rescan_partition_table'), \
                    mock.patch.object(data.shared, 'require_partition_device',
                                      return_value=str(partition)), \
                    mock.patch.object(data.shared, 'SYS_CLASS_BLOCK', root / 'sys'), \
                    mock.patch.object(data.shared, 'format_ext4') as formatter, \
                    mock.patch.object(data.os, 'sync'), \
                    mock.patch.object(data.shared, 'read_ext4_superblock',
                                      return_value=filesystem):
                yield disk, reader, protected, formatter, target

    def test_modeled_apply_preserves_boot_entries_and_formats_only_p3(self):
        with self.modeled_apply() as (disk, reader, protected, formatter, target):
            result = data.execute(self.args('apply'))
            self.assertTrue(result['verified'])
            self.assertEqual(disk.read_bytes(), data.expanded_mbr(base_mbr(), result['plan']))
            formatter.assert_called_once_with(str(disk) + '3')
            self.assertEqual(reader.call_count, 4)
            self.assertEqual(protected.call_count, 2)

    def test_reader_replacement_refused_before_first_metadata_write(self):
        with self.modeled_apply() as (disk, reader, protected, formatter, target):
            reader.side_effect = [target, dict(target, diskseq=55)]
            with self.assertRaisesRegex(ValueError, 'reader changed before data setup'):
                data.execute(self.args('apply'))
            self.assertEqual(disk.read_bytes(), base_mbr())
            formatter.assert_not_called()

    def test_wrong_bootstrap_refused_before_metadata_write(self):
        with self.modeled_apply() as (disk, reader, protected, formatter, target):
            protected.return_value = {'partition_1': 'a' * 64, 'partition_2': 'c' * 64}
            with self.assertRaisesRegex(ValueError, 'bootstrap differs'):
                data.execute(self.args('apply'))
            self.assertEqual(disk.read_bytes(), base_mbr())
            formatter.assert_not_called()

    def test_changed_protected_bytes_fail_after_format_without_rollback(self):
        with self.modeled_apply() as (disk, reader, protected, formatter, target):
            protected.side_effect = [
                {'partition_1': 'a' * 64, 'partition_2': 'b' * 64},
                {'partition_1': 'c' * 64, 'partition_2': 'b' * 64}]
            with self.assertRaisesRegex(ValueError, 'changed FAT or bootstrap'):
                data.execute(self.args('apply'))
            formatter.assert_called_once()
            self.assertEqual(disk.read_bytes(), data.expanded_mbr(
                base_mbr(), data.plan(base_mbr(), CAPACITY)))

    def test_real_board_geometry_and_32gb_leftover(self):
        mbr = base_mbr()
        plan = data.plan(mbr, CAPACITY)
        self.assertEqual(plan['layout_id'], 'de25-nano-appliance-2g-v1')
        self.assertEqual(plan['start_sector'], 4261888)
        self.assertEqual(plan['sector_count'], 56807424)
        after = data.expanded_mbr(mbr, plan)
        self.assertEqual(after[:478], mbr[:478])
        self.assertEqual(after[494:], mbr[494:])
        self.assertEqual(after[466], 0x83)  # DE25 ext4 bootstrap, not A2.
        self.assertEqual(after[482], 0x83)
        self.assertEqual(struct.unpack_from('<II', after, 486),
                         (4261888, 56807424))

    def test_capacity_rounds_down_without_crossing_disk_end(self):
        plan = data.plan(base_mbr(), CAPACITY + 512)
        self.assertEqual(plan['start_sector'] + plan['sector_count'], CAPACITY // 512)

    def test_refuses_wrong_board_and_existing_data_before_planning(self):
        for offset, value in ((450, 0x0c), (466, 0xa2), (478, 1), (494, 1), (510, 0)):
            mbr = bytearray(base_mbr())
            mbr[offset] = value
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                data.plan(bytes(mbr), CAPACITY)
        for offset in (454, 470):
            mbr = bytearray(base_mbr())
            struct.pack_into('<I', mbr, offset, 0x80000000)
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                data.plan(bytes(mbr), CAPACITY)

    def test_refuses_invalid_or_insufficient_capacity(self):
        for size in (True, -512, CAPACITY + 1, 1 << 42,
                     data.layout()['total_sectors'] * 512):
            with self.subTest(size=size), self.assertRaises(ValueError):
                data.plan(base_mbr(), size)

    def test_reference_requires_exact_hash_and_regular_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'base.img'
            source.write_bytes(base_mbr())
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            with self.assertRaisesRegex(ValueError, 'reference size'):
                data.reference(source, digest)
            with self.assertRaisesRegex(ValueError, 'exact verified'):
                data.reference(source, '0' * 64)
            link = root / 'link.img'
            link.symlink_to(source)
            with self.assertRaises((ValueError, OSError)):
                data.reference(link, digest)
            with self.assertRaisesRegex(ValueError, 'base-sha256'):
                data.reference(source, None)
            fifo = root / 'fifo'
            os.mkfifo(fifo)
            with self.assertRaisesRegex(ValueError, 'exact verified'):
                data.reference(fifo, digest)

    def args(self, command, **extra):
        return argparse.Namespace(command=command, mode='extra-partition',
                                  base_image=Path('/reference.img'), base_sha256='a' * 64,
                                  card_bytes=CAPACITY, image=Path(data.USB_BY_ID),
                                  data_receipt=None, **extra)

    def test_grow_fat_and_unknown_commands_never_read_device(self):
        for command, mode in (('apply', 'grow-fat'), ('write-base', 'extra-partition'),
                              ('synthesize', 'extra-partition')):
            args = self.args(command)
            args.mode = mode
            with mock.patch.object(data, 'reference') as reference, self.assertRaises(ValueError):
                data.execute(args)
            reference.assert_not_called()

    def test_apply_requires_both_root_and_explicit_write_gate(self):
        args = self.args('apply')
        for uid, write in ((1000, '1'), (0, ''), (0, '0')):
            with self.subTest(uid=uid, write=write), \
                    mock.patch.object(data, 'reference', return_value=(base_mbr(), data.layout(), 'b' * 64)), \
                    mock.patch.object(data.os, 'geteuid', return_value=uid), \
                    mock.patch.dict(os.environ, {'WRITE_GO': write}), \
                    mock.patch.object(data, 'reader') as reader, self.assertRaises(ValueError):
                data.execute(args)
            reader.assert_not_called()

    def test_raw_mmc_system_and_other_reader_paths_are_refused(self):
        for path in ('/dev/mmcblk0', '/dev/sda', '/dev/nvme0n1',
                     '/dev/disk/by-id/usb-another-reader'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                data.reader(path)

    def test_plan_only_never_opens_a_physical_device(self):
        args = self.args('plan')
        with mock.patch.object(data, 'reference', return_value=(base_mbr(), data.layout(), 'b' * 64)), \
                mock.patch.object(data, 'reader') as reader:
            result = data.execute(args)
        reader.assert_not_called()
        self.assertEqual(result['label'], 'FESDATA3')


if __name__ == '__main__':
    unittest.main()
