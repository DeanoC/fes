#!/usr/bin/env python3
"""Build the reusable pc386 game drives: FreeDOS hard-disk images holding
user-supplied DOS programs, named by their content SHA-256 and kept outside
the repository.

Each drive in the manifest (cores/fes-pc386/sim/game-drives.toml) is the
games profile of build_pc386_freedos_image.py (HIMEMX, UMBM, CuteMouse,
MOUSEDEF, FESENV and MEM; FDCONFIG.SYS and AUTOEXEC.BAT unchanged) on a disk
of the drive's cylinders (16 heads, 63 sectors; at most 260 cylinders, the
128 MiB the ATA disk holds), with each title's files copied from a source
the user supplies. A title names its source ([sources] in the manifest) and
its path there:

  zip   an eXoDOS zip, pinned by the zip's SHA-256 (FES_PC386_GAMES)
  tree  a folder, such as the DOS Benchmark Pack (FES_PC386_DOSBENCH), pinned
        by each file's relative path and SHA-256, sorted (tree_sha256)

Sources are read in place and checked against the manifest's SHA-256. Their
files pass through a private 0700 work directory in the store, which is
deleted at the end. Commercial programs never enter the repository: the
manifest and the drive's record carry titles, paths, hashes and counts.

A title's `copy` entries name a folder in its source and the C: directory it
goes to, in order, so a later entry (a sound setup's config folder) replaces
files of an earlier one. `skip` drops a folder's subdirectories by name, and
`flat` drops them all. A name that is not 8.3 is skipped and counted. A
title's `config` entries put a file from the store's sidecars/ (named by its
SHA-256: a config a game's own setup wrote) at a path on C:.

The drive is reproducible: file names are upper-cased short names (mtools
writes no long names), a zip's files keep their zip times and a folder's take
the image's fixed time, directories get that fixed time too, and directories
and files are written in sorted order. So the same manifest, sources and
FreeDOS lock give the same bytes. The store holds, for each built drive:

  <sha256>.img            the drive
  <sha256>.json           its record: geometry, base, titles (launch, keys,
                          source hashes, file counts), free bytes
  by-name/<drive>.img     symlinks to the latest build of each drive
  by-name/<drive>.json

The store is FES_PC386_DRIVES, default ~/fes-drives/pc386 (on Powerboat;
docs/pc386-game-drives.md). sim_fes_pc386.py --drive and the kit runners
take a drive by path, SHA-256 (or a unique prefix of 8 or more digits) or
name through resolve().

Examples:
  python3 scripts/build_pc386_game_drive.py --download real
  python3 scripts/build_pc386_game_drive.py --list
  python3 scripts/build_pc386_game_drive.py --resolve real
"""
from __future__ import annotations

import argparse
import calendar
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tomllib
import zipfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_pc386_freedos_image as freedos
from scripts import test_fes_i386_test386 as test386

ROOT = freedos.ROOT
MANIFEST = ROOT / 'cores/fes-pc386/sim/game-drives.toml'
STORE = Path(os.environ.get('FES_PC386_DRIVES', Path.home() / 'fes-drives/pc386'))
PROFILE = 'games'
SHORT_NAME = re.compile(r"^[A-Z0-9_$~!#%&'(){}@^`-]{1,8}(\.[A-Z0-9_$~!#%&'(){}@^`-]{1,3})?$")
HEX = re.compile(r'^[0-9a-f]{8,64}$')


