#!/usr/bin/env python3
"""Static Cyclone V CRAM overlay linker.

Refuse compressed-frame splicing. Always decompress, overlay a CRAM rectangle
from a link map, rewrite CRCs, and recompress. The composed bitstream is one
full-chip RBF.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Sequence

try:
    from .cyclonev_rbf import (
        SLOT_BEL,
        SX120F,
        TARGET_DEVICE,
        CramRect,
        classify_cram_diff,
        default_slot_rect,
        overlay_cram,
        rbf_load,
        rbf_save,
        tile_column_cram_x,
    )
except ImportError:  # pragma: no cover - script entry
    from cyclonev_rbf import (
        SLOT_BEL,
        SX120F,
        TARGET_DEVICE,
        CramRect,
        classify_cram_diff,
        default_slot_rect,
        overlay_cram,
        rbf_load,
        rbf_save,
        tile_column_cram_x,
    )


OVERLAY_CRAM_RECT = "cram_rect"
OVERLAY_M10K_RAM = "m10k_ram"
M10K_RAM_WORDS = 256
M10K_RAM_WIDTH = 40
ROM_LANE_DEPTH = 1024
ROM_LANE_WIDTH = 10
ZX81_BASIC_BYTES = 8192
ZX81_BASIC_BLOCKS = ZX81_BASIC_BYTES // ROM_LANE_DEPTH


class LinkError(ValueError):
    """Raised when a static overlay cannot be produced."""


def bel_to_bt_name(bel: str) -> str:
    """Convert ``MISTRAL_M10K.26.1.0`` to mistral-cv ``M10K.026.001``."""

    parts = bel.split(".")
    if len(parts) < 3:
        raise LinkError(f"unsupported slot BEL {bel!r}")
    kind = parts[0].removeprefix("MISTRAL_")
    try:
        x = int(parts[1], 10)
        y = int(parts[2], 10)
    except ValueError as exc:
        raise LinkError(f"unsupported slot BEL {bel!r}") from exc
    return f"{kind}.{x:03d}.{y:03d}"


def overlay_m10k_ram_bt(base_text: str, cart_text: str, bel: str = SLOT_BEL) -> str:
    """Replace slot M10K RAM mux lines in a mistral-cv decompile."""

    prefix = f"s {bel_to_bt_name(bel)}:RAM."
    cart_ram: dict[str, str] = {}
    for line in cart_text.splitlines():
        if line.startswith(prefix):
            cart_ram[line.split()[1]] = line
    if not cart_ram:
        raise LinkError(f"cart decompile has no RAM muxes for {bel}")
    replaced = 0
    out: list[str] = []
    for line in base_text.splitlines():
        if line.startswith(prefix):
            key = line.split()[1]
            if key not in cart_ram:
                raise LinkError(f"cart decompile is missing {key}")
            out.append(cart_ram[key])
            replaced += 1
        else:
            out.append(line)
    if replaced != len(cart_ram):
        raise LinkError(f"replaced {replaced} RAM muxes, cart has {len(cart_ram)}")
    return "\n".join(out) + "\n"


def load_hex_bytes(path: Path) -> bytes:
    """Load a one-byte-per-line hex image."""

    values: list[int] = []
    for raw in path.read_text(encoding="utf-8").splitlines():
        text = raw.split("//", 1)[0].strip()
        if not text or text.startswith("@") or text.startswith("//"):
            continue
        value = int(text, 16)
        if value > 0xFF:
            raise LinkError(f"{path} byte {len(values)} is wider than 8 bits")
        values.append(value)
    return bytes(values)


def zx81_basic_rom(image: bytes) -> bytes:
    """Return the 8 KiB ZX81 BASIC image at the front of ``zx8x``."""

    if len(image) < ZX81_BASIC_BYTES:
        raise LinkError(f"ZX81 image is {len(image)} bytes, need {ZX81_BASIC_BYTES}")
    rom = image[:ZX81_BASIC_BYTES]
    if rom[:2] != b"\xd3\xfd":
        raise LinkError("ZX81 BASIC image does not start with OUT (FD),A")
    return rom


# Legal column-26 M10K rows. Rows 3, 4, 7 and 8 are not M10K sites; the
# 16K cart uses this same 1,2 then 5,6 stride.
ZX81_BASIC_M10K_Y = (1, 2, 5, 6, 9, 10, 13, 14)


def zx81_basic_bels() -> list[str]:
    """Eight blank column-26 M10Ks, one 1024-byte lane each, low address first.

    These are the port-proof blank. The expansion cart owns this column, so
    the machine ROM uses :func:`zx81_machine_rom_bels` instead.
    """

    return [f"MISTRAL_M10K.26.{index}.0" for index in ZX81_BASIC_M10K_Y]


# Column 5 rows 73–80 are legal M10Ks on sx120f, outside the cart rectangle
# ``25 1 27 32``, and unused by the current shell's video and mailbox blocks.
ZX81_MACHINE_ROM_Y = tuple(range(73, 81))


def zx81_machine_rom_bels() -> list[str]:
    """Eight empty column-5 M10Ks for the machine ROM, low address first."""

    return [f"MISTRAL_M10K.5.{index}.0" for index in ZX81_MACHINE_ROM_Y]


def encoded_rom_blocks(image: bytes, bels: Sequence[str]) -> list[tuple[str, list[int]]]:
    """Pack the low 8 KiB into encoded RAM muxes for ``bels``, low address first."""

    rom = zx81_basic_rom(image)
    if len(bels) * ROM_LANE_DEPTH != len(rom):
        raise LinkError(f"{len(bels)} ROM blocks do not cover {len(rom)} bytes")
    blocks: list[tuple[str, list[int]]] = []
    for index, bel in enumerate(bels):
        start = index * ROM_LANE_DEPTH
        block = rom[start : start + ROM_LANE_DEPTH]
        stored = [encode_m10k_ram_word(word) for word in pack_1024x10(block)]
        blocks.append((bel, stored))
    return blocks


# nextpnr bitstream.cc permute_init. Output bit i comes from logical bit
# permutation[i], then the 40-bit chunk is inverted before it is stored
# as the M10K RAM mux.
M10K_INIT_PERMUTATION = (
    0, 20, 10, 30, 1, 21, 11, 31, 2, 22, 12, 32, 3, 23, 13, 33, 4, 24, 14, 34,
    5, 25, 15, 35, 6, 26, 16, 36, 7, 27, 17, 37, 8, 28, 18, 38, 9, 29, 19, 39,
)


def encode_m10k_ram_word(logical: int) -> int:
    """Store one logical 40-bit INIT chunk the way nextpnr writes ``RAM``."""

    if logical < 0 or logical >= (1 << M10K_RAM_WIDTH):
        raise LinkError(f"logical RAM word {logical:#x} is not 40 bits")
    output = 0
    for bit, source in enumerate(M10K_INIT_PERMUTATION):
        output |= ((logical >> source) & 1) << bit
    return (~output) & ((1 << M10K_RAM_WIDTH) - 1)


def decode_m10k_ram_word(stored: int) -> int:
    """Invert :func:`encode_m10k_ram_word`."""

    if stored < 0 or stored >= (1 << M10K_RAM_WIDTH):
        raise LinkError(f"stored RAM word {stored:#x} is not 40 bits")
    flipped = (~stored) & ((1 << M10K_RAM_WIDTH) - 1)
    logical = 0
    for bit, source in enumerate(M10K_INIT_PERMUTATION):
        logical |= ((flipped >> bit) & 1) << source
    return logical


def pack_1024x10(block: bytes) -> list[int]:
    """Pack 1024 payload bytes into 256 physical 40-bit RAM words.

    Each logical address is one 10-bit lane, matching the 1024×10 port map
    in ``880_m10k_async_rom``: lane ``address`` is INIT bits ``address*10``
    for 10 bits, and the byte occupies lane bits ``[7:0]``. Four lanes fill
    one 40-bit word because 40 is a multiple of 10. Lane bits ``[9:8]`` stay 0.
    """

    if len(block) != ROM_LANE_DEPTH:
        raise LinkError(f"1024x10 block is {len(block)} bytes")
    words = [0] * M10K_RAM_WORDS
    for address, byte in enumerate(block):
        bit = address * ROM_LANE_WIDTH
        words[bit // M10K_RAM_WIDTH] |= (byte & 0xFF) << (bit % M10K_RAM_WIDTH)
    return words


def unpack_1024x10(words: Sequence[int]) -> bytes:
    """Invert :func:`pack_1024x10`."""

    if len(words) != M10K_RAM_WORDS:
        raise LinkError(f"1024x10 init has {len(words)} RAM words")
    out = bytearray(ROM_LANE_DEPTH)
    for address in range(ROM_LANE_DEPTH):
        bit = address * ROM_LANE_WIDTH
        word = int(words[bit // M10K_RAM_WIDTH])
        if word < 0 or word >= (1 << M10K_RAM_WIDTH):
            raise LinkError(f"RAM word {bit // M10K_RAM_WIDTH} is not 40 bits")
        lane = (word >> (bit % M10K_RAM_WIDTH)) & ((1 << ROM_LANE_WIDTH) - 1)
        if lane & ~0xFF:
            raise LinkError(f"lane {address} padding is not zero")
        out[address] = lane & 0xFF
    return bytes(out)


def format_ram40(word: int) -> str:
    """Format one mistral-cv 40-bit RAM mux value."""

    if word < 0 or word >= (1 << M10K_RAM_WIDTH):
        raise LinkError(f"RAM word {word:#x} is not 40 bits")
    return f"{word >> 32:02x}.{word & 0xFFFFFFFF:08x}"


def parse_ram40(text: str) -> int:
    hi, separator, lo = text.partition(".")
    if not separator or len(hi) != 2 or len(lo) != 8:
        raise LinkError(f"unsupported RAM mux value {text!r}")
    word = (int(hi, 16) << 32) | int(lo, 16)
    if word >= (1 << M10K_RAM_WIDTH):
        raise LinkError(f"RAM mux value {text!r} is wider than 40 bits")
    return word


def _ram_token(bel: str, index: int) -> str:
    return f"{bel_to_bt_name(bel)}:RAM.{index}"


def m10k_block_present(text: str, bel: str) -> bool:
    """Return whether decompile configured this M10K at all.

    mistral-cv omits a mux whose stored value is the database default.
    ``m ram r-:40`` means an all-zero RAM word is omitted, so a placed block
    can show port config and zero ``RAM.`` lines. Any ``s`` line for the
    block, including one RAM word, means the site was written.
    """

    marker = bel_to_bt_name(bel) + ":"
    for line in text.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] == "s" and parts[1].startswith(marker):
            return True
    return False


def _parse_ram_index(token: str, prefix: str) -> int:
    if not token.startswith(prefix):
        raise LinkError(f"unknown RAM mux {token}")
    try:
        index = int(token.rsplit(".", 1)[1])
    except ValueError as exc:
        raise LinkError(f"unknown RAM mux {token}") from exc
    if index < 0 or index >= M10K_RAM_WORDS:
        raise LinkError(f"unknown RAM mux {token}")
    return index


def overlay_m10k_init_bt(base_text: str, bel: str, words: Sequence[int]) -> str:
    """Replace one placed M10K's RAM muxes. Port config stays on the blank.

    Omitted default RAM words are inserted. A site with no ``s`` lines for
    this block is refused, because there is no placed M10K to program.
    """

    if len(words) != M10K_RAM_WORDS:
        raise LinkError(f"{bel} init has {len(words)} RAM words")
    seen: set[int] = set()
    out: list[str] = []
    prefix = bel_to_bt_name(bel) + ":RAM."
    for line in base_text.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] == "s" and parts[1].startswith(prefix):
            index = _parse_ram_index(parts[1], prefix)
            if index in seen:
                raise LinkError(f"duplicate or unknown RAM mux {parts[1]}")
            seen.add(index)
            out.append(f"s {_ram_token(bel, index)} {format_ram40(int(words[index]))}")
        else:
            out.append(line)
    if seen != set(range(M10K_RAM_WORDS)):
        if not m10k_block_present(base_text, bel):
            raise LinkError(f"{bel} decompile has {len(seen)} RAM muxes, expected {M10K_RAM_WORDS}")
        for index in range(M10K_RAM_WORDS):
            if index not in seen:
                out.append(f"s {_ram_token(bel, index)} {format_ram40(int(words[index]))}")
    return "\n".join(out) + "\n"


def read_m10k_init_bt(text: str, bel: str) -> list[int]:
    words: dict[int, int] = {}
    prefix = bel_to_bt_name(bel) + ":RAM."
    for line in text.splitlines():
        parts = line.split()
        if len(parts) >= 3 and parts[0] == "s" and parts[1].startswith(prefix):
            index = _parse_ram_index(parts[1], prefix)
            if index in words:
                raise LinkError(f"duplicate RAM mux {parts[1]}")
            words[index] = parse_ram40(parts[2])
    if set(words) != set(range(M10K_RAM_WORDS)):
        if not m10k_block_present(text, bel):
            raise LinkError(f"{bel} decompile has {len(words)} RAM muxes, expected {M10K_RAM_WORDS}")
        for index in range(M10K_RAM_WORDS):
            words.setdefault(index, 0)
    return [words[index] for index in range(M10K_RAM_WORDS)]


def overlay_zx81_basic_bt(base_text: str, image: bytes, bels: Sequence[str] | None = None) -> str:
    """Write the 8 KiB ZX81 BASIC image into eight blank 1024×10 M10Ks."""

    rom = zx81_basic_rom(image)
    sites = list(zx81_basic_bels() if bels is None else bels)
    if len(sites) != ZX81_BASIC_BLOCKS:
        raise LinkError(f"ZX81 BASIC map has {len(sites)} blocks, expected {ZX81_BASIC_BLOCKS}")
    text = base_text
    for index, bel in enumerate(sites):
        block = rom[index * ROM_LANE_DEPTH : (index + 1) * ROM_LANE_DEPTH]
        text = overlay_m10k_init_bt(text, bel, pack_1024x10(block))
    return text


def lane_from_hex(path: Path, offset: int = 0) -> bytes:
    """Return one 1024-byte lane from a one-byte-per-line hex image."""

    image = load_hex_bytes(path)
    if offset == 0 and len(image) >= ZX81_BASIC_BYTES and image[:2] == b"\xd3\xfd":
        image = zx81_basic_rom(image)
    if offset < 0 or offset + ROM_LANE_DEPTH > len(image):
        raise LinkError(
            f"{path} has {len(image)} bytes, need {ROM_LANE_DEPTH} at offset {offset}"
        )
    return image[offset : offset + ROM_LANE_DEPTH]


def _classify_bt_lines(text: str, bel: str) -> tuple[list[str], list[str]]:
    """Split a decompile into this BEL's non-RAM lines and every other line."""

    prefix = bel_to_bt_name(bel) + ":"
    ram_prefix = prefix + "RAM."
    config: list[str] = []
    other: list[str] = []
    for line in text.splitlines():
        parts = line.split()
        token = parts[1] if len(parts) >= 2 else ""
        if token.startswith(ram_prefix):
            continue
        if token.startswith(prefix):
            config.append(line)
        else:
            other.append(line)
    return config, other


