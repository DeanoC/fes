"""Pinned Terasic DE25-Nano boot inputs and regular-file SD image assembly."""
from __future__ import annotations

import hashlib
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import tempfile
import tomllib
import urllib.request
import zipfile

LOCK_PATH = Path(__file__).with_name("vendor.lock.toml")
CHUNK = 4 * 1024 * 1024


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return range_digest(stream, path.stat().st_size)


def range_digest(stream, size: int) -> str:
    result = hashlib.sha256()
    while size:
        data = stream.read(min(CHUNK, size))
        if not data:
            raise ValueError("truncated image input")
        result.update(data)
        size -= len(data)
    return result.hexdigest()


def regular_file(path: Path) -> Path:
    path = Path(path)
    if not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError(f"expected a regular, non-symlink file: {path}")
    return path


def check_payload(path: Path, identity: dict) -> None:
    regular_file(path)
    if path.stat().st_size != identity["size"] or digest(path) != identity["sha256"]:
        raise ValueError(f"locked payload identity differs: {path.name}")


def load_lock(path: Path = LOCK_PATH) -> dict:
    with Path(path).open("rb") as stream:
        lock = tomllib.load(stream)
    expected = {"format", "board", "sector_size", "image_size", "image_member", "image_sha256",
                "prefix_size", "prefix_sha256", "mbr_sha256", "root_device", "console",
                "bsp", "resources", "partitions", "boot_files", "factory_boot"}
    if (set(lock) != expected or type(lock["format"]) is not int
            or lock["format"] != 1 or lock["board"] != "de25_nano"):
        raise ValueError("invalid or unknown DE25-Nano vendor lock fields")
    for name in ("sector_size", "image_size", "prefix_size"):
        if type(lock[name]) is not int or lock[name] <= 0:
            raise ValueError(f"invalid vendor geometry: {name}")
    for name in ("image_sha256", "prefix_sha256", "mbr_sha256"):
        if not isinstance(lock[name], str) or not re.fullmatch("[0-9a-f]{64}", lock[name]):
            raise ValueError(f"invalid vendor hash: {name}")
    if (lock["sector_size"] != 512 or lock["root_device"] != "/dev/mmcblk0p2"
            or lock["console"] != "ttyS0,115200" or len(lock["partitions"]) != 2):
        raise ValueError("unsupported vendor boot geometry or boot arguments")
    for index, part in enumerate(lock["partitions"]):
        if set(part) != {"type", "start_sector", "sectors"}:
            raise ValueError("unknown vendor partition fields")
        if any(type(part[key]) is not int or part[key] <= 0 for key in part):
            raise ValueError("invalid vendor partition geometry")
        if part["type"] != (0x0b, 0x83)[index]:
            raise ValueError("unsupported vendor partition type")
    first, second = lock["partitions"]
    if (first["start_sector"] < 1
            or first["start_sector"] + first["sectors"] != second["start_sector"]
            or second["start_sector"] * 512 != lock["prefix_size"]
            or (second["start_sector"] + second["sectors"]) * 512 != lock["image_size"]):
        raise ValueError("inconsistent vendor partition boundaries")
    for name in ("bsp", "resources"):
        item = lock[name]
        if set(item) != {"url", "filename", "size", "sha256"}:
            raise ValueError("unknown vendor download fields")
        if (not item["url"].startswith("https://dl2.terasic.com/resources/de25-nano/")
                or Path(item["filename"]).name != item["filename"]):
            raise ValueError("invalid vendor download location")
    for group, extra in (("boot_files", {"name", "fat_name"}),
                         ("factory_boot", {"name", "member", "role", "archive"})):
        names = set()
        for item in lock[group]:
            if set(item) != extra | {"size", "sha256"} or item["name"] in names:
                raise ValueError(f"invalid {group} identity fields")
            if Path(item["name"]).name != item["name"]:
                raise ValueError("vendor artifact name must be a basename")
            names.add(item["name"])
            if group == "factory_boot" and item["archive"] not in ("bsp", "resources"):
                raise ValueError("unknown factory/reference archive")
        if not names:
            raise ValueError(f"missing vendor {group}")
    for item in (lock["bsp"], lock["resources"], *lock["boot_files"], *lock["factory_boot"]):
        if (type(item["size"]) is not int or item["size"] <= 0
                or not isinstance(item["sha256"], str)
                or not re.fullmatch("[0-9a-f]{64}", item["sha256"])):
            raise ValueError("invalid locked artifact identity")
    return lock


