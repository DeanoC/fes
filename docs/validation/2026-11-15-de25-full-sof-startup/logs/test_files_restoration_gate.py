"""Synthetic archive-bound lifecycle fixtures, never actual reset authority.

The wire parser is separately checked against real archived UART in
 test_refresh_wire. Only that parser is mocked in these metadata models.
No fixture or positive binding is written to the actual candidate directory.
"""
import copy,hashlib,json,sys,tarfile,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
import context,restoration_gate as gate,run_trial
from fit_swap import Bundle
from transport import sha
B=Path(__file__).resolve().parent;N=B.parents[1]
def read(p):return json.loads(Path(p).read_text())
def write(p,v):p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,indent=2)+'\n')
class Fixture:
 def __init__(self,root):
  self.root=Path(root);self.restore=self.root/'restoration';self.evidence=self.root/'evidence';self.archive=self.root/'records.tar.gz';r=self.restore;a=r/'attempt';d=a/'recovery-rollback'
  donor=N/'out/usb-close-candidate-trial';plan=read(donor/'plan.json');write(r/'plan.json',plan)
  self.bundle=Bundle(B/'plan.json',sha(B/'plan.json'))
  reset=dict(scope='top-TRWU8CBL-physical-power-cycle-for-native-programming-failure-files-restoration-only',completed=True,original_procedure_sha256=gate.FAILURE,failure_archive_sha256=gate.FAILURE_ARCHIVE,human_quote='injected parser fixture only',confirmation_question_id='injected-fixture')
  write(r/'human-power-cycle.json',reset);write(a/'human-power-cycle.json',reset)
  proc=dict(scope='bounded-native-programming-failure-files-restoration-only',serial='TRWU8CBL',allowed_roles=['recovery-rollback','rollback'],diagnostic_authority=False,permanent_QSPI_write=False,SD_write_authority='exact-original-FIT-rollback-only',original_procedure_sha256=gate.FAILURE,failure_archive_sha256=gate.FAILURE_ARCHIVE,original_FIT_sha256=gate.ORIGINAL,plan_sha256=sha(r/'plan.json'),physical_power_cycle_confirmation_required=True,attempt_limit=1,worker_caps=dict(recovery=270,refresh=420),files={str(r/'human-power-cycle.json'):dict(sha256=sha(r/'human-power-cycle.json'))})
  write(r/'procedure.json',proc)
  actual=donor/'attempt/recovery-before';role=read(actual/'role-result.json');role['role']='recovery-rollback';write(d/'role-result.json',role)
  for name in ['supervisor-result.json','recovery-result.json','completed-program.json']:write(d/name,read(actual/name))
  for name in ['recovery-boot-program.log','recovery-console.log']:(d/name).write_bytes((actual/name).read_bytes())
  (d/'parameter-0.log').write_text('1\n');(d/'parameter-1.log').write_text('15M\n')
  resetgate=read(donor/'attempt/forward/refresh-result.json')['smmu_reset_gate']
  refresh=dict(status='verified',console_closed=True,plan_sha256=sha(r/'plan.json'),smmu_reset_gate=resetgate,bundle=dict(injected_only=True))
  write(a/'rollback/refresh-result.json',refresh);write(a/'rollback/supervisor-result.json',dict(status='passed',worker_stopped=True,timed_out=False,guard_failed=False));(a/'rollback/refresh-console.log').write_bytes(b'injected metadata model wire; not real UART')
  self.wire=dict(status='injected-wire-parser-result-only',target_FIT_sha256=gate.ORIGINAL,readbacks_before=17,readbacks_after=17)
  write(a/'result.json',dict(status='verified-original-FIT-files-only-restoration',fit_restored=True,native_runtime_dispatched=False,baseline_cycle_park_verified=False,diagnostic_retried=False,hardware_acceptance=False,recovery=role,rollback=refresh['bundle']))
  write(self.evidence/'result.json',dict(status='passed-independent-original-files-restoration-after-native-program-failure',procedure_sha256=sha(r/'procedure.json'),original_FIT_restored=True,original_native_runtime_booted=False,diagnostic_retry=False,root_process_scan=[dict(original_process_stopped=True)]*4,root_adapter_UART_FD_scan=dict(open_handles=[]),USB_identity_inode_permissions_restored=True,JTAG_auto_1_15M_restored=True))
  self.seal()
 def seal(self):
  write(self.restore/'attempt/operator-attempt.json',dict(scope='restoration-only',procedure_sha256=sha(self.restore/'procedure.json'),original_procedure_sha256=gate.FAILURE))
  with tarfile.open(self.archive,'w:gz') as t:
   for root in [self.restore,self.evidence]:
    for p in sorted(root.rglob('*')):
     if p.is_file():
      name='attempt/'+str(p.relative_to(self.restore/'attempt')) if p.is_relative_to(self.restore/'attempt') else ('procedure/'+p.name if p.parent==self.restore else 'analysis/'+p.name)
      t.add(p,arcname=name,recursive=False)
  self.binding=dict(scope=gate.SCOPE,restoration_root=str(self.restore),candidate_plan_sha256=self.bundle.sha256,archive=str(self.archive),archive_sha256=sha(self.archive),procedure_sha256=sha(self.restore/'procedure.json'),evidence_root=str(self.evidence))
 def validate(self):
  with patch.object(gate,'ROOT',self.root),patch.object(gate,'RESTORE',self.restore),patch.object(gate,'replay',return_value=self.wire):return gate.validate(self.binding,types.SimpleNamespace(bundle=self.bundle))
