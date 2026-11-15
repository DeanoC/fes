"""File-only restoration prerequisite; no device, native boot or DMA authority."""
from pathlib import Path
import hashlib,json,re,tarfile
from transport import require,sha
from fit_swap import Bundle
from restoration_wire import role
from refresh_wire import replay
ROOT=Path('/home/deano/fes/out/dev/de25-source-release')
BASE=Path(__file__).resolve().parent
RESTORE=ROOT/'out/four-second-program-failure-files-restoration'
FAILURE='dfef7b983d4d9df1b57206d973b912a3eabc3af6e9bf5eafc9e0f9476f00d4d9'
FAILURE_ARCHIVE='2c4f9ad5309b61b67ef00be64b26c3dabfc7c3b9a6ccedcc47976593a24e2b2e'
ORIGINAL='d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1'
SCOPE='verified-original-files-restoration-before-full-SOF-candidate'
def plain(p,limit=8<<20):
 p=Path(p)
 require(p.is_file() and not any(q.is_symlink() for q in (p,*p.parents)) and p.stat().st_size<=limit,'bounded plain restoration evidence required')
 return p
def read(p):return json.loads(plain(p).read_text())
def validate(binding,procedure):
 require(binding['scope']==SCOPE and binding['restoration_root']==str(RESTORE) and binding['candidate_plan_sha256']==procedure.bundle.sha256,'wrong restoration binding or candidate plan')
 archive=plain(binding['archive'],32<<20);require(sha(archive)==binding['archive_sha256'],'restoration archive changed')
 files={str(archive):binding['archive_sha256']};members={}
 with tarfile.open(archive,'r:gz') as t:
  for m in t:
   require(m.isfile() and m.name not in members and m.size<=8<<20,'regular unique bounded archive members required')
   members[m.name]=hashlib.file_digest(t.extractfile(m),'sha256').hexdigest()
 def add(p):
  p=plain(p);digest=sha(p)
  if p.is_relative_to(RESTORE/'attempt'):name='attempt/'+str(p.relative_to(RESTORE/'attempt'))
  elif p.parent==RESTORE:name='procedure/'+p.name
  elif p.parent==Path(binding['evidence_root']):name='analysis/'+p.name
  else:raise ValueError('unselected restoration archive path')
  require(members.get(name)==digest,'restoration differs from actual archive: '+str(p));files[str(p)]=digest;return p
 def record(p):return json.loads(add(p).read_text())
 p=record(RESTORE/'procedure.json');procsha=files[str(RESTORE/'procedure.json')]
 require(procsha==binding['procedure_sha256'] and p['scope']=='bounded-native-programming-failure-files-restoration-only' and p['serial']=='TRWU8CBL' and p['allowed_roles']==['recovery-rollback','rollback'] and p['diagnostic_authority'] is False and p['permanent_QSPI_write'] is False and p['SD_write_authority']=='exact-original-FIT-rollback-only' and p['physical_power_cycle_confirmation_required'] is True and p['attempt_limit']==1 and p['worker_caps']==dict(recovery=270,refresh=420),'wrong file-restoration scope')
 require(p['original_procedure_sha256']==FAILURE and p['failure_archive_sha256']==FAILURE_ARCHIVE and p['original_FIT_sha256']==ORIGINAL,'restoration does not follow the admitted failed candidate')
 plan=add(RESTORE/'plan.json');require(sha(plan)==p['plan_sha256'],'restoration plan changed');prior=Bundle(plan,p['plan_sha256'])
 require(prior.plan['rollback']['target_boot']==procedure.bundle.plan['rollback']['target_boot'] and prior.plan['rollback']['target_boot']['sha256']==ORIGINAL,'different original rollback FIT')
 a=RESTORE/'attempt';reset=record(RESTORE/'human-power-cycle.json')
 require(reset==record(a/'human-power-cycle.json') and reset['scope']=='top-TRWU8CBL-physical-power-cycle-for-native-programming-failure-files-restoration-only' and reset['completed'] is True and reset['original_procedure_sha256']==FAILURE and reset['failure_archive_sha256']==FAILURE_ARCHIVE and type(reset['human_quote']) is str and bool(reset['human_quote']) and type(reset['confirmation_question_id']) is str and bool(reset['confirmation_question_id']),'fresh failure-bound human reset confirmation absent')
 require(p['files'][str(RESTORE/'human-power-cycle.json')]['sha256']==files[str(RESTORE/'human-power-cycle.json')],'reset record not frozen before restoration')
 attempt=record(a/'operator-attempt.json');require(attempt==dict(scope='restoration-only',procedure_sha256=procsha,original_procedure_sha256=FAILURE),'different restoration attempt')
 result=record(a/'result.json');require(result['status']=='verified-original-FIT-files-only-restoration' and result['fit_restored'] is True and result['native_runtime_dispatched'] is False and result['baseline_cycle_park_verified'] is False and result['diagnostic_retried'] is False and result['hardware_acceptance'] is False,'files-only restoration incomplete or claims native admission')
 require(not any((a/n).exists() for n in ['native-baseline','native-diagnostic','recovery-before','forward']),'restoration dispatched an unselected role')
 d=a/'recovery-rollback';r=record(d/'role-result.json');s=record(d/'supervisor-result.json');spec=next(v for v in prior.plan['roles'] if v['role']=='recovery-rollback')
 require(role(r,s,name='recovery-rollback',plan=p['plan_sha256'],image=spec['image']['sha256']) and result['recovery']==r,'recovery role differs')
 session=record(d/'recovery-result.json');complete=record(d/'completed-program.json');program=add(d/'recovery-boot-program.log');console=add(d/'recovery-console.log')
 require(session==r['session_result'] and complete['phase']=='recovery' and complete['image_sha256']==r['image_sha256'] and complete['epoch']==r['transport_epoch'] and complete['grant']==session['programming_grant'] and complete['output_sha256']==sha(program) and session['console_sha256']==sha(console),'fresh recovery program/epoch/wire differs')
 require(b'Configuration succeeded' in program.read_bytes() and b'0 errors' in program.read_bytes() and b'SOCFPGA_AGILEX5 # ' in console.read_bytes(),'recovery program or prompt absent')
 params=sorted(d.glob('parameter-*.log'),key=lambda q:int(q.stem.split('-')[-1]));require(len(params)>=2 and [add(q).read_text().splitlines()[-1] for q in params[-2:]]==['1','15M'],'JTAG settings not restored')
 f=a/'rollback';refresh=record(f/'refresh-result.json');sup=record(f/'supervisor-result.json')
 require(refresh['status']=='verified' and refresh['console_closed'] is True and refresh['plan_sha256']==p['plan_sha256'] and sup['status']=='passed' and sup['worker_stopped'] is True and sup['timed_out'] is False and sup['guard_failed'] is False and result['rollback']==refresh['bundle'],'FIT restoration incomplete')
 gate=refresh['smmu_reset_gate'];require(gate['status']=='passed-read-only-NS-SMMU-reset-gate' and gate['reads']==19 and gate['register_writes']==gate['SD_writes']==gate['network_commands']==0 and gate['secure_bank_examined'] is False and gate['reset_domain_qualified'] is False and gate['DMA_authority'] is False,'read-only NS reset observation scope differs')
 wire=replay(prior,'rollback',refresh['bundle'],add(f/'refresh-console.log').read_bytes())
 require(wire['target_FIT_sha256']==ORIGINAL and wire['readbacks_before']==wire['readbacks_after']==17,'original FIT or preserved files not replayed')
 evidence=Path(binding['evidence_root']);closed=record(evidence/'result.json')
 require(closed['status']=='passed-independent-original-files-restoration-after-native-program-failure' and closed['procedure_sha256']==procsha and closed['original_FIT_restored'] is True and closed['original_native_runtime_booted'] is False and closed['diagnostic_retry'] is False and len(closed['root_process_scan'])>=4 and all(v['original_process_stopped'] is True for v in closed['root_process_scan']) and closed['root_adapter_UART_FD_scan']['open_handles']==[] and closed['USB_identity_inode_permissions_restored'] is True and closed['JTAG_auto_1_15M_restored'] is True,'restoration closure incomplete')
 return dict(scope=SCOPE,restoration_procedure_sha256=procsha,restoration_archive_sha256=binding['archive_sha256'],original_FIT_sha256=ORIGINAL,original_FIT_readback_verified=True,recovery_program_verified=True,restoration_wire=wire,files=files,baseline_final_Park_verified=False,Secure_SMMU_reset_qualified=False,DMA_authority=False,fresh_epoch_admission_required=True,hardware_actions=False)
def check(procedure):
 binding=BASE/'restoration-binding.json';value=validate(read(binding),procedure);value['files'][str(binding)]=sha(binding);return value
def recheck(path,expected):
 require(type(expected) is str and re.fullmatch('[0-9a-f]{64}',expected) is not None and sha(plain(path))==expected,'restoration snapshot changed')
 v=read(path);require(v['scope']==SCOPE and v['original_FIT_sha256']==ORIGINAL and v['original_FIT_readback_verified'] is True and v['recovery_program_verified'] is True and v['baseline_final_Park_verified'] is v['Secure_SMMU_reset_qualified'] is v['DMA_authority'] is v['hardware_actions'] is False and v['fresh_epoch_admission_required'] is True and len(v['files'])>=19,'wrong restoration snapshot authority')
 for p,h in v['files'].items():require(sha(plain(p,32<<20))==h,'restoration evidence changed')
 return True
