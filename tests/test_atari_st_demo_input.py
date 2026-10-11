# SPDX-License-Identifier: GPL-2.0-or-later
"""Bounded schedules and actual keyboard rows shared by both demo fixtures."""
import contextlib
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from scripts import sim_atari_st_demo as demo

ROOT = Path(__file__).resolve().parents[1]
HEADER = ROOT / 'sources/misteross/cores/fes-atari-st/sim/st_keyboard_stimulus.hpp'


class KeyboardScheduleTests(unittest.TestCase):
    def test_exact_conversion_zero_and_default_hold(self):
        event = demo.keyboard_schedule(['0:81', '0.152:81:2.5', '0.15651:44:2'], 1)
        self.assertEqual(event[0]['press_cycle'], 0)
        self.assertEqual(event[0]['release_cycle'], 7833600)
        self.assertEqual(event[1]['press_cycle'], 7938048)
        self.assertEqual(event[1]['release_cycle'], 8068608)
        # Exact decimal beyond a clock boundary must round up, never float-round down.
        exact = demo.keyboard_schedule(['0.000000019148285843:4:2'], 1)[0]
        self.assertEqual(exact['press_cycle'], 2)
        self.assertEqual(demo.schedule_bytes(event[:1]), b'0\t7833600\t81\n')

    def test_invalid_events_are_rejected(self):
        invalid = ['NaN:5', 'Infinity:5', '-0.1:5', '1:5', '0:3', '0:128',
                   '0:5:0', '0:5:-1', '0:5:NaN', '0:5:Infinity', '0:5:1000',
                   '0:5:1e-65', '0:5:1:2', '0:x', '0', '1e-999999:5',
                   '0:5:' + '1'*260]
        for value in invalid:
            with self.subTest(value=value), self.assertRaises(ValueError):
                demo.keyboard_schedule([value], 1)
        for values in [['0.2:5', '0.1:4'], ['0:5', '0.1:4'],
                       ['0:5', '0.151:5'], ['0:5', '0.151:4']]:
            with self.subTest(values=values), self.assertRaises(ValueError):
                demo.keyboard_schedule(values, 1)

    def test_full_digital_navigation_fits_bounded_schedule(self):
        # 115 Down presses, Return, numbered tune and Space need no expanded bound.
        values = [f'{i/100:.2f}:81:5' for i in range(115)]
        values += ['1.2:40:5', '1.3:30:5', '1.4:44:5']
        self.assertEqual(len(demo.keyboard_schedule(values, 2)), 118)
        self.assertEqual(len(demo.keyboard_schedule([f'{i/100:.2f}:81:5' for i in range(256)], 3)), 256)
        with self.assertRaisesRegex(ValueError, '256'):
            demo.keyboard_schedule(['0:5']*257, 30)

    def test_cli_legacy_compatibility_and_mutual_exclusion(self):
        common = ['--rom', 'unused', '--disk', 'unused', '--output', 'unused', '--seconds', '2']
        with patch.object(demo, 'run', return_value={'capture_completed': True}) as run:
            self.assertEqual(demo.main(common + ['--key-b-at', '1']), 0)
            args = run.call_args.args[0]
            self.assertEqual(args.input_schedule, demo.keyboard_schedule(['1:5:150'], 2))
            self.assertEqual(args.key_at, 1)
            self.assertEqual(demo.main(common + ['--key-event', '0:44:10']), 0)
        for extra in [['--key-event', '0:44', '--key-at', '1'],
                      ['--key-event', '0:44', '--key-usage', '81'],
                      ['--key-event', '0:44', '--key-usage', '5']]:
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as error:
                demo.main(common + extra)
            self.assertEqual(error.exception.code, 2)

    def test_compiled_shared_rows_reset_anchor_and_actual_edges(self):
        compiler = shutil.which('g++')
        if compiler is None:
            self.skipTest('g++ unavailable')
        source = r'''
#include "st_keyboard_stimulus.hpp"
#include <array>
#include <cassert>
using namespace st_keyboard_stimulus;
int main(int argc,char** argv) {
    try {
    assert(argc==3);
    auto events=load(argv[1],Hz);
    const uint64_t p=Hz/1000, r=3*p, next=r+ReleaseGap;
    assert(events.size()==3 && events[0].press==0);
    for (bool shared : {false,true}) {
        Stimulus keys;
        keys.configure(events,std::string(argv[2])+(shared?"-shared":"-callback"));
        // Shared-memory HOLD/initialization delay must not consume guest time.
        const uint64_t origin=shared?18000:0;
        std::array<uint32_t,5> rows{};
        if (shared) {
            keys.apply(rows,origin-1,origin,false);
            for (auto row:rows) assert(row==0);
        }
        keys.apply(rows,origin,origin);
        assert(rows[81/32]==(uint32_t(1)<<(81%32)));
        keys.apply(rows,origin+p,origin);
        for (auto row:rows) assert(row==0);
        keys.apply(rows,origin+r,origin);
        assert(rows[81/32]==(uint32_t(1)<<(81%32)));
        keys.apply(rows,origin+r+p,origin);
        for (auto row:rows) assert(row==0);
        keys.apply(rows,origin+next+p,origin);
        assert(rows[44/32]==(uint32_t(1)<<(44%32)));
        keys.apply(rows,origin+next+2*p,origin);
        for (auto row:rows) assert(row==0);
    }
    auto old=legacy(1,5);
    assert(old.size()==1 && old[0].press==Hz && old[0].release==Hz+Hz*150/1000);
    assert(legacy(0,5).empty());
    bool rejected=false;
    try {validate({{0,p,81},{p,p+1,81}},Hz);} catch(const std::invalid_argument&) {rejected=true;}
    assert(rejected);
    } catch (const std::exception&) { return 2; }
}
'''
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            schedule = demo.keyboard_schedule(['0:81:1', '0.003:81:1', '0.006:44:1'], 1)
            (path/'events.tsv').write_bytes(demo.schedule_bytes(schedule))
            (path/'test.cpp').write_text(source)
            subprocess.run([compiler, '-std=c++17', '-Wall', '-Wextra', '-Werror',
                            '-I', str(HEADER.parent), str(path/'test.cpp'), '-o', str(path/'test')],
                           check=True, capture_output=True)
            subprocess.run([str(path/'test'), str(path/'events.tsv'), str(path/'demo')],
                           check=True, capture_output=True)
            for fixture in ['callback', 'shared']:
                edges = [json.loads(line) for line in (path/f'demo-{fixture}-input.jsonl').read_text().splitlines()]
                self.assertEqual([(e['system_cycle'], e['hid_usage'], e['pressed']) for e in edges],
                                 [(0,81,True),(52224,81,False),(156672,81,True),
                                  (208896,81,False),(313344,44,True),(365568,44,False)])
            for invalid in [b'-1 2 5\n', b'0 1 128\n', b'0 999999999 5\n',
                            b'0 1 5 x\n', b'0 1 5\n1 2 5\n']:
                (path/'events.tsv').write_bytes(invalid)
                self.assertNotEqual(subprocess.run([str(path/'test'), str(path/'events.tsv'), str(path/'demo')],
                                                  capture_output=True).returncode, 0)


if __name__ == '__main__':
    unittest.main()