class DriveError(RuntimeError):
    pass


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, 'rb') as stream:
        while chunk := stream.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def manifest(path: Path = MANIFEST) -> dict:
    """The manifest's drives, each checked for the fields the build needs."""
    data = tomllib.loads(path.read_text())
    drives = data.get('drives')
    if not isinstance(drives, dict) or not drives:
        raise DriveError(f'{path} has no [drives]')
    seen = {}
    for name, drive in drives.items():
        if not re.fullmatch(r'[a-z0-9-]+', name):
            raise DriveError(f'drive name {name!r} is not lower-case letters, digits and hyphens')
        if not 1 <= drive.get('cylinders', 0) <= freedos.MAX_CYLINDERS:
            raise DriveError(f'drive {name}: cylinders must be 1-{freedos.MAX_CYLINDERS}')
        for title in drive.get('titles', []):
            for field in ('id', 'title', 'bucket', 'path', 'sha256', 'copy', 'launch'):
                if field not in title:
                    raise DriveError(f'drive {name}: a title has no {field}: {title.get("id")}')
            if title['id'] in seen:
                raise DriveError(f'title {title["id"]} is on drives {seen[title["id"]]} and {name}')
            seen[title['id']] = name
            if title['bucket'] not in data.get('buckets', {}):
                raise DriveError(f'title {title["id"]}: bucket {title["bucket"]} is not in [buckets]')
            if title.get('source', 'exodos') not in data.get('sources', {}):
                raise DriveError(f'title {title["id"]}: source {title.get("source")} is not in [sources]')
    return data


def drive_inputs(data: dict, name: str) -> str:
    """Identity of a drive's build inputs other than the sources, which the
    record pins by hash: the drive's manifest entry, this builder, the FreeDOS
    builder's profile inputs and the sidecar hashes."""
    digest = hashlib.sha256(json.dumps(data['drives'][name], sort_keys=True).encode())
    digest.update(Path(__file__).read_bytes())
    digest.update(freedos.inputs_key(PROFILE).encode())
    return digest.hexdigest()


def dos_name(name: str) -> str | None:
    """A name's upper-case DOS form, or None when it is no 8.3 name (eXoDOS's
    own extras, such as long-named manuals): DOS programs cannot open those."""
    upper = name.upper()
    return upper if SHORT_NAME.match(upper) else None


def tree_sha256(root: Path) -> str:
    """A folder source's identity: each file's relative path and SHA-256, sorted."""
    digest = hashlib.sha256()
    for path in sorted(p for p in root.rglob('*') if p.is_file()):
        digest.update(f'{path.relative_to(root).as_posix()}\0{sha256(path)}\n'.encode())
    return digest.hexdigest()


def source_files(data: dict, title: dict) -> dict:
    """A title's source files: {relative name: (read(), time)}, its identity checked."""
    kind, root = source_root(data, title.get('source', 'exodos'))
    source = root / title['path']
    if kind == 'zip':
        if not source.is_file():
            raise DriveError(f'{source} not found; set {data["sources"][title.get("source", "exodos")]["env"]}')
        got = sha256(source)
        archive = zipfile.ZipFile(source)
        items = {item.filename: (lambda item=item: archive.read(item),
                                 calendar.timegm(item.date_time + (0, 0, -1)))
                 for item in archive.infolist() if not item.is_dir()}
    else:
        if not source.is_dir():
            raise DriveError(f'{source} not found; set {data["sources"][title.get("source", "exodos")]["env"]}')
        got = tree_sha256(source)
        items = {path.relative_to(source).as_posix(): (path.read_bytes, freedos.FILE_TIME)
                 for path in source.rglob('*') if path.is_file()}
    if got != title['sha256']:
        raise DriveError(f'{title["id"]}: {source} hashes to {got}, the manifest pins {title["sha256"]}')
    return items


def source_root(data: dict, name: str) -> tuple[str, Path]:
    spec = data['sources'][name]
    return spec['kind'], Path(os.environ.get(spec['env'], spec['default'])).expanduser()


