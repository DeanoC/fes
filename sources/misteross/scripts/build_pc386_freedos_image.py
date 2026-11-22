#!/usr/bin/env python3
"""Build the FreeDOS hard-disk images the pc386 machine boots in simulation and
on kit runs.

The images are test fixtures only. They are written into the ignored build
directory and are never distributed, packaged or committed. Distributing
FreeDOS images, with the GPL source obligations that brings, is the FogCast
disk builder's job (docs/pc386-machine-design.md, "FreeDOS in the FogCast disk
builder"); this tool uses the same pinned FreeDOS archives so the two agree.

Two profiles:

  boot   build/sim/fes-pc386/freedos.img: the kernel, FreeCOM and nothing
         else. The simulation suites boot it.
  games  build/sim/fes-pc386/freedos-games.img: the setup the acceptance games
         run in, as the FogCast builder's image will be. FDCONFIG.SYS loads
         UMBM, which hands the machine's upper RAM below the EMS board's
         frame (C800h-DFFFh) to DOS as upper memory blocks (DOS=UMB), then
         HIMEMX high with all extended memory as XMS. POST leaves A20 off,
         so HIMEMX picks a real A20 method by itself. A boot menu offers
         four setups and takes the first after 3 seconds, so runs that
         press nothing get it:
           1  the kernel stays low (no DOS=HIGH), so Ultima VII's Voodoo
              memory manager can turn A20 off again on exit; the kernel's
              data, FILES=30, BUFFERS=16 and the shell go to upper memory
              (DOSDATA=UMB, FILESHIGH, BUFFERSHIGH, SHELLHIGH)
           2  DOS=HIGH,UMB for programs that need more conventional memory
              (Falcon 3.0); not for Ultima VII, because the kernel in the HMA
              holds an XMS A20 lock that makes Voodoo's global disable fail
           3  entry 1 with the EMS board's driver FESEMS.SYS
              (cores/fes-pc386/sim/fesems.asm) high, for the titles that need
              expanded memory; not for Ultima VII, whose menu refuses to run
              while any expanded memory manager is resident
           4  entry 2 with FreeDOS's SHARE high, for Windows 3.x in 386
              enhanced mode, whose DOSMGR needs a SHARE to run several
              virtual machines over one DOS; SHARE stays off the games'
              entries, so their file opens do not pass through it
         AUTOEXEC.BAT sets BLASTER from CMOS 60h-62h with FESENV.COM
         (cores/fes-pc386/sim/fesenv.asm), loads CuteMouse high and gives it
         a non-null default event handler with MOUSEDEF.COM
         (cores/fes-pc386/sim/mousedef.asm). MEM is in C:\\FDOS\\BIN with them.

No network access occurs unless --download is given. The packages are fetched
as cores/fes-pc386/sim/freedos.lock pins them: the kernel's source as FDOS/kernel's
ke2046 release archive, checked against its SHA-256, and the rest at their
package repositories' FreeDOS-v1.4 commits. Every file used is checked against
its pinned SHA-256:

  kernel   kernel 2046's 386 FAT12/FAT16 build with Windows 3.x support
           (WIN31SUPPORT), built from ke2046s.zip's SOURCE/ke2046 with Open
           Watcom and installed as KERNEL.SYS uncompressed. One patch from
           cores/fes-pc386/sim/freedos-patches is applied first: its
           SHARE_IS_FILE_OPEN stub kept SI, which DosRenameTrue relies on
           (fes#1777, FDOS/kernel#276); drop it when upstream ships a fix.
           boot/boot.asm from the same archive is the FAT16 boot sector,
           assembled with the test386 runner's pinned NASM. The FAT32 build
           reports DOS 7.10 and is not what Windows 3.x runs on
           (docs/pc386-machine-design.md)
  freecom  COMMAND.COM
  fdisk    bootnorm.c, the bytes of Free FDISK's master boot record
  himemx   HIMEMX.EXE (games)
  jemm     UMBM.EXE (games)
  ctmouse  CTMOUSE.EXE (games)
  mem      MEM.EXE (games)
  share    SHARE.COM (games), built with ia16-gcc from ke2046s.zip's share
           tree (FDOS/share 47f3d425). Kernel 2046 asks SHARE before every
           delete and rename (INT 2Fh 10A6h); FreeDOS 1.4's SHARE 08/2006a
           predates that call and failed them all (FDOS/share#27)

The kernel and SHARE builds are reproducible: each output must match the SHA-256
the lock pins, so a different toolchain or source fails the build instead of
changing the image. The toolchains are pinned in the lock's "toolchains" by
archive SHA-256 and unpacked under build/: Open Watcom v2's dated snapshot
tarball, and gcc-ia16-elf, binutils, newlib and libi86 as tkchia/build-ia16's
Debian packages (dpkg-deb unpacks them; nothing is installed). Each archive
is fetched from its upstream URL first and then from a byte-identical mirror,
the DeanoC/fes-pc386-toolchains release, in case upstream drops it. An image built
from source carries FDOS\\DOC\\SOURCES.TXT, naming each source archive, and the
patches, beside the binaries.

The disk is 65 cylinders of 16 heads and 63 sectors (32.0 MiB). One active
FAT16 partition (type 06h) starts at cylinder 0, head 1; mtools formats it with
2 KiB clusters, as DOS FORMAT does at this size, and the kernel's boot code is
laid around the BPB mtools wrote, as FreeDOS's SYS does. The partition entry's end CHS states the geometry, which is how the
machine (and FogCast) infer it. The image is reproducible: file times, the
volume serial and the label are fixed.

Examples:
  python3 scripts/build_pc386_freedos_image.py --download
  python3 scripts/build_pc386_freedos_image.py --profile games --download
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
import tarfile
import urllib.request
import zipfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import test_fes_i386_test386 as test386

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / 'cores/fes-pc386/sim/freedos.lock'
FESENV = ROOT / 'cores/fes-pc386/sim/fesenv.asm'
MOUSEDEF = ROOT / 'cores/fes-pc386/sim/mousedef.asm'
FESEMS = ROOT / 'cores/fes-pc386/sim/fesems.asm'
BUILD = ROOT / 'build/sim/fes-pc386/freedos'

CYLINDERS, HEADS, SPT = 65, 16, 63
TOTAL_SECTORS = CYLINDERS * HEADS * SPT
# The largest disk the machine's ATA window holds (128 MiB, fes.media.ata-disk).
MAX_CYLINDERS = 128 * 1024 * 1024 // (HEADS * SPT * 512)
PARTITION_START = SPT                  # cylinder 0, head 1, sector 1
PARTITION_SECTORS = TOTAL_SECTORS - PARTITION_START
# DOS FORMAT's FAT16 cluster for 16-128 MiB. mtools would pick 512 bytes,
# and FreeDOS walks a file's chain from its start on every backward seek
# (map_cluster), so a 512-byte cluster makes that walk four times as long.
CLUSTER_SECTORS = 4
VOLUME_SERIAL = 0x1386_0026
# 2026-10-26 12:00 UTC: the file times on the image.
FILE_TIME = 1793016000

SHELL = 'SHELL=C:\\COMMAND.COM C:\\ /E:1024 /P=C:\\AUTOEXEC.BAT\r\n'
# Each profile: its image, the lock's packages it takes, its FDCONFIG.SYS and
# AUTOEXEC.BAT, and the files in C:\FDOS\BIN (a package file, or a local
# program's source, which the build assembles).
PROFILES = {
    'boot': {
        'image': ROOT / 'build/sim/fes-pc386/freedos.img',
        'packages': ('kernel', 'freecom', 'fdisk'),
        'fdconfig': 'FILES=20\r\nBUFFERS=20\r\n' + SHELL,
        'autoexec': '@ECHO OFF\r\nPROMPT $P$G\r\nPATH C:\\\r\nVER\r\n',
        'bin': {},
    },
    'games': {
        'image': ROOT / 'build/sim/fes-pc386/freedos-games.img',
        'packages': ('kernel', 'freecom', 'fdisk', 'himemx', 'jemm', 'ctmouse', 'mem', 'share'),
        'fdconfig': ('MENUDEFAULT=1,3\r\n'
                     'MENU 1 - Games: DOS in conventional memory (default; Ultima VII needs this)\r\n'
                     'MENU 2 - More memory: DOS=HIGH, not for Ultima VII\r\n'
                     'MENU 3 - EMS: as 1 with the EMS board driver, not for Ultima VII\r\n'
                     'MENU 4 - Windows 3.x: as 2 with SHARE, not for Ultima VII\r\n'
                     '13?DOS=UMB\r\n13?DOSDATA=UMB\r\n24?DOS=HIGH,UMB\r\n'
                     'DEVICE=C:\\FDOS\\BIN\\UMBM.EXE /I=C800-DFFF\r\n'
                     'DEVICEHIGH=C:\\FDOS\\BIN\\HIMEMX.EXE /VERBOSE\r\n'
                     '3?DEVICEHIGH=C:\\FDOS\\BIN\\FESEMS.SYS\r\n'
                     '13?FILESHIGH=30\r\n13?BUFFERSHIGH=16\r\n24?FILES=30\r\n24?BUFFERS=16\r\n'
                     '4?INSTALLHIGH=C:\\FDOS\\BIN\\SHARE.COM\r\n'
                     '13?' + SHELL.replace('SHELL=', 'SHELLHIGH=') + '24?' + SHELL),
        'autoexec': ('@ECHO OFF\r\nPROMPT $P$G\r\nPATH C:\\FDOS\\BIN;C:\\\r\n'
                     'FESENV > C:\\FDOS\\FESENV.BAT\r\nCALL C:\\FDOS\\FESENV.BAT\r\nLH CTMOUSE\r\nMOUSEDEF\r\n'
                     'VER\r\n'),
        'bin': {'HIMEMX.EXE': ('himemx', 'BIN/HimemX.exe'), 'UMBM.EXE': ('jemm', 'BIN/UMBM.EXE'),
                'CTMOUSE.EXE': ('ctmouse', 'BIN/CTMOUSE.EXE'), 'MEM.EXE': ('mem', 'BIN/MEM.EXE'),
                'SHARE.COM': ('share', 'build-output/share.com'),
                'FESENV.COM': FESENV, 'MOUSEDEF.COM': MOUSEDEF, 'FESEMS.SYS': FESEMS},
    },
}

# The floppy fixtures the removable-media suites use (slice F2 of
# docs/design/pc386-removable-media.md). Each is a raw image of one of the
# formats the machine identifies by size: 'format' is mtools's -f size in KiB,
# 'label' the volume label the test reads from the boot sector, and 'files'
# the text files on it. A bootable floppy carries the kernel's FAT12 boot
# sector, KERNEL.SYS and COMMAND.COM, as SYS installs them.
FLOPPIES = {
    'floppy-boot': {
        'image': ROOT / 'build/sim/fes-pc386/freedos-floppy.img',
        'format': 1440, 'label': 'FDBOOT', 'bootable': True,
        'packages': ('kernel', 'freecom'),
        'fdconfig': 'FILES=20\r\nBUFFERS=20\r\n' + SHELL.replace('C:', 'A:'),
        'autoexec': '@ECHO OFF\r\nPROMPT $P$G\r\nPATH A:\\\r\nVER\r\n',
        'files': {},
    },
    'floppy-a': {
        'image': ROOT / 'build/sim/fes-pc386/floppy-a.img',
        'format': 1440, 'label': 'DISK1', 'bootable': False, 'packages': (),
        'files': {'DISK1.TXT': 'disk one\r\n'},
    },
    'floppy-a2': {
        'image': ROOT / 'build/sim/fes-pc386/floppy-a2.img',
        'format': 1440, 'label': 'DISK2', 'bootable': False, 'packages': (),
        'files': {'DISK2.TXT': 'disk two\r\n'},
    },
    'floppy-b': {
        'image': ROOT / 'build/sim/fes-pc386/floppy-b.img',
        'format': 1200, 'label': 'BDISK', 'bootable': False, 'packages': (),
        'files': {'BDISK.TXT': 'the drive B disk\r\n'},
    },
    'floppy-b360': {
        'image': ROOT / 'build/sim/fes-pc386/floppy-b360.img',
        'format': 360, 'label': 'DISK360', 'bootable': False, 'packages': (),
        'files': {'B360.TXT': 'a 360K disk in the 1.2M drive\r\n'},
    },
}
for _name, _spec in FLOPPIES.items():
    _spec['bin'] = {}
# Every fixture this builder makes. PROFILES stays the hard-disk images, so
# the boot suite's --image offers only those.
ALL_PROFILES = {**PROFILES, **FLOPPIES}


class ImageError(RuntimeError):
    pass


def lock() -> dict:
    return json.loads(LOCK.read_text())


def inputs_key(profile: str = 'boot') -> str:
    """Identity of everything a profile's image is built from: the profile, the
    lock (package commits and file hashes), the builder, NASM helper and
    assembler version, and the source of each local program the image carries."""
    digest = hashlib.sha256(profile.encode() + b'\0')
    sources = [LOCK.resolve(), Path(__file__).resolve(), Path(test386.__file__).resolve()]
    sources += [source.resolve() for source in ALL_PROFILES[profile]['bin'].values() if isinstance(source, Path)]
    packages = lock()['packages']
    sources += [(ROOT / patch).resolve() for name in ALL_PROFILES[profile]['packages']
                for patch in packages[name].get('build', {}).get('patches', [])]
    for path in sources:
        digest.update(path.relative_to(ROOT).as_posix().encode())
        digest.update(b'\0')
        digest.update(path.read_bytes())
        digest.update(b'\0')
    digest.update(test386.NASM_VERSION.encode())
    return digest.hexdigest()


def image_is_current(profile: str = 'boot') -> bool:
    """Check both the fixture's input identity and the image bytes."""
    image = ALL_PROFILES[profile]['image']
    try:
        record = json.loads(image.with_suffix('.json').read_text())
        image_digest = hashlib.sha256(image.read_bytes()).hexdigest()
    except (OSError, ValueError):
        return False
    return (isinstance(record, dict) and record.get('inputs') == inputs_key(profile) and
            record.get('sha256') == image_digest)


