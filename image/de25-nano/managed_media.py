"""DE25 regular-file appliance disk construction inside the shared media tools.

Callers establish release/factory provenance before this low-level constructor,
as for the DE10 constructor. Structural verification is not native acceptance.
"""
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from scripts import appliance, appliance_inside
from scripts import de25_owner_image as owner
from scripts import media_inside as media
boot, contract = owner.boot, owner.contract

LAYOUT_ID = 'de25-nano-appliance-2g-v1'
FAT_BYTES = 2 << 30
BOOTSTRAP_BYTES = appliance_inside.SIZE
SECTOR = 512
FAT_START = 2048
FAT_SERIAL = 0xde250001
FAT_LABEL = 'FESDATA'
# Pinned dosfstools 4.2 rounds a 255/63 filesystem down to whole tracks.
# Keep the 2 GiB partition and require its exact 16-sector zero tail.
FAT_FS_SECTORS = (FAT_BYTES // SECTOR) // 63 * 63
FAT_FS_BYTES = FAT_FS_SECTORS * SECTOR


@dataclass(frozen=True)
class Inputs:
    factory: Path
    bootstrap: Path
    manifest: Path
    idle: Path
    kernel: Path  # Full authenticated owner-kernel directory, not a DE10 zImage.
    vendor_boot: Path
    agent_config: Path | None = None
    launcher_config: Path | None = None


def appliance_layout(vendor):
    # Keep the vendor's MMC partition numbers and types. Its locked U-Boot
    # resolves root=/dev/mmcblk0p2 from the MBR; QSPI is an external dependency.
    if (vendor['board'] != 'de25_nano' or vendor['sector_size'] != SECTOR or
            vendor['root_device'] != '/dev/mmcblk0p2' or vendor['console'] != 'ttyS0,115200' or
            vendor['partitions'] != boot.load_lock()['partitions']):
        raise ValueError('unsupported DE25 source boot geometry')
    second = FAT_START + FAT_BYTES // SECTOR
    return {'sector_size': SECTOR, 'total_sectors': second + BOOTSTRAP_BYTES // SECTOR,
            'fat_filesystem_sectors': FAT_FS_SECTORS, 'fat_cluster_sectors': 8,
            'partition_1': {'type': 0x0b, 'start_sector': FAT_START, 'sectors': FAT_BYTES // SECTOR},
            'partition_2': {'type': 0x83, 'start_sector': second, 'sectors': BOOTSTRAP_BYTES // SECTOR}}


def check_capacity(factory_size, boot_payload_size):
    if (type(factory_size) is not int or factory_size <= 0 or
            type(boot_payload_size) is not int or boot_payload_size <= 0):
        raise ValueError('invalid DE25 payload capacity')
    # Factory, Good, Previous and one staged candidate must coexist on FAT.
    if 4 * factory_size + boot_payload_size + (16 << 20) > FAT_FS_BYTES:
        raise ValueError('DE25 FAT leaves insufficient update and rollback capacity')


def chs(lba):
    cylinder, remainder = divmod(lba, 255 * 63)
    head, sector = divmod(remainder, 63)
    if cylinder > 1023:
        return b'\xfe\xff\xff'
    return bytes((head, ((cylinder >> 2) & 0xc0) | (sector + 1), cylinder & 0xff))


def mbr_bytes(prefix, vendor, layout):
    with prefix.open('rb') as stream:
        original = stream.read(SECTOR)
    boot.validate_mbr(original, vendor)
    mbr = bytearray(original)
    for offset, name in ((446, 'partition_1'), (462, 'partition_2')):
        part = layout[name]
        # Preserve active/type/boot-code/disk-ID bytes; derive only geometry.
        mbr[offset + 1:offset + 4] = chs(part['start_sector'])
        mbr[offset + 5:offset + 8] = chs(part['start_sector'] + part['sectors'] - 1)
        struct.pack_into('<II', mbr, offset + 8, part['start_sector'], part['sectors'])
    return bytes(mbr)


def debugfs_cat(image, name):
    return owner.build.debugfs(image, 'cat ' + name)


def check_inputs(inputs, vendor, scratch):
    layout = appliance_layout(vendor)
    boot._verify_prepared_boot(inputs.vendor_boot, vendor)
    kernel = owner.selected_kernel(inputs.kernel)
    manifest = appliance.load_manifest(inputs.manifest, board='de25-nano')
    boot.check_payload(inputs.factory, {'size': manifest['image_size'], 'sha256': manifest['image_sha256']})
    if manifest['kernel_sha256'] != kernel['files']['Image']['sha256']:
        raise ValueError('DE25 factory and boot kernel differ')
    appliance.validate_ext4(inputs.factory)
    appliance.validate_ext4(inputs.bootstrap)
    if inputs.bootstrap.stat().st_size != BOOTSTRAP_BYTES:
        raise ValueError('DE25 bootstrap size differs from shared bootstrap recipe')
    for image in (inputs.factory, inputs.bootstrap):
        if debugfs_cat(image, '/etc/fes-board') != b'de25-nano\n':
            raise ValueError('DE25 filesystem board identity differs')
    embedded = json.loads(debugfs_cat(inputs.bootstrap, '/etc/fes/factory.json'), object_pairs_hook=appliance.unique)
    if embedded != manifest:
        raise ValueError('DE25 bootstrap factory binding differs')
    binary = scratch / 'fes-boot'
    media.run('debugfs', '-R', f'dump /sbin/init "{binary}"', inputs.bootstrap)
    appliance.validate_static_arm(binary, 'aarch64')
    software = json.loads(debugfs_cat(inputs.factory, '/etc/fes/software-base.json'), object_pairs_hook=appliance.unique)
    if software.get('board') != 'de25-nano' or software.get('architecture') != 'aarch64':
        raise ValueError('DE25 factory software base identity differs')
    if 'Type: directory' not in media.run('debugfs', '-R', 'stat /.fes-bootstrap', inputs.factory):
        raise ValueError('DE25 factory lacks bootstrap mount directory')
    boot.regular_file(inputs.idle)
    if hashlib.sha256(debugfs_cat(inputs.factory, '/usr/share/mister-runtime/idle.rbf')).hexdigest() != boot.digest(inputs.idle):
        raise ValueError('DE25 idle differs from the factory rootfs')
    if inputs.launcher_config and not inputs.agent_config:
        raise ValueError('launcher provisioning requires an agent configuration')
    for config in (inputs.agent_config, inputs.launcher_config):
        if config is not None and boot.regular_file(config).stat().st_size > 65536:
            raise ValueError('DE25 private configuration exceeds 64 KiB')
    _, report = owner.prepare_kernel_boot(inputs.vendor_boot, inputs.kernel, scratch,
                                          media.SOURCE_DATE_EPOCH, vendor=vendor)
    if report['root_device'] != '/dev/mmcblk0p2':
        raise ValueError('DE25 boot script changed the root device')
    owned = owned_files(inputs, manifest)
    check_capacity(manifest['image_size'], sum(p.stat().st_size for p in owned.values() if p != inputs.factory))
    return layout, manifest, report


def owned_files(inputs, manifest):
    identity = manifest['image_sha256']
    owned = {'/u-boot.itb': inputs.vendor_boot / 'u-boot.itb',
             '/boot.scr.uimg': inputs.vendor_boot / 'boot.scr.uimg',
             '/Image': inputs.kernel / 'Image',
             '/socfpga_agilex5_de25_nano.dtb': inputs.kernel / 'de25-owner.dtb',
             '/idle.rbf': inputs.idle,
             f'/fogcast/releases/images/{identity}.img': inputs.factory,
             f'/fogcast/releases/manifests/{identity}.json': inputs.manifest}
    if inputs.agent_config:
        owned['/fogcast/agent.toml'] = inputs.agent_config
    if inputs.launcher_config:
        owned['/fogcast/launcher.json'] = inputs.launcher_config
    return owned


def verify(image, inputs, vendor):
    image = boot.regular_file(image)
    with tempfile.TemporaryDirectory(prefix='de25-appliance-inspect-') as temporary:
        scratch = Path(temporary)
        layout, manifest, report = check_inputs(inputs, vendor, scratch)
        if image.stat().st_size != layout['total_sectors'] * SECTOR:
            raise ValueError('DE25 appliance disk length differs')
        prefix = inputs.vendor_boot / 'vendor-boot-prefix.bin'
        with image.open('rb') as actual, prefix.open('rb') as original:
            if actual.read(SECTOR) != mbr_bytes(prefix, vendor, layout):
                raise ValueError('DE25 appliance MBR differs')
            original.seek(SECTOR)
            count = FAT_START * SECTOR - SECTOR
            if boot.range_digest(actual, count) != boot.range_digest(original, count):
                raise ValueError('DE25 reserved boot bytes differ')
        offset = layout['partition_2']['start_sector'] * SECTOR
        with image.open('rb') as stream:
            stream.seek(offset)
            if boot.range_digest(stream, BOOTSTRAP_BYTES) != boot.digest(inputs.bootstrap):
                raise ValueError('DE25 partition-2 bootstrap differs')
            media.require_zero(stream, FAT_START * SECTOR + FAT_FS_BYTES,
                               FAT_BYTES - FAT_FS_BYTES, 'DE25 FAT track padding')
        fat = scratch / 'fat.img'
        media.copy_region(image, fat, FAT_START * SECTOR, FAT_BYTES)
        media.run('fsck.fat', '-vn', fat)
        with fat.open('rb') as stream:
            bpb = stream.read(SECTOR)
        if (struct.unpack_from('<H', bpb, 11)[0] != SECTOR or bpb[13] != 8 or
                struct.unpack_from('<I', bpb, 28)[0] != FAT_START or
                struct.unpack_from('<I', bpb, 32)[0] != FAT_FS_SECTORS or
                bpb[82:90] != b'FAT32   ' or struct.unpack_from('<I', bpb, 67)[0] != FAT_SERIAL or
                bpb[71:82] != FAT_LABEL.encode().ljust(11)):
            raise ValueError('DE25 FAT geometry or identity differs')
        device = str(image) + '@@' + str(FAT_START * SECTOR)
        listing = media.run('mdir', '-a', '-b', '-s', '-i', device, '::/')
        paths = sorted(line.removeprefix('::').rstrip('/') for line in listing.splitlines() if line)
        owned = owned_files(inputs, manifest)
        expected = set(owned) | {'/fogcast', '/fogcast/releases', '/fogcast/releases/images', '/fogcast/releases/manifests'}
        if set(paths) != expected or len(paths) != len(expected):
            raise ValueError('DE25 FAT owned paths differ')
        for index, (name, source) in enumerate(sorted(owned.items())):
            extracted = scratch / ('payload-' + str(index))
            media.run('mcopy', '-i', device, '::' + name, extracted)
            if source.stat().st_size != extracted.stat().st_size or boot.digest(source) != boot.digest(extracted):
                raise ValueError('DE25 FAT payload differs: ' + name)
        return {'image_sha256': boot.digest(image), 'image_size': image.stat().st_size,
                'layout_id': LAYOUT_ID, 'geometry': layout, 'paths': paths,
                'factory_sha256': manifest['image_sha256'], 'bootstrap_sha256': boot.digest(inputs.bootstrap),
                'kernel_sha256': manifest['kernel_sha256'], 'boot_contract': report,
                'uboot_itb_sha256': boot.digest(inputs.vendor_boot / 'u-boot.itb'),
                'boot_script_sha256': boot.digest(inputs.vendor_boot / 'boot.scr.uimg'),
                'agent_config_sha256': boot.digest(inputs.agent_config) if inputs.agent_config else None,
                'launcher_config_sha256': boot.digest(inputs.launcher_config) if inputs.launcher_config else None,
                'structural': 'pass', 'hardware': 'not-run'}


def assemble(output, inputs, vendor):
    output = Path(output)
    if output.exists() or output.is_symlink():
        raise ValueError('DE25 appliance output already exists')
    output.parent.mkdir(parents=True, exist_ok=True)
    mask = os.umask(0o077)
    try:
        with tempfile.TemporaryDirectory(prefix='.de25-appliance-', dir=output.parent) as temporary:
            scratch = Path(temporary)
            layout, manifest, _ = check_inputs(inputs, vendor, scratch)
            with output.open('xb') as target, (inputs.vendor_boot / 'vendor-boot-prefix.bin').open('rb') as prefix:
                boot.sparse_copy(prefix, target, FAT_START * SECTOR)
                target.truncate(layout['total_sectors'] * SECTOR)
                target.seek(0)
                target.write(mbr_bytes(inputs.vendor_boot / 'vendor-boot-prefix.bin', vendor, layout))
            media.run('mkfs.fat', '--invariant', '-F', '32', '-S', str(SECTOR), '-s', '8', '-g', '255/63',
                      '-h', str(FAT_START), '-i', f'{FAT_SERIAL:08x}', '-n', FAT_LABEL,
                      f'--offset={FAT_START}', output, str(FAT_BYTES // 1024))
            media.normalize_fat_label(output, FAT_START * SECTOR)
            device = str(output) + '@@' + str(FAT_START * SECTOR)
            for directory in ('fogcast', 'fogcast/releases', 'fogcast/releases/images', 'fogcast/releases/manifests'):
                media.run('mmd', '-i', device, '::/' + directory)
            for index, (name, source) in enumerate(sorted(owned_files(inputs, manifest).items())):
                staged = scratch / ('staged-' + str(index))
                shutil.copyfile(source, staged)
                os.utime(staged, (media.SOURCE_DATE_EPOCH, media.SOURCE_DATE_EPOCH))
                media.run('mcopy', '-m', '-i', device, staged, '::' + name)
            media.write_region(output, inputs.bootstrap, layout['partition_2']['start_sector'] * SECTOR)
            return verify(output, inputs, vendor)
    except BaseException:
        output.unlink(missing_ok=True)
        raise
    finally:
        os.umask(mask)
