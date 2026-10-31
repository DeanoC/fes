#!/usr/bin/env python3
"""Build the pc386 system BIOS and VGA BIOS from vendored source (host only).

The two images are the Bochs ROM BIOS (`rombios.c`, the BIOS-bochs-legacy build)
and the LGPL VGABios (`vgabios.c` without VBE or the PCI BIOS), both
LGPL-2.1-or-later, vendored under cores/fes-pc386/firmware/ with `source.json`
manifests that record the upstream revision, every file's hash and the dated
local changes. They are compiled with dev86 (bcc, as86; GPL-2.0) fetched at a
pinned revision into the ignored build directory only when --download is given,
and with the host gcc as the preprocessor, exactly as the upstream makefiles
build them. The resulting images must match the digests in
cores/fes-pc386/firmware/firmware.lock; --update-lock rewrites them after a
deliberate source or toolchain change.

Example:
  python3 scripts/build_pc386_firmware.py --download

Outputs build/firmware/pc386/pc386-bios.bin (64 KiB, mapped at F0000) and
pc386-vgabios.bin (32 KiB, mapped at C0000), and pc386-rom.hex, the two as
the M10K contents pc386_rom.sv reads: 6144 lines of 128 bits, the VGA BIOS
(padded with FFh) then the system BIOS, byte 0 in bits 7:0.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
FIRMWARE = ROOT / 'cores/fes-pc386/firmware'
LOCK = FIRMWARE / 'firmware.lock'
BUILD = ROOT / 'build/firmware/pc386'

DEV86_REPOSITORY = 'https://codeberg.org/jbruchon/dev86'
DEV86_REVISION = '0b72e907f7a282aaa777a768b8b688e41c77fc93'  # tag v1.0.1
DEV86_VERSION = '0.16.21'  # the VERSION its top-level Makefile passes down
DEV86_TARGETS = ('bcc86', 'cpp', 'as86', 'ld86')

IMAGES = {
    'bios': {'file': 'pc386-bios.bin', 'bytes': 0x10000},
    'vgabios': {'file': 'pc386-vgabios.bin', 'bytes': 0x8000},
}
ROM_HEX = BUILD / 'pc386-rom.hex'
ROM_BEAT = 16


class FirmwareError(RuntimeError):
    pass


def _run(command, cwd, log, env=None, stdout=None):
    with log.open('a') as destination:
        destination.write('$ ' + ' '.join(map(str, command)) + '\n')
        destination.flush()
        result = subprocess.run(command, cwd=cwd, env=env, stdout=stdout or destination,
                                stderr=destination)
    if result.returncode:
        tail = log.read_text().splitlines()[-30:]
        raise FirmwareError(f'{command[0]} failed in {cwd}; see {log}\n' + '\n'.join(tail))


def _git(source, *args):
    return subprocess.check_output(['git', '-C', str(source), *args], text=True).strip()


def read_lock() -> dict:
    return json.loads(LOCK.read_text())


def verify_sources(name: str) -> Path:
    """Check a vendored tree against its source.json and return it."""
    tree = FIRMWARE / name
    manifest = json.loads((tree / 'source.json').read_text())
    listed = {item['path'] for item in manifest['files']}
    for item in manifest['files']:
        digest = hashlib.sha256((tree / item['path']).read_bytes()).hexdigest()
        if digest != item['sha256']:
            raise FirmwareError(f'vendored {name}/{item["path"]} does not match source.json')
    extra = {p.name for p in tree.iterdir() if p.is_file()} - listed - {'source.json', '.gitignore'}
    if extra:
        raise FirmwareError(f'vendored {name} has files source.json does not list: {sorted(extra)}')
    return tree


def dev86(download: bool, jobs: int) -> Path:
    """Return the bin/ and lib/bcc directories of the pinned dev86, building it once."""
    tree = BUILD / ('dev86-' + DEV86_REVISION)
    stamp = tree / 'fes-firmware.stamp'
    if stamp.is_file():
        return tree
    if not (tree / '.git').is_dir():
        if not download:
            raise FirmwareError(f'dev86 {DEV86_REVISION} is not in {tree}; pass --download to fetch it')
        partial = tree.with_name(tree.name + '.partial')
        shutil.rmtree(partial, ignore_errors=True)
        partial.mkdir(parents=True)
        print(f'Fetching dev86 {DEV86_REVISION} (GPL-2.0)', flush=True)
        for args in (('init', '-q'), ('fetch', '-q', '--depth', '1', DEV86_REPOSITORY, DEV86_REVISION),
                     ('checkout', '-q', 'FETCH_HEAD')):
            subprocess.run(['git', '-C', str(partial), *args], check=True)
        partial.rename(tree)
    if _git(tree, 'rev-parse', 'HEAD') != DEV86_REVISION:
        raise FirmwareError(f'{tree} is not at the pinned dev86 revision')
    log = tree / 'fes-firmware-build.log'
    log.write_text('')
    print('Building dev86 (bcc, as86)', flush=True)
    _run(['make', 'make.fil'], tree, log)
    env = dict(os.environ, PATH=f'{tree / "bin"}:{os.environ["PATH"]}')
    _run(['make', '-j', str(jobs), '-f', 'make.fil', f'VERSION={DEV86_VERSION}', f'TOPDIR={tree}',
          *DEV86_TARGETS], tree, log, env)
    for tool in ('bin/bcc', 'bin/as86', 'lib/bcc/bcc-cc1', 'lib/bcc/bcc-cpp'):
        if not (tree / tool).is_file():
            raise FirmwareError(f'dev86 build did not produce {tool}; see {log}')
    stamp.write_text(DEV86_REVISION + '\n')
    return tree


def _bcc_build(work: Path, source: str, defines: list[str], as86_undefined: str, log: Path,
               env: dict) -> Path:
    """gcc -E, bcc -S, strip section directives, as86: the upstream makefiles' rule."""
    stem = Path(source).stem
    with (work / f'_{stem}_.c').open('w') as preprocessed:
        _run(['gcc', '-E', '-P', *defines, source], work, log, env, stdout=preprocessed)
    _run(['bcc', '-o', f'{stem}.s', '-C-c', '-D__i86__', '-0', '-S', f'_{stem}_.c'], work, log, env)
    lines = (work / f'{stem}.s').read_text().splitlines(keepends=True)
    stripped = [line[5:] if line.startswith(('.text', '.data')) else line for line in lines]
    (work / f'_{stem}_.s').write_text(''.join(stripped))
    _run(['as86', f'_{stem}_.s', '-b', f'{stem}.bin', as86_undefined, '-w-', '-g', '-0', '-j', '-O',
          '-l', f'{stem}.txt'], work, log, env)
    _run(['gcc', '-o', 'biossums', 'biossums.c'], work, log, env)
    _run(['./biossums', f'{stem}.bin'], work, log, env)
    return work / f'{stem}.bin'