def work_directory(profile: str) -> Path:
    """Keep simultaneous profile builds from deleting each other's scratch files."""
    return BUILD / f'work-{profile}'


def package(name: str, spec: dict, download: bool, jobs: int = 4) -> Path:
    """Return one FreeDOS package's pinned files, each verified: a package
    repository's checkout at its commit, or the named members of release
    archives, each archive checked against its SHA-256 (files under
    <archive>/<member>)."""
    if 'archives' in spec:
        return release_package(name, spec, download, jobs)
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
    verify(name, tree, spec)
    return tree


def source(pins: dict) -> str:
    """Where a locked package came from: its commit, or its release's page."""
    return pins['commit'] if 'commit' in pins else pins['release']


def package_record(pins: dict) -> dict:
    """A package as an image's record lists it, with what built it."""
    record = {'source': source(pins), 'version': pins['version'], 'license': pins['license']}
    if 'build' in pins:
        record['built'] = {'source_tree': pins['source_tree'], 'patches': pins['build']['patches'],
                           'toolchain': lock()['toolchains'][pins['build']['toolchain']]['version'],
                           'sha256': pins['build']['sha256'], 'upstream_issue': pins['upstream_issue']}
    return record


def verify(name: str, tree: Path, spec: dict) -> None:
    for path, digest in spec['files'].items():
        if hashlib.sha256((tree / path).read_bytes()).hexdigest() != digest:
            raise ImageError(f'FreeDOS {name}/{path} does not match freedos.lock')


