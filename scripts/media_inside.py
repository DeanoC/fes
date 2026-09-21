#!/usr/bin/env python3
"""Unprivileged, deterministic native media assembly and independent verification."""
import argparse
from dataclasses import asdict, dataclass
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import re
import stat
import struct
import subprocess
import tempfile
import tomllib

try:
    from .media_inputs import MediaLock, digest, verify_file
except ImportError:
    from media_inputs import MediaLock, digest, verify_file

SOURCE_DATE_EPOCH = 1751459412
SECTOR_SIZE = 512
PART1_OFFSET = 2048 * SECTOR_SIZE
PART1_SIZE = 524288 * SECTOR_SIZE
BOOT_OFFSET = 526336 * SECTOR_SIZE
BOOT_SIZE = 2048 * SECTOR_SIZE
DISK_SIZE = 528384 * SECTOR_SIZE
# Entire 512-byte dosfstools 4.2 boot sector for this locked geometry/identity.
# This also binds the OEM string, flags, boot code/message, and reserved bytes.
FAT_BOOT_SHA256 = "48aecc331306e46ace816aab028fa9640682da537ab1feafe7ce3d2fc14f8507"
ENV = {**os.environ, "TZ": "UTC", "SOURCE_DATE_EPOCH": str(SOURCE_DATE_EPOCH), "LC_ALL": "C",
       "MTOOLSRC": "/dev/null"}


@dataclass(frozen=True)
class Provenance:
    fes_revision: str
    profile: str
    media_recipe_sha256: str
    image_receipt_sha256: str
    child_manifest_sha256: str
    idle_repository: str
    idle_revision: str
    idle_path: str
    idle_size: int
    idle_sha256: str
    splash_repository: str | None = None
    splash_revision: str | None = None
    splash_path: str | None = None
    splash_size: int | None = None
    splash_sha256: str | None = None

    def __post_init__(self):
        # Transitional same-bytes pin: omitted splash identity copies idle.
        if self.splash_repository is None:
            object.__setattr__(self, 'splash_repository', self.idle_repository)
        if self.splash_revision is None:
            object.__setattr__(self, 'splash_revision', self.idle_revision)
        if self.splash_path is None:
            object.__setattr__(self, 'splash_path', self.idle_path)
        if self.splash_size is None:
            object.__setattr__(self, 'splash_size', self.idle_size)
        if self.splash_sha256 is None:
            object.__setattr__(self, 'splash_sha256', self.idle_sha256)
        for name in ('fes_revision', 'idle_revision', 'splash_revision'):
            if not isinstance(getattr(self, name), str) or not re.fullmatch('[0-9a-f]{40}', getattr(self, name)):
                raise ValueError('invalid media provenance revision')
        for name in ('media_recipe_sha256', 'image_receipt_sha256', 'child_manifest_sha256',
                     'idle_sha256', 'splash_sha256'):
            if not isinstance(getattr(self, name), str) or not re.fullmatch('[0-9a-f]{64}', getattr(self, name)):
                raise ValueError('invalid media provenance digest')
        if (self.profile != 'native-integration-dev' or self.idle_path != 'menu.rbf'
                or self.splash_path != 'menu.rbf'
                or not isinstance(self.idle_repository, str) or not self.idle_repository.startswith('https://')
                or not isinstance(self.splash_repository, str) or not self.splash_repository.startswith('https://')
                or type(self.idle_size) is not int or self.idle_size <= 0
                or type(self.splash_size) is not int or self.splash_size <= 0):
            raise ValueError('invalid media provenance profile or splash/idle source')

    @classmethod
    def load(cls, path):
        try:
            def unique(pairs):
                result = {}
                for key, value in pairs:
                    if key in result:
                        raise ValueError('duplicate media provenance field')
                    result[key] = value
                return result
            data = json.loads(Path(path).read_text(), object_pairs_hook=unique)
            return cls(**data)
        except (OSError, TypeError, ValueError):
            raise ValueError('invalid closed media provenance input') from None


