# SPDX-License-Identifier: GPL-2.0-or-later
"""Bounded physical-write observations retain byte masks and repeated values."""
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
HEADER = ROOT/'sources/misteross/cores/fes-atari-st/sim/st_ram_write_trace.hpp'

class RamWriteTraceTests(unittest.TestCase):
    def test_ranges_and_shared_fixture_admission(self):
        self.assertEqual(demo.ram_write_ranges(['0x380:0x392','0:2']),[(0,2),(0x380,0x392)])
        for values in [['0:0'],['-2:2'],['1:2'],['0:3'],['0:524290'],['0:4','2:6'],['x:2'],['0:2:4'],['0:2']*17]:
            with self.subTest(values=values),self.assertRaises(ValueError):demo.ram_write_ranges(values)
        common=['--rom','unused','--disk','unused','--output','unused','--seconds','6']
        with patch.object(demo,'run',return_value={'capture_completed':True}) as run:
            self.assertEqual(demo.main(common+['--shared-memory','--ram-write-range','0x380:0x392']),0)
            self.assertEqual(run.call_args.args[0].ram_write_ranges,[(0x380,0x392)])
        with contextlib.redirect_stderr(io.StringIO()),self.assertRaises(SystemExit):
            demo.main(common+['--ram-write-range','0:2'])

    def test_compiled_masks_repeated_writes_and_disabled_trace(self):
        compiler=shutil.which('g++')
        if compiler is None:self.skipTest('g++ unavailable')
        source=r'''
#include "st_ram_write_trace.hpp"
#include <cassert>
int main(int argc,char **argv) {
 try {
    assert(argc==3);
    StRamWriteTrace disabled;disabled.observe(1,10,0,123,3,0);disabled.finish();assert(!disabled.enabled());
    StRamWriteTrace trace;trace.open(argv[1],argv[2]);
    trace.observe(10,100,0,0xabff,2,0x45472);
    trace.observe(11,101,0,0xffcd,1,0x45474);
    trace.observe(12,102,0,0xffcd,1,0x45474);
    trace.observe(13,103,2,0xeeee,3,0);
    trace.observe(14,104,4,0x1234,3,0x454a8);
    trace.finish();assert(trace.events==4);
 } catch(const std::exception &) {return 2;}
}
'''
        with tempfile.TemporaryDirectory() as directory:
            d=Path(directory);(d/'test.cpp').write_text(source)
            subprocess.run([compiler,'-std=c++17','-Wall','-Wextra','-Werror','-I',str(HEADER.parent),str(d/'test.cpp'),'-o',str(d/'test')],check=True,capture_output=True)
            (d/'ranges.tsv').write_text('0\t2\n4\t6\n')
            subprocess.run([str(d/'test'),str(d/'ranges.tsv'),str(d/'writes.jsonl')],check=True,capture_output=True)
            events=[json.loads(line) for line in (d/'writes.jsonl').read_text().splitlines()]
            self.assertEqual([e['system_cycle'] for e in events],[10,11,12,14])
            self.assertEqual([e['physical_cycle'] for e in events],[100,101,102,104])
            words={0:0x1111,4:0xffff}
            for e in events:
                a=e['byte_address'];value=words[a]
                if e['byte_enable']&2:value=(value&0xff)|(e['data']&0xff00)
                if e['byte_enable']&1:value=(value&0xff00)|(e['data']&0xff)
                words[a]=value
            self.assertEqual(words,{0:0xabcd,4:0x1234})
            self.assertEqual(events[1]['data'],events[2]['data'])
            for invalid in ['', '0 3\n','-2 2\n','0 2 x\n','0 4\n2 6\n','0 524290\n']:
                (d/'ranges.tsv').write_text(invalid)
                self.assertNotEqual(subprocess.run([str(d/'test'),str(d/'ranges.tsv'),str(d/'writes.jsonl')],capture_output=True).returncode,0)

if __name__=='__main__':unittest.main()
