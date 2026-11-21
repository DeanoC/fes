#!/usr/bin/env python3
"""Fetch and stage the firmware and disks FES ships beside its cores (#1344).

Only images whose licence or permission allows redistribution are listed
here; docs/bundled-firmware.md records why. No firmware byte enters Git or a
sealed core package. Each firmware image is taken from a pinned archive; the
PC/XT FreeDOS boot disk is the pinned image in this tree, checked against the
FreeDOS packages it was assembled from. Every image is checked by SHA-256 and
staged as its own directory (manifest.json, the image and any licence text)
that the host imports and uses by default. Users can still choose their own
image for any title.
"""
import argparse
import hashlib
from io import BytesIO
import json
import os
from pathlib import Path
import tarfile
import tempfile
import urllib.request
from zipfile import ZipFile

from recipes import shared_cache_root

ROOT = Path(__file__).resolve().parents[1]
MAX_ARCHIVE = 8 * 1024 * 1024

ARCHIVES = {
    # Debian non-free's copy of the Amstrad ROMs. Its 48.rom, 128-0/1.rom and
    # plus3-0..3.rom are the images the 2026-10-22 Spectrum kit check booted
    # (docs/validation/2026-10-22-spectrum-models-seal.md).
    'spectrum-roms': {
        'url': 'https://deb.debian.org/debian/pool/non-free/s/spectrum-roms/spectrum-roms_20081224.orig.tar.gz',
        'sha256': '1ada437a6b05c91f51b6543da7ef8b0a9043df91203700b213b68147bc2be634',
    },
    # The official EmuTOS 1.4 192 KiB images, as fetch_atari_st_emutos.py pins.
    'emutos-192k': {
        'url': 'https://downloads.sourceforge.net/project/emutos/emutos/1.4/emutos-192k-1.4.zip',
        'sha256': '59abac06a2d29b0864c5a7cfb2af65f022c337aed34188e174a9a08cc737e4bc',
    },
    # The official FreeDOS 1.4 kernel and FreeCOM packages. Each carries its
    # binaries and, in SOURCE/*/SOURCES.ZIP, their complete source.
    'freedos-kernel': {
        'url': 'https://www.ibiblio.org/pub/micro/pc-stuff/freedos/files/repositories/1.4/base/kernel.zip',
        'sha256': '43ed55fec6e795bb24e658302c7fae9eec9eb8dfeaba15734317b3f8302706d6',
    },
    'freedos-freecom': {
        'url': 'https://www.ibiblio.org/pub/micro/pc-stuff/freedos/files/repositories/1.4/base/freecom.zip',
        'sha256': '3a7cfa03cce242031926cd67b69e1fd030d1c27127885d927629945f3137b6f0',
    },
}

# The sentence Amstrad asked emulator authors to show (Cliff Lawson, 1999).
AMSTRAD_PERMISSION = ('Amstrad have kindly given their permission for the redistribution of '
                      'their copyrighted material but retain that copyright.')
AMSTRAD = {
    'licence': 'LicenseRef-Amstrad-ROM-permission',
    'licence_name': "Amstrad's permission to redistribute",
    'notice': ('These ROMs are copyright Amstrad and their copyright messages are unaltered. ' +
               AMSTRAD_PERMISSION + ' FES makes no charge for them.'),
}


