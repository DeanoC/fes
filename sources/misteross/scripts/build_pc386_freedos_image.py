#!/usr/bin/env python3
"""Build the FreeDOS hard-disk image the pc386 machine simulation boots.

The image is a simulation fixture only. It is written into the ignored build
directory and is never distributed, packaged or committed. Distributing
FreeDOS images, with the GPL source obligations that brings, is the FogCast
disk builder's job (docs/pc386-machine-design.md, "FreeDOS in the FogCast disk
builder"); this tool uses the same pinned FreeDOS 1.4 package archives so the
two agree.

No network access occurs unless --download is given. The packages are fetched
at the commits in cores/fes-pc386/sim/freedos.lock (each package repository's
FreeDOS-v1.4 tag) and every file used is checked against its pinned SHA-256:

  kernel   KERNL386.SYS, installed as KERNEL.SYS, and boot/boot.asm, the FAT16
           boot sector, assembled with the test386 runner's pinned NASM
  freecom  COMMAND.COM
  fdisk    bootnorm.c, the bytes of Free FDISK's master boot record

The disk is 65 cylinders of 16 heads and 63 sectors (32.0 MiB). One active
FAT16 partition (type 06h) starts at cylinder 0, head 1; mtools formats it and
the kernel's boot code is laid around the BPB mtools wrote, as FreeDOS's SYS
does. The partition entry's end CHS states the geometry, which is how the
machine (and FogCast) infer it. The image is reproducible: file times, the
volume serial and the label are fixed.

Example:
  python3 scripts/build_pc386_freedos_image.py --download
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import test_fes_i386_test386 as test386

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'cores/fes-pc386/sim/freedos.lock'
BUILD = ROOT / 'build/sim/fes-pc386/freedos'
IMAGE = ROOT / 'build/sim/fes-pc386/freedos.img'

CYLINDERS, HEADS, SPT = 65, 16, 63
TOTAL_SECTORS = CYLINDERS * HEADS * SPT
PARTITION_START = SPT                  # cylinder 0, head 1, sector 1
PARTITION_SECTORS = TOTAL_SECTORS - PARTITION_START
VOLUME_SERIAL = 0x1386_0026
# 2026-10-26 12:00 UTC: the file times on the image.
FILE_TIME = 1793016000

FDCONFIG = 'FILES=20\r\nBUFFERS=20\r\nSHELL=C:\\COMMAND.COM C:\\ /E:1024 /P=C:\\AUTOEXEC.BAT\r\n'
AUTOEXEC = '@ECHO OFF\r\nPROMPT $P$G\r\nPATH C:\\\r\nVER\r\n'


class ImageError(RuntimeError):
    pass


def input_sha256() -> str:
    """Identify the lock and source code that determine the disk image."""
    digest = hashlib.sha256()
    for path in (LOCK.resolve(), Path(__file__).resolve(), Path(test386.__file__).resolve()):
        digest.update(path.relative_to(ROOT).as_posix().encode())
        digest.update(b'\0')
        digest.update(path.read_bytes())
        digest.update(b'\0')
    return digest.hexdigest()


def image_is_current(image: Path | None = None) -> bool:
    """Check both the fixture's input identity and the image bytes."""
    image = IMAGE if image is None else image
    sidecar = image.with_suffix('.json')
    try:
        record = json.loads(sidecar.read_text())
        image_digest = hashlib.sha256(image.read_bytes()).hexdigest()
    except (OSError, ValueError):
        return False
    if not isinstance(record, dict):
        return False
    return record.get('inputs_sha256') == input_sha256() and record.get('sha256') == image_digest


def lock() -> dict:
    return json.loads(LOCK.read_text())


def package(name: str, spec: dict, download: bool) -> Path:
    """Return the pinned checkout of one FreeDOS package, its files verified."""
    tree = BUILD / f'{name}-{spec["commit"]}'
    if not (tree / '.git').is_dir():
        if not download:
            raise ImageError(f'FreeDOS {name} {spec["commit"]} is not in {tree}; pass --download')
        partial = tree.with_name(tree.name + '.partial')
        shutil.rmtree(partial, ignore_errors=True)
        partial.mkdir(parents=True)
        print(f'Fetching FreeDOS {name} {spec["version"]} ({spec["license"]})', flush=True)
        for args in (('init', '-q'), ('fetch', '-q', '--depth', '1', spec['repository'], spec['commit']),
                     ('checkout', '-q', 'FETCH_HEAD')):
            subprocess.run(['git', '-C', str(partial), *args], check=True)
        partial.rename(tree)
    head = subprocess.check_output(['git', '-C', str(tree), 'rev-parse', 'HEAD'], text=True).strip()
    if head != spec['commit']:
        raise ImageError(f'{tree} is not at the pinned commit')
    for path, digest in spec['files'].items():
        if hashlib.sha256((tree / path).read_bytes()).hexdigest() != digest:
            raise ImageError(f'FreeDOS {name}/{path} does not match freedos.lock')
    return tree


def mbr_code(bootnorm: Path) -> bytes:
    """The 446 bytes of boot code in Free FDISK's bootnormal_code array."""
    text = bootnorm.read_text()
    body = re.search(r'bootnormal_code\[512\]\s*=\s*\{([^}]*)\}', text)
    if not body:
        raise ImageError('bootnorm.c has no bootnormal_code array')
    data = bytes(int(value, 0) for value in body.group(1).replace('\n', ' ').split(',') if value.strip())
    if len(data) != 512 or data[510:] != b'\x55\xaa':
        raise ImageError('bootnormal_code is not a 512-byte boot sector')
    return data[:446]


