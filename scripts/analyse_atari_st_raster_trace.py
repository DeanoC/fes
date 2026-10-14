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


def rendered_capture(directory, proof):
    """Validate sealed replay images against their actual HDMI canvas."""
    if 'rendered_capture' not in proof:
        return None  # Older source-bound captures have no replay-frame oracle.
    def sealed(name):
        if not name or Path(name).name != name:
            raise ValueError('invalid rendered artifact name')
        path = directory / name
        if proof['artifacts'].get(name) != sha256(path):
            raise ValueError('artifact digest mismatch: ' + name)
        return path.read_bytes()
    def ppm(name, width, height):
        data = sealed(name)
        header = f'P6\n{width} {height}\n255\n'.encode()
        if not data.startswith(header) or len(data) != len(header)+width*height*3:
            raise ValueError('rendered PPM geometry mismatch: ' + name)
        return data[len(header):]
    trace_name = proof['rendered_capture']['trace']
    entries = [json.loads(line) for line in sealed(trace_name).splitlines()]
    previous = 0
    saved = []
    unsaved_claims = []
    nonrepeated = []
    for entry in entries:
        if entry['hdmi_frame'] <= previous or entry['end_cycle'] <= entry['start_cycle']:
            raise ValueError('invalid rendered frame chronology')
        previous = entry['hdmi_frame']
        top, height = (84, 276) if entry['pal'] else (105, 255)
        if entry['canvas_xywh'] != [224, top, 832, height*2]:
            raise ValueError('rendered canvas geometry mismatch')
        full_name, native_name = entry['hdmi_file'], entry['canvas_file']
        if not full_name:
            # The trace still records a replication flag, but no HDMI PPM was
            # saved, so that flag is a claim rather than a pixel result.
            if native_name:
                raise ValueError('reduced canvas lacks HDMI source')
            claimed, valid = entry['repeat_2x2'], entry['valid']
            if not isinstance(claimed, bool) or not isinstance(valid, bool):
                raise ValueError('unsaved rendered frame metadata is invalid')
            unsaved_claims.append({
                'hdmi_frame': entry['hdmi_frame'], 'native_frame': entry['native_frame'],
                'valid': valid, 'repeat_2x2_claimed': claimed, 'pixel_verified': False})
            continue
        if not entry['valid']:
            raise ValueError('saved rendered frame is invalid')
        full = ppm(full_name, 1280, 720)
        reduced = bytearray()
        repeated = True
        for y in range(height):
            for x in range(416):
                offset = ((top+2*y)*1280+224+2*x)*3
                rgb = full[offset:offset+3]
                reduced += rgb
                for dy, dx in ((0, 1), (1, 0), (1, 1)):
                    other = offset+dy*1280*3+dx*3
                    repeated &= full[other:other+3] == rgb
        if entry['repeat_2x2'] != repeated:
            raise ValueError('rendered replication claim differs from HDMI pixels')
        if native_name:
            if not repeated or ppm(native_name, 416, height) != reduced:
                raise ValueError('reduced canvas differs from HDMI source')
        elif repeated:
            raise ValueError('replicated saved HDMI frame lacks reduced canvas')
        if not repeated:
            nonrepeated.append(entry['hdmi_frame'])
        saved.append({'hdmi_frame': entry['hdmi_frame'], 'native_frame': entry['native_frame'],
                      'hdmi_file': full_name, 'canvas_file': native_name, 'repeat_2x2': repeated})
    return {'complete_hdmi_frames_in_window': len(entries), 'saved_frames': saved,
            'nonrepeated_frames': nonrepeated, 'unsaved_replication_claims': unsaved_claims}


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
            # New captures record the complete color after byte replication.
            # Older source-bound captures retain their former lane-merge model.
            mask = 0x0777 if 'color' in event else ((0x0700 if lanes & 2 else 0) | (0x0077 if lanes & 1 else 0))
            old, known = palette_words.get(address, (0, 0))
            data = event.get('color', event['data']) & mask
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
        'rendered_capture': rendered_capture(directory, proof),
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