FIRMWARE = (
    dict(AMSTRAD, kind='firmware', id='amstrad-spectrum-48k', name='ZX Spectrum 48K ROM',
         version='48K BASIC (spectrum-roms 20081224)',
         core_id='fes.spectrum', rom_id='spectrum-firmware', file='spectrum-48k.rom',
         archive='spectrum-roms', members=('spectrum-roms/48.rom',), size=16384,
         sha256='d55daa439b673b0e3f5897f99ac37ecb45f974d1862b4dadb85dec34af99cb42'),
    dict(AMSTRAD, kind='firmware', id='amstrad-spectrum-128k', name='ZX Spectrum 128K ROMs',
         version='128K ROM 0 and ROM 1 (spectrum-roms 20081224)',
         core_id='fes.spectrum128', rom_id='spectrum128-firmware', file='spectrum-128k.rom',
         archive='spectrum-roms', members=('spectrum-roms/128-0.rom', 'spectrum-roms/128-1.rom'),
         size=32768, sha256='c1ff621d7910105d4ee45c31e9fd8fd0d79a545c78b66c69a562ee1ffbae8d72'),
    dict(AMSTRAD, kind='firmware', id='amstrad-spectrum-plus3', name='ZX Spectrum +2A/+3 ROMs',
         version='+3 ROMs 0-3 with +3DOS (spectrum-roms 20081224)',
         core_id='fes.spectrum-plus3', rom_id='spectrum-plus3-firmware', file='spectrum-plus3.rom',
         archive='spectrum-roms', members=tuple('spectrum-roms/plus3-%d.rom' % n for n in range(4)),
         size=65536, sha256='ee8218fa43ecb672ed45370114294228213a82318c6d1b007ec86bee3293d1f2'),
    dict(kind='firmware', id='emutos-192us', name='EmuTOS', version='1.4, 192 KiB US image (etos192us.img)',
         core_id='fes.atari-st', rom_id='atari-st-firmware', file='etos192us.img',
         archive='emutos-192k', members=('emutos-192k-1.4/etos192us.img',), size=196608,
         sha256='8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc',
         # The Caldera GEM files carry no "or later", so the binary is
         # GPL-2.0-only even though EmuTOS's own files allow later versions.
         licence='GPL-2.0-only', licence_name='GNU General Public License v2',
         licence_member='emutos-192k-1.4/doc/license.txt',
         notice=('EmuTOS 1.4 is free software from the EmuTOS team, distributed under the '
                 'GNU General Public License version 2. Its complete source code '
                 '(tag VERSION_1_4, commit 978e37569bff95841e42675d11fcc6799aad8483) is '
                 'published beside this image.'),
         sources=({'url': 'https://downloads.sourceforge.net/project/emutos/emutos/1.4/emutos-src-1.4.tar.gz',
                   'sha256': '98977ab2593d9ab81d6fcf9629f6781369912e3fe05c224a35147d5665749702'},)),
    # The PC/XT starts from this disk while no disk is chosen, and the host
    # copies it for each new "Starts FreeDOS" disk (Deano, 21 November 2026).
    # build_pcxt_freedos_image.py assembled it from the kernel's boot code and
    # KERNL86N.SYS and FreeCOM's COMMAND.COM; staging checks both binaries are
    # in the image byte for byte.
    dict(kind='disk', id='freedos-xt-360', name='FreeDOS boot disk',
         version='FreeDOS 1.4: kernel 2043 (8086) and FreeCOM 0.86a, 360 KiB',
         core_id='fes.pcxt-dos', file='freedos-xt-360.img', size=368640,
         tree_file='sources/misteross/cores/fes-pcxt/firmware/freedos-1.4-xt-360.img',
         sha256='140a2d0a507f7452790e73416dd4dab2c66a89e09a71e26b2ca8f95d1f3c5e5c',
         contains=(('freedos-kernel', 'BIN/KERNL86N.SYS'), ('freedos-freecom', 'BIN/COMMAND.COM')),
         licence='GPL-2.0-or-later AND GPL-2.0-only', licence_name='GNU General Public License v2',
         licence_archive='freedos-kernel', licence_member='DOC/KERNEL/COPYING',
         notice=('FreeDOS is free software. This disk holds the FreeDOS 1.4 kernel '
                 '(GPL-2.0-or-later) and the FreeCOM command shell (GPL-2.0). Their '
                 'complete source is in the FreeDOS 1.4 kernel and FreeCOM packages, '
                 'published beside this image.'),
         origin_archives=('freedos-kernel', 'freedos-freecom')),
)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def fetch(url, digest, cache):
    """Return the bytes at url, from cache when its digest still matches."""
    cache.mkdir(parents=True, exist_ok=True)
    path = cache / url.rsplit('/', 1)[-1]
    data = path.read_bytes() if path.is_file() else b''
    if sha256(data) != digest:
        with urllib.request.urlopen(url, timeout=120) as response:
            data = response.read(MAX_ARCHIVE + 1)
        if len(data) > MAX_ARCHIVE or sha256(data) != digest:
            raise ValueError(f'{url} does not match its pinned SHA-256')
        with tempfile.NamedTemporaryFile(dir=cache, delete=False) as temporary:
            temporary.write(data)
        os.replace(temporary.name, path)
    return data


def read_members(name, data, members):
    """Read regular members from a pinned .tar.gz or .zip archive."""
    out = []
    if name.endswith('.zip'):
        with ZipFile(BytesIO(data)) as archive:
            for member in members:
                out.append(archive.read(member))
        return out
    with tarfile.open(fileobj=BytesIO(data), mode='r:gz') as archive:
        for member in members:
            info = archive.getmember(member)
            if not info.isreg():
                raise ValueError(f'{member} is not a regular file')
            out.append(archive.extractfile(info).read())
    return out


