#!/usr/bin/env python3
"""Build a pinned 8086 FreeDOS 360 KiB floppy for PC/XT host simulation.

The image and downloaded third-party material stay in the ignored build tree;
they are not a package, release or license to redistribute those materials.
No network access occurs without --download. The FreeDOS BIOS reads this FAT12
floppy through ordinary INT 13h CHS requests and the emulated floppy controller.
"""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'cores/fes-pcxt/sim/freedos.lock'
BUILD = ROOT / 'build/sim/fes-pcxt/freedos'
IMAGE = BUILD / 'freedos-360.img'
SIZE = 40 * 2 * 9 * 512
FILE_TIME = 1793016000  # 2026-10-26 12:00:00 UTC; DOS timestamps have 2-second precision.
VOLUME_SERIAL = '50435854'
FILES = ('KERNEL.SYS', 'COMMAND.COM', 'FDCONFIG.SYS', 'AUTOEXEC.BAT', 'FESXT.TXT')
FDCONFIG = b'SHELL=A:\\COMMAND.COM /P\r\n'
AUTOEXEC = b'@ECHO OFF\r\nPROMPT $P$G\r\n'
MARKER = b'FES PC/XT DOS boot\r\n'


class ImageError(ValueError):
    pass


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def run(command, *, cwd=None, env=None):
    result = subprocess.run([str(c) for c in command], cwd=cwd, env=env,
                            capture_output=True, text=True)
    if result.returncode:
        raise ImageError('command failed: ' + ' '.join(map(str, command)) + '\n' + result.stdout + result.stderr)
    return result.stdout


def lock():
    return json.loads(LOCK.read_text())


def package(name, spec, download):
    """Fetch exactly one pinned release source checkout, then verify its files."""
    tree = BUILD / f'{name}-{spec["commit"]}'
    if not tree.is_dir():
        if not download:
            raise ImageError(f'{name} package missing; use --download or populate {tree}')
        with tempfile.TemporaryDirectory(prefix=f'.{name}-', dir=BUILD) as temporary:
            work = Path(temporary) / 'checkout'
            work.mkdir()
            run(['git', 'init', '-q'], cwd=work)
            run(['git', 'fetch', '-q', '--depth', '1', spec['repository'], spec['commit']], cwd=work)
            run(['git', 'checkout', '-q', 'FETCH_HEAD'], cwd=work)
            if run(['git', 'rev-parse', 'HEAD'], cwd=work).strip() != spec['commit']:
                raise ImageError(f'{name} source revision differs from lock')
            for relative, wanted in spec['files'].items():
                if digest((work / relative).read_bytes()) != wanted:
                    raise ImageError(f'{name}/{relative} differs from lock')
            work.rename(tree)
    if run(['git', 'rev-parse', 'HEAD'], cwd=tree).strip() != spec['commit']:
        raise ImageError(f'{name} source revision differs from lock')
    for relative, wanted in spec['files'].items():
        if digest((tree / relative).read_bytes()) != wanted:
            raise ImageError(f'{name}/{relative} differs from lock')
    return tree


def nasm(spec, download, jobs):
    """Build a private, checksum-pinned NASM release when explicitly requested."""
    version = spec['version']
    tree = BUILD / ('nasm-' + version)
    binary = tree / 'nasm'
    if binary.is_file():
        return binary
    archive = BUILD / ('nasm-' + version + '.tar.xz')
    if not archive.exists():
        if not download:
            raise ImageError('pinned NASM missing; use --download')
        request = urllib.request.Request(spec['url'], headers={'User-Agent': 'FES-PCXT-FreeDOS'})
        with urllib.request.urlopen(request, timeout=120) as response:
            data = response.read(16 * 1024 * 1024)
        if digest(data) != spec['sha256']:
            raise ImageError('NASM source archive differs from lock')
        archive.write_bytes(data)
    if digest(archive.read_bytes()) != spec['sha256']:
        raise ImageError('NASM source archive differs from lock')
    with tempfile.TemporaryDirectory(prefix='.nasm-', dir=BUILD) as temporary:
        work = Path(temporary)
        with tarfile.open(archive) as source:
            source.extractall(work, filter='data')
        extracted = work / ('nasm-' + version)
        run(['./configure'], cwd=extracted)
        run(['make', '-j', str(jobs), 'nasm'], cwd=extracted)
        check_nasm_version(extracted / 'nasm', version)
        extracted.rename(tree)
    return binary


def check_nasm_version(assembler, version):
    actual = run([assembler, '-v']).strip()
    expected = 'NASM version ' + version
    if actual != expected and not actual.startswith(expected + ' '):
        raise ImageError('NASM executable version differs from lock')