def _without_ram_lines(text: str, bels: Sequence[str]) -> list[str]:
    prefixes = tuple(bel_to_bt_name(bel) + ":RAM." for bel in bels)
    kept: list[str] = []
    for line in text.splitlines():
        parts = line.split()
        token = parts[1] if len(parts) >= 2 else ""
        if token.startswith(prefixes):
            continue
        kept.append(line)
    return kept


def splice_m10k_inits_rbf(
    base_path: Path, blocks: Sequence[tuple[str, Sequence[int]]], output_path: Path
) -> dict[str, Any]:
    """Replace placed M10K RAM muxes and read them back from the RBF.

    ``mistral-cv`` decompiles the placed bitstream once. Each block's 256
    RAM lines are replaced, then a second decompile of the recomposed RBF
    must reproduce those words. Every non-RAM line stays on the placed blank.
    """

    if not blocks:
        raise LinkError("init splice has no M10K blocks")
    seen_bels: set[str] = set()
    packed_blocks: list[tuple[str, list[int]]] = []
    for bel, words in blocks:
        if bel in seen_bels:
            raise LinkError(f"init splice repeats {bel}")
        seen_bels.add(bel)
        if len(words) != M10K_RAM_WORDS:
            raise LinkError(f"{bel} init has {len(words)} RAM words")
        packed_blocks.append((bel, [int(word) for word in words]))
    mistral_cv = find_mistral_cv()
    bels = [bel for bel, _words in packed_blocks]
    with tempfile.TemporaryDirectory(prefix="fes-m10k-init-") as directory:
        root = Path(directory)
        base_bt = root / "base.bt"
        composed_bt = root / "composed.bt"
        staged_rbf = root / "composed.rbf"
        readback_bt = root / "readback.bt"
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(base_path), str(base_bt)])
        original = base_bt.read_text(encoding="utf-8")
        original_words = {bel: read_m10k_init_bt(original, bel) for bel in bels}
        composed = original
        for bel, packed in packed_blocks:
            composed = overlay_m10k_init_bt(composed, bel, packed)
        if _without_ram_lines(original, bels) != _without_ram_lines(composed, bels):
            raise LinkError("init splice changed a non-RAM line before recompression")
        composed_bt.write_text(composed, encoding="utf-8")
        _run_mistral_cv(mistral_cv, ["comp", str(composed_bt), str(staged_rbf)])
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(staged_rbf), str(readback_bt)])
        readback = readback_bt.read_text(encoding="utf-8")
        if _without_ram_lines(original, bels) != _without_ram_lines(readback, bels):
            raise LinkError("readback changed a non-RAM line")
        ram0: dict[str, str] = {}
        for bel, packed in packed_blocks:
            read_words = read_m10k_init_bt(readback, bel)
            if read_words != packed:
                raise LinkError(f"{bel} readback RAM muxes do not match the spliced init")
            if read_words == original_words[bel]:
                raise LinkError(f"{bel} init splice did not change the RAM muxes")
            ram0[bel_to_bt_name(bel)] = format_ram40(read_words[0])
        output = staged_rbf.read_bytes()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_bytes(output)
    return {
        "device": TARGET_DEVICE,
        "bels": bels,
        "ram0": ram0,
        "base_sha256": hashlib.sha256(base_path.read_bytes()).hexdigest(),
        "output_sha256": hashlib.sha256(output).hexdigest(),
        "output_size": len(output),
    }