def origin_names(entry):
    """The archives an entry's bytes come from, in manifest order."""
    return tuple(entry.get('origin_archives') or (entry['archive'],))


def build(entry, fetched, archives=ARCHIVES, root=ROOT):
    """Return the image and licence text for one entry.

    fetched maps each of the entry's archive names to the archive's bytes.
    """
    def member(name, path):
        return read_members(archives[name]['url'], fetched[name], [path])[0]

    if entry.get('tree_file'):
        image = (Path(root) / entry['tree_file']).read_bytes()
        for name, path in entry.get('contains', ()):
            if member(name, path) not in image:
                raise ValueError(f"{entry['id']} image does not hold {path} from {name}")
    else:
        image = b''.join(read_members(archives[entry['archive']]['url'], fetched[entry['archive']], entry['members']))
    if len(image) != entry['size'] or sha256(image) != entry['sha256']:
        raise ValueError(f"{entry['id']} image does not match its pinned SHA-256")
    licence = None
    if entry.get('licence_member'):
        licence = member(entry.get('licence_archive', entry.get('archive')), entry['licence_member'])
    return image, licence


def manifest(entry, archives):
    keys = ['kind', 'id', 'name', 'version', 'core_id', 'file', 'size', 'sha256',
            'licence', 'licence_name', 'notice']
    if entry['kind'] == 'firmware':
        keys.insert(5, 'rom_id')
    out = {key: entry[key] for key in keys}
    out['format'] = 2
    if entry.get('licence_member'):
        out['licence_file'] = 'LICENSE.txt'
    out['origins'] = [{'url': archives[n]['url'], 'sha256': archives[n]['sha256']} for n in origin_names(entry)]
    sources = entry_sources(entry, archives)
    if sources:
        out['sources'] = sources
    return out


def entry_sources(entry, archives):
    """The corresponding source: named tarballs, or the origin packages that carry it."""
    if entry.get('sources'):
        return [dict(s) for s in entry['sources']]
    if entry['kind'] == 'disk':
        return [{'url': archives[n]['url'], 'sha256': archives[n]['sha256']} for n in origin_names(entry)]
    return []


def stage(output, cache, firmware=FIRMWARE, archives=ARCHIVES, fetcher=fetch, root=ROOT):
    """Write each object under output and return their manifests."""
    output = Path(output)
    if output.exists() and any(output.iterdir()):
        raise ValueError(f'{output} is not empty')
    output.mkdir(parents=True, exist_ok=True)
    fetched = {}
    staged = []
    for entry in firmware:
        for name in set(origin_names(entry)) | {n for n, _ in entry.get('contains', ())} | (
                {entry['licence_archive']} if entry.get('licence_archive') else set()):
            if name not in fetched:
                fetched[name] = fetcher(archives[name]['url'], archives[name]['sha256'], Path(cache))
        image, licence = build(entry, fetched, archives, root)
        data = manifest(entry, archives)
        directory = output / entry['id']
        directory.mkdir()
        files = {entry['file']: image, 'manifest.json': (json.dumps(data, indent=2, ensure_ascii=False) + '\n').encode()}
        if licence is not None:
            files['LICENSE.txt'] = licence
        for name, content in files.items():
            (directory / name).write_bytes(content)
            (directory / name).chmod(0o444)
        staged.append(data)
    return staged


def fetch_sources(output, cache, firmware=FIRMWARE, fetcher=fetch, archives=ARCHIVES):
    """Copy each free software image's corresponding source to output."""
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    written = []
    for entry in firmware:
        for source in entry_sources(entry, archives):
            path = output / source['url'].rsplit('/', 1)[-1]
            if str(path) in written:
                continue
            path.write_bytes(fetcher(source['url'], source['sha256'], Path(cache)))
            written.append(str(path))
    return written


def default_cache_dir():
    return shared_cache_root() / 'firmware'


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--output', type=Path, required=True, help='new directory for the bundled objects')
    parser.add_argument('--cache', type=Path, default=default_cache_dir())
    parser.add_argument('--sources', type=Path, help='also copy corresponding source archives here')
    args = parser.parse_args()
    staged = stage(args.output, args.cache)
    result = {'output': str(args.output.resolve()), 'firmware': [{'id': m['id'], 'sha256': m['sha256']} for m in staged]}
    if args.sources:
        result['sources'] = fetch_sources(args.sources, args.cache)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError) as error:
        raise SystemExit('bundled-firmware: ' + str(error))