def release_package(name: str, spec: dict, download: bool, jobs: int = 4) -> Path:
    """Unpack the lock's files from a package's release archives."""
    pins = {key: spec[key] for key in ('archives', 'files', 'source_tree', 'build') if key in spec}
    digest = hashlib.sha256(json.dumps(pins, sort_keys=True).encode())
    for patch in spec.get('build', {}).get('patches', []):
        digest.update((ROOT / patch).read_bytes())
    if 'build' in spec:
        digest.update(json.dumps(lock()['toolchains'][spec['build']['toolchain']], sort_keys=True).encode())
    identity = digest.hexdigest()[:16]
    tree = BUILD / f'{name}-{identity}'
    if not tree.is_dir():
        partial = tree.with_name(tree.name + '.partial')
        shutil.rmtree(partial, ignore_errors=True)
        for archive, pin in spec['archives'].items():
            stored = BUILD / 'archives' / f'{pin["sha256"]}.zip'
            if not stored.is_file():
                if not download:
                    raise ImageError(f'FreeDOS {name} archive {archive} is not in {stored.parent}; pass --download')
                print(f'Fetching FreeDOS {name} {archive} ({spec["license"]})', flush=True)
                stored.parent.mkdir(parents=True, exist_ok=True)
                fetched = stored.with_name(stored.name + '.partial')
                with urllib.request.urlopen(pin['url'], timeout=120) as response:
                    data = response.read()
                if hashlib.sha256(data).hexdigest() != pin['sha256']:
                    raise ImageError(f'FreeDOS {name} archive {pin["url"]} does not match freedos.lock')
                fetched.write_bytes(data)
                fetched.rename(stored)
            if hashlib.sha256(stored.read_bytes()).hexdigest() != pin['sha256']:
                stored.unlink()
                raise ImageError(f'FreeDOS {name} archive {stored} did not match freedos.lock and was removed; '
                                 'pass --download to fetch it again')
            with zipfile.ZipFile(stored) as members:
                wanted = [path.partition('/')[2] for path in spec['files'] if path.partition('/')[0] == archive]
                tree_archive, _, tree_prefix = spec.get('source_tree', '').partition('/')
                if tree_archive == archive:
                    wanted += [member for member in members.namelist()
                               if member.startswith(tree_prefix + '/') and not member.endswith('/')]
                for member in sorted(set(wanted)):
                    target = partial / archive / member
                    if not target.resolve().is_relative_to(partial.resolve()):
                        raise ImageError(f'FreeDOS {name} archive {archive} member {member} leaves its tree')
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(members.read(member))
        verify(name, partial, spec)
        if 'build' in spec:
            build_package(name, spec, partial, download, jobs)
        partial.rename(tree)
    verify(name, tree, spec)
    if 'build' in spec and hashlib.sha256(built(tree, spec).read_bytes()).hexdigest() != spec['build']['sha256']:
        raise ImageError(f'FreeDOS {name}: {built(tree, spec)} does not match freedos.lock')
    return tree


