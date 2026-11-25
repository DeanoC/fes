"""Pure-Python 360 KiB FAT12 floppy builder and lossless PNG/PPM image helpers."""
from __future__ import annotations

import hashlib
from pathlib import Path
import struct
import zlib

SIZE = 40 * 2 * 9 * 512  # 368,640 bytes
SECTOR_BYTES = 512
CLUSTER_SECTORS = 2
CLUSTER_BYTES = SECTOR_BYTES * CLUSTER_SECTORS
FAT1_OFFSET = 1 * SECTOR_BYTES
FAT2_OFFSET = 3 * SECTOR_BYTES
FAT_BYTES = 2 * SECTOR_BYTES
ROOT_OFFSET = 5 * SECTOR_BYTES
ROOT_BYTES = 7 * SECTOR_BYTES
ROOT_ENTRIES = ROOT_BYTES // 32  # 112
DATA_START_SECTOR = 12
TOTAL_CLUSTERS = (720 - DATA_START_SECTOR) // CLUSTER_SECTORS  # 354 (clusters 2..355)
MAX_CLUSTER = 2 + TOTAL_CLUSTERS  # 356 (exclusive)

BASE_SHA256 = '140a2d0a507f7452790e73416dd4dab2c66a89e09a71e26b2ca8f95d1f3c5e5c'
DOS_TIMESTAMP = (2026 - 1980) << 9 | (10 << 5) | 26  # 2026-10-26
DOS_TIME = 12 << 11  # 12:00:00


def checked_base(root: Path) -> bytes:
    """Read and verify the checked-in FreeDOS 1.4 360 KiB base floppy."""
    base_path = Path(root) / 'cores/fes-pcxt/firmware/freedos-1.4-xt-360.img'
    data = base_path.read_bytes()
    if len(data) != SIZE or hashlib.sha256(data).hexdigest() != BASE_SHA256:
        raise ValueError('FreeDOS base floppy differs from its locked digest')
    return data


def format_83(name: str) -> bytes:
    """Encode an 8.3 DOS filename component into an 11-byte uppercase FAT name."""
    if not name or name in ('.', '..'):
        raise ValueError(f'invalid FAT 8.3 name: {name!r}')
    if '.' in name:
        stem, ext = name.split('.', 1)
        if '.' in ext:
            raise ValueError(f'multiple dots in FAT 8.3 name: {name!r}')
    else:
        stem, ext = name, ''
    stem = stem.upper()
    ext = ext.upper()
    if not stem or len(stem) > 8 or len(ext) > 3:
        raise ValueError(f'FAT 8.3 name exceeds 8.3 limits: {name!r}')
    bad = set(' "/\\[]:;|=,<>+*?')
    if any(c in bad or ord(c) < 32 or ord(c) >= 127 for c in stem + ext):
        raise ValueError(f'illegal character in FAT 8.3 name: {name!r}')
    return f'{stem:<8}{ext:<3}'.encode('ascii')


def _get_fat12(img: bytearray, cluster: int) -> int:
    off = FAT1_OFFSET + cluster + cluster // 2
    pair = int.from_bytes(img[off:off + 2], 'little')
    return (pair >> 4) if (cluster & 1) else (pair & 0xfff)


def _set_fat12(img: bytearray, cluster: int, val: int) -> None:
    off = FAT1_OFFSET + cluster + cluster // 2
    pair = int.from_bytes(img[off:off + 2], 'little')
    if cluster & 1:
        pair = (pair & 0x000f) | ((val & 0xfff) << 4)
    else:
        pair = (pair & 0xf000) | (val & 0xfff)
    img[off:off + 2] = pair.to_bytes(2, 'little')
    img[FAT2_OFFSET + (off - FAT1_OFFSET):FAT2_OFFSET + (off - FAT1_OFFSET) + 2] = pair.to_bytes(2, 'little')


def _cluster_offset(cluster: int) -> int:
    return (DATA_START_SECTOR + (cluster - 2) * CLUSTER_SECTORS) * SECTOR_BYTES


def _free_chain(img: bytearray, first: int) -> None:
    cluster = first
    seen = set()
    while 2 <= cluster < MAX_CLUSTER:
        if cluster in seen:
            raise ValueError('FAT12 loop detected while freeing chain')
        seen.add(cluster)
        nxt = _get_fat12(img, cluster)
        _set_fat12(img, cluster, 0)
        cluster = nxt