def fetch_vendor(cache: Path, lock: dict | None = None) -> dict[str, Path]:
    lock = load_lock() if lock is None else lock
    cache = Path(cache)
    cache.mkdir(parents=True, exist_ok=True)
    result = {}
    for name in ("bsp", "resources"):
        identity = lock[name]
        destination = cache / identity["filename"]
        if not destination.exists():
            fd, temporary = tempfile.mkstemp(prefix=identity["filename"] + ".", dir=cache)
            try:
                with os.fdopen(fd, "wb") as target, urllib.request.urlopen(identity["url"], timeout=60) as source:
                    shutil.copyfileobj(source, target, CHUNK)
                check_payload(Path(temporary), identity)
                os.replace(temporary, destination)
            finally:
                Path(temporary).unlink(missing_ok=True)
        check_payload(destination, identity)
        result[name] = destination
    return result


def validate_mbr(mbr: bytes, lock: dict) -> None:
    if len(mbr) != 512 or mbr[510:] != b"\x55\xaa" or hashlib.sha256(mbr).hexdigest() != lock["mbr_sha256"]:
        raise ValueError("vendor MBR identity differs")
    for index, part in enumerate(lock["partitions"]):
        entry = mbr[446 + index * 16:462 + index * 16]
        if (entry[4] != part["type"]
                or struct.unpack_from("<II", entry, 8) != (part["start_sector"], part["sectors"])):
            raise ValueError("vendor partition table differs")
    if mbr[478:510] != bytes(32):
        raise ValueError("unexpected vendor partition")


def sparse_copy(source, target, size: int) -> None:
    while size:
        data = source.read(min(CHUNK, size))
        if not data:
            raise ValueError("truncated image input")
        if data == bytes(len(data)):
            target.seek(len(data), os.SEEK_CUR)
        else:
            target.write(data)
        size -= len(data)


def extract_boot_files(prefix: Path, output: Path, lock: dict) -> list[dict]:
    """Read only the four locked files from the vendor FAT32 root directory."""
    partition = lock["partitions"][0]
    offset = partition["start_sector"] * 512
    with prefix.open("rb") as source:
        source.seek(offset)
        bpb = source.read(512)
        sector_size = struct.unpack_from("<H", bpb, 11)[0]
        sectors_per_cluster = bpb[13]
        reserved = struct.unpack_from("<H", bpb, 14)[0]
        fats = bpb[16]
        fat_sectors = struct.unpack_from("<I", bpb, 36)[0]
        total_sectors = struct.unpack_from("<I", bpb, 32)[0]
        root_cluster = struct.unpack_from("<I", bpb, 44)[0]
        if (sector_size != 512 or not sectors_per_cluster or sectors_per_cluster & (sectors_per_cluster - 1)
                or bpb[82:90] != b"FAT32   " or bpb[510:] != b"\x55\xaa"
                or not 0 < total_sectors <= partition["sectors"]):
            raise ValueError("unsupported vendor FAT32 geometry")
        data_start = reserved + fats * fat_sectors
        cluster_limit = (total_sectors - data_start) // sectors_per_cluster + 2
        cluster_size = sectors_per_cluster * 512
        source.seek(offset + reserved * 512)
        fat = source.read(fat_sectors * 512)

        def chain(start):
            seen = set()
            while start < 0x0ffffff8:
                if start < 2 or start >= cluster_limit or start in seen or start * 4 + 4 > len(fat):
                    raise ValueError("invalid vendor FAT cluster chain")
                seen.add(start)
                source.seek(offset + (data_start + (start - 2) * sectors_per_cluster) * 512)
                data = source.read(cluster_size)
                if len(data) != cluster_size:
                    raise ValueError("truncated vendor FAT cluster")
                yield data
                start = struct.unpack_from("<I", fat, start * 4)[0] & 0x0fffffff

        entries = {}
        for block in chain(root_cluster):
            for index in range(0, len(block), 32):
                entry = block[index:index + 32]
                if entry[0] == 0:
                    break
                if entry[0] == 0xe5 or entry[11] == 0x0f or entry[11] & 8:
                    continue
                if entry[11] & 16 or entry[:11] in entries:
                    raise ValueError("unexpected vendor FAT root entry")
                entries[entry[:11]] = entry
        if set(entries) != {item["fat_name"].encode("ascii") for item in lock["boot_files"]}:
            raise ValueError("vendor FAT root file set differs")
        result = []
        for item in lock["boot_files"]:
            entry = entries[item["fat_name"].encode("ascii")]
            if struct.unpack_from("<I", entry, 28)[0] != item["size"]:
                raise ValueError("vendor FAT file length differs")
            cluster = (struct.unpack_from("<H", entry, 20)[0] << 16) | struct.unpack_from("<H", entry, 26)[0]
            destination = output / item["name"]
            remaining = item["size"]
            with destination.open("xb") as target:
                for block in chain(cluster):
                    target.write(block[:remaining])
                    remaining -= min(remaining, len(block))
                    if remaining == 0:
                        break
            if remaining:
                raise ValueError("truncated vendor FAT file")
            check_payload(destination, item)
            result.append({"path": str(destination), "name": item["name"], "size": item["size"], "sha256": item["sha256"]})
        return result