def build(download: bool = False, jobs: int = 4, update_lock: bool = False) -> dict[str, Path]:
    """Build both images into BUILD and check them against the lock; return their paths."""
    lock = read_lock()
    if lock['dev86']['revision'] != DEV86_REVISION:
        raise FirmwareError('firmware.lock names a different dev86 revision than this script')
    tools = dev86(download, jobs)
    env = dict(os.environ, PATH=f'{tools / "bin"}:{tools / "lib/bcc"}:{os.environ["PATH"]}',
               LC_ALL='C')
    work = BUILD / 'work'
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)
    log = BUILD / 'firmware-build.log'
    log.write_text('')

    # rombios.h includes ../bxversion.h, which Bochs' configure writes.
    bios = work / 'bios'
    shutil.copytree(verify_sources('rombios'), bios)
    version = (bios / 'bxversion.h.in').read_text()
    for key, value in lock['rombios']['bxversion'].items():
        version = version.replace(f'@{key}@', value)
    (work / 'bxversion.h').write_text(version)
    bios_image = _bcc_build(bios, 'rombios.c',
                            ['-DLEGACY', f'-DBIOS_BUILD_DATE="{lock["rombios"]["build_date"]}"'],
                            '-u-', log, env)

    vga = work / 'vgabios'
    shutil.copytree(verify_sources('vgabios'), vga)
    vga_image = _bcc_build(vga, 'vgabios.c',
                           [f'-DVGABIOS_VERS="{lock["vgabios"]["version"]}"',
                            f'-DVGABIOS_DATE="{lock["vgabios"]["date"]}"'],
                           '-u', log, env)

    outputs = {}
    for name, built in (('bios', bios_image), ('vgabios', vga_image)):
        data = built.read_bytes()
        if len(data) != IMAGES[name]['bytes']:
            raise FirmwareError(f'{name} image is {len(data)} bytes, expected {IMAGES[name]["bytes"]}')
        digest = hashlib.sha256(data).hexdigest()
        if update_lock:
            lock['images'][name] = {'file': IMAGES[name]['file'], 'bytes': len(data), 'sha256': digest}
        elif lock['images'].get(name, {}).get('sha256') != digest:
            raise FirmwareError(f'{name} image {digest} does not match firmware.lock; '
                                'rerun with --update-lock after a deliberate change')
        destination = BUILD / IMAGES[name]['file']
        destination.write_bytes(data)
        outputs[name] = destination
    # The as86 listings map addresses to source lines when debugging a boot.
    shutil.copy2(bios / 'rombios.txt', BUILD / 'pc386-bios.txt')
    shutil.copy2(vga / 'vgabios.txt', BUILD / 'pc386-vgabios.txt')
    if update_lock:
        LOCK.write_text(json.dumps(lock, indent=2) + '\n')
    return outputs


