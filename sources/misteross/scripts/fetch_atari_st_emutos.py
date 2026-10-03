#!/usr/bin/env python3
"""Fetch the original official EmuTOS 1.4 192 KiB US image for ST boot tests.

The GPLv2-or-later ROM is an external test input. No binary enters the source
closure or a sealed blank-ROM package. Both archive and extracted ROM are pinned.
"""
import argparse
import hashlib
from io import BytesIO
from pathlib import Path
import urllib.request
from zipfile import ZipFile

URL = 'https://downloads.sourceforge.net/project/emutos/emutos/1.4/emutos-192k-1.4.zip'
ARCHIVE_SHA256 = '59abac06a2d29b0864c5a7cfb2af65f022c337aed34188e174a9a08cc737e4bc'
ROM_SHA256 = '8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc'
SOURCE_COMMIT = '978e37569bff95841e42675d11fcc6799aad8483'
PREFIX = 'emutos-192k-1.4/'
MAX_ARCHIVE = 5 * 1024 * 1024

def fetch(destination: Path) -> Path:
    destination.mkdir(parents=True, exist_ok=True)
    rom_path = destination / 'etos192us.img'
    if rom_path.is_file() and hashlib.sha256(rom_path.read_bytes()).hexdigest() == ROM_SHA256:
        return rom_path
    archive_path = destination / 'emutos-192k-1.4.zip'
    archive = archive_path.read_bytes() if archive_path.is_file() else b''
    if hashlib.sha256(archive).hexdigest() != ARCHIVE_SHA256:
        with urllib.request.urlopen(URL, timeout=60) as response:
            archive = response.read(MAX_ARCHIVE + 1)
    if len(archive) > MAX_ARCHIVE or hashlib.sha256(archive).hexdigest() != ARCHIVE_SHA256:
        raise ValueError('official EmuTOS archive digest mismatch')
    with ZipFile(BytesIO(archive)) as z:
        rom = z.read(PREFIX + 'etos192us.img')
        license_text = z.read(PREFIX + 'doc/license.txt')
        readme = z.read(PREFIX + 'readme.txt')
    if len(rom) != 196608 or hashlib.sha256(rom).hexdigest() != ROM_SHA256:
        raise ValueError('official EmuTOS ROM digest mismatch')
    archive_path.write_bytes(archive)
    rom_path.write_bytes(rom)
    (destination / 'LICENSE.EmuTOS.txt').write_bytes(license_text)
    (destination / 'README.EmuTOS.txt').write_bytes(readme)
    return rom_path

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=Path(__file__).resolve().parents[1] / 'build/roms/emutos-1.4')
    args = parser.parse_args()
    print(fetch(args.output).resolve())
