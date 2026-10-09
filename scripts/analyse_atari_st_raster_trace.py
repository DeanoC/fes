#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Summarize a completed, source-bound BIG model raster/logo diagnostic.

Equality of the static logo is an observation, not a demo-compatibility oracle.
The callback storage model does not reproduce physical shared-memory arbitration.
"""
import argparse
from collections import Counter, defaultdict
import hashlib
import json
from pathlib import Path


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def rows(path):
    with path.open() as stream:
        for line in stream:
            yield json.loads(line)


def analyse(directory):
    proof = json.loads((directory / 'demo-proof.json').read_text())
    if not proof.get('capture_completed'):
        raise ValueError('diagnostic did not complete')
    for name in ('demo-logo.jsonl', 'demo-raster.jsonl'):
        if proof['artifacts'].get(name) != sha256(directory / name):
            raise ValueError('artifact digest mismatch: ' + name)
    logos = list(rows(directory / 'demo-logo.jsonl'))
    if not logos:
        raise ValueError('trace window contains no completed native captures')
    counts = Counter(row['logo_fnv1a64'] for row in logos)
    canonical = counts.most_common(1)[0][0]
    # A complete trace window may contain startup or changing demo screens.
    # Calling these noncanonical avoids assigning a visual fault automatically.
    differing = {row['frame'] for row in logos if row['logo_fnv1a64'] != canonical}
    canonical_rows = next(row.get('row_fnv1a64') for row in logos if row['logo_fnv1a64'] == canonical)
    differing_rows = {str(row['frame']): [y for y, (a, b) in enumerate(zip(canonical_rows, row['row_fnv1a64'])) if a != b]
                      for row in logos if row['frame'] in differing and canonical_rows and row.get('row_fnv1a64')}
    nearby = {frame + offset for frame in differing for offset in (-1, 0, 1)}
    per_frame = defaultdict(lambda: {'palette_writes': 0, 'iack_lines': defaultdict(list), 'sync_changes': []})
    iack_lines = defaultdict(Counter)
    mfp_vectors = Counter()
    palette_words = {}
    repeats = 0
    writes = 0
    last_sync = None
    pulse = None
    crossing_pulses = []
    for event in rows(directory / 'demo-raster.jsonl'):
        frame = per_frame[event['frame']]
        if 'sync_mode' in event:
            mode = event['sync_mode']
            if last_sync is not None and mode != last_sync:
                position = {key: event[key] for key in ('cycle', 'frame', 'line', 'horizontal_phase', 'sync_mode')}
                if event['frame'] in nearby:
                    frame['sync_changes'].append(position)
                if last_sync & 2 and not mode & 2:
                    pulse = position
                elif mode & 2 and pulse is not None:
                    if event['frame'] != pulse['frame']:
                        crossing_pulses.append({'opposite_sync': pulse, 'restored_pal': position})
                    pulse = None
            last_sync = mode
        if event.get('kind') == 'palette_write':
            writes += 1
            frame['palette_writes'] += 1
            address = event['address']
            lanes = event['lanes']
            # Only connected RGB bits participate. An unknown unwritten lane
            # cannot establish an unchanged write at the trace-window boundary.
            mask = (0x0700 if lanes & 2 else 0) | (0x0077 if lanes & 1 else 0)
            old, known = palette_words.get(address, (0, 0))
            data = event['data'] & mask
            if mask and known & mask == mask and old & mask == data:
                repeats += 1
            palette_words[address] = ((old & ~mask) | data, known | mask)
        elif event.get('iack'):
            level = str(event['iack'])
            iack_lines[level][event['line']] += 1
            if event['iack'] == 6 and 'mfp_vector' in event:
                mfp_vectors[event['mfp_vector']] += 1
            if event['frame'] in nearby:
                frame['iack_lines'][level].append([event['line'], event['horizontal_phase']])
    return {
        'analysis_script_sha256': sha256(Path(__file__)),
        'cross_frame_pal_pulses': crossing_pulses,
        'source_revision': proof['source_revision'],
        'source_mode': proof['source_mode'],
        'rom_sha256': proof['rom_sha256'],
        'disk_sha256': proof['original_disk_sha256'],
        'hardware_execution': False,
        'storage_model': proof['storage_model'],
        'raster_trace': proof['raster_trace'],
        'logo_crop_xywh': [65, 0, 180, 64],
        'logo_hash_algorithm': 'FNV-1a-64 expanded RGB; equality diagnostic only',
        'completed_captures_in_window': len(logos),
        'logo_hash_counts': dict(counts),
        'canonical_hash': canonical,
        'noncanonical_frames': sorted(differing),
        'noncanonical_logo_rows': differing_rows,
        'committed_palette_writes_in_window': writes,
        'known_unchanged_palette_writes_in_window': repeats,
        'mfp_vector_counts': dict(mfp_vectors),
        'iack_line_counts': {level: dict(count) for level, count in iack_lines.items()},
        'noncanonical_neighbour_events': {str(frame): dict(per_frame[frame]) for frame in sorted(nearby) if frame in per_frame},
        'max_capture_wait_system_clocks': max(row['max_fetch_wait'] for row in logos),
        'max_cpu_ram_wait_system_clocks': max(row['max_cpu_ram_wait'] for row in logos),
        'metrics': proof['metrics'],
        'proof_sha256': sha256(directory / 'demo-proof.json'),
        'demo_compatibility_asserted': False,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        result = json.dumps(analyse(args.directory), indent=2, sort_keys=True) + '\n'
        if args.output:
            args.output.write_text(result)
        else:
            print(result, end='')
    except (OSError, ValueError, KeyError) as error:
        parser.exit(1, str(error) + '\n')


if __name__ == '__main__':
    main()
