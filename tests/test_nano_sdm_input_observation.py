"""Explicitly synthetic full-input suffix; no physical acceptance is implied."""
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"platform/embedded/nano/sdm_trace"))
import copy,json,unittest
from input_observation import decode,FIELDS,PHASES
CONFIG=dict(core_size=1708032,chunk_bytes=1048512,core_sha384='1'*96);BUILD='2'*16
BOOT=dict(loader_ready=True,errors=[],capability_observation=dict(status=0,reads=26,owner=17,epoch=7))
def trace():
 total,chunk=CONFIG['core_size'],CONFIG['chunk_bytes'];tail=total-chunk
 def xfer(phase,done,pending):return f'FES_NANO_CORE_TRANSFER phase={phase} owner=11 generation=4 completed={done} pending={pending} total={total} base=94800000 counter=1 hash_ticks=1\n'
 def remap(kind,phase,operand,ar):return f'FES_NANO_REMAP kind={kind} phase={phase} operand={operand:x} por1=0 bypass=0 ctrl=3 ids=a000a ar={ar:x} aw={ar:x} access=unqualified\n'
 text=xfer('arm',0,0)+xfer('start',0,0)
 for n in PHASES:
  text+=f'FES_NANO_START phase={n} status={2 if n==7 else 0}\n'
  if n==12:text+=remap(1,0,0,0)+remap(1,1,0,0x80000000)
 text+='FES_NANO_CORE_ENGINE credits=4 block_bytes=1048560 next_job=0 owner=11 generation=4\n'
 text+=xfer('write',0,chunk)+remap(2,0,0x94800000,0x80000000)+remap(2,1,0x14800000,0x80000000)
 text+=f'FES_NANO_SDM_SUBMIT phase=before address=14800000 bytes={chunk:x} por1=0 bypass=0 ctrl=3 ids=a000a ar=80000000 aw=80000000 access=unqualified\nFES_NANO_SDM_RESULT status=0\n'
 text+=xfer('poll',0,chunk)+xfer('write',chunk,tail)+xfer('poll',chunk,tail)+xfer('status',total,0)
 row=dict(build=BUILD,status=0,detail=0,proof=1,owner='11',epoch='7',generation=4,bytes=total,accepted=total,posted=total,acknowledged=total,completed=total,posts=2,acks=2,buffers=2,pending=0,outstanding=0,configured=1,failed=0,first_error=0,service_error=0,source_failed=0,scope_reason=0,bad_offset='0',bad_value='0',arm_writes=12,elapsed_us=1000000,sha384=CONFIG['core_sha384'],retained=1,activated=0)
 text+='FES_NANO_INPUT_RESULT '+' '.join(f'{k}={row[k]}' for k in FIELDS)+'\nFES_NANO_INPUT_PARK retained=1 resume=0 activated=0\n';return text.encode()
WIRE=trace()
class InputObservationTests(unittest.TestCase):
 def decode(self,raw):return decode(raw,boot=BOOT,build=BUILD,configuration=CONFIG)
 def test_full_bytes_digest_and_no_activation(self):
  r=self.decode(WIRE);self.assertTrue(r['passed']);self.assertEqual(r['recorded_DATA_bytes'],CONFIG['core_size']);self.assertEqual(r['recorded_DATA_requests'],2);self.assertFalse(r['ordinary_runtime_activation']);self.assertFalse(r['buffer_reuse_authority'])
 def test_every_numeric_or_digest_result_mutation_rejects(self):
  line=next(v for v in WIRE.splitlines() if v.startswith(b'FES_NANO_INPUT_RESULT '))
  for field in FIELDS:
   if field=='build':value='f'*16
   elif field=='sha384':value='0'*96
   elif field=='elapsed_us':value='20000000'
   else:
    old=next(v.split('=',1)[1] for v in line.decode().split()[1:] if v.startswith(field+'='));value=str(int(old,16 if field in ('owner','epoch','bad_offset','bad_value') else 10)+1)
   new=b' '.join((v.split(b'=',1)[0]+b'='+value.encode()) if v.startswith(field.encode()+b'=') else v for v in line.split(b' '))
   with self.subTest(field=field):
    try:r=self.decode(WIRE.replace(line,new));self.assertFalse(r['passed'])
    except ValueError:pass
 def test_missing_duplicate_late_or_activation_rejects(self):
  line=next(v+b'\n' for v in WIRE.splitlines() if v.startswith(b'FES_NANO_INPUT_RESULT '))
  cases=(WIRE.replace(line,b''),WIRE.replace(line,line+line),WIRE+b'late\n',WIRE.replace(b'FES_NANO_INPUT_PARK',b'FES_NANO_FABRIC_ACTIVATION owner=11\nFES_NANO_INPUT_PARK'),WIRE.replace(b'phase=6 status=0',b'phase=6 status=-1'),WIRE.replace(b'address=14800000 bytes=',b'address=24800000 bytes='),WIRE.replace(b'next_job=0',b'next_job=1'),WIRE.replace(b'phase=status',b'phase=complete'))
  for i,raw in enumerate(cases):
   with self.subTest(case=i),self.assertRaises(ValueError):self.decode(raw)
 def test_modeled_late_event_is_observed_as_failure(self):
  raw=WIRE.replace(b'status=0 detail=0 proof=1',b'status=4 detail=0 proof=0').replace(b'source_failed=0',b'source_failed=1').replace(b'scope_reason=0',b'scope_reason=22').replace(b'bad_offset=0 bad_value=0',b'bad_offset=100a8 bad_value=1')
  r=self.decode(raw);self.assertFalse(r['passed']);self.assertEqual(r['result']['bad_offset'],0x100a8);self.assertFalse(r['source_flow_verified'])
if __name__=='__main__':unittest.main()