def extract(data: dict, title: dict, files: Path) -> tuple[int, int]:
    """Write a title's copy entries under files, in order; return the files
    written and the non-8.3 names skipped."""
    items = source_files(data, title)
    written, skipped = set(), 0
    for copy in title['copy']:
        prefix = copy['from'].rstrip('/') + '/' if copy['from'] else ''
        skip = {part.upper() for part in copy.get('skip', [])}
        target = [dos_name(part) for part in copy['to'].split('\\') if part]
        if not target or None in target:
            raise DriveError(f'{title["id"]}: copy target {copy["to"]!r} is not a DOS directory')
        matched = 0
        for filename in sorted(items):
            if not filename.startswith(prefix):
                continue
            parts = filename[len(prefix):].split('/')
            if len(parts) > 1 and (copy.get('flat') or parts[0].upper() in skip):
                continue
            names = [dos_name(part) for part in parts]
            if None in names:
                skipped += 1
                continue
            path = files.joinpath(*target, *names)
            path.parent.mkdir(parents=True, exist_ok=True)
            read, stamp = items[filename]
            path.write_bytes(read())
            os.utime(path, (stamp, stamp))
            written.add(path)
            matched += 1
        if not matched:
            raise DriveError(f'{title["id"]}: no files under {copy["from"]!r} in {title["path"]}')
    for config in title.get('config', []):
        sidecar = STORE / 'sidecars' / config['sha256']
        if not sidecar.is_file() or sha256(sidecar) != config['sha256']:
            raise DriveError(f'{title["id"]}: sidecar {config["sha256"]} is missing from {sidecar.parent}')
        path = files.joinpath(*(dos_name(part) or part for part in config['path'].split('\\') if part))
        if path not in written:
            raise DriveError(f'{title["id"]}: config {config["path"]} replaces no file the copy wrote')
        stamp = int(path.stat().st_mtime)
        shutil.copyfile(sidecar, path)
        os.utime(path, (stamp, stamp))
    return len(written), skipped


def write_tree(files: Path, volume: str, env: dict, at: str = '::') -> None:
    """Copy files to the volume: each directory's subdirectories, then its
    files, in sorted order, so the directory entries are always the same."""
    children = sorted(files.iterdir())
    directories = [child for child in children if child.is_dir()]
    plain = [child for child in children if not child.is_dir()]
    for child in directories:
        freedos.mtools('mmd', '-i', volume, at + child.name, env=env)
    if plain:
        freedos.mtools('mcopy', '-m', '-i', volume, *map(str, plain), at, env=env)
    for child in directories:
        write_tree(child, volume, env, at + child.name + '/')


def free_bytes(volume: str, env: dict) -> int:
    listing = subprocess.run(['mdir', '-i', volume, '::'], capture_output=True, text=True, env=env).stdout
    match = re.search(r'([\d ]+) bytes free', listing)
    if not match:
        raise DriveError('mdir printed no free space')
    return int(match.group(1).replace(' ', ''))


def build(name: str, data: dict, download: bool = False, jobs: int = 4) -> Path:
    drive = data['drives'][name]
    STORE.mkdir(parents=True, exist_ok=True)
    work = STORE / '.work' / name
    work.parent.mkdir(mode=0o700, exist_ok=True)
    os.chmod(work.parent, 0o700)
    try:
        disk, packages = freedos.system_disk(PROFILE, drive['cylinders'], work, download, jobs)
        os.chmod(work, 0o700)
        env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', MTOOLS_NO_VFAT='1', TZ='UTC',
                   SOURCE_DATE_EPOCH=str(freedos.FILE_TIME))
        volume = f'{disk}@@{freedos.PARTITION_START * 512}'
        files = work / 'titles'
        titles = []
        for title in drive.get('titles', []):
            print(f'{name}: {title["id"]} from {title["path"]}', flush=True)
            count, skipped = extract(data, title, files)
            titles.append({key: title[key] for key in title if key not in ('copy', 'config')} |
                          {'files': count, 'skipped_long_names': skipped,
                           'config': [config['sha256'] for config in title.get('config', [])]})
        if files.exists():
            occupied = {child.name for child in files.iterdir()} & {'FDOS', 'KERNEL.SYS', 'COMMAND.COM',
                                                                    'FDCONFIG.SYS', 'AUTOEXEC.BAT'}
            if occupied:
                raise DriveError(f'drive {name}: titles would replace the system files {sorted(occupied)}')
            write_tree(files, volume, env)
        shutil.rmtree(files, ignore_errors=True)
        free = free_bytes(volume, env)
        digest = sha256(disk)
        image = STORE / f'{digest}.img'
        shutil.copyfile(disk, image.with_suffix('.partial'))
        image.with_suffix('.partial').rename(image)
        record = {
            'drive': name,
            'sha256': digest,
            'bytes': image.stat().st_size,
            'geometry': {'cylinders': drive['cylinders'], 'heads': freedos.HEADS, 'sectors_per_track': freedos.SPT},
            'base': {'profile': PROFILE, 'packages': {package: pins['commit'] for package, pins in packages.items()},
                     'freedos_inputs': freedos.inputs_key(PROFILE)},
            'inputs': drive_inputs(data, name),
            'description': drive.get('description', ''),
            'free_bytes': free,
            'titles': titles,
            'distribution': 'user-supplied programs: never commit, upload or distribute',
        }
        image.with_suffix('.json').write_text(json.dumps(record, indent=1) + '\n')
        links = STORE / 'by-name'
        links.mkdir(exist_ok=True)
        for suffix in ('.img', '.json'):
            link = links / f'{name}{suffix}'
            link.unlink(missing_ok=True)
            link.symlink_to(Path('..') / f'{digest}{suffix}')
        return image
    finally:
        shutil.rmtree(work, ignore_errors=True)