def splice_m10k_init_rbf(
    base_path: Path, bel: str, words: Sequence[int], output_path: Path
) -> dict[str, Any]:
    """Replace one placed M10K's RAM muxes and read them back from the RBF."""

    receipt = splice_m10k_inits_rbf(base_path, [(bel, words)], output_path)
    receipt["bel"] = bel
    receipt["bt_name"] = bel_to_bt_name(bel)
    receipt["ram0"] = receipt["ram0"][bel_to_bt_name(bel)]
    return receipt


def find_mistral_cv() -> Path:
    env = os.environ.get("MISTRAL_CV")
    if env:
        path = Path(env)
        if path.is_file() and os.access(path, os.X_OK):
            return path
        raise LinkError(f"MISTRAL_CV is not an executable: {env}")
    local = Path(__file__).resolve().parents[1] / "build/toolchain/install/bin/mistral-cv"
    if local.is_file() and os.access(local, os.X_OK):
        return local
    found = shutil.which("mistral-cv")
    if found:
        return Path(found)
    raise LinkError("mistral-cv is required for overlay_mode=m10k_ram")


def parse_link_map(text: str) -> dict[str, Any]:
    """Parse a minimal TOML-like link map without extra dependencies."""

    data: dict[str, Any] = {}
    rect: dict[str, int] = {}
    in_rect = False
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        if line == "[cram_rect]":
            in_rect = True
            continue
        if line.startswith("[") and line.endswith("]"):
            in_rect = False
            continue
        if "=" not in line:
            raise LinkError(f"invalid link-map line: {raw!r}")
        key, value = (part.strip() for part in line.split("=", 1))
        parsed: Any
        if value.startswith("[") and value.endswith("]"):
            inner = value[1:-1].strip()
            parsed = [] if not inner else [item.strip().strip('"').strip("'") for item in inner.split(",")]
        elif value.startswith('"') and value.endswith('"'):
            parsed = value[1:-1]
        elif value.startswith("'") and value.endswith("'"):
            parsed = value[1:-1]
        elif value in ("true", "True"):
            parsed = True
        elif value in ("false", "False"):
            parsed = False
        else:
            try:
                parsed = int(value, 0)
            except ValueError as exc:
                raise LinkError(f"invalid link-map value for {key}") from exc
        if in_rect:
            if not isinstance(parsed, int):
                raise LinkError("cram_rect values must be integers")
            rect[key] = parsed
        else:
            data[key] = parsed
    if rect:
        data["cram_rect"] = rect
    return data


