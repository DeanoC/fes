"""A continuation record cannot replace source/retirement or native activation."""
import sys,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano/sdm_trace'))
from continuation_observation import decode
from test_nano_sdm_queue_observation import wire,row
from test_nano_sdm_input_observation import BOOT,BUILD,CONFIG
from input_observation import PARK
LINE=f'FES_NANO_INPUT_CONTINUE build={BUILD} owner=11 epoch=7 generation=4 ready=1 elapsed_us=12000 retained=1 reuse=0 activated=0\n'.encode()
WIRE=wire(row()).replace(PARK,LINE.rstrip(b'\n'))
class Tests(unittest.TestCase):
 def decode(self,raw):return decode(raw,boot=BOOT,build=BUILD,configuration=CONFIG)
 def test_full_proof_allows_only_retained_admission(self):
  r=self.decode(WIRE);self.assertTrue(r['continuation_verified'],r['errors'])
  self.assertFalse(r['native_activation_verified'] or r['memory_reuse_authorized'] or r['ordinary_runtime_activation'])
 def test_failed_retirement_or_changed_continuation_cannot_pass(self):
  for before,after in [(b'owner=11',b'owner=12'),(b'epoch=7',b'epoch=8'),(b'generation=4',b'generation=5'),(b'ready=1',b'ready=0'),(b'elapsed_us=12000',b'elapsed_us=1000000'),(b'elapsed_us=12000',b'elapsed_us=1'),(b'retained=1',b'retained=0'),(b'reuse=0',b'reuse=1'),(b'activated=0',b'activated=1')]:
   with self.subTest(after=after):
    raw=WIRE.replace(LINE,LINE.replace(before,after));self.assertFalse(self.decode(raw)['passed'])
  bad=wire(row(failed=1,error=-3)).replace(PARK,LINE.rstrip(b'\n'))
  self.assertFalse(self.decode(bad)['passed'])
 def test_partial_duplicate_or_park_only_cannot_claim_continuation(self):
  for raw in [wire(row()),WIRE[:-1],WIRE+LINE,LINE,WIRE+b'late\n',WIRE.replace(b' ready=1',b'')]:
   with self.subTest(raw=raw[-90:]),self.assertRaises(ValueError):self.decode(raw)
if __name__=='__main__':unittest.main()
