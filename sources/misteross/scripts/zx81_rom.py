#!/usr/bin/env python3
"""Check a user-supplied ZX81 BASIC ROM and write it as a simulation hex or MIF.

ZX81-family firmware is user-supplied, so no ROM bytes are committed.
Simulations, ROM-link tests and the Quartus oracle take the 8 KiB edition 3
image by path (FES_ZX81_ROM) and accept only its digest.
"""
import argparse
import hashlib
from pathlib import Path

ROM_BYTES = 8192
EDITION3_SHA256 = '14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa'


def load(path: Path) -> bytes:
    data = Path(path).read_bytes()
    if len(data) != ROM_BYTES or hashlib.sha256(data).hexdigest() != EDITION3_SHA256:
        raise ValueError(f'{path} is not the 8 KiB ZX81 edition 3 BASIC ROM (sha256 {EDITION3_SHA256})')
    return data


def hex_text(rom: bytes) -> str:
    return ''.join(f'{byte:02x}\n' for byte in rom)


def mif_text(rom: bytes) -> str:
    rows = ''.join(f'{address:04X}: {byte:02X};\n' for address, byte in enumerate(rom))
    return (f'DEPTH = {len(rom)};\nWIDTH = 8;\nADDRESS_RADIX = HEX;\nDATA_RADIX = HEX;\n'
            f'CONTENT BEGIN\n{rows}END;\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rom', required=True, type=Path)
    parser.add_argument('--hex', type=Path)
    parser.add_argument('--mif', type=Path)
    args = parser.parse_args()
    rom = load(args.rom)
    for path, text in ((args.hex, hex_text), (args.mif, mif_text)):
        if path is not None:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text(rom))