def built(tree: Path, spec: dict) -> Path:
    """A built package's output, kept beside its source as build-output/<name>."""
    return tree / 'build-output' / Path(spec['build']['output']).name


def build_package(name: str, spec: dict, tree: Path, download: bool, jobs: int) -> None:
    """Build a package from its source tree: copy the tree, apply the lock's
    patches (patch -p1), run the build command with the pinned toolchain on
    PATH, and keep the output only if it matches its pinned SHA-256. A
    toolchain or source that builds different bytes fails here."""
    build = spec['build']
    archive, _, prefix = spec['source_tree'].partition('/')
    work = tree / 'build-tree'
    shutil.copytree(tree / archive / prefix, work)
    for patch in build['patches']:
        result = subprocess.run(['patch', '-p1', '--batch', '--input', str(ROOT / patch)], cwd=work,
                                capture_output=True, text=True)
        if result.returncode:
            raise ImageError(f'FreeDOS {name}: {patch} did not apply:\n' + result.stdout + result.stderr)
    nasm = test386.nasm(download, jobs)
    env = dict(os.environ, **toolchain_environment(build['toolchain'], download), **build.get('environment', {}))
    env['PATH'] = os.pathsep.join((env['PATH'], str(nasm.parent)))
    env['SOURCE_DATE_EPOCH'] = str(FILE_TIME)
    print(f'Building FreeDOS {name} with {lock()["toolchains"][build["toolchain"]]["version"]}', flush=True)
    log = tree / 'build.log'
    with log.open('w') as output:
        result = subprocess.run(build['command'], cwd=work, env=env, stdout=output, stderr=subprocess.STDOUT)
    if result.returncode:
        raise ImageError(f'FreeDOS {name}: the build failed; see {log}')
    produced = work / build['output']
    digest = hashlib.sha256(produced.read_bytes()).hexdigest()
    if digest != build['sha256']:
        raise ImageError(f'FreeDOS {name}: the build made {build["output"]} with SHA-256 {digest}, not the '
                         f'{build["sha256"]} freedos.lock pins; see {log}')
    built(tree, spec).parent.mkdir()
    shutil.copyfile(produced, built(tree, spec))
    shutil.rmtree(work)


