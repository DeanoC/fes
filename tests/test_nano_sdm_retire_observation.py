"""Synthetic retirement records bound to the complete synthetic source suffix."""
import unittest
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"platform/embedded/nano/sdm_trace"))
from input_observation import PARK
from retire_observation import decode,FIELDS
from test_nano_sdm_input_observation import WIRE as SOURCE,BOOT,CONFIG,BUILD
ROW=dict(build=BUILD,owner='11',epoch='7',generation=4,bytes=CONFIG['core_size'],sha384=CONFIG['core_sha384'],status=0,phase=6,retired=1,first_error=0,writes=5,producer=12,consumer=12,control=0,abort='100000',elapsed_us=10000,bad_offset='0',bad_value='0',retained=1,reuse=0,activated=0)
LINE=('FES_NANO_RETIRE_RESULT '+' '.join(f'{k}={ROW[k]}' for k in FIELDS)+'\n').encode();WIRE=SOURCE.replace(PARK,LINE+PARK)
class RetirementTests(unittest.TestCase):
 def decode(self,data):return decode(data,boot=BOOT,build=BUILD,configuration=CONFIG)
 def test_complete_source_and_retirement_without_reuse_or_activation(self):
  r=self.decode(WIRE);self.assertTrue(r['passed'] and r['source_flow_verified'] and r['retirement_verified']);self.assertEqual(r['result']['producer'],12);self.assertFalse(r['buffer_reuse_authority'] or r['ordinary_runtime_activation'] or r['independent_interconnect_drain'])
 def test_every_retirement_field_mutation_rejects(self):
  for key in FIELDS:
   value='0'*16 if key=='build' else '0'*96 if key=='sha384' else '1000000' if key=='elapsed_us' else str(int(str(ROW[key]),16 if key in ('owner','epoch','abort','bad_offset','bad_value') else 10)+1)
   altered=b' '.join((v.split(b'=',1)[0]+b'='+value.encode()) if v.startswith(key.encode()+b'=') else v for v in LINE.rstrip(b'\n').split())+b'\n'
   with self.subTest(field=key):
    try:self.assertFalse(self.decode(WIRE.replace(LINE,altered))['passed'])
    except ValueError:pass
 def test_missing_duplicate_reordered_unframed_and_late_reject(self):
  cases=(SOURCE,WIRE.replace(LINE,LINE+LINE),WIRE.replace(LINE,b'').replace(b'FES_NANO_INPUT_RESULT ',LINE+b'FES_NANO_INPUT_RESULT '),WIRE.replace(LINE,LINE+b'FES_NANO_FAKE_ACTIVITY x=1\n'),WIRE+b'late\n',WIRE.replace(b'FES_NANO_RETIRE_RESULT',b'FES_NANO_RETIRE_RESULT malformed'))
  for n,data in enumerate(cases):
   with self.subTest(case=n),self.assertRaises(ValueError):self.decode(data)
 def test_retirement_failure_cannot_promote_successful_source(self):
  data=WIRE.replace(b'status=0 phase=6 retired=1 first_error=0',b'status=-1 phase=5 retired=0 first_error=-110');r=self.decode(data);self.assertFalse(r['passed']);self.assertTrue(r['input_observation']['passed']);self.assertFalse(r['retirement_verified'] or r['buffer_reuse_authority'])
if __name__=='__main__':unittest.main()
