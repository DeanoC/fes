#!/usr/bin/env python3
"""Verify the original evidence index after lossless reconstruction."""
from pathlib import Path, PurePosixPath
import hashlib
import subprocess
import sys
import tempfile
import zlib

HERE = Path(__file__).resolve().parent
BASE = '9037d15e84348c452885a236ec0d9f74ca889e07'
BASE_PATH = 'sources/misteross/cores/fes-common/sim/i386/lockstep_tb.cpp'
BASE_SHA = '4315693cd31614c2569fcace7ed5bca8e57bec8f14ed97851f7f78bc4461c369'
INDEX_SHA = '487fa9a6ef9081fbbbb671118de6e3deda68fa97c904d53028b9c5e21a7a338e'
MANIFEST_SHA = '9b71c3b80a92bfa77ac1e18694bc702bc5eab2e589b86725945d5e67314e0876'
MANIFEST_GZ_SHA = '4d42553423d5323d8ad4884d695ef722bed973d60f8ea8b096265183d233ec9a'
PATCH_SHA = 'c82ba6cb667ad442f28bddc90cae7258045abafbfed901d94fa48519ab4b1b6e'
HARNESS_SHA = 'ec0c705de53b4f86d13af5761ea0111e386a94bfcebb79a58b093582713cd261'


def checked(data, digest, label):
    if hashlib.sha256(data).hexdigest() != digest:
        raise ValueError(f'{label}: SHA-256 mismatch')
    return data


def main():
    index = checked((HERE / 'SHA256SUMS').read_bytes(), INDEX_SHA, 'historical index')
    compressed = checked((HERE / 'reference/before-frozen.json.gz').read_bytes(), MANIFEST_GZ_SHA, 'compressed manifest')
    dec = zlib.decompressobj(wbits=31)
    manifest = dec.decompress(compressed, 591451)
    if not dec.eof or dec.unused_data or dec.unconsumed_tail or len(manifest) != 591450:
        raise ValueError('manifest gzip is truncated, oversized, multi-member or has trailing bytes')
    checked(manifest, MANIFEST_SHA, 'reconstructed manifest')
    patch = checked((HERE / 'cpu-extra/instrumented-lockstep_tb.patch').read_bytes(), PATCH_SHA, 'harness patch')
    try:
        base = subprocess.run(['git', '-C', str(HERE), 'show', f'{BASE}:{BASE_PATH}'],
                              check=True, capture_output=True).stdout
    except subprocess.CalledProcessError as e:
        raise ValueError(f'exact Git base {BASE} is unavailable; fetch its history') from e
    checked(base, BASE_SHA, 'Git base harness')
    with tempfile.TemporaryDirectory(prefix='fes1902-evidence-') as temp:
        harness_path = Path(temp) / 'lockstep_tb.cpp'
        harness_path.write_bytes(base)
        subprocess.run(['git', 'apply', '--no-index', '-'], cwd=temp, input=patch,
                       check=True, capture_output=True)
        harness = harness_path.read_bytes()
    if len(harness) != 674468:
        raise ValueError('reconstructed harness: size mismatch')
    checked(harness, HARNESS_SHA, 'reconstructed harness')
    reconstructed = {'reference/before-frozen.json': manifest,
                     'cpu-extra/instrumented-lockstep_tb.cpp': harness}
    seen = set()
    for line in index.decode('utf-8').splitlines():
        digest, name = line.split('  ', 1)
        path = PurePosixPath(name)
        if len(digest) != 64 or not name.startswith('./') or '..' in path.parts or path.is_absolute():
            raise ValueError(f'invalid historical entry: {name}')
        key = str(path)
        if key in seen:
            raise ValueError(f'duplicate historical entry: {name}')
        seen.add(key)
        data = reconstructed[key] if key in reconstructed else (HERE / key).read_bytes()
        checked(data, digest, name)
    if not set(reconstructed).issubset(seen):
        raise ValueError('historical index omits a reconstructed file')
    print(f'PASS: {len(seen)} historical entries, including both byte-exact reconstructions')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, zlib.error, subprocess.CalledProcessError) as exc:
        print(f'FAIL: {exc}', file=sys.stderr)
        sys.exit(1)