def toolchain_environment(name: str, download: bool) -> dict:
    """The environment that puts a pinned toolchain first on PATH."""
    root = toolchain(name, download)
    if name == 'openwatcom':
        return {'WATCOM': str(root), 'PATH': os.pathsep.join((str(root / 'binl64'), str(root / 'binl'),
                                                                os.environ['PATH']))}
    if name == 'ia16':
        return {'PATH': os.pathsep.join((str(root / 'usr/bin'), os.environ['PATH'])),
                'LD_LIBRARY_PATH': str(root / 'usr/x86_64-linux-gnu/ia16-elf/lib')}
    raise ImageError(f'no environment for toolchain {name}')


def fetch(name: str, archive: dict, download: bool) -> Path:
    """One pinned toolchain archive in BUILD/archives, from the first of its
    URLs that serves the pinned bytes."""
    stored = BUILD / 'archives' / f'{archive["sha256"]}-{archive["name"]}'
    if stored.is_file() and hashlib.sha256(stored.read_bytes()).hexdigest() == archive['sha256']:
        return stored
    if not download:
        raise ImageError(f'toolchain {name} archive {archive["name"]} is not in {stored.parent}; pass --download')
    stored.parent.mkdir(parents=True, exist_ok=True)
    refused = []
    for url in archive['urls']:
        print(f'Fetching toolchain {name} {archive["name"]} from {url}', flush=True)
        try:
            with urllib.request.urlopen(url, timeout=600) as response:
                data = response.read()
        except OSError as error:
            refused.append(f'{url}: {error}')
            continue
        if hashlib.sha256(data).hexdigest() != archive['sha256']:
            refused.append(f'{url}: not the pinned SHA-256')
            continue
        partial = stored.with_name(stored.name + '.partial')
        partial.write_bytes(data)
        partial.rename(stored)
        return stored
    raise ImageError(f'no source supplied toolchain {name} {archive["name"]}:\n  ' + '\n  '.join(refused))