def tools_path(name):
    path = shutil.which(name)
    if not path:
        raise ImageError(f'{name} is required')
    # mtools dispatches by argv[0]; retain the mformat/mcopy symlink name.
    return Path(path).absolute()


def read_root(data):
    """Read the fixed FAT12 root without asking mtools to interpret the image."""
    root = data[5 * 512:12 * 512]
    result = []
    for i in range(0, len(root), 32):
        entry = root[i:i+32]
        if not entry[0]:
            break
        if entry[0] == 0xe5 or entry[11] & 0x08:
            continue
        if entry[11] & 0x10:
            raise ImageError('unexpected directory in boot volume')
        result.append((entry[:11], int.from_bytes(entry[26:28], 'little'),
                       int.from_bytes(entry[28:32], 'little')))
    return result


def read_fat12_file(data, first, size):
    """Follow FAT12 chains with bounds, overlap and premature-end checks."""
    fat = data[512:3*512]
    found, cluster = set(), first
    output = bytearray()
    while len(output) < size:
        if cluster < 2 or cluster >= 356 or cluster in found:
            raise ImageError('invalid FAT12 cluster chain')
        found.add(cluster)
        start = (12 + (cluster - 2) * 2) * 512
        output.extend(data[start:start + 1024])
        offset = cluster + cluster // 2
        pair = int.from_bytes(fat[offset:offset+2], 'little')
        cluster = (pair >> 4) if cluster & 1 else (pair & 0xfff)
    if cluster < 0xff8:
        raise ImageError('FAT12 file has an unexpected trailing cluster')
    return bytes(output[:size]), found


def validate_image(data, boot, files):
    """Check the entire fixture, including BPB, original boot code and file chains."""
    if len(data) != SIZE or len(boot) != 512 or boot[-2:] != b'\x55\xaa' or data[510:512] != b'\x55\xaa':
        raise ImageError('invalid floppy image or boot signature')
    expected_bpb = bytes.fromhex('0002020100027000d002fd02000900020000000000000000')
    if (data[11:35] != expected_bpb or data[0x24] != 0 or data[0x26] != 0x29
            or data[0x27:0x2b] != bytes.fromhex('54584350')
            or data[0x2b:0x36] != b'FESXT      ' or data[0x36:0x3e] != b'FAT12   '):
        raise ImageError('incorrect 360 KiB FAT12 BPB')
    if data[:11] != boot[:11] or data[0x3e:510] != boot[0x3e:510]:
        raise ImageError('boot code differs from assembled FreeDOS FAT12 sector')
    if data[512:3*512] != data[3*512:5*512] or data[512:515] != b'\xfd\xff\xff':
        raise ImageError('FAT copies or media marker differ')
    entries = read_root(data)
    names = [entry[0] for entry in entries]
    expected_names = [f'{name.split(".")[0]:<8}{name.split(".")[1]:<3}'.encode('ascii') for name in FILES]
    if names != expected_names:
        raise ImageError('root files or kernel-first order differ')
    seen = set()
    for (raw_name, first, size), name in zip(entries, FILES):
        contents, clusters = read_fat12_file(data, first, size)
        if clusters & seen or contents != files[name]:
            raise ImageError(f'{name} bytes differ or share clusters')
        seen.update(clusters)
    return {'files': {name: {'size': len(files[name]), 'sha256': digest(files[name])} for name in FILES},
            'allocated_clusters': len(seen)}



def validate_selected_image(data):
    """Bind arbitrary on-disk bytes to the pinned FreeDOS and exact FAT12 code.

    This permits a runner to validate the bytes it will hand to the simulated
    device without relying on one platform-specific mtools archive hash.
    """
    if len(data) != SIZE:
        raise ImageError('invalid 360 KiB floppy size')
    spec = lock()
    if digest(data[:11] + data[0x3e:512]) != spec['nasm']['boot_code_sha256']:
        raise ImageError('boot code differs from the selected FreeDOS fixture')
    entries = read_root(data)
    expected_names = [f'{name.split(".")[0]:<8}{name.split(".")[1]:<3}'.encode('ascii') for name in FILES]
    if [entry[0] for entry in entries] != expected_names:
        raise ImageError('root files or kernel-first order differ')
    files = {}
    for (_, first, size), name in zip(entries, FILES):
        files[name], _ = read_fat12_file(data, first, size)
    expected = {'KERNEL.SYS': spec['packages']['kernel']['files']['BIN/KERNL86N.SYS'],
                'COMMAND.COM': spec['packages']['freecom']['files']['BIN/COMMAND.COM'],
                'FDCONFIG.SYS': digest(FDCONFIG), 'AUTOEXEC.BAT': digest(AUTOEXEC),
                'FESXT.TXT': digest(MARKER)}
    for name in FILES:
        if digest(files[name]) != expected[name]:
            raise ImageError(f'{name} differs from the pinned fixture')
    detail = validate_image(data, data[:512], files)
    detail['image_sha256'] = digest(data)
    return detail

