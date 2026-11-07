"""Add FESDATA3 after the existing DE25 FAT and ext4 bootstrap partitions.

Called only by the shared appliance expand command. Reference images remain
regular, immutable files; physical apply is limited to the explicitly selected
removable reader and preserves both existing partitions byte for byte.
"""
from pathlib import Path
import fcntl
import hashlib
import json
import os
import re
import stat
import struct
import sys

try:
    from . import appliance_expand_dry_run as shared
except ImportError:
    import appliance_expand_dry_run as shared

ROOT = Path(__file__).resolve().parents[1]
USB_BY_ID = '/dev/disk/by-id/usb-Generic_STORAGE_DEVICE-0:0'
USB_SERIAL = 'Generic_STORAGE_DEVICE-0:0'
ALIGN = 2048
MIN_DATA_BYTES = 32 << 20
BOOTSTRAP_IDENTITY = 'ext4-mount-read-bookkeeping-v1'


def media_module():
    sys.path.insert(0, str(ROOT / 'image/de25-nano'))
    import managed_media
    if managed_media.ROOT.resolve() != ROOT.resolve():
        raise ValueError('DE25 data layout differs from selected FES source')
    return managed_media


def layout():
    media = media_module()
    return media.appliance_layout(media.boot.load_lock())


def plan(mbr, card_bytes):
    geometry = layout()
    if (type(card_bytes) is not int or card_bytes % 512 or
            card_bytes // 512 > 0xffffffff):
        raise ValueError('DE25 data capacity must fit an aligned MBR disk')
    if len(mbr) != 512 or mbr[510:] != b'\x55\xaa':
        raise ValueError('DE25 base MBR signature differs')
    for offset, name in ((446, 'partition_1'), (462, 'partition_2')):
        part = geometry[name]
        if (mbr[offset] not in (0, 0x80) or mbr[offset + 4] != part['type'] or
                struct.unpack_from('<II', mbr, offset + 8) !=
                (part['start_sector'], part['sectors'])):
            raise ValueError('DE25 FAT/bootstrap geometry differs')
    if mbr[478:510] != bytes(32):
        raise ValueError('DE25 data setup requires empty third and fourth MBR entries')
    start = shared.align_up(geometry['total_sectors'], ALIGN)
    end = shared.align_down(card_bytes // 512, ALIGN)
    if (end - start) * 512 < MIN_DATA_BYTES:
        raise ValueError('DE25 card has insufficient unused capacity for FESDATA3')
    return {'board': 'de25-nano', 'layout_id': media_module().LAYOUT_ID,
            'card_bytes': card_bytes, 'start_sector': start,
            'sector_count': end - start, 'type': 0x83, 'label': 'FESDATA3'}


def expanded_mbr(before, data_plan):
    # Keep all Terasic boot bytes, disk identity, FAT and bootstrap descriptors.
    start, count = data_plan['start_sector'], data_plan['sector_count']
    chs = media_module().chs
    third = struct.pack('<B3sB3sII', 0, chs(start), 0x83,
                        chs(start + count - 1), start, count)
    return before[:478] + third + before[494:]


def bootstrap_read_ranges(reference):
    """Only access/mount bookkeeping in the exact FES bootstrap ext4 profile.

    Field definitions: kernel.org/doc/html/latest/filesystems/ext4/{super,
    inodes,group_descr}.html. Derive inode locations only from the verified
    reference. Modes, owners, other timestamps, allocation, extents, file
    contents, unused bytes and filesystem/error state remain protected.
    """
    if len(reference) < 2048:
        raise ValueError('truncated DE25 bootstrap reference')
    sb = reference[1024:2048]
    u32 = lambda offset: struct.unpack_from('<I', sb, offset)[0]
    inode_count, blocks, first, log_size, per_group, inodes_per_group = (
        u32(0), u32(4), u32(20), u32(24), u32(32), u32(40))
    inode_size = struct.unpack_from('<H', sb, 88)[0]
    if (struct.unpack_from('<H', sb, 56)[0] != 0xef53 or
            struct.unpack_from('<H', sb, 58)[0] != 1 or
            log_size != 0 or inode_size != 256 or
            u32(96) != 0x42 or u32(100) != 0x3 or
            blocks * 1024 != len(reference) or first != 1 or
            not 0 < per_group <= 8192 or not 0 < inodes_per_group <= 8192 or
            not 0 < inode_count <= 128 or
            sb[120:136].rstrip(b'\0') != b'FES_BOOTSTRAP' or
            sb[136:200] not in (bytes(64), b'/' + bytes(63))):
        raise ValueError('unsupported DE25 bootstrap ext4 bookkeeping profile')
    groups = (blocks - first + per_group - 1) // per_group
    if not 0 < groups <= 32 or inode_count > groups * inodes_per_group:
        raise ValueError('invalid DE25 bootstrap group geometry')
    ranges = [(1024 + 44, 1024 + 54), (1024 + 136, 1024 + 137),
              (1024 + 376, 1024 + 384)]
    for group in range(groups):
        descriptor = 2048 + group * 32
        bitmap, table = struct.unpack_from('<II', reference, descriptor + 4)
        count = min(inodes_per_group, inode_count - group * inodes_per_group)
        if (bitmap * 1024 < 3072 or (bitmap + 1) * 1024 > len(reference) or
                table * 1024 < 3072 or
                table * 1024 + inodes_per_group * inode_size > len(reference)):
            raise ValueError('invalid DE25 bootstrap inode metadata bounds')
        for index in range(max(0, count)):
            if not reference[bitmap * 1024 + index // 8] & (1 << (index % 8)):
                continue
            offset = table * 1024 + index * inode_size
            mode = struct.unpack_from('<H', reference, offset)[0] & 0xf000
            flags = struct.unpack_from('<I', reference, offset + 32)[0]
            # EA_INODE repurposes i_atime as an attribute-value checksum.
            if mode not in (0x4000, 0x8000, 0xa000) or flags & 0x200000:
                continue
            ranges.append((offset + 8, offset + 12))
            extra = struct.unpack_from('<H', reference, offset + 128)[0]
            if extra > inode_size - 128:
                raise ValueError('invalid DE25 bootstrap extended inode size')
            if extra >= 16:
                ranges.append((offset + 140, offset + 144))
    ranges.sort()
    if any(right[0] < left[1] for left, right in zip(ranges, ranges[1:])):
        raise ValueError('aliased DE25 bootstrap bookkeeping fields')
    return ranges


def bootstrap_digest(data, ranges):
    normalized = bytearray(data)
    for start, end in ranges:
        if not 0 <= start < end <= len(normalized):
            raise ValueError('bootstrap bookkeeping range exceeds filesystem')
        normalized[start:end] = bytes(end - start)
    return hashlib.sha256(normalized).hexdigest()


def check_bootstrap(path, geometry, identity):
    part = geometry['partition_2']
    with shared._open_path(path, 'rb') as source:
        source.seek(part['start_sector'] * 512)
        data = source.read(part['sectors'] * 512)
    if (len(data) != part['sectors'] * 512 or
            data[1024 + 136:1024 + 200] not in (bytes(64), b'/' + bytes(63)) or
            bootstrap_digest(data, identity['ranges']) != identity['normalized_sha256']):
        raise ValueError('DE25 bootstrap differs from retained verified reference')
    return hashlib.sha256(data).hexdigest()


def reference(path, expected_sha256):
    path = Path(path)
    if not re.fullmatch('[0-9a-f]{64}', expected_sha256 or ''):
        raise ValueError('DE25 data setup requires --base-image and --base-sha256')
    geometry = layout()
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode):
            raise ValueError('DE25 reference must be the exact verified regular card image')
        if hashlib.file_digest(source, 'sha256').hexdigest() != expected_sha256:
            raise ValueError('DE25 reference must be the exact verified regular card image')
        if info.st_size != geometry['total_sectors'] * 512:
            raise ValueError('DE25 reference size differs from the appliance layout')
        source.seek(0)
        mbr = source.read(512)
        part = geometry['partition_2']
        source.seek(part['start_sector'] * 512)
        bootstrap = source.read(part['sectors'] * 512)
        if len(bootstrap) != part['sectors'] * 512:
            raise ValueError('truncated DE25 bootstrap')
        ranges = bootstrap_read_ranges(bootstrap)
        identity = {'sha256': hashlib.sha256(bootstrap).hexdigest(),
                    'normalized_sha256': bootstrap_digest(bootstrap, ranges),
                    'rule': BOOTSTRAP_IDENTITY, 'ranges': ranges}
        after = os.fstat(source.fileno())
        if any(getattr(info, name) != getattr(after, name) for name in
               ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns', 'st_ctime_ns')):
            raise ValueError('DE25 reference changed while verifying')
    # Validate the fixed partitions even when capacity has not been selected.
    plan(mbr, path.stat().st_size + MIN_DATA_BYTES + (ALIGN * 512))
    return mbr, geometry, identity


def require_tools():
    blockdev = shared._find_tool('blockdev')
    formatter = shared._find_tool('mkfs.ext4') or shared._find_tool('mke2fs')
    checker = shared._find_tool('e2fsck')
    if (not blockdev or not formatter or not checker or
            any(Path(tool[0]).resolve().name == 'busybox' for tool in (formatter, checker))):
        raise ValueError('DE25 data setup requires blockdev and full ext4 format/check tools')


def rescan(device):
    shared._run_tool(shared._find_tool('blockdev') + ['--rereadpt', device],
                     30, 'DE25 partition-table reread failed')
    settle = shared._find_tool('udevadm')
    if settle:
        shared._run_tool(settle + ['settle', '--timeout=10'], 15, 'DE25 udev settle failed')


def reader(path, card_bytes=None):
    path = Path(path)
    if str(path) != USB_BY_ID:
        raise ValueError('DE25 data operation requires its exact removable USB by-id')
    resolved = path.resolve(strict=True)
    info = resolved.stat()
    if not stat.S_ISBLK(info.st_mode) or shared.is_system_disk(str(resolved)):
        raise ValueError('DE25 reader must resolve to a whole non-system block device')
    node = shared.SYS_CLASS_BLOCK / resolved.name
    if (node / 'partition').exists() or (node / 'removable').read_text().strip() != '1':
        raise ValueError('DE25 reader must contain a removable whole disk')
    if not any(part.startswith('usb') for part in node.resolve().parts):
        raise ValueError('DE25 reader sysfs path is not USB')
    size = int((node / 'size').read_text()) * 512
    if size <= 0 or (card_bytes is not None and card_bytes != size):
        raise ValueError('DE25 reader capacity differs or media is absent')
    properties = shared._udev_properties(str(path))
    expected = {'DEVNAME': str(resolved), 'ID_BUS': 'usb',
                'ID_MODEL': 'STORAGE_DEVICE', 'ID_SERIAL': USB_SERIAL}
    if any(properties.get(key) != value for key, value in expected.items()):
        raise ValueError('DE25 reader USB identity differs')
    shared.require_unmounted(str(resolved))
    return {'by_id': str(path), 'resolved_path': str(resolved), 'size_bytes': size,
            'diskseq': int((node / 'diskseq').read_text()), 'device': info.st_rdev}


def protected_hashes(path, geometry):
    return {name: shared.sha256_region(path, part['sectors'] * 512,
                                      part['start_sector'] * 512)
            for name, part in ((name, geometry[name])
                               for name in ('partition_1', 'partition_2'))}


def verify_table(path, expected):
    with shared._open_path(path, 'rb') as source:
        if source.read(512) != expected:
            raise ValueError('DE25 partition table changed unexpectedly')


def execute(args):
    if args.mode != 'extra-partition' or args.command not in ('plan', 'inspect', 'apply', 'verify'):
        raise ValueError('DE25 supports only extra-partition plan/inspect/apply/verify')
    if args.base_image is None:
        raise ValueError('DE25 requires the retained verified --base-image')
    base_mbr, geometry, reference_boot = reference(args.base_image, args.base_sha256)
    if args.command == 'plan':
        if args.card_bytes is None:
            raise ValueError('DE25 plan requires explicit --card-bytes')
        return plan(base_mbr, args.card_bytes)
    if args.image is None:
        raise ValueError('DE25 requires --image with its reader USB by-id')
    if args.command == 'apply' and (os.geteuid() != 0 or os.environ.get('WRITE_GO') != '1'):
        raise ValueError('DE25 physical data setup requires root and WRITE_GO=1')
    if args.command == 'apply':
        require_tools()
    target = reader(args.image, args.card_bytes)
    data_plan = plan(base_mbr, target['size_bytes'])
    expected = expanded_mbr(base_mbr, data_plan)
    path = target['resolved_path']
    if args.command == 'inspect':
        verify_table(path, base_mbr)
        return {'target': target, 'plan': data_plan, 'existing_data': False,
                'bootstrap_sha256': check_bootstrap(path, geometry, reference_boot),
                'bootstrap_identity': BOOTSTRAP_IDENTITY}
    if args.command == 'verify':
        if args.data_receipt is None:
            raise ValueError('DE25 verification requires --data-receipt from apply')
        receipt = json.loads(Path(args.data_receipt).read_text())
        if (receipt.get('plan') != data_plan or receipt.get('base_sha256') != args.base_sha256
                or receipt.get('target') != target or
                receipt.get('reference_bootstrap_sha256') != reference_boot['sha256'] or
                receipt.get('bootstrap_identity') != BOOTSTRAP_IDENTITY or
                receipt.get('bootstrap_normalized_sha256') != reference_boot['normalized_sha256']):
            raise ValueError('DE25 data receipt differs from selected card and source')
        verify_table(path, expected)
        if protected_hashes(path, geometry) != receipt['protected_sha256']:
            raise ValueError('DE25 FAT/bootstrap changed after data setup')
        partition = shared.require_partition_device(path, 3, data_plan['sector_count'])
        filesystem = shared.read_ext4_superblock(partition)
        if filesystem != receipt['filesystem']:
            raise ValueError('DE25 data filesystem differs from apply receipt')
        return dict(receipt, independently_verified=True)
    # All admission and reference checks precede the first device write.
    verify_table(path, base_mbr)
    before = protected_hashes(path, geometry)
    if check_bootstrap(path, geometry, reference_boot) != before['partition_2']:
        raise ValueError('DE25 bootstrap changed while validating')
    with shared._open_path(path, 'r+b') as device:
        fcntl.flock(device.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        if reader(args.image, args.card_bytes) != target:
            raise ValueError('DE25 reader changed before data setup')
        if os.fstat(device.fileno()).st_rdev != target['device']:
            raise ValueError('DE25 open device differs from selected reader')
        verify_table(path, base_mbr)
        if device.write(expected) != len(expected):
            raise OSError('DE25 partition-table write was incomplete')
        device.flush()
        os.fsync(device.fileno())
        verify_table(path, expected)
    rescan(path)
    partition = shared.require_partition_device(path, 3, data_plan['sector_count'])
    node = shared.SYS_CLASS_BLOCK / Path(partition).name
    if int((node / 'start').read_text()) != data_plan['start_sector']:
        raise ValueError('DE25 kernel data-partition start differs')
    if reader(args.image, args.card_bytes) != target:
        raise ValueError('DE25 reader changed before data format')
    verify_table(path, expected)
    shared.format_ext4(partition)
    os.sync()
    shared._run_tool(shared._find_tool('e2fsck') + ['-fn', partition],
                     300, 'DE25 data filesystem verification failed')
    filesystem = shared.read_ext4_superblock(partition)
    if (filesystem['label'] != 'FESDATA3' or
            not int(filesystem['incompat_features'], 16) & 0x40 or
            filesystem['block_count'] * filesystem['block_size'] != data_plan['sector_count'] * 512):
        raise ValueError('DE25 data format is not the expected full ext4 volume')
    if reader(args.image, args.card_bytes) != target:
        raise ValueError('DE25 reader changed after data format')
    verify_table(path, expected)
    if protected_hashes(path, geometry) != before:
        raise ValueError('DE25 data setup changed FAT or bootstrap bytes')
    return {'kind': 'fes-de25-data-setup', 'plan': data_plan, 'target': target,
            'base_sha256': args.base_sha256, 'protected_sha256': before,
            'reference_bootstrap_sha256': reference_boot['sha256'],
            'bootstrap_identity': BOOTSTRAP_IDENTITY,
            'bootstrap_normalized_sha256': reference_boot['normalized_sha256'],
            'mbr_sha256': hashlib.sha256(expected).hexdigest(),
            'filesystem': filesystem, 'verified': True, 'hardware_acceptance': 'not-run'}