def toolchain(name: str, download: bool) -> Path:
    """A pinned toolchain unpacked once under BUILD/toolchains: tar archives
    with tarfile's data filter, Debian packages with dpkg-deb -x (their
    payloads are zstd, which tarfile cannot read)."""
    spec = lock()['toolchains'][name]
    identity = hashlib.sha256(json.dumps(spec, sort_keys=True).encode()).hexdigest()[:16]
    root = BUILD / 'toolchains' / f'{name}-{identity}'
    if root.is_dir():
        return root
    partial = root.with_name(root.name + '.partial')
    shutil.rmtree(partial, ignore_errors=True)
    partial.mkdir(parents=True)
    for archive in spec['archives']:
        stored = fetch(name, archive, download)
        if archive['name'].endswith('.deb'):
            if not shutil.which('dpkg-deb'):
                raise ImageError(f'toolchain {name} needs dpkg-deb to unpack {archive["name"]}')
            subprocess.run(['dpkg-deb', '-x', str(stored), str(partial)], check=True)
        else:
            with tarfile.open(stored) as members:
                members.extractall(partial, filter='data')
    partial.rename(root)
    return root


def source_notes(packages: dict) -> dict[str, bytes]:
    """For each package built from source: where the source is, and our
    patches, as FDOS/DOC files beside the binaries (both are GPL)."""
    notes, lines = {}, []
    for name, pins in packages.items():
        if 'build' not in pins:
            continue
        archive, _, tree = pins['source_tree'].partition('/')
        lines += [f'{name}: built from {tree} in {pins["archives"][archive]["url"]}',
                  f'  (SHA-256 {pins["archives"][archive]["sha256"]}), {pins["license"]}',
                  f'  with {lock()["toolchains"][pins["build"]["toolchain"]]["version"]}']
        for number, patch in enumerate(pins['build']['patches'], 1):
            doc = f'{name[:6].upper()}{number}.DIF'
            notes[doc] = (ROOT / patch).read_bytes()
            lines.append(f'  patched with {doc} (fes {patch}); upstream {pins["upstream_issue"]}')
        lines.append('')
    if lines:
        notes['SOURCES.TXT'] = '\r\n'.join(lines).encode()
    return notes


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


def assemble(nasm: Path, source: Path, output: Path, *options: str) -> bytes:
    result = subprocess.run([str(nasm), *options, '-f', 'bin', str(source), '-o', str(output)],
                            capture_output=True, text=True)
    if result.returncode:
        raise ImageError(f'NASM failed on {source.name}:\n' + result.stderr)
    return output.read_bytes()


def boot_sector(boot_asm: Path, nasm: Path, work: Path) -> bytes:
    # boot.asm includes magic.mac from its own directory.
    data = assemble(nasm, boot_asm, work / 'fat16com.bin', '-dISFAT16', f'-I{boot_asm.parent}/')
    if len(data) != 512 or data[510:] != b'\x55\xaa':
        raise ImageError('boot.asm did not assemble to a boot sector')
    return data


def mtools(*command: str, env: dict) -> None:
    result = subprocess.run(list(command), capture_output=True, text=True, env=env)
    if result.returncode:
        raise ImageError(' '.join(command) + ' failed:\n' + result.stdout + result.stderr)


