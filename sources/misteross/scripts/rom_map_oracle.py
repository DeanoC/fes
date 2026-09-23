#!/usr/bin/env python3
"""Generate synthetic-only Mistral ROM linker qualification artifacts on a host.

No ROM distribution, FPGA compiler build, package export or hardware access.
The minimal device images are test fixtures, not executable core packages.
"""
from __future__ import annotations

import argparse
import hashlib
import gzip
import io
import json
from pathlib import Path
import random
import subprocess

try:
    from .link_static_rbf import encode_m10k_ram_word, pack_1024x10, format_ram40, read_m10k_init_bt, decode_m10k_ram_word, unpack_1024x10
    from .rom_map import build_rom_map
    from .cyclonev_rbf import rbf_load
except ImportError:
    from link_static_rbf import encode_m10k_ram_word, pack_1024x10, format_ram40, read_m10k_init_bt, decode_m10k_ram_word, unpack_1024x10
    from rom_map import build_rom_map
    from cyclonev_rbf import rbf_load


def generate(mistral: Path, source: Path, output: Path) -> None:
    output.mkdir(parents=True, exist_ok=True)
    rng = random.Random(0)
    patterns = {'blank': bytes(8192), 'ones': b'\xff'*8192,
                'ramp': bytes(range(256))*32,
                'walking': bytes(1 << (i % 8) for i in range(8192)),
                'random': bytes(rng.randrange(256) for _ in range(8192))}
    for name, data in patterns.items():
        lines = ['m 5CSEBA6U23I7']
        for index, row in enumerate(range(73, 81)):
            for word, value in enumerate(pack_1024x10(data[index*1024:(index+1)*1024])):
                lines.append(f's M10K.005.{row:03d}:RAM.{word} {format_ram40(encode_m10k_ram_word(value))}')
        bt = output/f'{name}.bt'
        bt.write_text('\n'.join(lines)+'\n')
        (output/f'{name}.rom').write_bytes(data)
        subprocess.run([str(mistral), 'comp', str(bt), str(output/f'{name}.rbf')], check=True)
    mapping, evidence = build_rom_map(source, (output/'blank.rbf').read_bytes())
    encoded = (json.dumps(mapping, separators=(',', ':'))+'\n').encode()
    (output/'map.json').write_bytes(encoded)
    evidence['map_sha256'] = hashlib.sha256(encoded).hexdigest()
    evidence['mistral_cv_sha256'] = hashlib.sha256(mistral.read_bytes()).hexdigest()
    evidence['synthetic_only'] = True
    blank = rbf_load((output/'blank.rbf').read_bytes())
    mask = bytearray(len(blank.cram))
    for block in mapping['blocks']:
        for word in block['word_bits']:
            for bit in word:
                mask[bit >> 3] |= 1 << (bit & 7)
    evidence['checks'] = {}
    for name, data in patterns.items():
        readback = output/f'{name}.readback.bt'
        subprocess.run([str(mistral), 'decomp', '5CSEBA6U23I7',
                        str(output/f'{name}.rbf'), str(readback)], check=True)
        text = readback.read_text()
        restored = b''.join(unpack_1024x10([decode_m10k_ram_word(word) for word in
                            read_m10k_init_bt(text, f'MISTRAL_M10K.5.{row}.0')])
                            for row in range(73, 81))
        if restored != data:
            raise ValueError(f'{name}: Mistral ROM readback mismatch')
        linked = rbf_load((output/f'{name}.rbf').read_bytes())
        if linked.header != blank.header or any((a ^ b) & ~allowed for a, b, allowed
                                              in zip(blank.cram, linked.cram, mask)):
            raise ValueError(f'{name}: change outside declared INIT destinations')
        evidence['checks'][name] = {'mistral_readback': True, 'outside_map_changes': 0}

    evidence['fixture_sha256'] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                 for p in sorted(output.iterdir()) if p.suffix in ('.rom', '.rbf', '.bt')}
    (output/'oracle-provenance.json').write_text(json.dumps(evidence, indent=2)+'\n')


def export_fixtures(output: Path, destination: Path) -> None:
    """Publish modest, deterministic gzip fixtures and Mistral golden hashes."""
    evidence = json.loads((output/'oracle-provenance.json').read_text())
    destination.mkdir(parents=True, exist_ok=True)
    for name in ['blank.rbf', 'map.json'] + [f'{case}.rom' for case in evidence['checks']]:
        buffer = io.BytesIO()
        with gzip.GzipFile(filename='', mode='wb', fileobj=buffer, mtime=0) as stream:
            stream.write((output/name).read_bytes())
        (destination/(name+'.gz')).write_bytes(buffer.getvalue())
    cases = []
    for name in evidence['checks']:
        data = (output/f'{name}.rbf').read_bytes()
        cases.append({'name': name, 'rom_gzip': f'{name}.rom.gz',
                      'rom_sha256': evidence['fixture_sha256'][f'{name}.rom'],
                      'rbf_sha256': hashlib.sha256(data).hexdigest(), 'rbf_size': len(data)})
    record = {'format': 1, 'synthetic_only': True, 'cases': cases,
              'provenance': evidence}
    (destination/'oracle.json').write_text(json.dumps(record, indent=2)+'\n')


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mistral-cv', type=Path, required=True)
    parser.add_argument('--mistral-source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, help='optional checked-in fixture output directory')
    args = parser.parse_args()
    generate(args.mistral_cv.resolve(), args.mistral_source, args.output)
    if args.fixtures:
        export_fixtures(args.output, args.fixtures)


if __name__ == '__main__':
    main()