def _prepare_boot_directory(archives: dict[str, Path], output: Path, lock: dict) -> None:
    prefix = output / "vendor-boot-prefix.bin"
    with zipfile.ZipFile(archives["bsp"]) as archive:
        members = [info for info in archive.infolist() if not info.is_dir()]
        expected_members = {lock["image_member"]} | {
            item["member"] for item in lock["factory_boot"] if item["archive"] == "bsp"}
        image_member = archive.getinfo(lock["image_member"])
        if (len(members) != len(expected_members) or {item.filename for item in members} != expected_members
                or image_member.file_size != lock["image_size"]):
            raise ValueError("vendor BSP ZIP member differs")
        with archive.open(image_member) as source, prefix.open("xb") as target:
            image_hash = hashlib.sha256()
            remaining_prefix = lock["prefix_size"]
            while data := source.read(CHUNK):
                image_hash.update(data)
                part = data[:remaining_prefix]
                if part == bytes(len(part)):
                    target.seek(len(part), os.SEEK_CUR)
                else:
                    target.write(part)
                remaining_prefix -= len(part)
            target.truncate(lock["prefix_size"])
            if remaining_prefix or image_hash.hexdigest() != lock["image_sha256"]:
                raise ValueError("vendor BSP image identity differs")
    check_payload(prefix, {"size": lock["prefix_size"], "sha256": lock["prefix_sha256"]})
    with prefix.open("rb") as source:
        validate_mbr(source.read(512), lock)
    extract_boot_files(prefix, output, lock)
    factory = output / "factory-boot"
    factory.mkdir()
    for item in lock["factory_boot"]:
        with zipfile.ZipFile(archives[item["archive"]]) as archive:
            with archive.open(item["member"]) as source, (factory / item["name"]).open("xb") as target:
                shutil.copyfileobj(source, target, CHUNK)
            check_payload(factory / item["name"], item)


def _verify_prepared_boot(output: Path, lock: dict) -> dict:
    if not stat.S_ISDIR(output.lstat().st_mode):
        raise ValueError("prepared vendor boot output is not a real directory")
    prefix = output / "vendor-boot-prefix.bin"
    factory = output / "factory-boot"
    expected = {"vendor-boot-prefix.bin", "factory-boot"}
    expected.update(item["name"] for item in lock["boot_files"])
    expected.update("factory-boot/" + item["name"] for item in lock["factory_boot"])
    actual = set()
    for path in output.rglob("*"):
        mode = path.lstat().st_mode
        if not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)):
            raise ValueError("prepared vendor boot output contains a symlink or special file")
        actual.add(path.relative_to(output).as_posix())
    if actual != expected:
        raise ValueError("prepared vendor boot output file set differs")
    check_payload(prefix, {"size": lock["prefix_size"], "sha256": lock["prefix_sha256"]})
    with prefix.open("rb") as source:
        validate_mbr(source.read(512), lock)
    boot_files = []
    for item in lock["boot_files"]:
        check_payload(output / item["name"], item)
        boot_files.append({"path": str(output / item["name"]), "name": item["name"],
                           "size": item["size"], "sha256": item["sha256"]})
    for item in lock["factory_boot"]:
        check_payload(factory / item["name"], item)
    return {"prefix": str(prefix), "prefix_sha256": lock["prefix_sha256"],
            "boot_files": boot_files, "factory_boot": [dict(item) for item in lock["factory_boot"]],
            "factory_boot_directory": str(factory), "factory_programming": "not-performed"}


