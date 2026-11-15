import copy,json,unittest
from pathlib import Path
from unittest.mock import patch
import image_selection as m
from fit_swap import Bundle
from transport import sha
B=Path(__file__).resolve().parent
class Tests(unittest.TestCase):
 def inputs(self):
  p=json.loads((B/'plan.json').read_text());s=json.loads((B/'native-image-selection.json').read_text());return copy.deepcopy(s['image']),s,p
 def test_actual_complete_SOF_and_SPL_are_independently_bound(self):
  r=m.check();self.assertEqual(r['image']['kind'],'matched-native-sof');self.assertEqual(r['embedded_SPL']['spl_bytes'],164001);self.assertFalse(r['phase_one_fallback'] or r['hardware_acceptance'] or r['programming_authority']);self.assertGreaterEqual(len(r['files']),26)
 def test_phase_one_or_foreign_image_rejected(self):
  image,spec,plan=self.inputs();c=json.loads((Path(spec['composition'])/'result.json').read_text())
  for change in [dict(kind='hps-phase1-rbf'),dict(path=str(Path(spec['composition'])/'native-hps-first.hps.rbf'),sha256=c['phase1_sha256']),dict(sha256='0'*64),dict(bytes=image['bytes']+1)]:
   with self.subTest(change=change),self.assertRaises(ValueError):m.validate(dict(image,**change),spec,plan)
 def test_flash_or_alternate_operation_never_selected(self):
  image,spec,plan=self.inputs()
  for change in [dict(flash_operation=True),dict(volatile=False),dict(operation='pi;'+image['path']+'@1'),dict(operation=image['operation'].replace('@1','@2'))]:
   bad=dict(image,**change);s=copy.deepcopy(spec);s['image']=bad
   with self.subTest(change=change),self.assertRaises(ValueError):m.validate(bad,s,plan)
 def test_mixed_source_FIT_or_SPL_rejected(self):
  image,spec,plan=self.inputs()
  for key in ['source_head','source_root','composition_result_sha256','fit_sha256','spl_sha256','inspector_sha256']:
   s=copy.deepcopy(spec);s[key]='0'*64
   with self.subTest(key=key),self.assertRaises(ValueError):m.validate(image,s,plan)
 def test_selected_row_is_not_an_automatic_fallback(self):
  image,spec,plan=self.inputs();plan['roles']=[v for v in plan['roles'] if v['role']!='native-diagnostic'];bundle=type('B',(),dict(plan=plan,verify=lambda _:True))()
  with self.assertRaises(ValueError):m.check(bundle)