@dataclass(frozen=True)
class ImageInputs:
    rootfs: Path
    idle: Path
    kernel: Path
    uboot: Path
    agent_config: Path | None = None
    provenance: Provenance | None = None
    launcher_config: Path | None = None
    launcher_config_sha256: str | None = None
    splash: Path | None = None

    def splash_payload(self):
        """FAT / U-Boot splash bytes. Defaults to idle for the same-bytes pin."""
        return self.splash if self.splash is not None else self.idle


@dataclass(frozen=True)
class Verification:
    partition_types: tuple[int, int]
    fat_type: str
    paths: tuple[str, ...]
    image_sha256: str


def run(*args):
    result = subprocess.run([str(arg) for arg in args], env=ENV, capture_output=True, text=True)
    if result.returncode:
        raise ValueError(f"media tool {args[0]} failed: {result.stderr.strip() or result.stdout.strip()}")
    return result.stdout


def regular(path, label):
    path = Path(path)
    if not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError(f"{label} must be a regular file")
    return path


def check_inputs(inputs, lock, scratch):
    for name in ("rootfs", "idle", "kernel", "uboot"):
        regular(getattr(inputs, name), name)
    regular(inputs.splash_payload(), "splash")
    verify_file(inputs.kernel, lock.kernel.size, lock.kernel.sha256, "kernel")
    verify_file(inputs.uboot, lock.uboot.size, lock.uboot.sha256, "U-Boot")
    if lock.uboot.size > BOOT_SIZE:
        raise ValueError("boot payload exceeds partition")
    uboot_data = inputs.uboot.read_bytes()
    if any(value.encode() not in uboot_data for value in lock.environment):
        raise ValueError("U-Boot environment differs from lock")
    # debugfs command uses only a generated scratch filename, never caller text.
    extracted = scratch / "embedded-idle.rbf"
    run("debugfs", "-R", f'dump /usr/share/mister-runtime/idle.rbf "{extracted}"', inputs.rootfs)
    if not extracted.is_file() or digest(extracted) != digest(inputs.idle):
        raise ValueError("idle payload differs from embedded rootfs idle.rbf")
    if inputs.launcher_config is not None:
        regular(inputs.launcher_config, "launcher config")
        if inputs.launcher_config.stat().st_size > 65536 or digest(inputs.launcher_config) != inputs.launcher_config_sha256:
            raise ValueError("launcher config hash or size differs")
    if inputs.agent_config is not None:
        regular(inputs.agent_config, "agent config")
        if inputs.agent_config.stat().st_size > 65536:
            raise ValueError("agent config exceeds 64 KiB")


def write_region(image, source, offset):
    with image.open("r+b", buffering=0) as target, source.open("rb") as payload:
        target.seek(offset)
        shutil.copyfileobj(payload, target, length=1024 * 1024)


def copy_region(image, output, offset, length):
    with image.open("rb") as source, output.open("wb") as target:
        source.seek(offset)
        while length:
            block = source.read(min(length, 1024 * 1024))
            if not block:
                raise ValueError("truncated image region")
            target.write(block)
            length -= len(block)


def require_zero(stream, start, length, label):
    stream.seek(start)
    while length:
        block = stream.read(min(1024 * 1024, length))
        if not block or any(block):
            raise ValueError(f"nonzero or truncated {label}")
        length -= len(block)