def resolve(reference: str, store: Path | None = None, verify: bool = True) -> tuple[Path, dict]:
    """A drive by path, by SHA-256 (or a unique prefix of 8+ digits) or by
    name in the store; returns the image and its record, the bytes checked."""
    store = store or STORE
    path = Path(reference).expanduser()
    if path.suffix == '.img' and path.is_file():
        image = path.resolve()
    elif HEX.match(reference):
        matches = sorted(store.glob(f'{reference}*.img'))
        if len(matches) != 1:
            raise DriveError(f'{len(matches)} drives in {store} start with {reference}')
        image = matches[0]
    elif (store / 'by-name' / f'{reference}.img').exists():
        image = (store / 'by-name' / f'{reference}.img').resolve()
    else:
        raise DriveError(f'no drive {reference!r}: not an image path, a SHA-256 or a name in {store}/by-name')
    record_path = image.with_suffix('.json')
    record = json.loads(record_path.read_text()) if record_path.is_file() else {}
    if verify:
        digest = sha256(image)
        if record.get('sha256') not in (None, digest):
            raise DriveError(f'{image} hashes to {digest}, its record says {record["sha256"]}')
        record.setdefault('sha256', digest)
    return image, record


def title_of(record: dict, title: str) -> dict:
    for entry in record.get('titles', []):
        if entry['id'] == title:
            return entry
    raise DriveError(f'drive {record.get("drive")} has no title {title}; it has '
                     + ', '.join(entry['id'] for entry in record.get('titles', [])))


def geometry(record: dict) -> str:
    """The X-FogCast-Disk-Geometry value a kit launch sends."""
    shape = record['geometry']
    return f'{shape["cylinders"]}/{shape["heads"]}/{shape["sectors_per_track"]}'


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('drives', nargs='*', help='drives to build (default: every drive in the manifest)')
    parser.add_argument('--manifest', type=Path, default=MANIFEST)
    parser.add_argument('--download', action='store_true', help='fetch the pinned FreeDOS packages and NASM if missing')
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 4)
    parser.add_argument('--list', action='store_true', help="print each drive's titles and the store's builds")
    parser.add_argument('--resolve', metavar='DRIVE', help='print the image path and record of a drive')
    args = parser.parse_args(argv)
    try:
        if args.resolve:
            image, record = resolve(args.resolve)
            print(json.dumps({'image': str(image), **record}, indent=1))
            return 0
        data = manifest(args.manifest)
        if args.list:
            for name, drive in data['drives'].items():
                built = (STORE / 'by-name' / f'{name}.img')
                print(f'{name}: {len(drive.get("titles", []))} titles, {drive["cylinders"]} cylinders, '
                      f'built {built.resolve().stem if built.exists() else "never"}')
            return 0
        for name in args.drives or list(data['drives']):
            if name not in data['drives']:
                raise DriveError(f'no drive {name} in {args.manifest}')
            image = build(name, data, args.download, args.jobs)
            record = json.loads(image.with_suffix('.json').read_text())
            print(f'{name} {image} {record["free_bytes"]:,} bytes free')
    except (DriveError, freedos.ImageError, test386.Test386Error, OSError) as error:
        print(f'build_pc386_game_drive: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
