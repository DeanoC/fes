"""Contained MMU logging observation and write-site admission failures."""
import sys,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import mmu_logging as log


def row(phase):return 'FES_NANO_EL3_MMU_LOG phase='+phase


def observe(phases,fault_after=None):
    s=log.state();errors=[]
    for i,phase in enumerate(phases):
        e=log.observe(s,row(phase),i,fault=fault_after is not None and i>=fault_after)
        if e:errors.append(e)
    return s,errors


class MMULoggingTests(unittest.TestCase):
    def test_normal_lifetime_requires_arm_and_verified_restore(self):
        phases=('arm-before','armed','restore-before','restored')
        for count in range(5):
            s,e=observe(phases[:count]);self.assertFalse(e)
            self.assertEqual(s['complete'],count==4)
            self.assertEqual(s['write_attempted'],count>=1)
            self.assertEqual(s['restore_attempted'],count>=3)
            self.assertFalse(s['hardware_authority'])

    def test_fault_cleanup_is_diagnostic_and_never_complete_boot_evidence(self):
        for prefix in (('arm-before',),('arm-before','armed')):
            s,e=observe(prefix+('restore-before','restored'),len(prefix))
            self.assertFalse(e);self.assertTrue(s['restored']);self.assertFalse(s['complete'])
        for prefix,phase in [((),'inactive'),(('arm-before',),'inactive'),(('arm-before','armed'),'interrupted'),
                             (('arm-before','armed','restore-before'),'interrupted'),
                             (('arm-before','armed','restore-before'),'retired')]:
            s,e=observe(prefix+(phase,),len(prefix));self.assertFalse(e,(prefix,phase))
            self.assertFalse(s['restored'] or s['complete'])

    def test_missing_replayed_malformed_or_failed_records_stay_closed(self):
        phases=('arm-before','armed','restore-before','restored')
        for i in range(4):
            for seq in (phases[:i]+phases[i+1:],phases[:i]+(phases[i],)+phases[i:]):
                s,e=observe(seq);self.assertFalse(s['complete'])
        for phase in ('admit','enable','restore','status'):
            s=log.state()
            self.assertIsNotNone(log.observe(s,'FES_NANO_EL3_MMU_LOG FAIL phase='+phase,1,fault=False))
            for i in range(100):log.observe(s,row('restored'),i+2,fault=True)
            self.assertFalse(s['complete'] or s['restored']);self.assertLessEqual(len(s['records']),6)
        for phase in ('inactive','interrupted','retired','unknown'):
            s,e=observe((phase,));self.assertTrue(e);self.assertFalse(s['complete'])

    def test_failed_enable_can_retain_shared_cleanup_without_reopening_admission(self):
        s=log.state();self.assertIsNone(log.observe(s,row('arm-before'),1,fault=False))
        self.assertIsNotNone(log.observe(s,'FES_NANO_EL3_MMU_LOG FAIL phase=enable',2,fault=False))
        for i,phase in enumerate(('restore-before','restored'),3):
            self.assertIsNone(log.observe(s,row(phase),i,fault=False))
        self.assertTrue(s['restored'] and s['failed']);self.assertFalse(s['valid'] or s['complete'])


if __name__=='__main__':unittest.main()
