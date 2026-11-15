"""Read-only inventory generation and bounded diagnostic observation."""
import copy,sys,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import mmu_status as status


def values():
    return [0x100+i for i in range(len(status.MMIO))]+[1<<28,0x412fd050,0,2,0,0x123,0,0x456,0x789,0x456,0]


def rows(v,phase='before',kind='complete',reason=None):
    return ['FES_NANO_EL3_MMU_STATUS phase='+phase+' index='+format(i,'02d')+' value='+format(value,'016x') for i,value in enumerate(v)]+[
        'FES_NANO_EL3_MMU_STATUS phase='+phase+' END ras='+kind+(' reason='+reason if reason else '')]


def run(lines,phase='before'):
    s=status.state();errors=[]
    for n,line in enumerate(lines):
        error=status.observe(s,line,n,phase)
        if error:errors.append(error)
    return s,errors


class NanoMMUStatusTests(unittest.TestCase):
    def test_canonical_reads_match_reviewed_map_and_never_write_status_or_selectors(self):
        text=status.assembly()
        self.assertEqual(len(status.MMIO),16);self.assertEqual(len(status.PROBES),27)
        self.assertNotIn('msr',text.lower());self.assertNotIn('str ',text.lower());self.assertNotIn(' bl ',text.lower())
        for name,address in status.MMIO:self.assertIn('mmu_constant x0,'+hex(address)+'\nldr w0,[x0]',text)
        for name,reg in status.RAS:self.assertIn('mrs x0,'+reg,text)
        self.assertIn('mov x6,x0',text);self.assertIn('cmp x0,x6',text)
        injected=status.assembly({0:0x80043200,16:0x80043280});self.assertIn('mmu_constant x0,0x80043280\nldr x0,[x0]',injected)
        for bad in ({-1:0},{27:0},{0:3},{True:8},{0:1<<32},[]):
            with self.subTest(bad=bad),self.assertRaises(ValueError):status.assembly(bad)

    def test_complete_inventory_retains_values_but_never_grants_authority(self):
        for phase in ('before','fault'):
            s,e=run(rows(values(),phase),phase);self.assertFalse(e);self.assertTrue(s['complete'])
            self.assertEqual([r['value'] for r in s['records']],values());self.assertFalse(s['hardware_authority'])
        v=values();v[-7]=v[-1]=1;s,e=run(rows(v));self.assertTrue(s['complete']);self.assertFalse(e)

    def test_each_guarded_skip_requires_the_exact_failed_prefix(self):
        base=len(status.MMIO)
        for reason,index,value in [('feature',0,0),('cpu',1,0),('count',3,3),('selector',4,2),('changed',10,1)]:
            v=values();v[base+index]=value;prefix=v[:base+index+1]
            s,e=run(rows(prefix,kind='skip',reason=reason));self.assertFalse(e,reason);self.assertTrue(s['complete'])
            s,e=run(rows(values()[:base+index+1],kind='skip',reason=reason));self.assertTrue(e);self.assertFalse(s['complete'])
            for bad in (prefix[:-1],prefix+[0]):
                s,e=run(rows(bad,kind='skip',reason=reason));self.assertTrue(e);self.assertFalse(s['complete'])
        v=values();v[base]=0
        s,e=run(rows(v,kind='skip',reason='changed'));self.assertTrue(e);self.assertFalse(s['complete'])

    def test_missing_duplicate_malformed_cross_phase_and_pending_reads_reject_completion(self):
        good=rows(values())
        for i in range(len(good)):
            for bad in (good[:i]+good[i+1:],good[:i]+[good[i]]+good[i:]):
                s,e=run(bad);self.assertFalse(s['complete'])
        for line in (good[0].replace('index=00','index=99'),good[0].replace('phase=before','phase=fault'),good[0]+'x'):
            s,e=run([line,*good]);self.assertTrue(e);self.assertFalse(s['complete'])
        pending=good[0].split('value=')[0]+'value='
        s,e=run([pending]);self.assertFalse(e);self.assertIsNotNone(s['pending']);self.assertFalse(s['complete'])
        s,e=run([pending,*good]);self.assertTrue(e);self.assertFalse(s['complete'])
        s,e=run(good+good*100);self.assertTrue(e);self.assertLessEqual(len(s['records']),len(status.PROBES));self.assertFalse(s['complete'])

if __name__=='__main__':unittest.main()
