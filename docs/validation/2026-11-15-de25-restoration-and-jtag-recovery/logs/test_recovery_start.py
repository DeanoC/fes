import copy,unittest
import recovery_start as r
class Tests(unittest.TestCase):
 def test_actual_recovery_is_archived_but_not_native_admission(self):
  v=r.check();self.assertTrue(v['fresh_recovery_observed']);self.assertFalse(v['Secure_SMMU_reset_qualified'] or v['DMA_authority'] or v['native_dispatch']);self.assertTrue(v['fresh_epoch_required'])
 def test_incomplete_or_excessive_authority_rejects(self):
  v=r.read(r.D/'result.json');p=r.read(r.D/'retention-proof.json')
  for key,value in [('fresh_recovery_verified',False),('read_only_registers_verified',False),('NS_reset_reads',18),('SD_or_QSPI_writes',1),('ordinary_runtime_admission',True),('Secure_SMMU_reset_qualified',True),('source_reuse_authorized',True)]:
   bad=copy.deepcopy(v);bad['observation'][key]=value
   with self.subTest(key=key),self.assertRaises(ValueError):r.classify(bad,p)
  with self.assertRaises(ValueError):r.classify(v,dict(p,all_hardware_handles_closed=False))
