#!/usr/bin/env python3
"""Cyclone V RBF CRAM codec matching Mistral ``rbf_load`` / ``rbf_save``.

The production device is DE10-Nano ``5CSEBA6U23I7`` (sx120f). Tests may pass a
smaller ``DieInfo``. Linking never splices compressed frames: decompress,
overlay a CRAM rectangle, rewrite CRCs, then recompress.
"""

from __future__ import annotations

from dataclasses import dataclass


TARGET_DEVICE = "5CSEBA6U23I7"
SLOT_BEL = "MISTRAL_M10K.26.1.0"
SLOT_COLUMN = 26


@dataclass(frozen=True)
class DieInfo:
    """Subset of Mistral ``CycloneV::die_info`` needed to pack CRAM frames."""

    name: str
    cram_sx: int
    cram_sy: int
    frame_size: int
    pram_sizes: tuple[int, ...]
    noedcrc_zones: tuple[int, ...]
    postamble_1: int
    postamble_2: int
    x_to_bx: tuple[int, ...] = ()
    # Tile columns that hold CRAM ECC/CRC, not routing. Diffs here follow any
    # payload change and are rewritten when the RBF is saved.
    ecc_columns: tuple[int, ...] = ()


    # Pinned Mistral b28e30a sx120f constants from libmistral/cvd-sx120f.cc.
    # Column 42 is the documented ECC strip. Columns 41/45/49 sit on
    # noedcrc_zones (3491, ~3920, 4174) and only change as ~1024-row CRC
    # companions of socket CRAM; they are not routing. A taller 16-cell
    # occupancy also flips the neighbouring CRC strips 43/47/50 on the same
    # ~1024-row cadence; those bits are rewritten when the RBF is saved.
SX120F = DieInfo(
    name="sx120f",
    cram_sx=7605,
    cram_sy=7024,
    frame_size=227,
    pram_sizes=(
        5806,
        7434,
        9669,
        8169,
        4862,
        2772,
        7162,
        9607,
        3438,
        7484,
        5751,
        1984,
        9500,
        6800,
        9136,
    )
    + (0,) * 17,
    noedcrc_zones=(318, 1121, 2099, 3059, 3491, 4174, 4940, 5862, 6530, 7605, 0, 0),
    postamble_1=190,
    postamble_2=412,
    x_to_bx=(
        0, 65, 124, 183, 259, 315, 615, 691, 750, 826,
        885, 944, 1003, 1062, 1118, 1418, 1499, 1558, 1617, 1676,
        1737, 1769, 1845, 1904, 1963, 2023, 2096, 2396, 2455, 2531,
        2590, 2649, 2710, 2742, 2806, 2882, 2941, 3000, 3056, 3356,
        3432, 3488, 3788, 3847, 3906, 3921, 3980, 4039, 4115, 4171,
        4471, 4530, 4589, 4665, 4726, 4758, 4822, 4881, 4937, 5237,
        5313, 5372, 5431, 5490, 5549, 5608, 5684, 5744, 5803, 5859,
        6159, 6218, 6277, 6353, 6412, 6471, 6527, 6827, 6891, 6967,
        7026, 7085, 7144, 7220, 7279, 7355, 7416, 7448, 7524, 7583,
    ),
    ecc_columns=(41, 42, 43, 45, 46, 47, 49, 50),
)


@dataclass(frozen=True)
class CramRect:
    """Inclusive CRAM x0/y0, exclusive x1/y1."""

    x0: int
    y0: int
    x1: int
    y1: int

    def contains(self, x: int, y: int) -> bool:
        return self.x0 <= x < self.x1 and self.y0 <= y < self.y1


@dataclass
class BoundingBox:
    x0: int
    y0: int
    x1: int
    y1: int
    bits: int

    def as_dict(self) -> dict[str, int]:
        return {"x0" : self.x0, "y0": self.y0, "x1": self.x1, "y1": self.y1, "bits": self.bits}


@dataclass
class LoadedRbf:
    die: DieInfo
    header: bytes
    cram: bytearray
    compressed: bool


def crc16(src: bytes) -> int:
    accum = 0xFFFF
    for pos in range(len(src) * 8):
        bit = ((src[pos >> 3] >> (pos & 7)) ^ accum) & 1
        accum >>= 1
        if bit:
            accum ^= 0xA001
    return accum & 0xFFFF