def load_link_map(path: Path) -> dict[str, Any]:
    return parse_link_map(path.read_text(encoding="utf-8"))


def rect_from_map(mapping: dict[str, Any]) -> CramRect:
    device = mapping.get("device", TARGET_DEVICE)
    if device != TARGET_DEVICE:
        raise LinkError(f"unsupported device {device!r}")
    die = mapping.get("die", SX120F.name)
    if die != SX120F.name:
        raise LinkError(f"unsupported die {die!r}")
    rect = mapping.get("cram_rect")
    if isinstance(rect, dict) and {"x0", "y0", "x1", "y1"} <= set(rect):
        return CramRect(int(rect["x0"]), int(rect["y0"]), int(rect["x1"]), int(rect["y1"]))
    column = int(mapping.get("slot_column", 26))
    return default_slot_rect(SX120F, column)


def overlay_mode_from_map(mapping: dict[str, Any]) -> str:
    mode = mapping.get("overlay_mode", OVERLAY_CRAM_RECT)
    if mode not in (OVERLAY_CRAM_RECT, OVERLAY_M10K_RAM):
        raise LinkError(f"unsupported overlay_mode {mode!r}")
    return str(mode)


def _run_mistral_cv(binary: Path, args: Sequence[str]) -> None:
    try:
        subprocess.run([str(binary), *args], check=True, capture_output=True, text=True)
    except subprocess.CalledProcessError as exc:
        detail = (exc.stderr or exc.stdout or "").strip()
        raise LinkError(detail or f"mistral-cv {' '.join(args)} failed") from exc


