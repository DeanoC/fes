# SPDX-License-Identifier: GPL-2.0-or-later
"""Fixture and public-input oracle checks; guest execution has its own RTL run."""
import importlib.util
from pathlib import Path
import struct
import sys
import unittest
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
import atari_st_io_diagnostic as diagnostic
import atari_st_disk_diagnostic as disk

class IoDiagnosticTest(unittest.TestCase):
    def test_auto_disk_contains_exact_original_guest_and_no_pass_marker(self):
        image=diagnostic.build_disk()
        self.assertEqual(len(image),737280)
        self.assertEqual(image,diagnostic.build_disk())
        parsed=disk.Fat12Disk(image)
        auto=parsed.read(b'AUTO       ')
        self.assertIsNotNone(auto)
        prg=parsed.read(b'IOTEST  PRG',auto)
        self.assertEqual(prg,diagnostic.build_prg())
        self.assertIsNone(parsed.read(b'PASS    TXT'))
        self.assertNotIn(b'DISKTEST',parsed.read(b'README  TXT'))
        magic,text,data,bss,symbols,reserved,flags,absolute=struct.unpack('>H6IH',prg[:28])
        self.assertEqual((magic,text,data,bss,symbols,reserved,flags,absolute),(0x601A,len(prg)-28,0,0,0,0,0,1))
        self.assertNotEqual(sum(struct.unpack('>256H',image[:512]))&0xffff,0x1234)

    def test_public_event_sequence_matches_independent_ikbd_byte_oracle(self):
        scans={4:0x1E,5:0x30,41:1,42:0x0E,0xE0:0x1D,0xE4:0x1D,
               0xE1:0x2A,0xE5:0x36,0xE2:0x38,0xE6:0x38}
        held=set(); pads=[set(),set()]
        for name,events,expected in diagnostic.groups():
            observed=[]
            for event in events:
                self.assertEqual(event['Value'],0)
                down=event['Action']==1
                if event['Device']==0:
                    usage=event['Code']-0x1000
                    before={scans[u] for u in held}
                    (held.add if down else held.discard)(usage)
                    after={scans[u] for u in held}
                    observed.extend(sorted(after-before))
                    observed.extend(scan|0x80 for scan in sorted(before-after))
                else:
                    player=event['Player']; bit=event['Code']-100
                    self.assertIn(player,(0,1)); self.assertIn(bit,range(6))
                    def state():
                        return sum(1<<b for b in pads[player] if b<4) | (0x80 if pads[player]&{4,5} else 0)
                    before=state(); (pads[player].add if down else pads[player].discard)(bit)
                    if state()!=before: observed.extend((0xFF if player==0 else 0xFE,state()))
            self.assertEqual(observed,expected,name)
            self.assertFalse(held,name); self.assertEqual(pads,[set(),set()],name)

    def test_audio_schedule_matches_documented_yamaha_equation(self):
        phases=diagnostic.schedule()['audio_phases']
        self.assertEqual([p.get('channel') for p in phases[:3]],['A','B','C'])
        for phase in phases[:3]: self.assertAlmostEqual(phase['tone_hz'],2_000_000/(16*phase['period']))
        self.assertEqual(diagnostic.PHASE_TICKS,600)
        self.assertEqual(phases[3]['noise_period'],16)
        self.assertEqual(phases[4]['shape'],14)
        self.assertTrue(phases[5]['all_volumes_zero'])

if __name__=='__main__': unittest.main()