def system_disk(profile: str, cylinders: int, work: Path, download: bool = False, jobs: int = 4) -> tuple[Path, dict]:
    """A profile's FreeDOS disk of `cylinders` cylinders, built in work (emptied
    first) as work/disk.img, and the lock's pins of the packages it took."""
    if not 1 <= cylinders <= MAX_CYLINDERS:
        raise ImageError(f'{cylinders} cylinders is outside 1-{MAX_CYLINDERS} (the 128 MiB ATA disk limit)')
    spec = ALL_PROFILES[profile]
    packages = {name: lock()['packages'][name] for name in spec['packages']}
    trees = {name: package(name, pins, download, jobs) for name, pins in packages.items()}
    nasm = test386.nasm(download, jobs)
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)

    total = cylinders * HEADS * SPT
    partition = total - PARTITION_START
    image = bytearray(total * 512)
    mbr = bytearray(512)
    mbr[:446] = mbr_code(trees['fdisk'] / 'SOURCE/FDISK/bootnorm.c')
    entry = bytes((0x80,)) + chs(PARTITION_START) + bytes((0x06,)) + chs(total - 1) + \
        PARTITION_START.to_bytes(4, 'little') + partition.to_bytes(4, 'little')
    mbr[0x1be:0x1ce] = entry
    mbr[510:] = b'\x55\xaa'
    image[:512] = mbr
    disk = work / 'disk.img'
    disk.write_bytes(image)

    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', SOURCE_DATE_EPOCH=str(FILE_TIME), TZ='UTC')
    volume = f'{disk}@@{PARTITION_START * 512}'
    mtools('mformat', '-i', volume, '-h', str(HEADS), '-s', str(SPT), '-c', str(CLUSTER_SECTORS),
           '-H', str(PARTITION_START), '-T', str(partition), '-N', f'{VOLUME_SERIAL:08x}',
           '-v', 'FREEDOS', '::', env=env)

    # The kernel's boot code around the BPB mformat wrote, as SYS installs it.
    data = bytearray(disk.read_bytes())
    offset = PARTITION_START * 512
    bpb = data[offset + 0x0b:offset + 0x3e]
    sector = bytearray(boot_sector(trees['kernel'] / packages['kernel']['boot_asm'], nasm, work))
    sector[0x0b:0x3e] = bpb
    sector[0x24] = 0x80                     # BIOS drive number: the first hard disk
    if bytes(sector[0x36:0x3e]).rstrip() != b'FAT16' or sector[0x0d] != CLUSTER_SECTORS:
        raise ImageError(f'mformat did not make a FAT16 volume of {CLUSTER_SECTORS}-sector clusters')
    data[offset:offset + 512] = sector
    disk.write_bytes(data)

    files = {
        'KERNEL.SYS': built(trees['kernel'], packages['kernel']),
        'COMMAND.COM': trees['freecom'] / 'BIN/COMMAND.COM',
    }
    staged = work / 'files'
    staged.mkdir()
    for target, source in files.items():
        shutil.copyfile(source, staged / target)
    (staged / 'FDCONFIG.SYS').write_bytes(spec['fdconfig'].encode())
    (staged / 'AUTOEXEC.BAT').write_bytes(spec['autoexec'].encode())
    # KERNEL.SYS first, as SYS places it, then the rest in a fixed order.
    for name in ('KERNEL.SYS', 'COMMAND.COM', 'FDCONFIG.SYS', 'AUTOEXEC.BAT'):
        os.utime(staged / name, (FILE_TIME, FILE_TIME))
        mtools('mcopy', '-m', '-i', volume, str(staged / name), '::' + name, env=env)
    if spec['bin']:
        mtools('mmd', '-i', volume, '::FDOS', '::FDOS/BIN', env=env)
    for name, source in spec['bin'].items():
        if isinstance(source, Path):
            assemble(nasm, source, staged / name)
        else:
            shutil.copyfile(trees[source[0]] / source[1], staged / name)
        os.utime(staged / name, (FILE_TIME, FILE_TIME))
        mtools('mcopy', '-m', '-i', volume, str(staged / name), '::FDOS/BIN/' + name, env=env)
    write_notes(packages, staged, volume, env)
    return disk, packages


def write_notes(packages: dict, staged: Path, volume: str, env: dict) -> None:
    """Copy source_notes() into FDOS/DOC on the volume."""
    notes = source_notes(packages)
    if not notes:
        return
    listing = subprocess.run(['mdir', '-b', '-i', volume, '::'], capture_output=True, text=True, env=env).stdout
    if '::/FDOS' not in listing.upper().replace('\\', '/'):
        mtools('mmd', '-i', volume, '::FDOS', env=env)
    mtools('mmd', '-i', volume, '::FDOS/DOC', env=env)
    for name in sorted(notes):
        (staged / name).write_bytes(notes[name])
        os.utime(staged / name, (FILE_TIME, FILE_TIME))
        mtools('mcopy', '-m', '-i', volume, str(staged / name), '::FDOS/DOC/' + name, env=env)