def crc32_frame(src: bytes, frame_size: int) -> int:
    cram_frame_bits = frame_size - 7
    accum = 0x00000001
    for bitid in range(32):
        for block in range(cram_frame_bits):
            pos = 224 + (31 - bitid) + 32 * block
            bit = ((src[pos >> 3] >> (pos & 7)) ^ (accum >> 31)) & 1
            accum = ((accum << 1) & 0xFFFFFFFF)
            if bit:
                accum ^= 0xF4ACFB13
    return accum & 0xFFFFFFFF


def _pram_frame_bytes(die: DieInfo) -> int:
    return (die.frame_size + 2) * 4


def _oram_frame_bytes(die: DieInfo) -> int:
    return _pram_frame_bytes(die) + 104


def _pram_blocks(die: DieInfo) -> int:
    mpram = max(die.pram_sizes) if die.pram_sizes else 0
    if mpram <= 0:
        return 0
    return (mpram + die.frame_size - 1) // die.frame_size


def _header_bytes(die: DieInfo) -> int:
    return _oram_frame_bytes(die) + _pram_frame_bytes(die) * _pram_blocks(die)


def header_nbytes(die: DieInfo) -> int:
    return _header_bytes(die)


def _padding(die: DieInfo) -> int:
    return (64 - (die.cram_sy & 63)) ^ 32


def _cram_frame_bytes(die: DieInfo) -> int:
    return (die.cram_sy + 256 + _padding(die)) // 8


def _cram_bytes(die: DieInfo) -> int:
    bits = die.cram_sx * die.cram_sy
    return (bits + 7) // 8


def cram_get(cram: bytes | bytearray, die: DieInfo, x: int, y: int) -> int:
    cpos = x + die.cram_sx * y
    return (cram[cpos >> 3] >> (cpos & 7)) & 1


def cram_set(cram: bytearray, die: DieInfo, x: int, y: int, value: int) -> None:
    cpos = x + die.cram_sx * y
    mask = 1 << (cpos & 7)
    if value:
        cram[cpos >> 3] |= mask
    else:
        cram[cpos >> 3] &= 0xFF ^ mask


def tile_column_cram_x(die: DieInfo, column: int) -> tuple[int, int]:
    """Return exclusive CRAM x range for one tile column."""

    if column < 0 or column >= len(die.x_to_bx):
        raise ValueError(f"unknown tile column {column}")
    start = die.x_to_bx[column]
    if column + 1 < len(die.x_to_bx):
        end = die.x_to_bx[column + 1]
    else:
        end = die.cram_sx
    return start, end


def default_slot_rect(die: DieInfo = SX120F, column: int = SLOT_COLUMN) -> CramRect:
    """First-cut slot rectangle: the whole M10K column's CRAM x range."""

    x0, x1 = tile_column_cram_x(die, column)
    return CramRect(x0=x0, y0=32, x1=x1, y1=die.cram_sy)


def _decompress_nibbles(payload: bytes, framed_bytes: int) -> bytes:
    out = bytearray(framed_bytes)
    high = False
    index = 0

    def read_nibble() -> int:
        nonlocal high, index
        if index >= len(payload):
            raise ValueError("truncated compressed CRAM")
        if high:
            value = payload[index] >> 4
            index += 1
            high = False
            return value
        high = True
        return payload[index] & 0xF

    written = 0
    while written < framed_bytes:
        key = read_nibble()
        v = read_nibble() if key & 1 else 0
        v |= (read_nibble() << 4) if key & 2 else 0
        out[written] = v
        written += 1
        if written >= framed_bytes:
            break
        v = read_nibble() if key & 4 else 0
        v |= (read_nibble() << 4) if key & 8 else 0
        out[written] = v
        written += 1
    return bytes(out)


def _compress_nibbles(framed: bytes) -> bytes:
    out = bytearray()
    high = False

    def write_nibble(nib: int) -> None:
        nonlocal high
        if high:
            out[-1] |= (nib & 0xF) << 4
            high = False
        else:
            out.append(nib & 0xF)
            high = True

    for i in range(0, len(framed), 2):
        sv1 = framed[i]
        sv2 = framed[i + 1] if i + 1 < len(framed) else 0
        mask = 0
        if sv1 & 0x0F:
            mask |= 1
        if sv1 & 0xF0:
            mask |= 2
        if sv2 & 0x0F:
            mask |= 4
        if sv2 & 0xF0:
            mask |= 8
        write_nibble(mask)
        if mask & 1:
            write_nibble(sv1 & 0xF)
        if mask & 2:
            write_nibble(sv1 >> 4)
        if mask & 4:
            write_nibble(sv2 & 0xF)
        if mask & 8:
            write_nibble(sv2 >> 4)
    if high:
        write_nibble(0xF)
    return bytes(out)