def _alloc_clusters(img: bytearray, count: int) -> list[int]:
    if count == 0:
        return []
    allocated: list[int] = []
    for cluster in range(2, MAX_CLUSTER):
        if _get_fat12(img, cluster) == 0:
            allocated.append(cluster)
            if len(allocated) == count:
                break
    if len(allocated) < count:
        raise ValueError(f'360 KiB floppy out of space (need {count} clusters, found {len(allocated)})')
    for idx, cluster in enumerate(allocated):
        _set_fat12(img, cluster, allocated[idx + 1] if idx + 1 < count else 0xfff)
    return allocated


def _write_chain_data(img: bytearray, clusters: list[int], payload: bytes) -> None:
    for idx, cluster in enumerate(clusters):
        off = _cluster_offset(cluster)
        chunk = payload[idx * CLUSTER_BYTES:(idx + 1) * CLUSTER_BYTES]
        img[off:off + CLUSTER_BYTES] = chunk.ljust(CLUSTER_BYTES, b'\x00')


def _read_dir_entries(img: bytearray, cluster: int | None) -> list[tuple[int, bytes]]:
    if cluster is None:
        return [(ROOT_OFFSET + i * 32, bytes(img[ROOT_OFFSET + i * 32:ROOT_OFFSET + (i + 1) * 32]))
                for i in range(ROOT_ENTRIES)]
    entries = []
    cur = cluster
    seen = set()
    while 2 <= cur < MAX_CLUSTER:
        if cur in seen:
            raise ValueError('FAT12 loop in directory chain')
        seen.add(cur)
        base = _cluster_offset(cur)
        for i in range(CLUSTER_BYTES // 32):
            off = base + i * 32
            entries.append((off, bytes(img[off:off + 32])))
        cur = _get_fat12(img, cur)
    return entries


def _make_dir_entry(raw_name: bytes, attr: int, first_cluster: int, size: int) -> bytes:
    entry = bytearray(32)
    entry[0:11] = raw_name
    entry[11] = attr
    entry[22:24] = DOS_TIME.to_bytes(2, 'little')
    entry[24:26] = DOS_TIMESTAMP.to_bytes(2, 'little')
    entry[26:28] = first_cluster.to_bytes(2, 'little')
    entry[28:32] = size.to_bytes(4, 'little')
    return bytes(entry)


def _find_or_alloc_slot(img: bytearray, dir_cluster: int | None, raw_name: bytes) -> tuple[int, bytes | None]:
    entries = _read_dir_entries(img, dir_cluster)
    free_slot = None
    for off, raw in entries:
        if raw[0] == 0x00 or raw[0] == 0xe5:
            if free_slot is None:
                free_slot = off
            if raw[0] == 0x00:
                break
        elif not (raw[11] & 0x08) and raw[0:11] == raw_name:
            return off, raw
    if free_slot is not None:
        return free_slot, None
    if dir_cluster is None:
        raise ValueError('FAT12 root directory is full (112 entries)')
    # Extend subdirectory by one cluster.
    new_cluster = _alloc_clusters(img, 1)[0]
    cur = dir_cluster
    while True:
        nxt = _get_fat12(img, cur)
        if nxt >= 0xff8:
            _set_fat12(img, cur, new_cluster)
            break
        cur = nxt
    base = _cluster_offset(new_cluster)
    img[base:base + CLUSTER_BYTES] = bytes(CLUSTER_BYTES)
    return base, None


def _ensure_subdir(img: bytearray, parent_cluster: int | None, dir_name: str) -> int:
    raw_name = format_83(dir_name)
    off, existing = _find_or_alloc_slot(img, parent_cluster, raw_name)
    if existing is not None:
        if not (existing[11] & 0x10):
            raise ValueError(f'path component {dir_name!r} exists as a file')
        return int.from_bytes(existing[26:28], 'little')
    cluster = _alloc_clusters(img, 1)[0]
    base = _cluster_offset(cluster)
    img[base:base + CLUSTER_BYTES] = bytes(CLUSTER_BYTES)
    dot = _make_dir_entry(b'.          ', 0x10, cluster, 0)
    dotdot = _make_dir_entry(b'..         ', 0x10, parent_cluster or 0, 0)
    img[base:base + 32] = dot
    img[base + 32:base + 64] = dotdot
    img[off:off + 32] = _make_dir_entry(raw_name, 0x10, cluster, 0)
    return cluster


def inject_files(base_image: bytes, files: dict[str, bytes]) -> bytes:
    """Return a modified 360 KiB FAT12 floppy with the given files written/replaced."""
    if len(base_image) != SIZE:
        raise ValueError('base image must be 368,640 bytes')
    img = bytearray(base_image)
    for rel_path, payload in sorted(files.items()):
        parts = [p for p in rel_path.replace('\\', '/').strip('/').split('/') if p]
        if not parts:
            raise ValueError(f'empty target path: {rel_path!r}')
        dir_cluster: int | None = None
        for folder in parts[:-1]:
            dir_cluster = _ensure_subdir(img, dir_cluster, folder)
        raw_name = format_83(parts[-1])
        off, existing = _find_or_alloc_slot(img, dir_cluster, raw_name)
        if existing is not None:
            if existing[11] & 0x10:
                raise ValueError(f'cannot overwrite directory with file: {rel_path!r}')
            old_first = int.from_bytes(existing[26:28], 'little')
            if old_first >= 2:
                _free_chain(img, old_first)
        n_clusters = (len(payload) + CLUSTER_BYTES - 1) // CLUSTER_BYTES
        clusters = _alloc_clusters(img, n_clusters)
        _write_chain_data(img, clusters, payload)
        first = clusters[0] if clusters else 0
        img[off:off + 32] = _make_dir_entry(raw_name, 0x20, first, len(payload))
    return bytes(img)


def read_file(image: bytes, rel_path: str) -> bytes:
    """Read a file from a 360 KiB FAT12 image for verification."""
    img = bytearray(image)
    parts = [p for p in rel_path.replace('\\', '/').strip('/').split('/') if p]
    if not parts:
        raise ValueError('empty path')
    dir_cluster: int | None = None
    for idx, part in enumerate(parts):
        raw_name = format_83(part)
        found = None
        for _, raw in _read_dir_entries(img, dir_cluster):
            if raw[0] == 0x00:
                break
            if raw[0] == 0xe5 or (raw[11] & 0x08):
                continue
            if raw[0:11] == raw_name:
                found = raw
                break
        if found is None:
            raise FileNotFoundError(rel_path)
        is_dir = bool(found[11] & 0x10)
        first = int.from_bytes(found[26:28], 'little')
        size = int.from_bytes(found[28:32], 'little')
        if idx + 1 < len(parts):
            if not is_dir:
                raise NotADirectoryError(part)
            dir_cluster = first
        else:
            if is_dir:
                raise IsADirectoryError(rel_path)
            if size == 0:
                return b''
            out = bytearray()
            cur = first
            seen = set()
            while len(out) < size:
                if cur < 2 or cur >= MAX_CLUSTER or cur in seen:
                    raise ValueError('invalid FAT12 cluster chain')
                seen.add(cur)
                off = _cluster_offset(cur)
                out.extend(img[off:off + CLUSTER_BYTES])
                cur = _get_fat12(img, cur)
            if cur < 0xff8:
                raise ValueError('unterminated FAT12 cluster chain')
            return bytes(out[:size])
    raise AssertionError('unreachable')


def _png_chunk(tag: bytes, payload: bytes) -> bytes:
    crc = zlib.crc32(tag + payload) & 0xffffffff
    return struct.pack('>I', len(payload)) + tag + payload + struct.pack('>I', crc)


def write_png(path: Path, width: int, height: int, rgb: bytes) -> None:
    """Write an 8-bit RGB PNG using only the Python standard library."""
    if len(rgb) != width * height * 3:
        raise ValueError('RGB buffer size does not match width * height * 3')
    stride = width * 3
    raw = b''.join(b'\x00' + rgb[y * stride:(y + 1) * stride] for y in range(height))
    ihdr = struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)
    data = b'\x89PNG\r\n\x1a\n' + _png_chunk(b'IHDR', ihdr) + _png_chunk(
        b'IDAT', zlib.compress(raw, level=6)) + _png_chunk(b'IEND', b'')
    Path(path).write_bytes(data)


def ppm_to_png(ppm_path: Path, png_path: Path) -> tuple[int, int, bytes]:
    """Read a binary P6 PPM and save an equivalent lossless PNG."""
    raw = Path(ppm_path).read_bytes()
    if not raw.startswith(b'P6\n'):
        raise ValueError(f'not a P6 PPM file: {ppm_path}')
    lines = raw.split(b'\n', 3)
    w, h = map(int, lines[1].split())
    if lines[2] != b'255':
        raise ValueError(f'unsupported PPM maxval: {lines[2]!r}')
    rgb = lines[3]
    write_png(png_path, w, h, rgb)
    return w, h, rgb