def image(download=False, jobs=4, nasm_path=None):
    BUILD.mkdir(parents=True, exist_ok=True)
    # A fixture may be requested by multiple simulations in this worktree.
    # Hold the advisory lock until the image and its receipt are both complete.
    with (BUILD / '.build.lock').open('a+b') as lock_file:
        fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
        return _image(download, jobs, nasm_path)


def _image(download, jobs, nasm_path):
    spec = lock()
    trees = {name: package(name, item, download) for name, item in spec['packages'].items()}
    assembler = Path(nasm_path).resolve() if nasm_path else nasm(spec['nasm'], download, jobs)
    check_nasm_version(assembler, spec['nasm']['version'])
    mformat, mcopy = tools_path('mformat'), tools_path('mcopy')
    environment = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null',
                       SOURCE_DATE_EPOCH=str(FILE_TIME), TZ='UTC', LC_ALL='C')
    with tempfile.TemporaryDirectory(prefix='.image-', dir=BUILD) as temporary:
        work = Path(temporary)
        bootfile = work / 'boot.bin'
        run([assembler, '-dISFAT12', '-f', 'bin', trees['kernel'] / 'SOURCE/KERNEL/boot/boot.asm',
             '-o', bootfile])
        boot = bootfile.read_bytes()
        disk = work / 'floppy.img'
        disk.write_bytes(bytes(SIZE))
        run([mformat, '-i', disk, '-f', '360', '-N', VOLUME_SERIAL, '-v', 'FESXT', '::'], env=environment)
        data = bytearray(disk.read_bytes())
        sector = bytearray(boot)
        if (len(sector) != 512 or sector[-2:] != b'\x55\xaa' or
                digest(sector[:11] + sector[0x3e:]) != spec['nasm']['boot_code_sha256']):
            raise ImageError('assembler did not produce the pinned FAT12 boot code')
        sector[0x0b:0x3e] = data[0x0b:0x3e]
        sector[0x24] = 0  # BIOS drive A; the boot sector uses CHS for drive zero.
        data[:512] = sector
        disk.write_bytes(data)
        files = {'KERNEL.SYS': (trees['kernel'] / 'BIN/KERNL86N.SYS').read_bytes(),
                 'COMMAND.COM': (trees['freecom'] / 'BIN/COMMAND.COM').read_bytes(),
                 'FDCONFIG.SYS': FDCONFIG, 'AUTOEXEC.BAT': AUTOEXEC, 'FESXT.TXT': MARKER}
        for name in FILES:
            staged = work / name
            staged.write_bytes(files[name])
            os.utime(staged, (FILE_TIME, FILE_TIME))
            run([mcopy, '-m', '-i', disk, staged, '::' + name], env=environment)
        result = disk.read_bytes()
        checked = validate_image(result, boot, files)
        validate_selected_image(result)
        receipt = {'format': 1, 'sha256': digest(result), 'bytes': len(result),
                   'geometry': {'cylinders': 40, 'heads': 2, 'sectors_per_track': 9, 'sector_bytes': 512},
                   'source_lock_sha256': digest(LOCK.read_bytes()),
                   'packages': {name: {'repository': p['repository'], 'commit': p['commit'],
                                       'license': p['license']} for name, p in spec['packages'].items()},
                   'tools': {name: {'path': str(path), 'sha256': digest(path.read_bytes())}
                             for name, path in (('nasm', assembler), ('mformat', mformat), ('mcopy', mcopy))},
                   'contents': checked, 'distribution': 'simulation fixture only; not packaged'}
        receipt_file = work / 'receipt.json'
        receipt_file.write_text(json.dumps(receipt, indent=2) + '\n')
        disk.replace(IMAGE)
        receipt_file.replace(IMAGE.with_suffix('.json'))
    return IMAGE


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--download', action='store_true', help='fetch pinned FreeDOS packages and NASM')
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--nasm', type=Path, help='local NASM 2.16.03; its actual binary hash is recorded')
    args = parser.parse_args()
    if args.jobs < 1:
        parser.error('--jobs must be positive')
    try:
        path = image(args.download, args.jobs, args.nasm)
        print(f'{path} {digest(path.read_bytes())}')
    except (ImageError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'build_pcxt_freedos_image: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
