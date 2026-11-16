import copy,hashlib,json,unittest
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"platform/embedded/nano/sdm_trace"))
from timing_observation import decode,parse_timing,FIELDS
from test_nano_sdm_retire_observation import WIRE as OLD,ROW,LINE as RETIRE
from test_nano_sdm_input_observation import BOOT,CONFIG,BUILD
B=Path(__file__).resolve().parent
def row(**edits):
    r=dict(build=BUILD,owner='11',epoch='7',generation=4,bytes=CONFIG['core_size'],sha384=CONFIG['core_sha384'],
        phase=6,timed=1,mask='7f',start_us=1000,last_us=11000,observed_us=11000,
        guards=1000,samples=1000,owners=1000,sources=5,validated=5,reads=100,writes=5,polls=4,
        first_us=[1000,2000,3000,4000,5000,9000,10000],latest_us=[1900,2900,3900,4900,8900,9900,11000],retained=1,reuse=0,activated=0)
    r.update(edits);return r
def line(r):
    return ('FES_NANO_RETIRE_TIMING '+' '.join(k+'='+(','.join(f'{n:x}' for n in r[k]) if k in ('first_us','latest_us') else str(r[k])) for k in FIELDS)+'\n').encode()
def wire(r):return OLD.replace(RETIRE,RETIRE+line(r))
class Tests(unittest.TestCase):
    def decode(self,raw):return decode(raw,boot=BOOT,build=BUILD,configuration=CONFIG)
    def test_complete_retirement_and_observed_timing(self):
        r=self.decode(wire(row()));self.assertTrue(r['passed'] and r['retirement_verified'] and r['timing_validated']);self.assertFalse(r['ordinary_runtime_activation'] or r['buffer_reuse_authority'] or r['DMA_authority'])
        self.assertEqual(r['recorded_DATA_bytes'],CONFIG['core_size']);self.assertEqual(r['recorded_DATA_requests'],2)
    def test_every_identity_field_is_bound_to_source(self):
        for k,v in dict(build='0'*16,owner='12',epoch='8',generation=5,bytes=64,sha384='0'*96).items():
            with self.subTest(field=k):self.assertFalse(self.decode(wire(row(**{k:v})))['passed'])
    def test_invalid_clock_counter_phase_and_authority_relations(self):
        changes=dict(phase=5,timed=0,mask='3f',start_us=1001,last_us=11001,observed_us=11001,
            guards=1001,samples=0,owners=999,sources=6,validated=6,reads=0,writes=6,polls=3,
            first_us=[1001,2000,3000,4000,5000,9000,10000],latest_us=[1900,2900,3900,4900,8900,9900,11001],retained=0,reuse=1,activated=1)
        # One extra guard can describe a time callback failure, but success cannot.
        for k,v in changes.items():
            with self.subTest(field=k):self.assertFalse(self.decode(wire(row(**{k:v})))['passed'])
    def test_missing_duplicate_reordered_truncated_and_late(self):
        good=wire(row());t=line(row())
        for raw in (OLD,good.replace(t,t+t),good.replace(RETIRE+t,t+RETIRE),good[:-1],good+b'late\n',good.replace(t,t+b'unknown\n')):
            with self.subTest(raw=raw[-80:]),self.assertRaises(ValueError):self.decode(raw)
    def test_each_missing_field_unknown_field_and_unbounded_value(self):
        t=line(row());tokens=t.rstrip().split()
        for i in range(1,len(tokens)):
            raw=b' '.join(tokens[:i]+tokens[i+1:])+b'\n'
            with self.subTest(missing=i),self.assertRaises(ValueError):self.decode(OLD.replace(RETIRE,RETIRE+raw))
        for r in (row(guards=1<<32),row(start_us=1<<64),row(first_us=[0]*8),row(latest_us=[1<<64]*7),row(build='g'*16)):
            with self.assertRaises(ValueError):parse_timing(line(r))
        with self.assertRaises(ValueError):parse_timing(t.replace(b' reads=',b' foreign=0 reads='))
    def test_valid_failed_timing_never_promotes_retirement(self):
        failed=dict(ROW,status=-1,phase=2,retired=0,first_error=-110,writes=1,producer=0,consumer=0,elapsed_us=0,owner='0',epoch='0',generation=0)
        from retire_observation import FIELDS as RF
        result=('FES_NANO_RETIRE_RESULT '+' '.join(k+'='+str(failed[k]) for k in RF)+'\n').encode()
        t=row(phase=2,mask='7',last_us=500000,observed_us=1100000,sources=2,validated=2,writes=1,polls=1,
            first_us=[1000,400000,500000,0,0,0,0],latest_us=[399999,499999,1100000,0,0,0,0])
        r=self.decode(OLD.replace(RETIRE,result+line(t)));self.assertTrue(r['source_flow_verified'] and r['timing_validated']);self.assertFalse(r['passed'] or r['retirement_verified'] or r['buffer_reuse_authority'])
    def test_unseen_phase_cannot_supply_a_timing_sample(self):
        for phase in range(7):
            t=row(mask=f'{(1<<phase)-1:x}');self.assertFalse(self.decode(wire(t))['passed'])
    def test_ZERO_origin_is_distinct_from_unsampled_phase(self):
        t=row(start_us=0,last_us=10000,observed_us=10000,first_us=[0,1000,2000,3000,4000,8000,9000],latest_us=[900,1900,2900,3900,7900,8900,10000])
        r=self.decode(wire(t));self.assertTrue(r['passed'])
if __name__=='__main__':unittest.main()
