#!/usr/bin/env python3
"""Select the authenticated DE25 compiler archive for normal kit development."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile

sys.path.insert(0, str(Path(__file__).resolve().parent))

from recipes import shared_cache_root
from kit_software import de25_sdk, load

ROOT = Path(__file__).resolve().parents[1]


def ensure_de25(root, image):
    root, image = Path(root).resolve(), Path(image).resolve()
    selected = de25_sdk(root, image)
    # The independently selected board is part of the expected cache identity;
    # cached JSON cannot turn a DE10 toolchain into a DE25 compiler.
    binding = {'board': 'de25-nano', 'architecture': 'aarch64',
               'archive_sha256': selected['archive_sha256']}
    key = hashlib.sha256(b'FES-DE25-SDK-CACHE-1\n' +
                         json.dumps(binding, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    cache = load(root / 'image/scripts/toolchain_cache.py', 'kit_de25_cache')
    shared = cache.shared_root(root / 'image') or shared_cache_root() / 'target-image'
    directory = shared / 'toolchains' / key
    if cache.validated_sha(directory, key) != selected['archive_sha256']:
        cache.import_archive(image / 'aarch64-sdk.tar.gz', directory, key, selected['archive_sha256'])
    if cache.validated_sha(directory, key) != selected['archive_sha256']:
        raise ValueError('DE25 SDK cache does not match its authenticated image')
    return {'board': 'de25-nano', 'architecture': 'aarch64', 'key': key,
            'directory': str(directory), 'archive_sha256': selected['archive_sha256'], 'source': selected}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--board', choices=('de10-nano', 'de25-nano'), default='de10-nano')
    parser.add_argument('--de25-image', type=Path)
    args = parser.parse_args()
    try:
        if args.board != 'de25-nano' or args.de25_image is None:
            raise ValueError('DE25 SDK selection requires --board de25-nano and --de25-image together; use make kit-sdk for DE10')
        print(json.dumps(ensure_de25(ROOT, args.de25_image), indent=2, sort_keys=True))
    except (OSError, ValueError, subprocess.CalledProcessError, tarfile.TarError) as error:
        parser.exit(1, f'kit-sdk: {error}\n')


if __name__ == '__main__':
    main()