def _unpack_cram(die: DieInfo, framed: bytes) -> bytearray:
    cram = bytearray(_cram_bytes(die))
    frame_bytes = _cram_frame_bytes(die)
    cram_frame_bits = die.frame_size - 7
    offset = _padding(die) - 32
    for x in range(die.cram_sx):
        d = framed[x * frame_bytes : (x + 1) * frame_bytes]
        for y in range(32, die.cram_sy):
            ya = (y + offset) % cram_frame_bits
            yb = (y + offset) // cram_frame_bits
            pos = 224 + (31 ^ yb) + 32 * ya
            if (d[pos >> 3] >> (pos & 7)) & 1:
                cram_set(cram, die, x, y, 1)
    return cram


def _pack_cram(die: DieInfo, cram: bytes | bytearray) -> bytearray:
    frame_bytes = _cram_frame_bytes(die)
    framed = bytearray(die.cram_sx * frame_bytes)
    if frame_bytes >= 3:
        framed[0] = 0x84
        framed[1] = 0x3E
        framed[2] = 0x01
        last = (die.cram_sx - 1) * frame_bytes
        framed[last] = 0x42
        framed[last + 1] = 0x9F
        framed[last + 2] = 0x00
    cram_frame_bits = die.frame_size - 7
    offset = _padding(die) - 32
    idx = 0
    zones = die.noedcrc_zones
    for x in range(die.cram_sx):
        d_off = x * frame_bytes
        for y in range(32, die.cram_sy):
            if cram_get(cram, die, x, y):
                ya = (y + offset) % cram_frame_bits
                yb = (y + offset) // cram_frame_bits
                pos = 224 + (31 ^ yb) + 32 * ya
                framed[d_off + (pos >> 3)] |= 1 << (pos & 7)
        zone = zones[idx] if idx < len(zones) else die.cram_sx
        edcrc = 0 if x >= zone else crc32_frame(bytes(framed[d_off : d_off + frame_bytes]), die.frame_size)
        framed[d_off + frame_bytes - 8] = edcrc & 0xFF
        framed[d_off + frame_bytes - 7] = (edcrc >> 8) & 0xFF
        framed[d_off + frame_bytes - 6] = (edcrc >> 16) & 0xFF
        framed[d_off + frame_bytes - 5] = (edcrc >> 24) & 0xFF
        if x == zone + 255:
            idx += 1
        frame = bytes(framed[d_off : d_off + frame_bytes - 2])
        crc = crc16(frame)
        framed[d_off + frame_bytes - 2] = crc & 0xFF
        framed[d_off + frame_bytes - 1] = (crc >> 8) & 0xFF
    return framed


def _looks_compressed(die: DieInfo, payload: bytes) -> bool:
    framed_bytes = die.cram_sx * _cram_frame_bytes(die)
    return len(payload) < framed_bytes


def rbf_load(data: bytes, die: DieInfo = SX120F) -> LoadedRbf:
    header_len = _header_bytes(die)
    if len(data) < header_len:
        raise ValueError("RBF is shorter than the ORAM/PRAM header")
    header = data[:header_len]
    rest = data[header_len:]
    framed_bytes = die.cram_sx * _cram_frame_bytes(die)
    compressed = _looks_compressed(die, rest)
    if compressed:
        framed = _decompress_nibbles(rest, framed_bytes)
    else:
        if len(rest) < framed_bytes:
            raise ValueError("uncompressed CRAM is truncated")
        framed = rest[:framed_bytes]
    return LoadedRbf(die=die, header=header, cram=_unpack_cram(die, framed), compressed=compressed)


def rbf_save(loaded: LoadedRbf, *, compressed: bool | None = None) -> bytes:
    die = loaded.die
    use_compressed = loaded.compressed if compressed is None else compressed
    framed = _pack_cram(die, loaded.cram)
    out = bytearray(loaded.header)
    if use_compressed:
        out.extend(_compress_nibbles(bytes(framed)))
    else:
        out.extend(framed)
    coff = len(out)
    out.extend(b"\x00" * die.postamble_1)
    if die.postamble_1 >= 2:
        out[coff] = 0xEC
        out[coff + 1] = 0x64
    crc = crc16(bytes(out[coff : coff + die.postamble_1]))
    out.extend(bytes((crc & 0xFF, crc >> 8)))
    coff = len(out)
    out.extend(b"\x00" * 10)
    out[coff] = 0xAE
    out[coff + 1] = 0xFB
    crc = crc16(bytes(out[coff : coff + 10]))
    out.extend(bytes((crc & 0xFF, crc >> 8)))
    out.extend(b"\xff" * die.postamble_2)
    return bytes(out)


