"""Closed first-party ZX81 firmware inputs for the DE25 diagnostic builders.

Current builds assemble the shared open ROM. Historical captures retain their
original tracked ROM inputs; their receipts are never rebound to new bytes.
"""
from pathlib import Path
import hashlib
import json
import subprocess
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
PREFIX = 'sources/misteross/'
LEGACY = tuple(PREFIX + 'cores/fes-zx81/rtl/' + name for name in ('zx8x.hex', 'zx8x.mif'))
INPUTS = tuple(PREFIX + name for name in (
    'boards/de25nano/zx81/firmware.py',
    'scripts/build_zx81_openrom.py', 'scripts/z80asm.py',
    'cores/fes-zx81/openrom/core.asm', 'cores/fes-zx81/openrom/edit.asm',
    'cores/fes-zx81/openrom/expr.asm', 'cores/fes-zx81/openrom/font.txt',
    'cores/fes-zx81/openrom/fp.asm', 'cores/fes-zx81/openrom/hw.asm',
    'cores/fes-zx81/openrom/init.asm', 'cores/fes-zx81/openrom/openrom.asm',
    'cores/fes-zx81/openrom/print.asm', 'cores/fes-zx81/openrom/stmt.asm',
    'cores/fes-zx81/openrom/sysvars.inc', 'cores/fes-zx81/openrom/tables.asm'))


def source_paths(revision=None):
    # Consult Git, not leftover generated files in a working tree. A historical
    # receipt still captures exactly the inputs its original recipe declared.
    tracked = subprocess.check_output(
        ['git', 'ls-tree', '-r', '--name-only', revision or 'HEAD', '--', *LEGACY],
        cwd=ROOT, text=True).splitlines()
    if tracked and set(tracked) != set(LEGACY):
        raise ValueError('incomplete historical ZX81 firmware input pair')
    return [ROOT / name for name in (LEGACY if tracked else INPUTS)]


def generate(sources, output):
    """Assemble from captured source bytes in a new private output directory."""
    output = Path(output)
    output.mkdir(parents=True, exist_ok=False)
    snapshot = output / 'source'
    for name in INPUTS:
        path = snapshot / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(sources[name])
    module = snapshot / PREFIX
    assembled = output / 'assembled'
    # A fresh interpreter prevents a previously imported assembler from another
    # checkout replacing the captured module selected for this build.
    subprocess.run([sys.executable, str(module / 'scripts/build_zx81_openrom.py'),
                    '--out', str(assembled)], cwd=module, check=True)
    rom = (assembled / 'zx81-openrom.bin').read_bytes()
    if len(rom) != 8192:
        raise ValueError('open ZX81 firmware must be exactly 8192 bytes')
    padded = rom + bytes([255]) * 8192
    hexadecimal = ''.join(f'{byte:02x}\n' for byte in padded).encode()
    rows = ''.join(f'{address:04X}: {byte:02X};\n' for address, byte in enumerate(padded))
    mif = ('DEPTH = 16384;\nWIDTH = 8;\nADDRESS_RADIX = HEX;\nDATA_RADIX = HEX;\n'
           'CONTENT BEGIN\n' + rows + 'END;\n').encode()
    files = {'zx8x.hex': hexadecimal, 'zx8x.mif': mif}
    symbols = {name: int(address, 16) for address, name in
               (line.split() for line in (assembled / 'zx81-openrom.sym').read_text().splitlines())}
    sha = lambda data: hashlib.sha256(data).hexdigest()
    record = {'kind': 'zx81-openrom', 'rom_bytes': len(rom), 'storage_bytes': len(padded),
              'rom_sha256': sha(rom), 'unreachable_padding': '0xff above the 8 KiB ZX81 window',
              'simulation_symbols': {name: symbols[name] for name in ('get_key_raw', 'edit_line.enter')},
              'source_sha256': {name: sha(sources[name]) for name in INPUTS},
              'generated_sha256': {name: sha(data) for name, data in files.items()}}
    (output / 'firmware.json').write_text(json.dumps(record, sort_keys=True, indent=2) + '\n')
    return files, record


def simulation_header(record):
    """Observe real editor entry points from this assembly, never guessed PCs."""
    symbols = record['simulation_symbols']
    return ('#pragma once\n'
            f'constexpr unsigned Zx81EditorPoll = 0x{symbols["get_key_raw"]:04x};\n'
            f'constexpr unsigned Zx81Newline = 0x{symbols["edit_line.enter"]:04x};\n').encode()