def chs(lba: int) -> bytes:
    cylinder, rest = divmod(lba, HEADS * SPT)
    head, sector = divmod(rest, SPT)
    return bytes((head, ((cylinder >> 2) & 0xc0) | (sector + 1), cylinder & 0xff))


def boot_sector(kernel: Path, nasm: Path, work: Path) -> bytes:
    output = work / 'fat16com.bin'
    result = subprocess.run([str(nasm), '-dISFAT16', '-f', 'bin', str(kernel / 'SOURCE/KERNEL/boot/boot.asm'),
                             '-o', str(output)], capture_output=True, text=True)
    if result.returncode:
        raise ImageError('NASM failed on boot.asm:\n' + result.stderr)
    data = output.read_bytes()
    if len(data) != 512 or data[510:] != b'\x55\xaa':
        raise ImageError('boot.asm did not assemble to a boot sector')
    return data


def mtools(*command: str, env: dict) -> None:
    result = subprocess.run(list(command), capture_output=True, text=True, env=env)
    if result.returncode:
        raise ImageError(' '.join(command) + ' failed:\n' + result.stdout + result.stderr)


def build(download: bool = False, jobs: int = 4) -> Path:
    packages = lock()['packages']
    trees = {name: package(name, spec, download) for name, spec in packages.items()}
    nasm = test386.nasm(download, jobs)
    work = BUILD / 'work'
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)

    image = bytearray(TOTAL_SECTORS * 512)
    mbr = bytearray(512)
    mbr[:446] = mbr_code(trees['fdisk'] / 'SOURCE/FDISK/bootnorm.c')
    entry = bytes((0x80,)) + chs(PARTITION_START) + bytes((0x06,)) + chs(TOTAL_SECTORS - 1) + \
        PARTITION_START.to_bytes(4, 'little') + PARTITION_SECTORS.to_bytes(4, 'little')
    mbr[0x1be:0x1ce] = entry
    mbr[510:] = b'\x55\xaa'
    image[:512] = mbr
    disk = work / 'disk.img'
    disk.write_bytes(image)

    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', SOURCE_DATE_EPOCH=str(FILE_TIME), TZ='UTC')
    volume = f'{disk}@@{PARTITION_START * 512}'
    mtools('mformat', '-i', volume, '-h', str(HEADS), '-s', str(SPT),
           '-H', str(PARTITION_START), '-T', str(PARTITION_SECTORS), '-N', f'{VOLUME_SERIAL:08x}',
           '-v', 'FREEDOS', '::', env=env)

    # The kernel's boot code around the BPB mformat wrote, as SYS installs it.
    data = bytearray(disk.read_bytes())
    offset = PARTITION_START * 512
    bpb = data[offset + 0x0b:offset + 0x3e]
    sector = bytearray(boot_sector(trees['kernel'], nasm, work))
    sector[0x0b:0x3e] = bpb
    sector[0x24] = 0x80                     # BIOS drive number: the first hard disk
    if bytes(sector[0x36:0x3e]).rstrip() != b'FAT16':
        raise ImageError('mformat did not make a FAT16 volume')
    data[offset:offset + 512] = sector
    disk.write_bytes(data)

    files = {
        'KERNEL.SYS': trees['kernel'] / 'BIN/KERNL386.SYS',
        'COMMAND.COM': trees['freecom'] / 'BIN/COMMAND.COM',
    }
    staged = work / 'files'
    staged.mkdir()
    for target, source in files.items():
        shutil.copyfile(source, staged / target)
    (staged / 'FDCONFIG.SYS').write_bytes(FDCONFIG.encode())
    (staged / 'AUTOEXEC.BAT').write_bytes(AUTOEXEC.encode())
    # KERNEL.SYS first, as SYS places it, then the rest in a fixed order.
    for name in ('KERNEL.SYS', 'COMMAND.COM', 'FDCONFIG.SYS', 'AUTOEXEC.BAT'):
        os.utime(staged / name, (FILE_TIME, FILE_TIME))
        mtools('mcopy', '-m', '-i', volume, str(staged / name), '::' + name, env=env)

    IMAGE.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(disk, IMAGE)
    record = {
        'image': str(IMAGE.relative_to(ROOT)),
        'sha256': hashlib.sha256(IMAGE.read_bytes()).hexdigest(),
        'inputs_sha256': input_sha256(),
        'geometry': {'cylinders': CYLINDERS, 'heads': HEADS, 'sectors_per_track': SPT},
        'packages': {name: {'commit': spec['commit'], 'version': spec['version'], 'license': spec['license']}
                     for name, spec in packages.items()},
        'distribution': 'simulation fixture only; never distributed',
    }
    IMAGE.with_suffix('.json').write_text(json.dumps(record, indent=2) + '\n')
    return IMAGE


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true', help='fetch the pinned packages and NASM if missing')
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 4)
    args = parser.parse_args(argv)
    try:
        image = build(args.download, args.jobs)
    except (ImageError, test386.Test386Error) as error:
        print(f'build_pc386_freedos_image: {error}', file=sys.stderr)
        return 1
    print(f'{image} {hashlib.sha256(image.read_bytes()).hexdigest()}')
    return 0


if __name__ == '__main__':
    sys.exit(main())