def overlay_cram(base: LoadedRbf, cart: LoadedRbf, rect: CramRect) -> LoadedRbf:
    if base.die != cart.die:
        raise ValueError("base and cart dies do not match")
    if rect.x0 < 0 or rect.y0 < 0 or rect.x1 > base.die.cram_sx or rect.y1 > base.die.cram_sy:
        raise ValueError("CRAM rectangle is outside the die")
    if rect.x1 <= rect.x0 or rect.y1 <= rect.y0:
        raise ValueError("CRAM rectangle is empty")
    cram = bytearray(base.cram)
    for y in range(rect.y0, rect.y1):
        for x in range(rect.x0, rect.x1):
            cram_set(cram, base.die, x, y, cram_get(cart.cram, cart.die, x, y))
    return LoadedRbf(die=base.die, header=base.header, cram=cram, compressed=base.compressed)


def _iter_cram_diffs(left: LoadedRbf, right: LoadedRbf):
    if left.die != right.die:
        raise ValueError("diff dies do not match")
    die = left.die
    limit = min(len(left.cram), len(right.cram))
    for index in range(limit):
        changed = left.cram[index] ^ right.cram[index]
        if not changed:
            continue
        base = index << 3
        bit = 0
        while changed:
            if changed & 1:
                cpos = base + bit
                yield cpos % die.cram_sx, cpos // die.cram_sx
            changed >>= 1
            bit += 1


def diff_cram(left: LoadedRbf, right: LoadedRbf, rect: CramRect | None = None) -> BoundingBox | None:
    min_x = min_y = max_x = max_y = None
    bits = 0
    for x, y in _iter_cram_diffs(left, right):
        if rect is not None and not rect.contains(x, y):
            continue
        bits += 1
        min_x = x if min_x is None else min(min_x, x)
        max_x = x if max_x is None else max(max_x, x)
        min_y = y if min_y is None else min(min_y, y)
        max_y = y if max_y is None else max(max_y, y)
    if bits == 0:
        return None
    return BoundingBox(x0=min_x, y0=min_y, x1=max_x + 1, y1=max_y + 1, bits=bits)


def classify_cram_diff(
    left: LoadedRbf, right: LoadedRbf, slot: CramRect | None = None
) -> dict[str, object]:
    """Bucket CRAM diffs by tile column so a chip-wide bbox does not hide locality."""

    die = left.die
    if slot is None:
        slot = default_slot_rect(die)
    column_bits: dict[int, int] = {}
    inside = 0
    outside = 0
    min_x = min_y = max_x = max_y = None
    for x, y in _iter_cram_diffs(left, right):
        if min_x is None:
            min_x = max_x = x
            min_y = max_y = y
        else:
            min_x = min(min_x, x)
            max_x = max(max_x, x)
            min_y = min(min_y, y)
            max_y = max(max_y, y)
        column = 0
        for index, start in enumerate(die.x_to_bx):
            nxt = die.x_to_bx[index + 1] if index + 1 < len(die.x_to_bx) else die.cram_sx
            if start <= x < nxt:
                column = index
                break
        column_bits[column] = column_bits.get(column, 0) + 1
        if column in die.ecc_columns:
            continue
        if slot.contains(x, y):
            inside += 1
        else:
            outside += 1
    total = inside + outside
    box = (
        None
        if total == 0
        else BoundingBox(x0=min_x, y0=min_y, x1=max_x + 1, y1=max_y + 1, bits=total)
    )
    return {
        "identical": total == 0,
        "bits_inside_slot": inside,
        "bits_outside_slot": outside,
        "column_bits": {str(key): value for key, value in sorted(column_bits.items())},
        "diff": None if box is None else box.as_dict(),
        "inside_slot_column": False if box is None else rect_inside(box, slot),
    }


def rect_inside(inner: BoundingBox, outer: CramRect) -> bool:
    return outer.x0 <= inner.x0 and inner.x1 <= outer.x1 and outer.y0 <= inner.y0 and inner.y1 <= outer.y1
