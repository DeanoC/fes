"""Decode actual Go service receipts; keep boot/core/USB and physical gates separate."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'platform/embedded/nano'))
from menu_service_trace import MenuTrace, PREFIX


class MenuServiceTraceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='nano-menu-service-wire-', dir=ROOT/'out')
        cls.addClassCleanup(cls.temporary.cleanup)
        directory = Path(cls.temporary.name)/'wire'
        result = subprocess.run(['go', '-C', str(ROOT/'platform/embedded/tamago'), 'test', '-count=1',
            '-timeout', '90s', '-run', '^TestMenuProgramObservationsFollowVerifiedLifecycle$', './nativefabric'],
            env=dict(os.environ, GOWORK='off', FES_NANO_MENU_OBSERVATIONS=str(directory)),
            capture_output=True, text=True, timeout=120)
        if result.returncode:
            raise AssertionError(result.stdout+result.stderr)
        cls.contract = json.loads((directory/'contract.json').read_text())
        cls.lines = (directory/'observations.log').read_bytes().splitlines(keepends=True)
        cls.rows = [json.loads(line[len(PREFIX):]) for line in cls.lines]

    def trace(self):
        return MenuTrace(copy.deepcopy(self.contract))

    @staticmethod
    def line(row):
        return PREFIX+json.dumps(row, separators=(',', ':')).encode()+b'\n'

    def test_actual_service_frames_launch_return_restore_and_park(self):
        trace = self.trace()
        for line in self.lines:
            observation = trace.feed(line)
            observation['opened']['Owner'] = 999  # caller cannot change the retained comparison
        result = trace.finish()
        self.assertEqual((result['menu_sessions'], result['observations']), (2, 5))
        self.assertFalse(result['boot_verified'] or result['core_cycle_verified'] or result['physical_display_verified'])
        self.assertEqual([r['event'] for r in self.rows], ['frame', 'frame', 'return', 'frame', 'return'])

    def test_source_identity_counter_frame_and_return_changes_reject(self):
        mutations = [('scope', 'other'), ('event', 'ready'), ('menu', True), ('menu', 0),
            ('source_generation', True), ('source_generation', (1<<64)-2), ('frames', True), ('frames', 0),
            ('frames', 1<<32), ('source_sha256', '0'*64), ('memory_sha256', '1'*64),
            ('control_sha256', '1'*64), ('access_sha256', '1'*64), ('unexpected', 1)]
        for key, value in mutations:
            row = copy.deepcopy(self.rows[0]);row[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                self.trace().feed(self.line(row))
        for member, key, value in [('identity', 'BuildID', '0'*32), ('identity', 'Major', True),
            ('opened', 'Owner', 18), ('opened', 'Generation', 99), ('opened', 'ResetMask', 13),
            ('opened', 'NeverEnabled', 0), ('opened', 'MastersContained', 0),
            ('frame', 'Sequence', 2), ('frame', 'Submitted', 0), ('frame', 'Slot', 1),
            ('frame', 'Underflows', 1), ('frame', 'Underflows', False)]:
            row = copy.deepcopy(self.rows[0]);row[member][key] = value
            with self.subTest(member=member, key=key), self.assertRaises(ValueError):
                self.trace().feed(self.line(row))
        for key, value in [('Generation', 1), ('ResetMask', 5), ('MastersContained', False)]:
            row = copy.deepcopy(self.rows[2]);row['after'][key] = value
            trace = self.trace()
            for line in self.lines[:2]:trace.feed(line)
            with self.subTest(return_field=key), self.assertRaises(ValueError):trace.feed(self.line(row))

    def test_truncation_replay_skipped_frame_open_return_and_nonfinite_json(self):
        sequences = [[self.lines[2]], [self.lines[0], self.lines[0]], [self.lines[1]],
            [*self.lines[:3], self.lines[2]], [self.lines[0], self.lines[3]], [*self.lines[:3], self.lines[4]]]
        for lines in sequences:
            trace = self.trace()
            with self.subTest(length=len(lines)), self.assertRaises(ValueError):
                for line in lines:trace.feed(line)
        for line in (self.lines[0][:-1], self.lines[0]*2, b'x'*4097, PREFIX+b'{"frames":NaN}\n',
            PREFIX+b'{"scope":"a","scope":"b"}\n', PREFIX+b'['*1100+b']'*1100+b'\n'):
            trace = self.trace()
            with self.assertRaises(ValueError):trace.feed(line)
            with self.assertRaises(ValueError):trace.feed(self.lines[0])
        trace = self.trace();trace.feed(self.lines[0])
        with self.assertRaises(ValueError):trace.finish()

    def test_complete_independent_contract_and_fresh_storage_gap_required(self):
        for key in self.contract:
            contract = copy.deepcopy(self.contract);del contract[key]
            with self.subTest(key=key), self.assertRaises(ValueError):MenuTrace(contract)
        trace = self.trace()
        for line in self.lines[:3]:trace.feed(line)
        row = copy.deepcopy(self.rows[3]);row['source_generation'] = self.rows[2]['after']['Generation']+2
        for key in ('opened', 'after'):row[key]['Generation'] = row['source_generation']+2
        with self.assertRaises(ValueError):trace.feed(self.line(row))


if __name__ == '__main__':
    unittest.main()