def images(download: bool = False, jobs: int = 4) -> dict[str, Path]:
    """Return locked images, building them when missing or stale."""
    lock = read_lock()
    paths = {name: BUILD / IMAGES[name]['file'] for name in IMAGES}
    if all(path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest() == lock['images'][name]['sha256']
           for name, path in paths.items()):
        for name in ('rombios', 'vgabios'):
            verify_sources(name)
        return paths
    return build(download, jobs)


def rom_hex(paths: dict[str, Path]) -> str:
    """pc386_rom.sv's contents: the VGA BIOS then the system BIOS in 16-byte beats."""
    vga = paths['vgabios'].read_bytes()
    if len(vga) > IMAGES['vgabios']['bytes']:
        raise FirmwareError(f'the VGA BIOS is larger than {IMAGES["vgabios"]["bytes"]} bytes')
    bios = paths['bios'].read_bytes()
    if len(bios) != IMAGES['bios']['bytes']:
        raise FirmwareError(f'the system BIOS must be {IMAGES["bios"]["bytes"]} bytes')
    data = vga.ljust(IMAGES['vgabios']['bytes'], b'\xff') + bios
    return ''.join(data[at:at + ROM_BEAT][::-1].hex() + '\n' for at in range(0, len(data), ROM_BEAT))


def write_rom_hex(paths: dict[str, Path]) -> Path:
    """Write ROM_HEX from the locked images and return it."""
    text = rom_hex(paths)
    if not ROM_HEX.is_file() or ROM_HEX.read_text() != text:
        ROM_HEX.write_text(text)
    return ROM_HEX


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true', help='fetch the pinned dev86 if it is missing')
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 4)
    parser.add_argument('--update-lock', action='store_true',
                        help='record the built digests in firmware.lock instead of checking them')
    args = parser.parse_args(argv)
    try:
        outputs = build(args.download, args.jobs, args.update_lock)
    except FirmwareError as error:
        print(f'build_pc386_firmware: {error}', file=sys.stderr)
        return 1
    outputs['rom'] = write_rom_hex(outputs)
    for name, path in outputs.items():
        print(f'{name}: {path} {hashlib.sha256(path.read_bytes()).hexdigest()}')
    return 0


if __name__ == '__main__':
    sys.exit(main())