class Tests(unittest.TestCase):
 def test_actual_latest_original_restoration_archive(self):
  bundle=Bundle(B/'plan.json',sha(B/'plan.json'));v=gate.check(types.SimpleNamespace(bundle=bundle));self.assertEqual(v['restoration_archive_sha256'],'9ffdfd84adf45ea186c120afd14935a79e14ccad8ed245302977d854c7b76e1e');self.assertFalse(v['Secure_SMMU_reset_qualified'] or v['DMA_authority'] or v['baseline_final_Park_verified'])
 def test_positive_file_only_fixture_grants_no_native_or_DMA_proof(self):
  with tempfile.TemporaryDirectory() as d:
   f=Fixture(d);v=f.validate();self.assertTrue(v['original_FIT_readback_verified']);self.assertFalse(v['baseline_final_Park_verified'] or v['Secure_SMMU_reset_qualified'] or v['DMA_authority']);self.assertGreaterEqual(len(v['files']),19)
   q=Path(d)/'snapshot.json';write(q,v);self.assertTrue(gate.recheck(q,sha(q)))
 def test_failed_wrong_or_native_restorations_reject_even_if_rearchived(self):
  changes=[('procedure.json','scope','old-baseline-restoration'),('procedure.json','allowed_roles',['recovery-rollback','rollback','native-baseline']),('procedure.json','permanent_QSPI_write',True),('procedure.json','original_procedure_sha256','0'*64),('attempt/result.json','status','stopped'),('attempt/result.json','native_runtime_dispatched',True),('attempt/result.json','baseline_cycle_park_verified',True),('attempt/result.json','diagnostic_retried',True),('human-power-cycle.json','completed',False),('human-power-cycle.json','failure_archive_sha256','0'*64)]
  for name,key,value in changes:
   with self.subTest(name=name,key=key),tempfile.TemporaryDirectory() as d:
    f=Fixture(d);p=f.restore/name;j=read(p);j[key]=value;write(p,j);f.seal()
    with self.assertRaises(ValueError):f.validate()
 def test_changed_raw_bytes_or_archive_are_not_accepted(self):
  for target in ['archive','raw']:
   with self.subTest(target=target),tempfile.TemporaryDirectory() as d:
    f=Fixture(d);p=f.archive if target=='archive' else f.restore/'attempt/rollback/refresh-console.log'
    with p.open('ab') as stream:stream.write(b'changed')
    with self.assertRaises(ValueError):f.validate()
 def test_open_UART_or_live_process_and_foreign_candidate_reject(self):
  for name,key,value in [('result.json','root_adapter_UART_FD_scan',dict(open_handles=['synthetic-live-fd'])),('result.json','root_process_scan',[dict(original_process_stopped=False)]*4)]:
   with self.subTest(name=name),tempfile.TemporaryDirectory() as d:
    f=Fixture(d);j=read(f.evidence/name);j[key]=value;write(f.evidence/name,j);f.seal()
    with self.assertRaises(ValueError):f.validate()
  with tempfile.TemporaryDirectory() as d:
   f=Fixture(d);f.binding['candidate_plan_sha256']='0'*64
   with self.assertRaises(ValueError):f.validate()
 def test_old_USB_probe_cannot_supply_file_restoration_authority(self):
  with tempfile.TemporaryDirectory() as d:
   f=Fixture(d);f.binding['scope']='verified-recovered-loader-before-candidate-qualification'
   with self.assertRaises(ValueError):f.validate()
 def test_snapshot_cannot_hide_empty_dependencies_or_add_authority(self):
  for key,value in [('files',{}),('DMA_authority',True),('baseline_final_Park_verified',True),('Secure_SMMU_reset_qualified',True)]:
   with self.subTest(key=key),tempfile.TemporaryDirectory() as d:
    f=Fixture(d);v=f.validate();v[key]=value;p=Path(d)/'snapshot.json';write(p,v)
    with self.assertRaises(ValueError):gate.recheck(p,sha(p))
 def test_missing_real_restoration_blocks_before_attempt_or_device_observation(self):
  p=types.SimpleNamespace(files=lambda:None,approval=lambda:None,observe_stability=lambda _:self.fail('observation forbidden'))
  with tempfile.TemporaryDirectory() as d,patch.object(gate,'BASE',Path(d)),patch.object(run_trial,'BASE',Path(d)),patch.object(run_trial,'Procedure',return_value=p),patch.object(run_trial,'before_hardware',return_value={}),patch.object(run_trial.os,'geteuid',return_value=0),patch.object(sys,'argv',['runner','--procedure-sha256','0'*64,'--execute']):
   with self.assertRaises(ValueError):run_trial.main()
   self.assertFalse((Path(d)/'attempt').exists())
