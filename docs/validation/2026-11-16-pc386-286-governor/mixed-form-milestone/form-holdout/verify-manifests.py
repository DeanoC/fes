#!/usr/bin/env python3
"""Verify the two originally byte-identical frozen VM manifests."""
from pathlib import Path
import hashlib
import sys
import tempfile
import zlib

HERE = Path(__file__).resolve().parent
ARCHIVE_SHA = '40725658be8f0b8cca8cd49c16ef59fdfd7fdeb4a6e5fdb0c9d6ef60fffc5182'
ORIGINAL_SHA = '92b7bd0e462eb25993bcde5e74d6231d6ac7569f1cd2bf532ad70f3155552ff8'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    data = (HERE / 'reference-frozen-manifest.json.gz').read_bytes()
    if sha(data) != ARCHIVE_SHA:
        raise ValueError('compressed manifest SHA-256 mismatch')
    dec = zlib.decompressobj(wbits=31)
    original = dec.decompress(data, 532487)
    if not dec.eof or dec.unused_data or dec.unconsumed_tail or len(original) != 532486:
        raise ValueError('manifest gzip is truncated, oversized, multi-member or has trailing bytes')
    if sha(original) != ORIGINAL_SHA:
        raise ValueError('original manifest SHA-256 mismatch')
    with tempfile.TemporaryDirectory(prefix='fes1902-manifests-') as temp:
        root = Path(temp) / 'reference-vm'
        root.mkdir()
        for name in ('frozen-before.json', 'frozen-after.json'):
            reconstructed = root / name
            reconstructed.write_bytes(original)
            if sha(reconstructed.read_bytes()) != ORIGINAL_SHA:
                raise ValueError(f'{name}: reconstruction mismatch')
    print('PASS: both historical manifests reconstruct byte-for-byte')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, zlib.error) as exc:
        print(f'FAIL: {exc}', file=sys.stderr)
        sys.exit(1)
