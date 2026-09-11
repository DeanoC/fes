#!/usr/bin/env python3
"""Plan and apply the leftover-capacity expand for the appliance media.

Regular files remain the dry-run path. Physical operations are deliberately
narrower: only the exact removable USB-by-id recorded for the spare-card HIL
is admitted, and ``WRITE_GO=1`` is required. A physical apply creates p3 and
formats it as ext4; it never grows FAT or moves the A2 boot partition.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import time

try:
    from .appliance_media_inside import LAYOUT_ID, appliance_layout
    from .media_inputs import MediaLock
except ImportError:
    from appliance_media_inside import LAYOUT_ID, appliance_layout
    from media_inputs import MediaLock

SECTOR = 512
ALIGN_SECTORS = 2048
MBR_DISK_ID_OFFSET = 440
MBR_PART_OFFSETS = (446, 462, 478, 494)
CHS = bytes.fromhex('feffff')
# 28.8 GiB model of the Generic STORAGE DEVICE class recorded in
# docs/validation/2026-09-08-appliance-release.md. Not a live measurement of
# the 0.2.0-dev.4 card; this host had no USB reader attached.
SYNTHETIC_CARD_BYTES = (288 * (1 << 30)) // 10 // SECTOR * SECTOR
USB_BY_ID_PREFIX = '/dev/disk/by-id/usb-'
USB_BY_ID = '/dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0'
USB_VENDOR = 'Generic'
USB_MODEL = 'STORAGE_DEVICE'
USB_SERIAL = '000000000819'
# Read from /sys/class/block/sda/size on the designated spare. Keep this
# exact: a human-readable lsblk value such as 58.2G is not a safe identity.
USB_SIZE_SECTORS = 122138624
USB_SIZE_BYTES = USB_SIZE_SECTORS * SECTOR
EXT4_LABEL = 'FESDATA3'
PARTITION_RESCAN_TIMEOUT = 15
# Raw node names are never admitted. USB-by-id may resolve to /dev/sdX or
# /dev/diskN; only these resolved prefixes remain denied after WRITE_GO.
SYSTEM_RESOLVED_DENY_PREFIXES = (
    '/dev/disk0',
    '/dev/nvme',
    '/dev/mmcblk',
    '/dev/mapper',
    '/dev/md',
    '/dev/root',
)
RAW_BLOCK_PREFIXES = ('/dev/sd', '/dev/disk', '/dev/nvme', '/dev/mmcblk', '/dev/mapper', '/dev/md')


class ExpandError(ValueError):
    pass


def align_up(value, alignment):
    return (value + alignment - 1) // alignment * alignment


def align_down(value, alignment):
    return value // alignment * alignment


def load_layout(lock_path):
    return appliance_layout(MediaLock.load(lock_path))


def pack_partition(active, type_code, start, count):
    entry = bytearray(16)
    struct.pack_into('<B3sB3sII', entry, 0, 0x80 if active else 0, CHS, type_code, CHS, start, count)
    return bytes(entry)


def unpack_partition(entry):
    status, chs_start, type_code, chs_end, start, count = struct.unpack_from('<B3sB3sII', entry)
    return {
        'active': status == 0x80,
        'status': status,
        'chs_start': chs_start.hex(),
        'type': type_code,
        'chs_end': chs_end.hex(),
        'start_sector': start,
        'sector_count': count,
        'bytes': count * SECTOR,
    }


def read_mbr(path):
    with Path(path).open('rb') as stream:
        mbr = stream.read(512)
    if len(mbr) != 512:
        raise ExpandError('image is shorter than one MBR sector')
    if mbr[510:] != b'\x55\xaa':
        raise ExpandError('MBR signature missing')
    disk_id, = struct.unpack_from('<I', mbr, MBR_DISK_ID_OFFSET)
    partitions = [unpack_partition(mbr[offset:offset + 16]) for offset in MBR_PART_OFFSETS]
    return {'disk_id': disk_id, 'partitions': partitions, 'raw': mbr}


def write_mbr(path, disk_id, partitions):
    if len(partitions) > 4:
        raise ExpandError('MBR supports at most four partitions')
    mbr = bytearray(512)
    struct.pack_into('<I', mbr, MBR_DISK_ID_OFFSET, disk_id)
    for offset, part in zip(MBR_PART_OFFSETS, partitions):
        mbr[offset:offset + 16] = pack_partition(part['active'], part['type'],
                                                 part['start_sector'], part['sector_count'])
    mbr[510:512] = b'\x55\xaa'
    with Path(path).open('r+b') as stream:
        stream.write(mbr)
        stream.flush()
        os.fsync(stream.fileno())
    return bytes(mbr)


def fat32_table_bytes(partition_bytes, cluster_size=SECTOR):
    if cluster_size < SECTOR or partition_bytes < cluster_size:
        return 0
    clusters = partition_bytes // cluster_size
    return 2 * clusters * 4


def expected_image_partitions(layout):
    return [
        {
            'active': layout.partition_1.active,
            'type': layout.partition_1.type,
            'start_sector': layout.partition_1.start_sector,
            'sector_count': layout.partition_1.sector_count,
            'role': 'fat',
        },
        {
            'active': layout.partition_2.active,
            'type': layout.partition_2.type,
            'start_sector': layout.partition_2.start_sector,
            'sector_count': layout.partition_2.sector_count,
            'role': 'a2-spl-uboot',
        },
    ]


def require_appliance_table(mbr, layout):
    parts = [part for part in mbr['partitions'] if part['sector_count']]
    expected = expected_image_partitions(layout)
    if mbr['disk_id'] != layout.disk_id:
        raise ExpandError('disk identifier differs from locked appliance media')
    if len(parts) != 2:
        raise ExpandError('appliance image must have exactly two populated MBR entries')
    for actual, want in zip(parts, expected):
        if (actual['start_sector'] != want['start_sector'] or actual['sector_count'] != want['sector_count']
                or actual['type'] != want['type'] or actual['active'] != want['active']):
            raise ExpandError('MBR does not match de10-nano-appliance-1g-v1')


def inspect_image(path, layout, card_bytes=SYNTHETIC_CARD_BYTES):
    path = Path(path)
    size = path.stat().st_size
    mbr = read_mbr(path)
    require_appliance_table(mbr, layout)
    image_bytes = layout.total_sectors * SECTOR
    card_sectors = card_bytes // SECTOR
    leftover = max(0, card_bytes - image_bytes)
    return {
        'layout_id': LAYOUT_ID,
        'path': str(path),
        'file_size': size,
        'image_bytes': image_bytes,
        'disk_id': hex(mbr['disk_id']),
        'partitions': [
            {k: v for k, v in part.items() if k != 'status'}
            for part in mbr['partitions'] if part['sector_count']
        ],
        'a2_start_sector': layout.partition_2.start_sector,
        'a2_bytes': layout.partition_2.sector_count * SECTOR,
        'uboot_load': 'A2 start + 0x200 sectors (locked SPL contract)',
        'mmcroot': '/dev/mmcblk0p1 (locked U-Boot; FAT must remain partition 1)',
        'synthetic_card_bytes': card_bytes,
        'synthetic_card_sectors': card_sectors,
        'unpartitioned_if_written_to_synthetic_card': leftover,
        'fat32_table_bytes_512_cluster_1g': fat32_table_bytes(layout.partition_1.sector_count * SECTOR, SECTOR),
        'fat32_table_bytes_512_cluster_card': fat32_table_bytes(leftover + layout.partition_1.sector_count * SECTOR, SECTOR),
        'fat32_table_bytes_64k_cluster_card': fat32_table_bytes(leftover + layout.partition_1.sector_count * SECTOR, 64 << 10),
        'live_dev4_untouched': True,
    }


def plan_extra_partition(layout, card_bytes=SYNTHETIC_CARD_BYTES):
    card_sectors = card_bytes // SECTOR
    a2_end = layout.partition_2.start_sector + layout.partition_2.sector_count
    start = align_up(a2_end, ALIGN_SECTORS)
    count = align_down(card_sectors - start, ALIGN_SECTORS)
    if count < ALIGN_SECTORS:
        raise ExpandError('synthetic card has no aligned leftover for a data partition')
    data_bytes = count * SECTOR
    return {
        'mode': 'extra-partition',
        'layout_id': LAYOUT_ID,
        'recommended': True,
        'touches_fat': False,
        'touches_a2': False,
        'safe_while_fat_mounted': True,
        'partition_3': {
            'active': False,
            'type': 0x83,
            'start_sector': start,
            'sector_count': count,
            'bytes': data_bytes,
            'role': 'fogcast-mutable-ext4',
        },
        'keep_on_fat': [
            '/linux/linux.img',
            '/linux/zImage_dtb',
            '/menu.rbf',
            '/fogcast/agent.toml',
            '/fogcast/launcher.json',
            '/fogcast/releases/',
            '/fogcast/target-id',
        ],
        'move_or_bind_to_p3': [
            '/media/fat/fogcast/cache',
            '/media/fat/fogcast/saves',
            '/media/fat/fogcast/core-data',
            '/media/fat/fogcast/launcher-cache',
            '/media/fat/fogcast/evidence',
        ],
        'notes': [
            'Cyclone V ROM still finds type 0xA2 as partition 2; U-Boot mmcroot stays p1.',
            'Bootstrap continues to read factory/known-good from FAT; no boot ABI change.',
            'FogCast default cache_max_bytes is 2 GiB and cannot fit on the 1 GiB FAT.',
            'Creating p3 does not grow the FAT filesystem and does not rewrite A2.',
        ],
    }


def plan_grow_fat(layout, card_bytes=SYNTHETIC_CARD_BYTES):
    card_sectors = card_bytes // SECTOR
    a2_count = layout.partition_2.sector_count
    new_a2_start = align_down(card_sectors - a2_count, ALIGN_SECTORS)
    new_fat_count = new_a2_start - layout.partition_1.start_sector
    if new_a2_start <= layout.partition_2.start_sector:
        raise ExpandError('synthetic card is not larger than the assembled image')
    grown_bytes = new_fat_count * SECTOR
    return {
        'mode': 'grow-fat',
        'layout_id': LAYOUT_ID,
        'recommended': False,
        'touches_fat': True,
        'touches_a2': True,
        'safe_while_fat_mounted': False,
        'requires_unmounted_fat': True,
        'moved_a2': {
            'active': False,
            'type': 0xa2,
            'start_sector': new_a2_start,
            'sector_count': a2_count,
            'bytes': a2_count * SECTOR,
            'copy_entire_partition': True,
        },
        'grown_fat': {
            'active': True,
            'type': 0x0c,
            'start_sector': layout.partition_1.start_sector,
            'sector_count': new_fat_count,
            'bytes': grown_bytes,
        },
        'fat32_table_bytes_if_cluster_stays_512': fat32_table_bytes(grown_bytes, SECTOR),
        'fat32_table_bytes_if_recreated_64k': fat32_table_bytes(grown_bytes, 64 << 10),
        'notes': [
            'A2 sits immediately after FAT, so grow-FAT must copy the whole 1 MiB A2 partition first.',
            'FAT32 cannot be grown while it is the mounted kernel root / media/fat.',
            'Current mkfs uses 512-byte clusters; in-place fatresize would keep that cluster size.',
            'On-target first boot therefore cannot safely grow FAT; host-side unmounted rewrite could.',
            'Do not run this against the live 0.2.0-dev.4 card.',
        ],
    }


def is_block_device(path):
    try:
        mode = os.stat(path, follow_symlinks=True).st_mode
    except OSError as error:
        raise ExpandError(f'cannot stat target: {error}') from error
    return stat.S_ISBLK(mode)


def _matches_prefix(path, prefixes):
    path = str(path)
    for prefix in prefixes:
        if path == prefix or path.startswith(prefix):
            return True
    return False


def is_system_disk(path):
    return _matches_prefix(os.path.realpath(path), SYSTEM_RESOLVED_DENY_PREFIXES)


def is_raw_block_path(path):
    return _matches_prefix(path, RAW_BLOCK_PREFIXES)


def _udev_properties(path):
    try:
        result = subprocess.run(
            ['udevadm', 'info', '--query=property', '--name', str(path)],
            check=False, capture_output=True, text=True, timeout=10)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ExpandError(f'cannot verify USB identity with udev: {error}') from error
    if result.returncode != 0:
        raise ExpandError('udev refused USB identity inspection')
    properties = {}
    for line in result.stdout.splitlines():
        if '=' in line:
            key, value = line.split('=', 1)
            properties[key] = value
    return properties


def verify_usb_device(path, write=False):
    """Return the identity of the one physical device admitted for this HIL."""

    path = str(path)
    if path != USB_BY_ID:
        raise ExpandError(f'physical target must be the exact spare USB by-id: {USB_BY_ID}')
    if not Path(path).exists():
        raise ExpandError(f'required spare USB device is missing: {path}')
    if not is_block_device(path):
        raise ExpandError('spare USB by-id is not a block device')
    resolved = os.path.realpath(path)
    if is_system_disk(path) or is_system_disk(resolved):
        raise ExpandError('USB target resolves to a denied system disk')
    device_name = Path(resolved).name
    sysfs = Path('/sys/class/block') / device_name
    if (sysfs / 'partition').exists():
        raise ExpandError('USB target is a partition, not a whole removable disk')
    try:
        sectors = int((sysfs / 'size').read_text().strip())
        removable = (sysfs / 'removable').read_text().strip()
    except (OSError, ValueError) as error:
        raise ExpandError(f'cannot verify USB capacity/removability: {error}') from error
    if sectors != USB_SIZE_SECTORS:
        raise ExpandError(
            f'USB size mismatch: expected {USB_SIZE_SECTORS} sectors ({USB_SIZE_BYTES} bytes), '
            f'got {sectors} sectors')
    if removable != '1':
        raise ExpandError('USB target is not marked removable')
    properties = _udev_properties(path)
    expected = {
        'DEVNAME': resolved,
        'ID_BUS': 'usb',
        'ID_TYPE': 'disk',
        'ID_VENDOR': USB_VENDOR,
        'ID_MODEL': USB_MODEL,
        'ID_SERIAL_SHORT': USB_SERIAL,
    }
    for key, value in expected.items():
        if properties.get(key) != value:
            raise ExpandError(f'USB identity mismatch for {key}: expected {value!r}')
    return {
        'by_id': path,
        'resolved_path': resolved,
        'device_name': device_name,
        'size_sectors': sectors,
        'size_bytes': sectors * SECTOR,
        'removable': True,
        'vendor': properties['ID_VENDOR'],
        'model': properties['ID_MODEL'],
        'serial': properties['ID_SERIAL_SHORT'],
        'udev_serial': properties.get('ID_SERIAL'),
        'verified': True,
        'write_gate': 'WRITE_GO=1' if write else 'read-only',
    }


def require_regular_or_gated_usb(path, env, write):
    path = str(path)
    if is_raw_block_path(path) and not path.startswith(USB_BY_ID_PREFIX):
        raise ExpandError('refusing raw block node; USB admission is /dev/disk/by-id/usb-* only')
    if is_system_disk(path):
        raise ExpandError('refusing system disk (NVMe/internal/mmc/mapper)')
    if not write:
        if path == USB_BY_ID:
            return verify_usb_device(path, write=False)
        if Path(path).exists() and is_block_device(path):
            return verify_usb_device(path, write=False)
        return
    if path == USB_BY_ID:
        if env.get('WRITE_GO') != '1':
            raise ExpandError('block-device apply requires WRITE_GO=1')
        return verify_usb_device(path, write=True)
    if Path(path).is_symlink() and not path.startswith(USB_BY_ID_PREFIX):
        raise ExpandError('refusing symlink apply target')
    if not is_block_device(path):
        mode = os.lstat(path).st_mode
        if not stat.S_ISREG(mode):
            raise ExpandError('apply target must be a regular file unless WRITE_GO names a USB disk')
        return
    if env.get('WRITE_GO') != '1':
        raise ExpandError('block-device apply requires WRITE_GO=1')
    if not path.startswith(USB_BY_ID_PREFIX):
        raise ExpandError('WRITE_GO apply is limited to /dev/disk/by-id/usb-*')
    if is_system_disk(path):
        raise ExpandError('WRITE_GO target resolved to a denied system disk')
    return verify_usb_device(path, write=True)


def _sha256_stream(stream, length=None, offset=0):
    if offset:
        stream.seek(offset)
    digest = hashlib.sha256()
    remaining = length
    while remaining is None or remaining:
        chunk_size = 8 << 20 if remaining is None else min(8 << 20, remaining)
        chunk = stream.read(chunk_size)
        if not chunk:
            if remaining:
                raise ExpandError('short read while hashing media')
            break
        digest.update(chunk)
        if remaining is not None:
            remaining -= len(chunk)
    return digest.hexdigest()


def sha256_path(path):
    with Path(path).open('rb') as stream:
        return _sha256_stream(stream)


def sha256_region(path, length, offset=0):
    with Path(path).open('rb') as stream:
        return _sha256_stream(stream, length=length, offset=offset)


def _partition_device(device, number):
    device = str(device)
    return device + (f'p{number}' if re.search(r'\d$', device) else str(number))


def _mounted_target_sources(device):
    resolved = os.path.realpath(device)
    candidates = {resolved}
    candidates.update(_partition_device(resolved, number) for number in range(1, 5))
    try:
        mountinfo = Path('/proc/self/mountinfo').read_text()
    except OSError as error:
        raise ExpandError(f'cannot inspect mounted filesystems: {error}') from error
    mounted = []
    for line in mountinfo.splitlines():
        if ' - ' not in line:
            continue
        post_mount = line.split(' - ', 1)[1].split()
        if len(post_mount) < 2 or not post_mount[1].startswith('/dev/'):
            continue
        source = os.path.realpath(post_mount[1])
        if source in candidates:
            mounted.append(source)
    return mounted


def require_unmounted(device):
    mounted = _mounted_target_sources(device)
    if mounted:
        raise ExpandError('USB disk or one of its partitions is mounted: ' + ','.join(sorted(set(mounted))))


def _find_tool(name):
    direct = shutil.which(name)
    if direct:
        return [direct]
    busybox = shutil.which('busybox')
    if busybox:
        try:
            applets = subprocess.run([busybox, '--list'], check=False,
                                     stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                     text=True, timeout=10)
        except (OSError, subprocess.TimeoutExpired):
            return None
        if applets.returncode == 0 and name in applets.stdout.split():
            return [busybox, name]
    return None


def _run_tool(command, timeout, failure):
    try:
        result = subprocess.run(command, check=False, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ExpandError(f'{failure}: {error}') from error
    if result.returncode != 0:
        raise ExpandError(f'{failure} (exit {result.returncode})')
    return result


def rescan_partition_table(device):
    command = _find_tool('partprobe')
    if command is None:
        raise ExpandError('partprobe is required to expose the new p3 partition')
    _run_tool(command + [str(device)], 30, 'partition-table rescan failed')
    settle = _find_tool('udevadm')
    if settle and settle[-1] == 'udevadm':
        _run_tool(settle + ['settle', '--timeout=10'], 15, 'udev partition settle failed')


def require_partition_device(device, number, sector_count):
    partition = _partition_device(device, number)
    deadline = time.monotonic() + PARTITION_RESCAN_TIMEOUT
    while time.monotonic() < deadline:
        if Path(partition).exists() and is_block_device(partition):
            sysfs = Path('/sys/class/block') / Path(partition).name
            try:
                actual_sectors = int((sysfs / 'size').read_text().strip())
            except (OSError, ValueError):
                actual_sectors = None
            if actual_sectors == sector_count:
                return partition
        time.sleep(0.1)
    raise ExpandError(f'kernel did not expose p{number} with the expected size')


def format_ext4(partition):
    command = _find_tool('mkfs.ext4')
    if command is None:
        command = _find_tool('mke2fs')
        if command is None:
            raise ExpandError('mkfs.ext4 or mke2fs is required for physical p3 apply')
        command += ['-t', 'ext4']
    command += ['-F', '-m', '0', '-L', EXT4_LABEL, str(partition)]
    _run_tool(command, 900, 'ext4 format failed')


def read_ext4_superblock(partition):
    with Path(partition).open('rb') as stream:
        stream.seek(1024)
        superblock = stream.read(1024)
    if len(superblock) != 1024:
        raise ExpandError('p3 ext4 superblock is truncated')
    magic, = struct.unpack_from('<H', superblock, 0x38)
    if magic != 0xef53:
        raise ExpandError('p3 does not contain an ext filesystem')
    block_count_lo, = struct.unpack_from('<I', superblock, 0x04)
    block_count_hi, = struct.unpack_from('<I', superblock, 0x150)
    log_block_size, = struct.unpack_from('<I', superblock, 0x18)
    block_size = 1024 << log_block_size
    label = superblock[0x78:0x88].split(b'\0', 1)[0].decode('ascii', 'replace')
    uuid = superblock[0x68:0x78].hex()
    incompat, = struct.unpack_from('<I', superblock, 0x60)
    return {
        'type': 'ext4',
        'label': label,
        'uuid': uuid,
        'block_size': block_size,
        'block_count': block_count_lo | (block_count_hi << 32),
        'incompat_features': hex(incompat),
        'superblock_sha256': hashlib.sha256(superblock).hexdigest(),
    }


def require_expanded_table(mbr, layout, card_bytes):
    parts = [part for part in mbr['partitions'] if part['sector_count']]
    if len(parts) != 3:
        raise ExpandError('expanded appliance media must have exactly three populated MBR entries')
    expected = expected_image_partitions(layout)
    plan = plan_extra_partition(layout, card_bytes)
    expected.append(plan['partition_3'])
    if mbr['disk_id'] != layout.disk_id:
        raise ExpandError('disk identifier differs from locked appliance media')
    for actual, want in zip(parts, expected):
        if (actual['start_sector'] != want['start_sector'] or actual['sector_count'] != want['sector_count']
                or actual['type'] != want['type'] or actual['active'] != want['active']):
            raise ExpandError('expanded MBR does not match de10-nano-appliance-1g-v1 plus p3')
    return plan


def write_base_image(path, layout, base_image, expected_sha256=None, env=None):
    """Write and read-back the fixed appliance image on the admitted USB only."""

    env = os.environ if env is None else env
    target = str(path)
    target_info = require_regular_or_gated_usb(target, env, write=True)
    require_unmounted(target_info['resolved_path'])
    base = Path(base_image)
    try:
        base_stat = base.lstat()
    except OSError as error:
        raise ExpandError(f'base image is unavailable: {error}') from error
    if not stat.S_ISREG(base_stat.st_mode):
        raise ExpandError('base image must be a regular file')
    image_bytes = layout.total_sectors * SECTOR
    if base_stat.st_size != image_bytes:
        raise ExpandError(f'base image size must be exactly {image_bytes} bytes')
    base_mbr = read_mbr(base)
    require_appliance_table(base_mbr, layout)
    source_sha256 = sha256_path(base)
    if expected_sha256 is not None and source_sha256 != expected_sha256.lower():
        raise ExpandError('base image SHA-256 does not match --base-sha256')

    # Reconfirm identity immediately before opening the raw target. Compare
    # the open fd's device number with the by-id path to catch a relink race.
    target_info = verify_usb_device(target, write=True)
    require_unmounted(target_info['resolved_path'])
    try:
        fd = os.open(target, os.O_WRONLY)
    except OSError as error:
        raise ExpandError(f'cannot open admitted USB for base write: {error}') from error
    try:
        if os.fstat(fd).st_rdev != os.stat(target).st_rdev:
            raise ExpandError('USB by-id changed while opening the target')
        with base.open('rb') as source:
            remaining = image_bytes
            while remaining:
                chunk = source.read(min(8 << 20, remaining))
                if not chunk:
                    raise ExpandError('base image was truncated during write')
                view = memoryview(chunk)
                while view:
                    written = os.write(fd, view)
                    if written <= 0:
                        raise ExpandError('USB base write made no progress')
                    view = view[written:]
                remaining -= len(chunk)
        os.fsync(fd)
    finally:
        os.close(fd)
    target_info = verify_usb_device(target, write=True)
    target_sha256 = sha256_region(target, image_bytes)
    if target_sha256 != source_sha256:
        raise ExpandError('read-back SHA-256 differs from the appliance base image')
    require_appliance_table(read_mbr(target), layout)
    return {
        'applied': 'base-image',
        'path': target,
        'target': target_info,
        'image_bytes': image_bytes,
        'source_sha256': source_sha256,
        'readback_sha256': target_sha256,
        'verified': True,
    }


def apply_physical_extra_partition(path, layout, env=None):
    """Add and format p3 on the already-written, exact spare USB card."""

    env = os.environ if env is None else env
    target = str(path)
    target_info = require_regular_or_gated_usb(target, env, write=True)
    resolved = target_info['resolved_path']
    require_unmounted(resolved)
    before = read_mbr(target)
    require_appliance_table(before, layout)
    plan = plan_extra_partition(layout, target_info['size_bytes'])
    a2_offset = layout.partition_2.start_sector * SECTOR
    a2_bytes = layout.partition_2.sector_count * SECTOR
    a2_sha256_before = sha256_region(target, a2_bytes, a2_offset)

    # The identity check is repeated after all read-only validation and just
    # before the first metadata write.
    target_info = verify_usb_device(target, write=True)
    require_unmounted(resolved)
    write_mbr(target, layout.disk_id, expected_image_partitions(layout) + [plan['partition_3']])
    require_expanded_table(read_mbr(target), layout, target_info['size_bytes'])
    rescan_partition_table(resolved)
    partition = require_partition_device(resolved, 3, plan['partition_3']['sector_count'])
    require_unmounted(resolved)
    format_ext4(partition)
    os.sync()
    target_info = verify_usb_device(target, write=False)
    if target_info['resolved_path'] != resolved:
        raise ExpandError('USB by-id changed after p3 format')
    ext4 = read_ext4_superblock(partition)
    after = read_mbr(target)
    require_expanded_table(after, layout, target_info['size_bytes'])
    a2_sha256_after = sha256_region(target, a2_bytes, a2_offset)
    if a2_sha256_before != a2_sha256_after:
        raise ExpandError('A2 boot partition changed during p3 apply')
    return {
        'applied': 'physical-extra-partition',
        'mode': 'extra-partition',
        'path': target,
        'target': target_info,
        'plan': plan,
        'partition_device': partition,
        'ext4': ext4,
        'mbr_sha256': hashlib.sha256(after['raw']).hexdigest(),
        'a2_sha256': a2_sha256_after,
        'verified': True,
        'live_dev4_untouched': True,
    }


def verify_expanded_media(path, layout, env=None):
    env = os.environ if env is None else env
    target = str(path)
    target_info = require_regular_or_gated_usb(target, env, write=False)
    plan = require_expanded_table(read_mbr(target), layout, target_info['size_bytes'])
    partition = require_partition_device(target_info['resolved_path'], 3,
                                         plan['partition_3']['sector_count'])
    ext4 = read_ext4_superblock(partition)
    return {
        'verified': True,
        'path': target,
        'target': target_info,
        'plan': plan,
        'partition_device': partition,
        'ext4': ext4,
        'mbr_sha256': hashlib.sha256(read_mbr(target)['raw']).hexdigest(),
        'live_dev4_untouched': True,
    }


def apply_plan(path, layout, mode, card_bytes=None, env=None):
    env = os.environ if env is None else env
    if str(path) == USB_BY_ID:
        if mode != 'extra-partition':
            raise ExpandError('physical apply only supports the recommended extra-partition mode')
        target_info = require_regular_or_gated_usb(path, env, write=True)
        if card_bytes is not None and card_bytes != target_info['size_bytes']:
            raise ExpandError('physical --card-bytes does not match the verified USB capacity')
        return apply_physical_extra_partition(path, layout, env=env)
    require_regular_or_gated_usb(path, env, write=True)
    card_bytes = SYNTHETIC_CARD_BYTES if card_bytes is None else card_bytes
    size = Path(path).stat().st_size
    if size < card_bytes:
        raise ExpandError('image is smaller than the modeled card; synthesize a sparse card first')
    mbr = read_mbr(path)
    require_appliance_table(mbr, layout)
    if mode == 'extra-partition':
        plan = plan_extra_partition(layout, card_bytes)
        parts = expected_image_partitions(layout)
        parts.append(plan['partition_3'])
        write_mbr(path, layout.disk_id, parts)
        return {'applied': 'mbr-only', 'mode': mode, 'path': str(path), 'plan': plan}
    if mode != 'grow-fat':
        raise ExpandError('unknown expand mode')
    plan = plan_grow_fat(layout, card_bytes)
    a2_offset = layout.partition_2.start_sector * SECTOR
    a2_size = layout.partition_2.sector_count * SECTOR
    dest = plan['moved_a2']['start_sector'] * SECTOR
    with Path(path).open('r+b') as stream:
        stream.seek(0, os.SEEK_END)
        if stream.tell() < dest + a2_size:
            stream.truncate(dest + a2_size)
        stream.seek(a2_offset)
        payload = stream.read(a2_size)
        if len(payload) != a2_size:
            raise ExpandError('A2 partition is truncated; refusing grow-FAT apply')
        stream.seek(dest)
        stream.write(payload)
        stream.flush()
        os.fsync(stream.fileno())
    write_mbr(path, layout.disk_id, [plan['grown_fat'], plan['moved_a2']])
    return {
        'applied': 'mbr-and-a2-copy',
        'mode': mode,
        'path': str(path),
        'filesystem_grown': False,
        'plan': plan,
        'note': 'FAT filesystem was not resized; cluster-size rewrite is out of scope for this dry-run.',
    }


def synthesize_image(path, layout, card_bytes=SYNTHETIC_CARD_BYTES, a2_marker=b'A2-SPL-UBOOT'):
    path = Path(path)
    if path.exists() or path.is_symlink():
        raise ExpandError('synthetic image output already exists')
    image_bytes = layout.total_sectors * SECTOR
    with path.open('xb') as stream:
        stream.truncate(max(image_bytes, card_bytes))
        stream.seek(layout.partition_2.start_sector * SECTOR)
        marker = (a2_marker + b'\0' * SECTOR)[:SECTOR]
        stream.write(marker)
        stream.flush()
        os.fsync(stream.fileno())
    write_mbr(path, layout.disk_id, expected_image_partitions(layout))
    return {'path': str(path), 'size': path.stat().st_size, 'layout_id': LAYOUT_ID}


def default_lock_path():
    return Path(__file__).resolve().parents[1] / 'boot-media.lock.toml'


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('inspect', 'plan', 'synthesize', 'write-base', 'apply', 'verify'))
    parser.add_argument('--image', type=Path,
                        help='regular dry-run file or the exact admitted USB by-id')
    parser.add_argument('--output', type=Path, help='new synthetic sparse image path')
    parser.add_argument('--base-image', type=Path,
                        help='fixed de10-nano-appliance-1g-v1 card.img for write-base/apply')
    parser.add_argument('--base-sha256', help='optional SHA-256 required for the base image')
    parser.add_argument('--lock', type=Path, default=default_lock_path())
    parser.add_argument('--mode', choices=('extra-partition', 'grow-fat'), default='extra-partition')
    parser.add_argument('--card-bytes', type=int, default=None,
                        help='regular-file model size; physical apply derives the verified USB size')
    args = parser.parse_args(argv)
    try:
        layout = load_layout(args.lock)
        modeled_card_bytes = SYNTHETIC_CARD_BYTES if args.card_bytes is None else args.card_bytes
        if args.command == 'synthesize':
            if args.output is None:
                raise ExpandError('--output is required for synthesize')
            result = synthesize_image(args.output, layout, modeled_card_bytes)
        elif args.command == 'inspect':
            if args.image is None:
                raise ExpandError('--image is required for inspect')
            target_info = require_regular_or_gated_usb(args.image, os.environ, write=False)
            card_bytes = modeled_card_bytes if target_info is None else target_info['size_bytes']
            result = inspect_image(args.image, layout, card_bytes)
            if target_info is not None:
                result['target'] = target_info
        elif args.command == 'plan':
            if args.mode == 'grow-fat':
                result = plan_grow_fat(layout, modeled_card_bytes)
            else:
                result = plan_extra_partition(layout, modeled_card_bytes)
        elif args.command == 'write-base':
            if args.image is None or args.base_image is None:
                raise ExpandError('--image and --base-image are required for write-base')
            result = write_base_image(args.image, layout, args.base_image,
                                      expected_sha256=args.base_sha256)
        elif args.command == 'verify':
            if args.image is None:
                raise ExpandError('--image is required for verify')
            result = verify_expanded_media(args.image, layout)
        else:
            if args.image is None:
                raise ExpandError('--image is required for apply')
            if args.base_sha256 is not None and args.base_image is None:
                raise ExpandError('--base-sha256 requires --base-image')
            base_result = None
            if args.base_image is not None:
                base_result = write_base_image(args.image, layout, args.base_image,
                                               expected_sha256=args.base_sha256)
            expanded = apply_plan(args.image, layout, args.mode, args.card_bytes)
            result = expanded if base_result is None else {'base': base_result, 'expand': expanded}
        print(json.dumps(result, sort_keys=True, indent=2))
    except (OSError, ExpandError, ValueError, TypeError) as error:
        parser.exit(1, f'appliance expand dry-run: {error}\n')


if __name__ == '__main__':
    main()
