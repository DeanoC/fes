#!/usr/bin/env python3
"""Export machine ROM INIT destinations from the selected Mistral database.

This is a producer tool, never a kit dependency. word_bits uses stored mux bit
order and linear CRAM addresses (y * 7605 + x), not offsets into an RBF file.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re

try:
    from .cyclonev_rbf import SX120F, TARGET_DEVICE, rbf_load
except ImportError:
    from cyclonev_rbf import SX120F, TARGET_DEVICE, rbf_load


DATABASE_FILES = ('data/m10k-mux.txt', 'libmistral/cvd-sx120f.cc', 'libmistral/cyclonev.h')
ZX81_LANE_ROWS = tuple(range(73, 81))
SMS_LANE_ROWS = tuple(range(32, 56)) + ZX81_LANE_ROWS
SG1000_LANE_ROWS = tuple(range(32, 48))


def lane_locations(lanes: tuple[int | tuple[int, int], ...]) -> tuple[tuple[int, int], ...]:
    if not lanes or len(lanes) > 256:
        raise ValueError('invalid ROM lane selection')
    locations = tuple((5, lane) if type(lane) is int else lane for lane in lanes)
    if any(not isinstance(lane, tuple) or len(lane) != 2 or
           any(type(value) is not int for value in lane) for lane in locations) or len(set(locations)) != len(locations):
        raise ValueError('invalid ROM lane selection')
    return locations


def read_database(root: Path, pins: dict[str, str] | None = None) -> dict[str, bytes]:
    """Read a snapshot, rejecting linked paths and (for production) unpinned bytes."""
    result = {}
    if pins is not None and set(pins) != set(DATABASE_FILES):
        raise ValueError('ROM database pins must cover exactly the selected files')
    for name in DATABASE_FILES:
        path = root / name
        if any(part.is_symlink() for part in (path, *path.parents)) or not path.is_file():
            raise ValueError(f'ROM database must be a regular non-symlink file: {name}')
        data = path.read_bytes()
        if pins is not None and hashlib.sha256(data).hexdigest() != pins[name]:
            raise ValueError(f'ROM database digest differs from pinned Mistral source: {name}')
        result[name] = data
    return result


def validate_routed_rom(routed: dict, lane_rows: tuple[int, ...] = ZX81_LANE_ROWS) -> None:
    """Require the actual routed BEL, lane shape and empty INIT for every bank."""
    locations = lane_locations(lane_rows)
    cells = routed.get('modules', {}).get('top', {}).get('cells', {})
    if not isinstance(cells, dict):
        raise ValueError('routed ROM cells missing')
    for index, (column, row) in enumerate(locations):
        name = f'machine.rom.lane{index}'
        cell = cells.get(name, {})
        bel = f'MISTRAL_M10K.{column}.{row}.0'
        if cell.get('type') != 'MISTRAL_M10K' or cell.get('attributes', {}).get('NEXTPNR_BEL') != bel:
            raise ValueError(f'routed ROM lane {name} must occupy {bel}')
        parameters = cell.get('parameters', {})
        for key, expected in (('CFG_ABITS', 10), ('CFG_DBITS', 10), ('CFG_ASYNC_READ', 1)):
            value = parameters.get(key)
            if isinstance(value, str) and re.fullmatch('[01]+', value):
                value = int(value, 2)
            if type(value) is not int or value != expected:
                raise ValueError(f'routed ROM lane {name} has invalid {key}')
        if parameters.get('INIT') != '0'*10240:
            raise ValueError(f'routed ROM lane {name} must have blank 10240-bit INIT')
        occupants = [other for other, data in cells.items()
                     if data.get('attributes', {}).get('NEXTPNR_BEL') == bel]
        if occupants != [name]:
            raise ValueError(f'routed ROM BEL {bel} has duplicate occupants')


def parse_ram_offsets(text: str) -> list[list[tuple[int, int]]]:
    lines = text.splitlines()
    headers = [i for i, line in enumerate(lines) if line.split() == ['m', 'ram', 'r-:40']]
    if len(headers) != 1:
        raise ValueError('expected one M10K 40-bit RAM mux definition')
    words = []
    seen = set()
    for line in lines[headers[0]+1:]:
        fields = line.split()
        if not fields or fields[0] != '*':
            break
        if len(fields) != 41:
            raise ValueError('M10K RAM word must have 40 destinations')
        word = []
        for field in fields[1:]:
            if not re.fullmatch(r'[0-9]+\.[0-9]+', field):
                raise ValueError('invalid M10K RAM coordinate')
            x, y = map(int, field.split('.'))
            if not 0 <= x < 300 or not 0 <= y < 86:
                raise ValueError('M10K RAM coordinate outside tile')
            if (x, y) in seen:
                raise ValueError('duplicate M10K RAM destination')
            seen.add((x, y))
            word.append((x, y))
        words.append(word)
    if len(words) != 256:
        raise ValueError('M10K RAM must have 256 words')
    return words


def rom_blocks(words: list[list[tuple[int, int]]], lane_rows: tuple[int, ...]) -> list[dict]:
    return [dict(bel=f'M10K.{column:03d}.{row:03d}', source_offset=index*1024,
                 word_bits=[[(2+86*row+y)*SX120F.cram_sx+SX120F.x_to_bx[column]+x
                             for x, y in word] for word in words])
            for index, (column, row) in enumerate(lane_locations(lane_rows))]


def _section(text: str, label: str) -> str:
    match = re.search(r'// '+re.escape(label)+r'\s*\{([^}]+)\}', text)
    if not match:
        raise ValueError(f'Mistral die database missing {label}')
    return match[1]


def build_rom_map(mistral_source: Path | dict[str, bytes], base: bytes, *,
                  routed: dict | None = None,
                  lane_rows: tuple[int | tuple[int, int], ...] = ZX81_LANE_ROWS,
                  reserved_rect: tuple[int, int, int, int] | None = None) -> tuple[dict, dict]:
    locations = lane_locations(lane_rows)
    if routed is not None:
        validate_routed_rom(routed, lane_rows)
    paths = DATABASE_FILES
    sources = read_database(mistral_source) if isinstance(mistral_source, Path) else mistral_source
    die = sources[paths[1]].decode()
    columns = tuple(map(int, re.findall(r'\d+', _section(die, 'x to bit x'))))
    kinds = re.findall(r'T_\w+', _section(die, 'column types'))
    if columns != SX120F.x_to_bx or any(column >= len(kinds) or kinds[column] != 'T_M10K'
                                      for column, _ in locations):
        raise ValueError('Mistral sx120f column geometry differs from codec')
    if not re.search(r'7605\s*,\s*7024\s*,\s*// cram size', die):
        raise ValueError('Mistral sx120f CRAM geometry differs from codec')
    spans = re.search(r'sx120f_bel_spans_info\[\]\s*=\s*\{([^}]+)\}', die)
    if not spans:
        raise ValueError('Mistral sx120f site spans missing')
    numbers = [int(s, 0) for s in re.findall(r'0x[0-9a-f]+|\d+', spans[1])]
    legal: set[tuple[int, int]] = set()
    while numbers and numbers[0] != 255:
        low, high, count = numbers[:3]
        if len(numbers) < 3+2*count:
            raise ValueError('truncated Mistral site spans')
        for column, _ in locations:
            if low <= column <= high:
                for i in range(count):
                    legal.update((column, row) for row in range(numbers[3+2*i], numbers[4+2*i]+1))
        numbers = numbers[3+2*count:]
    if not set(locations) <= legal:
        raise ValueError('machine ROM placement is not legal in database')
    header = sources[paths[2]].decode()
    if not re.search(r'y\s*=\s*2\s*\+\s*86\s*\*\s*(?:pos2y\(pos\)|pos\.y\(\))', header):
        raise ValueError('Mistral tile row geometry differs from codec')
    blocks = rom_blocks(parse_ram_offsets(sources[paths[0]].decode()), lane_rows)
    destinations = set()
    for block in blocks:
        for word in block['word_bits']:
            for bit in word:
                if bit in destinations:
                    raise ValueError('duplicate M10K RAM destination across lanes')
                destinations.add(bit)
                if reserved_rect is not None:
                    x, y = bit % SX120F.cram_sx, bit // SX120F.cram_sx
                    if reserved_rect[0] <= x < reserved_rect[2] and reserved_rect[1] <= y < reserved_rect[3]:
                        raise ValueError('ROM destination overlaps reserved socket')
    loaded = rbf_load(base)
    for block in blocks:
        for word in block['word_bits']:
            for bit in word:
                if not (loaded.cram[bit >> 3] >> (bit & 7)) & 1:
                    raise ValueError(f"{block['bel']} is not a blank ROM INIT")
    mapping = dict(format=1, device=TARGET_DEVICE, encoding='m10k-1024x10-v1',
                   base_sha256=hashlib.sha256(base).hexdigest(),
                   source_size=1024 * len(blocks), blocks=blocks)
    evidence = dict(format=1, database_sha256={name: hashlib.sha256(data).hexdigest()
                                             for name, data in sources.items()})
    return mapping, evidence


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mistral-source', type=Path, required=True)
    parser.add_argument('--base', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    mapping, evidence = build_rom_map(args.mistral_source, args.base.read_bytes())
    encoded = (json.dumps(mapping, separators=(',', ':'))+'\n').encode()
    evidence['map_sha256'] = hashlib.sha256(encoded).hexdigest()
    evidence['base_sha256'] = mapping['base_sha256']
    args.output.write_bytes(encoded)
    args.output.with_suffix(args.output.suffix+'.provenance.json').write_text(json.dumps(evidence, indent=2)+'\n')


if __name__ == '__main__':
    main()