def overlay_m10k_ram_files(
    base_path: Path, cart_path: Path, mapping: dict[str, Any], output_path: Path
) -> bytes:
    bels = mapping.get("slot_bels", [SLOT_BEL])
    if not isinstance(bels, list) or not bels:
        bels = [SLOT_BEL]
    mistral_cv = find_mistral_cv()
    with tempfile.TemporaryDirectory(prefix="fes-m10k-ram-") as directory:
        root = Path(directory)
        base_bt = root / "base.bt"
        cart_bt = root / "cart.bt"
        composed_bt = root / "composed.bt"
        composed_rbf = root / "composed.rbf"
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(base_path), str(base_bt)])
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(cart_path), str(cart_bt)])
        text = base_bt.read_text(encoding="utf-8")
        cart_text = cart_bt.read_text(encoding="utf-8")
        for bel in bels:
            text = overlay_m10k_ram_bt(text, cart_text, str(bel))
        composed_bt.write_text(text, encoding="utf-8")
        _run_mistral_cv(mistral_cv, ["comp", str(composed_bt), str(composed_rbf)])
        output = composed_rbf.read_bytes()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_bytes(output)
    return output


def overlay_files(base_path: Path, cart_path: Path, map_path: Path, output_path: Path) -> dict[str, Any]:
    mapping = load_link_map(map_path)
    if mapping.get("device", TARGET_DEVICE) != TARGET_DEVICE:
        raise LinkError(f"unsupported device {mapping.get('device')!r}")
    if mapping.get("die", SX120F.name) != SX120F.name:
        raise LinkError(f"unsupported die {mapping.get('die')!r}")
    mode = overlay_mode_from_map(mapping)
    base_bytes = base_path.read_bytes()
    cart_bytes = cart_path.read_bytes()
    if mode == OVERLAY_M10K_RAM:
        output = overlay_m10k_ram_files(base_path, cart_path, mapping, output_path)
        rect = None
    else:
        rect = rect_from_map(mapping)
        base = rbf_load(base_bytes)
        cart = rbf_load(cart_bytes)
        classified = classify_cram_diff(base, cart, rect)
        if mapping.get("require_slot_only", True) and int(classified["bits_outside_slot"]) != 0:
            raise LinkError(
                "cart CRAM changes "
                f"{classified['bits_outside_slot']} bits outside the reserved rect"
            )
        composed = overlay_cram(base, cart, rect)
        output = rbf_save(composed, compressed=True)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_bytes(output)
    receipt = {
        "device": TARGET_DEVICE,
        "die": SX120F.name,
        "overlay_mode": mode,
        "slot_bels": mapping.get("slot_bels", [SLOT_BEL]),
        "base_sha256": hashlib.sha256(base_bytes).hexdigest(),
        "cart_sha256": hashlib.sha256(cart_bytes).hexdigest(),
        "map_sha256": hashlib.sha256(map_path.read_bytes()).hexdigest(),
        "output_sha256": hashlib.sha256(output).hexdigest(),
        "output_size": len(output),
    }
    if rect is not None:
        receipt["cram_rect"] = {"x0": rect.x0, "y0": rect.y0, "x1": rect.x1, "y1": rect.y1}
    receipt_path = output_path.with_suffix(output_path.suffix + ".receipt.json")
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    return receipt