def _assemble_once(image, inputs, lock, scratch):
    # Callers validate their layout; the original media lock retains its exact
    # geometry. Appliance media derives a larger FAT with identical boot bytes.
    part1_offset = lock.partition_1.start_sector * lock.sector_size
    part1_size = lock.partition_1.sector_count * lock.sector_size
    boot_offset = lock.partition_2.start_sector * lock.sector_size
    with image.open("xb") as stream:
        stream.truncate(lock.total_sectors * lock.sector_size)
        mbr = bytearray(512)
        struct.pack_into("<I", mbr, 440, lock.disk_id)
        for offset, partition in ((446, lock.partition_1), (462, lock.partition_2)):
            struct.pack_into("<B3sB3sII", mbr, offset, 0x80 if partition.active else 0,
                             bytes.fromhex(partition.chs_start), partition.type,
                             bytes.fromhex(partition.chs_end), partition.start_sector, partition.sector_count)
        mbr[510:512] = b"\x55\xaa"
        stream.seek(0)
        stream.write(mbr)
    run("mkfs.fat", "--invariant", "-F", "32", "-S", str(lock.sector_size), "-s", "1", "-g", "1/1", "-h", str(lock.partition_1.start_sector),
        "-i", f"{lock.fat_serial:08x}", "-n", lock.fat_label,
        f"--offset={lock.partition_1.start_sector}", image, str(part1_size // 1024))
    device = f"{image}@@{part1_offset}"
    # mkfs invariant mode uses its own label date. Normalize that one record;
    # mtools honors SOURCE_DATE_EPOCH for every subsequently created entry.
    stamp = datetime.datetime.fromtimestamp(SOURCE_DATE_EPOCH, datetime.timezone.utc)
    date = ((stamp.year - 1980) << 9) | (stamp.month << 5) | stamp.day
    time = (stamp.hour << 11) | (stamp.minute << 5) | (stamp.second // 2)
    with image.open("r+b") as stream:
        stream.seek(part1_offset)
        bpb = stream.read(512)
        fat_sectors = struct.unpack_from("<I", bpb, 36)[0]
        label_offset = part1_offset + (32 + 2 * fat_sectors) * 512
        stream.seek(label_offset + 13)
        stream.write(struct.pack("<BHHH", 0, time, date, date))
        stream.seek(label_offset + 22)
        stream.write(struct.pack("<HH", time, date))
    staged = scratch / "payloads"
    staged.mkdir()
    for name, source in (("menu.rbf", inputs.splash_payload()), ("zImage_dtb", inputs.kernel), ("linux.img", inputs.rootfs)):
        destination = staged / name
        shutil.copyfile(source, destination)
        os.utime(destination, (SOURCE_DATE_EPOCH, SOURCE_DATE_EPOCH))
    run("mcopy", "-m", "-i", device, staged / "menu.rbf", "::/menu.rbf")
    run("mmd", "-i", device, "::/linux")
    run("mcopy", "-m", "-i", device, staged / "zImage_dtb", "::/linux/zImage_dtb")
    run("mcopy", "-m", "-i", device, staged / "linux.img", "::/linux/linux.img")
    run("mmd", "-i", device, "::/fogcast")
    if inputs.agent_config is not None:
        config = staged / "agent.toml"
        shutil.copyfile(inputs.agent_config, config)
        os.chmod(config, 0o600)
        os.utime(config, (SOURCE_DATE_EPOCH, SOURCE_DATE_EPOCH))
        run("mcopy", "-m", "-i", device, config, "::/fogcast/agent.toml")
    if inputs.launcher_config is not None:
        config = staged / "launcher.json"
        shutil.copyfile(inputs.launcher_config, config)
        os.chmod(config, 0o600)
        os.utime(config, (SOURCE_DATE_EPOCH, SOURCE_DATE_EPOCH))
        run("mcopy", "-m", "-i", device, config, "::/fogcast/launcher.json")
    write_region(image, inputs.uboot, boot_offset)


def manifest_data(image, inputs, lock, config_sha, assembly_hashes, *, rootfs_verified=False):
    provenance = inputs.provenance
    if not isinstance(provenance, Provenance):
        raise ValueError('complete media provenance input is required')
    verify_file(inputs.idle, provenance.idle_size, provenance.idle_sha256, 'idle provenance')
    verify_file(inputs.splash_payload(), provenance.splash_size, provenance.splash_sha256, 'splash provenance')
    if (type(assembly_hashes) is not list or len(assembly_hashes) != 2
            or any(type(value) is not str or not re.fullmatch('[0-9a-f]{64}', value) for value in assembly_hashes)):
        raise ValueError('invalid independent assembly hashes')
    if config_sha is not None and (type(config_sha) is not str or not re.fullmatch('[0-9a-f]{64}', config_sha)):
        raise ValueError('invalid agent config hash')
    result = {'format': 1, 'target': 'de10-nano', 'layout': lock.layout,
              'source_date_epoch': SOURCE_DATE_EPOCH, 'provisioned': config_sha is not None,
              'hardware': 'not-run',
              'fes': {'revision': provenance.fes_revision, 'profile': provenance.profile,
                      'media_recipe_sha256': provenance.media_recipe_sha256},
              'rootfs': {'path': 'out/native-integration-dev/linux.img', 'destination': '/linux/linux.img',
                         'size': inputs.rootfs.stat().st_size, 'sha256': digest(inputs.rootfs),
                         'image_receipt_sha256': provenance.image_receipt_sha256,
                         'child_manifest_sha256': provenance.child_manifest_sha256},
              'splash': {'repository': provenance.splash_repository, 'revision': provenance.splash_revision,
                         'path': provenance.splash_path, 'size': provenance.splash_size,
                         'sha256': provenance.splash_sha256, 'fat_destination': '/menu.rbf'},
              'idle': {'repository': provenance.idle_repository, 'revision': provenance.idle_revision,
                       'path': provenance.idle_path, 'size': provenance.idle_size, 'sha256': provenance.idle_sha256,
                       'rootfs_destination': '/usr/share/mister-runtime/idle.rbf'},
              'disk': {'sector_size': lock.sector_size, 'size': DISK_SIZE, 'identifier': lock.disk_id},
              'fat': {'label': lock.fat_label, 'serial': lock.fat_serial},
              'partition_1': asdict(lock.partition_1), 'partition_2': asdict(lock.partition_2),
              'output': {'path': 'fes.img', 'size': Path(image).stat().st_size, 'sha256': digest(image)},
              'assembly': {'sha256': list(assembly_hashes)},
              'checks': {'structural_media': 'pass', 'assembly_reproducibility': 'pass',
                         'rootfs_structural': 'pass' if rootfs_verified else 'not-run',
                         'rootfs_qemu': 'pass' if rootfs_verified else 'not-run'}}
    if config_sha is not None:
        result['agent_config_sha256'] = config_sha
    launcher_sha = inputs.launcher_config_sha256
    if launcher_sha is not None:
        if not config_sha or not re.fullmatch('[0-9a-f]{64}', launcher_sha):
            raise ValueError('invalid launcher config hash')
        result['launcher_config_sha256'] = launcher_sha
    for name, destination in (('kernel', '/linux/zImage_dtb'), ('uboot', 'partition_2')):
        source = getattr(lock, name)
        result[name] = {'repository': lock.repository, 'revision': lock.commit, 'path': source.path,
                        'size': source.size, 'sha256': source.sha256, 'destination': destination}
    return result


def write_manifest(path, data):
    # The schema uses scalar keys plus one level of tables; JSON scalars/arrays
    # are valid TOML values and provide deterministic quoting and booleans.
    lines = [f'{key} = {json.dumps(value)}\n' for key, value in sorted(data.items()) if not isinstance(value, dict)]
    for name, table in sorted(data.items()):
        if isinstance(table, dict):
            lines.append(f'\n[{name}]\n')
            lines.extend(f'{key} = {json.dumps(value)}\n' for key, value in sorted(table.items()))
    Path(path).write_text(''.join(lines))


def load_manifest(path):
    try:
        return tomllib.loads(regular(path, 'manifest').read_text())
    except (OSError, tomllib.TOMLDecodeError) as error:
        raise ValueError('invalid media manifest') from error


def same_schema(actual, expected):
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, dict):
        return set(actual) == set(expected) and all(same_schema(actual[key], value) for key, value in expected.items())
    if isinstance(expected, list):
        return len(actual) == len(expected) and all(same_schema(left, right) for left, right in zip(actual, expected))
    return True


def assemble(output, inputs, lock):
    """Produce fes.img/fes-media.toml only after two independent images match."""
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    image, manifest = output / "fes.img", output / "fes-media.toml"
    if image.exists() or manifest.exists():
        raise ValueError("media output already exists")
    with tempfile.TemporaryDirectory(prefix=".assembly-", dir=output) as temporary:
        scratch = Path(temporary)
        check_inputs(inputs, lock, scratch)
        images = []
        for name in ("first", "second"):
            directory = scratch / name
            directory.mkdir()
            candidate = directory / "fes.img"
            _assemble_once(candidate, inputs, lock, directory)
            images.append(candidate)
        assembly_hashes = [digest(candidate) for candidate in images]
        if assembly_hashes[0] != assembly_hashes[1]:
            raise ValueError("independent media assembly hashes differ")
        config_sha = digest(inputs.agent_config) if inputs.agent_config else None
        data = manifest_data(images[0], inputs, lock, config_sha, assembly_hashes)
        candidate_manifest = scratch / "fes-media.toml"
        write_manifest(candidate_manifest, data)
        verify_image(images[0], candidate_manifest, inputs, lock, config_sha)
        os.replace(images[0], image)
        os.replace(candidate_manifest, manifest)
    return image, manifest


def _verify_mbr(image, lock):
    with image.open("rb") as stream:
        mbr = stream.read(512)
        if mbr[:440] != bytes(440) or mbr[444:446] != bytes(2) or mbr[478:510] != bytes(32):
            raise ValueError("MBR reserved bytes differ")
        if struct.unpack_from("<I", mbr, 440)[0] != lock.disk_id or mbr[510:] != b"\x55\xaa":
            raise ValueError("MBR identifier or signature differs")
        for offset, expected in ((446, (128, b"\xfe\xff\xff", 12, b"\xfe\xff\xff", 2048, 524288)),
                                 (462, (0, b"\xfe\xff\xff", 162, b"\xfe\xff\xff", 526336, 2048))):
            if struct.unpack_from("<B3sB3sII", mbr, offset) != expected:
                raise ValueError("MBR partition differs")
        require_zero(stream, 512, PART1_OFFSET - 512, "disk padding")
        require_zero(stream, BOOT_OFFSET + lock.uboot.size, BOOT_SIZE - lock.uboot.size, "boot partition tail")


def _verify_fat_unused(fat, fat_sectors, clusters, has_config, has_launcher=False):
    """Walk allocation metadata independently and require zero slack/free space."""
    stamp = datetime.datetime.fromtimestamp(SOURCE_DATE_EPOCH, datetime.timezone.utc)
    date = ((stamp.year - 1980) << 9) | (stamp.month << 5) | stamp.day
    time = (stamp.hour << 11) | (stamp.minute << 5) | (stamp.second // 2)
    data_offset = (32 + 2 * fat_sectors) * 512
    with fat.open("rb") as stream:
        for start, count in ((2, 4), (8, 24)):
            require_zero(stream, start * 512, count * 512, "reserved FAT padding")
        for sector in (1, 7):
            require_zero(stream, sector * 512 + 4, 480, "FAT FSInfo padding")
            require_zero(stream, sector * 512 + 496, 14, "FAT FSInfo padding")
        for sector in (0, 6):
            require_zero(stream, sector * 512 + 52, 12, "FAT BPB padding")
            require_zero(stream, sector * 512 + 219, 291, "FAT boot code padding")
        stream.seek(32 * 512)
        table = stream.read(fat_sectors * 512)
        stream.seek((32 + fat_sectors) * 512)
        if stream.read(len(table)) != table:
            raise ValueError("FAT allocation copies differ")
        entries = struct.unpack(f"<{len(table) // 4}I", table)
        if any(value & 0xf0000000 for value in entries):
            raise ValueError("reserved FAT allocation bits are nonzero")
        if entries[:2] != (0x0ffffff8, 0x0fffffff):
            raise ValueError("reserved FAT allocation entries differ")
        if any(entries[clusters + 2:]):
            raise ValueError("unused FAT table padding is nonzero")
        allocated_clusters = [index for index in range(2, clusters + 2) if entries[index]]
        # mtools updates only the primary FSInfo. The backup retains mkfs's
        # initial root-only free count/hint; both complete records are canonical.
        for sector, free_count, hint in ((1, clusters - len(allocated_clusters), max(allocated_clusters)),
                                         (7, clusters - 1, 2)):
            expected_info = bytearray(512)
            struct.pack_into("<I", expected_info, 0, 0x41615252)
            struct.pack_into("<III", expected_info, 484, 0x61417272, free_count, hint)
            struct.pack_into("<I", expected_info, 508, 0xaa550000)
            stream.seek(sector * 512)
            if stream.read(512) != expected_info:
                raise ValueError("FAT FSInfo metadata differs from canonical allocation")

        def chain(first):
            visited = set()
            while first < 0x0ffffff8:
                if first < 2 or first >= clusters + 2 or first in visited:
                    raise ValueError("invalid FAT allocation chain")
                visited.add(first)
                yield first
                first = entries[first]

        def position(cluster):
            return data_offset + (cluster - 2) * 512

        free_start = None
        for cluster in range(2, clusters + 3):
            free = cluster < clusters + 2 and entries[cluster] == 0
            if free and free_start is None:
                free_start = cluster
            elif not free and free_start is not None:
                require_zero(stream, position(free_start), (cluster - free_start) * 512, "unused FAT data")
                free_start = None
        data_end = data_offset + clusters * 512
        require_zero(stream, data_end, PART1_SIZE - data_end, "unused FAT end padding")
        # Exact short names, attribute/case bytes, and the one canonical VFAT
        # record. Raw enumeration cannot hide entries through mtools filtering.
        dot = (b".          \x10\x00", ".")
        dotdot = (b"..         \x10\x00", "..")
        schemas = {
            "/": [(b"FESDATA    \x08\x00", "label"),
                  (b"MENU    RBF\x20\x18", "/menu.rbf"),
                  (b"LINUX      \x10\x08", "/linux"),
                  (b"FOGCAST    \x10\x08", "/fogcast")],
            "/linux": [dot, dotdot,
                       (bytes.fromhex("417a0049006d00610067000f008465005f0064007400620000000000ffffffff"), "lfn"),
                       (b"ZIMAGE~1   \x20\x00", "/linux/zImage_dtb"),
                       (b"LINUX   IMG\x20\x18", "/linux/linux.img")],
            "/fogcast": [dot, dotdot],
        }
        # agent.toml has a four-character extension, so mtools uses a VFAT alias.
        if has_config:
            schemas["/fogcast"] = [dot, dotdot,
                (bytes.fromhex("416100670065006e0074000f00322e0074006f006d006c0000000000ffffffff"), "lfn"),
                (b"AGENT~1 TOM\x20\x00", "/fogcast/agent.toml")]
        if has_launcher:
            schemas["/fogcast"] += [(bytes.fromhex("416c00610075006e0063000f00ec6800650072002e006a00730000006f006e00"), "lfn"),
                (b"LAUNCH~1JSO\x20\x00", "/fogcast/launcher.json")]
        directories, seen = [(2, "/", 0)], set()
        while directories:
            first, path, parent = directories.pop()
            if first in seen:
                raise ValueError("duplicate FAT directory allocation")
            seen.add(first)
            allocated = list(chain(first))
            if len(allocated) != 1:
                raise ValueError("FAT directory allocation differs from canonical paths")
            stream.seek(position(first))
            block = stream.read(512)
            schema = schemas[path]
            if any(block[len(schema) * 32:]):
                raise ValueError("extra FAT directory paths or nonzero directory padding")
            for index, (prefix, name) in enumerate(schema):
                entry = block[index * 32:(index + 1) * 32]
                if not entry.startswith(prefix):
                    raise ValueError("FAT directory names/types/attributes differ from canonical paths")
                if name == "lfn":
                    continue
                if (entry[13] != 0 or struct.unpack_from("<HHH", entry, 14) != (time, date, date)
                        or struct.unpack_from("<HH", entry, 22) != (time, date)):
                    raise ValueError("FAT timestamp differs from normalized epoch")
                first_cluster = (struct.unpack_from("<H", entry, 20)[0] << 16) | struct.unpack_from("<H", entry, 26)[0]
                size = struct.unpack_from("<I", entry, 28)[0]
                if entry[11] == 0x10:
                    if size:
                        raise ValueError("FAT directory size is nonzero")
                    if name in (".", ".."):
                        if first_cluster != (first if name == "." else parent):
                            raise ValueError("FAT directory link differs")
                    else:
                        directories.append((first_cluster, name, 0 if path == "/" else first))
                elif entry[11] == 0x08:
                    if first_cluster or size:
                        raise ValueError("FAT volume label allocation differs")
                else:
                    allocated = list(chain(first_cluster)) if first_cluster else []
                    if len(allocated) != (size + 511) // 512:
                        raise ValueError("FAT payload allocation size differs")
                    if size % 512:
                        require_zero(stream, position(allocated[-1]) + size % 512, 512 - size % 512,
                                     "FAT payload slack")


def _verify_fat(image, lock, scratch, has_config, has_launcher=False):
    fat = scratch / "fat.img"
    copy_region(image, fat, PART1_OFFSET, PART1_SIZE)
    with fat.open("rb") as stream:
        bpb = stream.read(512)
        if hashlib.sha256(bpb).hexdigest() != FAT_BOOT_SHA256:
            raise ValueError("FAT boot metadata differs from canonical pinned formatter output")
        sector_size = struct.unpack_from("<H", bpb, 11)[0]
        sectors_per_cluster = bpb[13]
        reserved = struct.unpack_from("<H", bpb, 14)[0]
        fats = bpb[16]
        total = struct.unpack_from("<I", bpb, 32)[0]
        fat_sectors = struct.unpack_from("<I", bpb, 36)[0]
        clusters = ((total - reserved - fats * fat_sectors) // sectors_per_cluster) if sectors_per_cluster else 0
        if (sector_size != 512 or total != 524288 or sectors_per_cluster != 1 or fats != 2
                or reserved != 32 or clusters < 65525 or bpb[17:19] != bytes(2)
                or bpb[22:24] != bytes(2) or bpb[82:90] != b"FAT32   "
                or struct.unpack_from("<I", bpb, 28)[0] != 2048
                or struct.unpack_from("<I", bpb, 67)[0] != lock.fat_serial
                or bpb[71:82] != lock.fat_label.encode().ljust(11, b" ") or bpb[510:] != b"\x55\xaa"):
            raise ValueError("FAT32 metadata differs from policy")
        stream.seek(6 * 512)
        if stream.read(512) != bpb:
            raise ValueError("FAT backup boot metadata differs")
    run("fsck.fat", "-vn", fat)
    _verify_fat_unused(fat, fat_sectors, clusters, has_config, has_launcher)
    return fat


def verify_image(image, manifest, inputs, lock, agent_config_sha256=None, *, rootfs_verified=False):
    """Verify disk structures and extracted bytes independently of assembly."""
    image = regular(image, "image")
    if image.stat().st_size != DISK_SIZE:
        raise ValueError("media image size differs from layout")
    data = load_manifest(manifest)
    try:
        hashes = data['assembly']['sha256']
    except (KeyError, TypeError):
        raise ValueError('media manifest assembly evidence is missing') from None
    expected = manifest_data(image, inputs, lock, agent_config_sha256, hashes, rootfs_verified=rootfs_verified)
    if not same_schema(data, expected):
        raise ValueError('media manifest fields differ from closed schema')
    if data.get('agent_config_sha256') != expected.get('agent_config_sha256'):
        raise ValueError('agent config hash differs from manifest')
    _verify_mbr(image, lock)
    with tempfile.TemporaryDirectory(prefix="fes-media-verify-") as temporary:
        scratch = Path(temporary)
        check_inputs(inputs, lock, scratch)
        _verify_fat(image, lock, scratch, bool(agent_config_sha256), bool(inputs.launcher_config_sha256))
        device = f"{image}@@{PART1_OFFSET}"
        listing = run("mdir", "-a", "-b", "-s", "-i", device, "::/")
        paths = tuple(sorted(line.removeprefix("::").rstrip("/") for line in listing.splitlines() if line))
        owned = {"/menu.rbf": inputs.splash_payload(), "/linux/zImage_dtb": inputs.kernel,
                 "/linux/linux.img": inputs.rootfs}
        expected_paths = set(owned) | {"/linux", "/fogcast"}
        if agent_config_sha256:
            expected_paths.add("/fogcast/agent.toml")
        if inputs.launcher_config_sha256:
            expected_paths.add("/fogcast/launcher.json")
        if set(paths) != expected_paths or len(paths) != len(expected_paths):
            raise ValueError("FAT owned paths differ from policy")
        for index, (name, source) in enumerate(sorted(owned.items())):
            extracted = scratch / f"payload-{index}"
            run("mcopy", "-i", device, "::" + name, extracted)
            if extracted.stat().st_size != source.stat().st_size or digest(extracted) != digest(source):
                raise ValueError(f"FAT payload differs: {name}")
        if agent_config_sha256:
            extracted = scratch / "agent.toml"
            run("mcopy", "-i", device, "::/fogcast/agent.toml", extracted)
            if extracted.stat().st_size > 65536 or digest(extracted) != agent_config_sha256:
                raise ValueError("agent config payload differs")
        if inputs.launcher_config_sha256:
            extracted = scratch / "launcher.json"
            run("mcopy", "-i", device, "::/fogcast/launcher.json", extracted)
            if extracted.stat().st_size > 65536 or digest(extracted) != inputs.launcher_config_sha256:
                raise ValueError("launcher config payload differs")
        boot = scratch / "boot.img"
        copy_region(image, boot, BOOT_OFFSET, lock.uboot.size)
        if digest(boot) != lock.uboot.sha256:
            raise ValueError("boot payload differs from lock")
    if data != expected or any(value != expected['output']['sha256'] for value in hashes):
        raise ValueError('media manifest provenance, checks, payload or assembly hash differs')
    return Verification((0x0c, 0xa2), "FAT32", paths, expected['output']['sha256'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name in ("assemble", "verify"):
        command = subparsers.add_parser(name)
        command.add_argument("--lock", type=Path, default=Path("/work/boot-media.lock.toml"))
        command.add_argument("--provenance", type=Path, required=True)
        for payload in ("rootfs", "idle", "kernel", "uboot"):
            command.add_argument("--" + payload, type=Path, required=True)
        command.add_argument("--splash", type=Path)
        command.add_argument("--launcher-config", type=Path)
        command.add_argument("--launcher-config-sha256")
        if name == "assemble":
            command.add_argument("--output", type=Path, required=True)
            command.add_argument("--agent-config", type=Path)
        else:
            command.add_argument("--image", type=Path, required=True)
            command.add_argument("--manifest", type=Path, required=True)
            command.add_argument("--agent-config-sha256")
            command.add_argument("--rootfs-verified", action="store_true")
    args = parser.parse_args()
    try:
        lock = MediaLock.load(args.lock)
        inputs = ImageInputs(args.rootfs, args.idle, args.kernel, args.uboot, getattr(args, "agent_config", None), Provenance.load(args.provenance), args.launcher_config, args.launcher_config_sha256, args.splash)
        if args.command == "assemble":
            image, manifest = assemble(args.output, inputs, lock)
            print(json.dumps({"image_sha256": digest(image), "assembly_sha256": load_manifest(manifest)["assembly"]["sha256"]}, sort_keys=True))
        else:
            result = verify_image(args.image, args.manifest, inputs, lock, args.agent_config_sha256, rootfs_verified=args.rootfs_verified)
            print(json.dumps({"fat_type": result.fat_type, "image_sha256": result.image_sha256, "paths": result.paths}, sort_keys=True))
    except (ValueError, OSError) as error:
        parser.exit(1, f"media: {error}\n")


if __name__ == "__main__":
    main()
