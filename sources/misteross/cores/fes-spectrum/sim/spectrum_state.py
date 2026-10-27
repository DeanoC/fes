#!/usr/bin/env python3
"""Convert a ZX Spectrum .sna or .z80 snapshot into the simulation state format.

The output is read by sim/run_tb.cpp: register lines, the paging ports, the
border and every 16 KiB RAM bank as hex bytes. 48K snapshots name banks 5, 2
and 0 (the 128K numbering of $4000, $8000 and $C000). Formats follow the
World of Spectrum .sna and .z80 (versions 1, 2 and 3) descriptions.
"""
from __future__ import annotations

import argparse
from pathlib import Path


class SnapshotError(ValueError):
    pass


def word(data: bytes, offset: int) -> int:
    return data[offset] | data[offset + 1] << 8


def read_sna(data: bytes) -> dict:
    if len(data) not in (49179, 131103, 147487):
        raise SnapshotError(f'unexpected .sna size {len(data)}')
    h = data[:27]
    regs = {
        'i': h[0], 'hl_': word(h, 1), 'de_': word(h, 3), 'bc_': word(h, 5), 'af_': word(h, 7),
        'hl': word(h, 9), 'de': word(h, 11), 'bc': word(h, 13), 'iy': word(h, 15), 'ix': word(h, 17),
        'iff2': (h[19] >> 2) & 1, 'r': h[20], 'af': word(h, 21), 'sp': word(h, 23), 'im': h[25] & 3,
    }
    regs['iff1'] = regs['iff2']
    border = h[26] & 7
    ram = data[27:27 + 49152]
    banks = {5: ram[0:16384], 2: ram[16384:32768]}
    state = {'regs': regs, 'border': border, 'banks': banks}
    if len(data) == 49179:
        banks[0] = ram[32768:49152]
        # A 48K .sna stores PC on the stack, as RETN would pop it.
        sp = regs['sp']
        memory = bytearray(65536)
        memory[0x4000:] = ram
        regs['pc'] = memory[sp] | memory[(sp + 1) & 0xffff] << 8
        regs['sp'] = (sp + 2) & 0xffff
        state['model'] = '48'
        return state
    tail = data[27 + 49152:]
    regs['pc'] = word(tail, 0)
    port = tail[2]
    paged = port & 7
    banks[paged] = ram[32768:49152]
    rest = tail[4:]
    order = [b for b in (0, 1, 3, 4, 6, 7) if b != paged]
    for index, bank in enumerate(order):
        chunk = rest[index * 16384:(index + 1) * 16384]
        if len(chunk) == 16384:
            banks[bank] = chunk
    state['port7ffd'] = port
    state['model'] = '128'
    return state


def decompress_z80(block: bytes, size: int | None) -> bytes:
    out = bytearray()
    i = 0
    while i < len(block):
        if size is None and block[i:i + 4] == b'\x00\xed\xed\x00':
            break
        if i + 3 < len(block) and block[i] == 0xED and block[i + 1] == 0xED:
            out += bytes([block[i + 3]]) * block[i + 2]
            i += 4
        else:
            out.append(block[i])
            i += 1
        if size is not None and len(out) >= size:
            break
    return bytes(out)


def read_z80(data: bytes) -> dict:
    h = data[:30]
    regs = {
        'af': h[0] << 8 | h[1], 'bc': word(h, 2), 'hl': word(h, 4), 'pc': word(h, 6),
        'sp': word(h, 8), 'i': h[10], 'r': (h[11] & 0x7f) | ((h[12] & 1) << 7),
        'de': word(h, 13), 'bc_': word(h, 15), 'de_': word(h, 17), 'hl_': word(h, 19),
        'af_': h[21] << 8 | h[22], 'iy': word(h, 23), 'ix': word(h, 25),
        'iff1': 1 if h[27] else 0, 'iff2': 1 if h[28] else 0, 'im': h[29] & 3,
    }
    flags = 1 if h[12] == 255 else h[12]
    border = (flags >> 1) & 7
    state = {'regs': regs, 'border': border, 'banks': {}}
    if regs['pc'] != 0:
        memory = decompress_z80(data[30:], 49152) if flags & 0x20 else data[30:30 + 49152]
        if len(memory) != 49152:
            raise SnapshotError('short version 1 memory image')
        state['banks'] = {5: memory[0:16384], 2: memory[16384:32768], 0: memory[32768:49152]}
        state['model'] = '48'
        return state
    extra = word(data, 30)
    hx = data[32:32 + extra]
    regs['pc'] = word(hx, 0)
    hardware = hx[2]
    version3 = extra >= 54
    if version3:
        # Version 3 inserted 48K + M.G.T. as type 3, moving 128K to 4.
        model = {0: '48', 1: '48', 3: '48', 4: '128', 5: '128', 6: '128', 7: '+3', 8: '+3',
                 9: 'pentagon', 12: '+2', 13: '+2A'}.get(hardware)
    else:
        model = {0: '48', 1: '48', 3: '128', 4: '128', 7: '+3', 8: '+3', 9: 'pentagon',
                 12: '+2', 13: '+2A'}.get(hardware)
    if model is None:
        raise SnapshotError(f'unsupported .z80 hardware type {hardware}')
    if hx[5] & 0x80 and model in ('48',):
        raise SnapshotError('modified-hardware .z80 snapshots are not supported')
    state['model'] = model
    if model != '48':
        state['port7ffd'] = hx[3]
        if extra >= 55 and model in ('+3', '+2A'):
            state['port1ffd'] = hx[54] & 7
    offset = 32 + extra
    while offset + 3 <= len(data):
        length = word(data, offset)
        page = data[offset + 2]
        offset += 3
        if length == 0xffff:
            block = data[offset:offset + 16384]
            offset += 16384
        else:
            block = decompress_z80(data[offset:offset + length], 16384)
            offset += length
        if len(block) != 16384:
            raise SnapshotError(f'page {page} decompresses to {len(block)} bytes')
        if model == '48':
            bank = {8: 5, 4: 2, 5: 0}.get(page)
        else:
            bank = page - 3 if 3 <= page <= 10 else None
        if bank is not None:
            state['banks'][bank] = block
    return state


def write_state(state: dict, path: Path) -> None:
    lines = [f'reg {name} {value:x}' for name, value in sorted(state['regs'].items())]
    if 'port7ffd' in state:
        lines.append(f'port7ffd {state["port7ffd"]:x}')
    if 'port1ffd' in state:
        lines.append(f'port1ffd {state["port1ffd"]:x}')
    lines.append(f'border {state["border"]}')
    for bank, data in sorted(state['banks'].items()):
        lines.append(f'bank {bank}')
        for row in range(0, 16384, 32):
            lines.append(' '.join(f'{b:02x}' for b in data[row:row + 32]))
    path.write_text('\n'.join(lines) + '\n', encoding='ascii')


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('snapshot', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    data = args.snapshot.read_bytes()
    state = read_sna(data) if args.snapshot.suffix.lower() == '.sna' else read_z80(data)
    write_state(state, args.output)
    print(state['model'])


if __name__ == '__main__':
    main()