def prepare_boot(cache: Path, output: Path, lock: dict | None = None) -> dict:
    lock = load_lock() if lock is None else lock
    archives = fetch_vendor(cache, lock)
    output = Path(output)
    if output.exists() or output.is_symlink():
        return _verify_prepared_boot(output, lock)
    output.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=output.name + ".", dir=output.parent))
    try:
        _prepare_boot_directory(archives, stage, lock)
        stage.rename(output)
    finally:
        if stage.exists():
            shutil.rmtree(stage)
    return _verify_prepared_boot(output, lock)


def check_rootfs(rootfs: Path, capacity: int) -> int:
    regular_file(rootfs)
    size = rootfs.stat().st_size
    if not 2048 <= size <= capacity or size % 512:
        raise ValueError("rootfs does not fit the vendor Linux partition")
    with rootfs.open("rb") as source:
        source.seek(1024)
        superblock = source.read(1024)
    if superblock[56:58] != b"\x53\xef" or not struct.unpack_from("<I", superblock, 96)[0] & 0x40:
        raise ValueError("rootfs is not an ext4 image with extents")
    return size


def assemble(prefix: Path, rootfs_ext4: Path, output_image: Path, lock: dict | None = None) -> dict:
    lock = load_lock() if lock is None else lock
    prefix, rootfs_ext4, output_image = Path(prefix), Path(rootfs_ext4), Path(output_image)
    check_payload(prefix, {"size": lock["prefix_size"], "sha256": lock["prefix_sha256"]})
    with prefix.open("rb") as source:
        validate_mbr(source.read(512), lock)
    rootfs_size = check_rootfs(rootfs_ext4, lock["partitions"][1]["sectors"] * 512)
    with output_image.open("xb") as target:
        with prefix.open("rb") as source:
            sparse_copy(source, target, lock["prefix_size"])
        target.seek(lock["prefix_size"])
        with rootfs_ext4.open("rb") as source:
            sparse_copy(source, target, rootfs_size)
        target.truncate(lock["image_size"])
    return verify_image(output_image, rootfs_ext4, lock)


def verify_image(image: Path, rootfs: Path, lock: dict | None = None) -> dict:
    lock = load_lock() if lock is None else lock
    image, rootfs = regular_file(Path(image)), Path(rootfs)
    rootfs_size = check_rootfs(rootfs, lock["partitions"][1]["sectors"] * 512)
    if image.stat().st_size != lock["image_size"]:
        raise ValueError("SD image length differs from locked vendor geometry")
    rootfs_sha256 = digest(rootfs)
    with image.open("rb") as source:
        validate_mbr(source.read(512), lock)
        source.seek(0)
        if range_digest(source, lock["prefix_size"]) != lock["prefix_sha256"]:
            raise ValueError("SD image vendor boot prefix differs")
        if range_digest(source, rootfs_size) != rootfs_sha256:
            raise ValueError("SD image rootfs differs")
        remaining = lock["image_size"] - source.tell()
        while remaining:
            data = source.read(min(CHUNK, remaining))
            if not data or data != bytes(len(data)):
                raise ValueError("SD image rootfs partition padding differs")
            remaining -= len(data)
    return {"path": str(image), "size": image.stat().st_size, "sha256": digest(image),
            "prefix_sha256": lock["prefix_sha256"], "rootfs_sha256": rootfs_sha256,
            "rootfs_size": rootfs_size, "root_device": lock["root_device"],
            "structural_media": "pass", "hardware_boot": "not-run"}