# A floppy fixture: one raw image of its format, formatted FAT12, with the
# kernel's boot sector when it is bootable, and its files copied in.
def system_floppy(profile: str, work: Path, download: bool = False, jobs: int = 4) -> tuple[Path, dict]:
    spec = ALL_PROFILES[profile]
    packages = {name: lock()['packages'][name] for name in spec['packages']}
    trees = {name: package(name, pins, download, jobs) for name, pins in packages.items()}
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)
    disk = work / 'floppy.img'
    disk.write_bytes(bytes(spec['format'] * 1024))
    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', SOURCE_DATE_EPOCH=str(FILE_TIME), TZ='UTC')
    mtools('mformat', '-i', str(disk), '-f', str(spec['format']), '-N', f'{VOLUME_SERIAL:08x}',
           '-v', spec['label'], '::', env=env)
    if spec['bootable']:
        nasm = test386.nasm(download, jobs)
        data = bytearray(disk.read_bytes())
        sector = bytearray(assemble(nasm, trees['kernel'] / packages['kernel']['boot_asm'],
                                    work / 'fat12com.bin', '-dISFAT12',
                                    f'-I{(trees["kernel"] / packages["kernel"]["boot_asm"]).parent}/'))
        if len(sector) != 512 or sector[510:] != b'\x55\xaa':
            raise ImageError('boot.asm did not assemble to a FAT12 boot sector')
        sector[0x0b:0x3e] = data[0x0b:0x3e]     # the BPB mformat wrote
        sector[0x24] = 0x00                     # BIOS drive number: the first floppy
        if bytes(sector[0x36:0x3e]).rstrip() != b'FAT12':
            raise ImageError('mformat did not make a FAT12 volume')
        data[:512] = sector
        disk.write_bytes(data)
    staged = work / 'files'
    staged.mkdir()
    names = []
    if spec['bootable']:
        shutil.copyfile(built(trees['kernel'], packages['kernel']), staged / 'KERNEL.SYS')
        shutil.copyfile(trees['freecom'] / 'BIN/COMMAND.COM', staged / 'COMMAND.COM')
        (staged / 'FDCONFIG.SYS').write_bytes(spec['fdconfig'].encode())
        (staged / 'AUTOEXEC.BAT').write_bytes(spec['autoexec'].encode())
        names = ['KERNEL.SYS', 'COMMAND.COM', 'FDCONFIG.SYS', 'AUTOEXEC.BAT']
    for name, text in spec['files'].items():
        (staged / name).write_bytes(text.encode())
        names.append(name)
    for name in names:
        os.utime(staged / name, (FILE_TIME, FILE_TIME))
        mtools('mcopy', '-m', '-i', str(disk), str(staged / name), '::' + name, env=env)
    if spec['bootable']:
        write_notes(packages, staged, str(disk), env)
    return disk, packages


def build(download: bool = False, jobs: int = 4, profile: str = 'boot') -> Path:
    spec = ALL_PROFILES[profile]
    if profile in FLOPPIES:
        return build_floppy(download, jobs, profile)
    disk, packages = system_disk(profile, CYLINDERS, work_directory(profile), download, jobs)
    target = spec['image']
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(disk, target)
    record = {
        'image': str(target.relative_to(ROOT)),
        'profile': profile,
        'sha256': hashlib.sha256(target.read_bytes()).hexdigest(),
        'geometry': {'cylinders': CYLINDERS, 'heads': HEADS, 'sectors_per_track': SPT},
        'packages': {name: package_record(pins) for name, pins in packages.items()},
        'distribution': 'test fixture only; never distributed',
        'inputs': inputs_key(profile),
    }
    target.with_suffix('.json').write_text(json.dumps(record, indent=2) + '\n')
    return target


def build_floppy(download: bool, jobs: int, profile: str) -> Path:
    spec = ALL_PROFILES[profile]
    disk, packages = system_floppy(profile, work_directory(profile), download, jobs)
    target = spec['image']
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(disk, target)
    record = {
        'image': str(target.relative_to(ROOT)),
        'profile': profile,
        'sha256': hashlib.sha256(target.read_bytes()).hexdigest(),
        'format': f'{spec["format"]}K', 'label': spec['label'], 'bootable': spec['bootable'],
        'packages': {name: package_record(pins) for name, pins in packages.items()},
        'distribution': 'test fixture only; never distributed',
        'inputs': inputs_key(profile),
    }
    target.with_suffix('.json').write_text(json.dumps(record, indent=2) + '\n')
    return target


def image(download: bool = False, jobs: int = 4, profile: str = 'boot') -> Path:
    """A profile's image, rebuilt unless the one in build/ came from the current inputs."""
    if image_is_current(profile):
        return ALL_PROFILES[profile]['image']
    return build(download, jobs, profile)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--profile', choices=sorted(ALL_PROFILES), default='boot',
                        help='which image or floppy fixture to build')
    parser.add_argument('--download', action='store_true', help='fetch the pinned packages and NASM if missing')
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 4)
    args = parser.parse_args(argv)
    try:
        image = build(args.download, args.jobs, args.profile)
    except (ImageError, test386.Test386Error) as error:
        print(f'build_pc386_freedos_image: {error}', file=sys.stderr)
        return 1
    print(f'{image} {hashlib.sha256(image.read_bytes()).hexdigest()}')
    return 0


if __name__ == '__main__':
    sys.exit(main())
