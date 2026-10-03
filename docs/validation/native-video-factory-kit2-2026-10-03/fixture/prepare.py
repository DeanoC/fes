#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Reproduce the existing BIOS-free graphics/SN and SGM probe fixtures offline."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

BASE = Path(__file__).resolve().parent
ROOT = BASE.parents[2]
DIAGNOSTIC = ROOT / 'sources/misteross/cores/fes-coleco/diagnostic'
sys.path.insert(0, str(BASE.parent / 'analysis'))
import native_oracle


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    sys.path.insert(0, str(DIAGNOSTIC))
    spec = importlib.util.spec_from_file_location('open_sgm_probe', DIAGNOSTIC / 'sgm_probe.py')
    sgm = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(sgm)
    variants = {'graphics-sn': native_oracle.tone_adapter(native_oracle.emitted_rom()),
                'sgm-probe': sgm.cartridge()}
    expected = {'graphics-sn': (1086, 'e9e63faa08c53de62c819b85ebf4d030bfbfb8015e33e127e1eb178a4daeb73d'),
                'sgm-probe': (1261, '33705344c6ae221a8ec3b9c862996b6136b9c236052c9c6517e9926eeb2a5317')}
    for name, data in variants.items():
        assert (len(data), hashlib.sha256(data).hexdigest()) == expected[name]
        (BASE / (name + '.rom')).write_bytes(data)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    sources = {}
    for name in ('generate.py', 'sgm_probe.py', 'LICENSE'):
        path = DIAGNOSTIC / name
        relative = str(path.relative_to(ROOT))
        committed = subprocess.check_output(['git', 'show', revision + ':' + relative], cwd=ROOT)
        assert hashlib.sha256(committed).hexdigest() == digest(path)
        sources[name] = {'path': relative, 'sha256': digest(path), 'source_revision': revision}
    receipt = {'classification': 'Offline generated fixtures only; no kit or capture access.',
               'sources': sources,
               'preparation_sha256': digest(Path(__file__)),
               'sn_adapter_source': {'path': str(Path(native_oracle.__file__).relative_to(ROOT)),
                                     'sha256': digest(Path(native_oracle.__file__))},
               'roms': {name: {'path': str(BASE / (name + '.rom')), 'sha256': expected[name][1],
                               'size': expected[name][0], 'sgm_required': name == 'sgm-probe'} for name in variants},
               'nominal_tones_hz': {'SN': 3579545 / (32 * 516), 'AY': 1789772.5 / (16 * 254)},
               'probe': {'visual_pass': 'The checkerboard is enabled only after all existing SGM checks pass.',
                         'memory_reads': 8, 'memory_writes': 7, 'ay_register_readbacks': 4,
                         'scope': 'RAM-window boundary sentinels and console-RAM isolation; not an exhaustive SRAM test.'},
               'license': {'path': str(BASE / 'LICENSE'), 'sha256': digest(BASE / 'LICENSE'),
                           'sgm_probe_source_notice': 'SPDX-License-Identifier: MIT; Copyright (c) 2026 FES contributors'},
               'native_picture': {'width': 256, 'height': 192, 'scale': 2,
                                  'viewport_exclusive': [384, 168, 896, 552], 'horizontal_delay_pixels': 0}}
    (BASE / 'prepared.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    print(json.dumps(receipt, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
