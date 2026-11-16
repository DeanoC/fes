#!/usr/bin/env python3
"""Check retained probe bytes, controls, block-count arithmetic and CPU markers.

This verifies the evidence transcription, not the provenance of the emulator
outputs or correctness of the CPU model. Optionally rebuild the assembly with
--nasm /absolute/path/to/nasm and check the original binary digests.
"""
import argparse
import hashlib
import json
import re
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def check_hashes():
    for line in (HERE / 'SHA256SUMS').read_text().splitlines():
        expected, name = line.split('  ', 1)
        path = HERE / name
        if digest(path) != expected:
            raise SystemExit(f'hash mismatch: {name}')

def rows(group, names, controls=()):
    data = [(HERE / group / name).read_bytes() for name in names]
    if len(set(data)) != 1:
        raise SystemExit(f'{group}: repeats differ')
    content = data[0].decode('ascii')
    for control in controls:
        if control not in content.splitlines():
            raise SystemExit(f'{group}: missing {control}')
    return {name: int(value) for name, value in re.findall(r'^([A-Z_]+)=(\d+)\r?$', content, re.M)}

def clocks(count):
    return 12_500_000 * 64 / (18.2065 * 1000 * count)

def marker_charges(path):
    found = {}
    for line in path.read_text().splitlines():
        m = re.fullmatch(r'MARK 80 (\d+) (\d+) (\d+)', line)
        if m:
            key, cycle, charge = map(int, m.groups())
            if key in found:
                raise SystemExit(f'duplicate marker {key}')
            found[key] = (cycle, charge)
    if len(found) % 2 or set(found) != set(range(len(found))):
        raise SystemExit(f'incomplete markers in {path}')
    return [(found[i + 1][1] - found[i][1]) / 1000 for i in range(0, len(found), 2)]

def assemble(nasm):
    sources = {
        'sequence/heldout': 'c6f2d5c18361954508adbd95568ecec9332d7a7487b00c2e4c06723710b6b9f8',
        'forms/reference': 'd736a6bba022dec94d988037443988fbd75cfe61850631f164936fc8017fcee4',
        'forms/rtl': '9d88d26d937da57bb7744667dbd2c85d18cae4a08ffee8a6141088ab123f638f',
        'rep-verified/rep_verified': '7dfa8c3329fbe71bae54b7b108ad362ad7da50bb7e660ed96109d8d4c3f3d783',
        'rep-verified/cpu_rep': '108149fb8e4707bc3410aec00be6badadaa26343b92b53928bfd480b208f14e5',
        'rep-chunks/chunks': '4d3bd8e4c16c390bd60da22f0f9ca310580865f3d5b5af410ac72a74ef282523',
        'rep-chunks/cpu_chunks': '39ed6fe0a9ec6be804ac775988e538debadd9b651fef8a6f3f02ea83509beaaa',
    }
    with tempfile.TemporaryDirectory(prefix='fes1902-probes-') as temp:
        for source, expected in sources.items():
            output = Path(temp) / (source.replace('/', '_') + '.bin')
            subprocess.run([str(nasm), '-f', 'bin', '-o', str(output), str(HERE / (source + '.asm'))], check=True)
            if digest(output) != expected:
                raise SystemExit(f'assembled binary differs: {source}')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nasm', type=Path, help='optional NASM executable for a byte reproduction check')
    args = parser.parse_args()
    check_hashes()
    sequence = rows('sequence', ['H1.TXT', 'H2.TXT', 'H3.TXT'])
    if sequence != dict(BD=2436, A=1291, B=1155, C=1291, D=1416, E=1416):
        raise SystemExit('unexpected sequence counts')
    forms = rows('forms', ['SURVEY1.TXT', 'SURVEY2.TXT', 'SURVEY3.TXT'])
    comparison = json.loads((HERE / 'forms/comparison.json').read_text())
    rtl = marker_charges(HERE / 'forms/rtl.log')
    if list(forms) != list(comparison) or len(rtl) != len(forms):
        raise SystemExit('forms keys or markers differ')
    for (name, count), charge in zip(forms.items(), rtl):
        entry = comparison[name]
        if entry['blocks'] != count or abs(entry['reference_estimate_clocks_per_iteration'] - clocks(count)) > .0001 or abs(entry['rtl_clocks_per_iteration'] - charge) > .0001:
            raise SystemExit(f'forms arithmetic mismatch: {name}')
    rep = rows('rep-verified', ['V1.TXT', 'V2.TXT', 'V3.TXT'], ['DS_ES_MATCH', 'RAM_WRITE_VERIFIED'])
    if rep != dict(BD=2436, A=1372, B=1219, C=686, D=149, E=686):
        raise SystemExit('unexpected verified REP counts')
    chunks = rows('rep-chunks', ['C1.TXT', 'C2.TXT', 'C3.TXT'], ['DS_ES_MATCH', 'RAM_WRITE_VERIFIED'])
    if chunks != dict(BD=2436, A=344, B=333, C=323, D=305, E=180, F=175, G=149):
        raise SystemExit('unexpected chunk counts')
    charges = marker_charges(HERE / 'rep-chunks/cpu-v4.log')
    if charges != [128.006, 132.004, 136.004, 140.004, 240.004, 244.004, 288.004]:
        raise SystemExit('unexpected chunk CPU charges')
    differential = json.loads((HERE / 'cpu-differential/default-off-differential.json').read_text())
    results = differential['results']
    if set(results) != {'0-0-0', '1-0-0', '3-0-0', '1-0-1'} or any(
        not result['identical'] or result['before'] != result['after'] for result in results.values()
    ):
        raise SystemExit('default-off CPU differential differs')
    for name, expected_failed in [('seed41-netlist.log', 1), ('seed41-netlist-after-merge.log', 0)]:
        lines = (HERE / 'cpu-differential' / name).read_text().splitlines()
        summaries = [json.loads(line) for line in lines if line.startswith('{')]
        if len(summaries) != 1 or summaries[0]['runs'] != 17 or summaries[0]['failed'] != expected_failed:
            raise SystemExit(f'unexpected seed-41 result: {name}')
    if args.nasm:
        assemble(args.nasm)
    print('Retained hashes, repeated outputs, controls and arithmetic pass; emulator provenance is not revalidated.')

if __name__ == '__main__':
    main()