def diff_files(left_path: Path, right_path: Path) -> dict[str, Any]:
    left = rbf_load(left_path.read_bytes())
    right = rbf_load(right_path.read_bytes())
    slot = default_slot_rect()
    classified = classify_cram_diff(left, right, slot)
    return {
        "device": TARGET_DEVICE,
        "slot_column_x": list(tile_column_cram_x(SX120F, 26)),
        "slot_rect": {"x0": slot.x0, "y0": slot.y0, "x1": slot.x1, "y1": slot.y1},
        **classified,
    }


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    overlay = sub.add_parser("overlay", help="compose base+cart CRAM into one RBF")
    overlay.add_argument("--base", required=True, type=Path)
    overlay.add_argument("--cart", required=True, type=Path)
    overlay.add_argument("--map", required=True, type=Path)
    overlay.add_argument("--output", required=True, type=Path)
    diff = sub.add_parser("diff", help="report the CRAM bounding box between two RBFs")
    diff.add_argument("--a", required=True, type=Path)
    diff.add_argument("--b", required=True, type=Path)
    init = sub.add_parser("init", help="replace placed M10K RAM init and read it back")
    init.add_argument("--base", required=True, type=Path)
    init.add_argument("--bel", default="MISTRAL_M10K.26.1.0")
    init.add_argument("--image", required=True, type=Path)
    init.add_argument("--offset", type=int, default=0)
    init.add_argument(
        "--basic",
        action="store_true",
        help="splice all eight ZX81 BASIC lanes onto the column-26 proof sites",
    )
    init.add_argument(
        "--machine",
        action="store_true",
        help="splice all eight ZX81 BASIC lanes onto the column-5 machine ROM sites",
    )
    init.add_argument("--output", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "overlay":
            receipt = overlay_files(args.base, args.cart, args.map, args.output)
            print(json.dumps(receipt, indent=2))
            return 0
        if args.command == "init":
            if args.basic and args.machine:
                raise LinkError("init --basic and --machine select different ROM sites")
            if args.basic or args.machine:
                image = load_hex_bytes(args.image)
                rom = zx81_basic_rom(image)
                bels = zx81_machine_rom_bels() if args.machine else zx81_basic_bels()
                blocks = encoded_rom_blocks(image, bels)
                receipt = splice_m10k_inits_rbf(args.base, blocks, args.output)
                receipt["image_heads"] = [
                    f"{rom[index * ROM_LANE_DEPTH]:02x}" for index in range(len(bels))
                ]
                receipt["image_head"] = rom[:8].hex()
                receipt["image_sha256"] = hashlib.sha256(rom).hexdigest()
            else:
                block = lane_from_hex(args.image, args.offset)
                stored = [encode_m10k_ram_word(word) for word in pack_1024x10(block)]
                receipt = splice_m10k_init_rbf(args.base, args.bel, stored, args.output)
                receipt["image_offset"] = args.offset
                receipt["image_head"] = block[:8].hex()
            print(json.dumps(receipt, indent=2))
            return 0
        print(json.dumps(diff_files(args.a, args.b), indent=2))
        return 0
    except (LinkError, ValueError, OSError) as exc:
        print(f"link_static_rbf: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
