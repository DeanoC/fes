# SPDX-License-Identifier: GPL-2.0-or-later
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from scripts.analyse_atari_st_raster_trace import analyse


class RasterTraceTests(unittest.TestCase):
    def fixture(self, root):
        # Partial-byte writes, then repeated writes including disconnected bits.
        events = [
            {'kind': 'palette_write', 'frame': 10, 'address': 0xff8240, 'lanes': 2, 'data': 0x700},
            {'kind': 'palette_write', 'frame': 10, 'address': 0xff8240, 'lanes': 1, 'data': 0x77},
            {'kind': 'palette_write', 'frame': 11, 'address': 0xff8240, 'lanes': 3, 'data': 0xf777},
            {'kind': 'palette_write', 'frame': 11, 'address': 0xff8240, 'lanes': 2, 'data': 0x600},
            {'kind': 'palette_write', 'frame': 12, 'address': 0xff8240, 'lanes': 1, 'data': 0x877},
            {'frame': 11, 'iack': 4, 'line': 1, 'horizontal_phase': 20},
        ]
        logos = [{'frame': f, 'logo_fnv1a64': 'a' if f != 11 else 'b',
                  'max_fetch_wait': 18, 'max_cpu_ram_wait': 54,
                  'row_fnv1a64': ['row0', 'different' if f == 11 else 'row1']} for f in (10, 11, 12)]
        for name, rows in [('demo-raster.jsonl', events), ('demo-logo.jsonl', logos)]:
            (root / name).write_text(''.join(json.dumps(r)+'\n' for r in rows))
        proof = {'capture_completed': True, 'source_revision': 'test', 'source_mode': 'selected revision',
                 'rom_sha256': 'test-rom', 'original_disk_sha256': 'test-disk', 'storage_model': {},
                 'raster_trace': {}, 'metrics': {},
                 'artifacts': {name: hashlib.sha256((root/name).read_bytes()).hexdigest()
                               for name in ('demo-raster.jsonl', 'demo-logo.jsonl')}}
        (root / 'demo-proof.json').write_text(json.dumps(proof))

    def test_lane_aware_repeats_and_noncanonical_neighbours(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            self.fixture(root)
            result = analyse(root)
            self.assertEqual(result['known_unchanged_palette_writes_in_window'], 2)
            self.assertEqual(result['committed_palette_writes_in_window'], 5)
            self.assertEqual(result['noncanonical_frames'], [11])
            self.assertEqual(result['noncanonical_logo_rows'], {'11': [1]})
            self.assertEqual(result['noncanonical_neighbour_events']['11']['iack_lines']['4'], [[1, 20]])
            self.assertEqual(result['iack_line_counts']['4'], {1: 1})
            self.assertEqual(result['max_cpu_ram_wait_system_clocks'], 54)

    def test_changed_trace_is_rejected(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            self.fixture(root)
            with (root/'demo-raster.jsonl').open('a') as stream:
                stream.write('{}\n')
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                analyse(root)

    def test_incomplete_capture_is_rejected(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            (root/'demo-proof.json').write_text('{"capture_completed":false}')
            with self.assertRaisesRegex(ValueError, 'did not complete'):
                analyse(root)
