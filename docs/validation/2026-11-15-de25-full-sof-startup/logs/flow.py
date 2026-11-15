"""Durable one-attempt choreography over independently qualified platform hooks.

No device implementation is selected here. Hooks must perform the existing
private JTAG, boot/capture and cleanup checks; this layer cannot grant them.
"""
import json,math,os,re,time
from pathlib import Path
from fit_swap import Bundle,require,member,validate_swap
REFRESH_SECONDS=420
ROLES=('recovery-before','native-diagnostic','recovery-rollback','native-baseline')
class OperationFailure(RuntimeError):
 def __init__(self,message,*,cleanup_verified=False,may_have_written=False,recovery_safe=False):
  super().__init__(message)
  require(type(cleanup_verified) is type(may_have_written) is type(recovery_safe) is bool,'explicit failure scope required')
  self.cleanup_verified=cleanup_verified;self.may_have_written=may_have_written;self.recovery_safe=recovery_safe

class Flow:
 def __init__(self,bundle,directory,hooks,*,now=time.monotonic):
  require(isinstance(bundle,Bundle) and all(callable(getattr(hooks,k,None)) for k in
   ('owns','recovery','native','refresh')) and callable(now),'fixed bundle and qualified hooks required')
  self.bundle,self.directory,self.hooks,self.now=bundle,Path(directory),hooks,now
  require(self.directory.is_dir() and not any(p.is_symlink() for p in (self.directory,*self.directory.parents)),'plain ledger directory required')
  self.retired=False;self.used=set();self.epochs=set();self.start=None;self.last=None;self.leg_start=None;self.sequence=0
  specs=bundle.plan['roles'];require(tuple(r['role'] for r in specs)==ROLES,'fixed four-role order required')
  require(bundle.plan['transaction_bound_seconds']==1800 and bundle.plan['leg_bound_seconds']==800 and
   bundle.plan['refresh_bound_seconds']==REFRESH_SECONDS,'fixed phase/transaction bounds required')
  for row in specs:
   image=row['image'];member(image)
   require(image['volatile'] is True and image['flash_operation'] is False and
    image['operation']=='p;'+image['path']+'@1','only exact volatile programming admitted')
   require(row['program_phase']==('recovery' if row['role'].startswith('recovery') else 'native'),'role phase differs')
   expected_kind='matched-recovery-sof' if row['program_phase']=='recovery' else ('matched-native-sof' if row['role']=='native-diagnostic' else 'hps-phase1-rbf')
   require(image['kind']==expected_kind,'role image kind differs')
   require(image['path'].endswith('.hps.rbf' if row['role']=='native-baseline' else '.sof'),'wrong volatile image container')
   require(not any(c in image['path'] for c in ';@,') and row['bound_seconds']==(270 if row['program_phase']=='recovery' else 480),
    'unselected image grammar or epoch bound')
  require(specs[0]['image']==specs[2]['image'] and specs[1]['image']['sha256']!=specs[3]['image']['sha256'],
   'recovery must stay fixed and diagnostic/baseline images distinct')
  self.specs={r['role']:r for r in specs}
 def guard(self):
  self.bundle.verify();require(self.hooks.owns() is True,'top-kit claim lost')
  value=self.now()
  require(type(value) in (int,float) and math.isfinite(value) and self.last<=value<=min(self.start+1800,self.leg_start+800),
          'transaction/leg clock bound failed')
  self.last=value
 def record(self,event,**values):
  payload=json.dumps(dict(sequence=self.sequence,event=event,plan_sha256=self.bundle.sha256,**values),sort_keys=True)+'\n'
  with (self.directory/'journal.jsonl').open('a') as stream:
   stream.write(payload);stream.flush();os.fsync(stream.fileno())
  self.sequence+=1
 def epoch(self,role):
  self.guard();require(role not in self.used,'epoch already consumed');spec=self.specs[role];self.used.add(role)
  with (self.directory/(role+'-consumed.json')).open('x') as stream:
   json.dump(dict(role=role,plan_sha256=self.bundle.sha256,image_sha256=spec['image']['sha256']),stream)
   stream.flush();os.fsync(stream.fileno())
  self.record('epoch-started',role=role)
  began=self.last
  callback=self.hooks.recovery if role.startswith('recovery') else self.hooks.native
  result=callback(role,json.loads(json.dumps(spec)),self.bundle.sha256)
  self.guard()
  if self.last-began>spec['bound_seconds']:
   raise OperationFailure('epoch deadline exceeded',cleanup_verified=isinstance(result,dict) and result.get('transport_cleaned') is True,recovery_safe=role!='native-diagnostic')
  require(type(result) is dict and result.get('status')=='verified' and result.get('role')==role and
   result.get('plan_sha256')==self.bundle.sha256 and result.get('serial')=='TRWU8CBL' and
   result.get('image_sha256')==spec['image']['sha256'] and
   result.get('transport_cleaned') is result.get('claim_verified') is True,'unqualified role completion')
  epoch=result.get('transport_epoch')
  require(type(epoch) is str and re.fullmatch('[1-9][0-9]*:[0-9]+',epoch) is not None and epoch not in self.epochs,
          'fresh private transport epoch required')
  self.epochs.add(epoch)
  if role.startswith('native'):
   expected=self.bundle.plan['forward' if role=='native-diagnostic' else 'rollback']['target_boot']['sha256']
   require(result.get('fit_sha256')==expected and result.get('boot_verified') is True and
    (result.get('source_rearm_cycle_park_verified') is True if role=='native-diagnostic' else result.get('launch_return_park_verified') is True),
           'native boot/launch/return/Park not verified')
   require(result.get('capability_observed') is (role=='native-diagnostic'),'wrong diagnostic selection')
  self.record('epoch-completed',role=role,transport_epoch=epoch)
  return result
 def refresh(self,direction,epoch):
  self.guard();self.record('refresh-may-write',direction=direction)
  began=self.last
  receipt=self.hooks.refresh(direction,epoch,self.bundle)
  self.guard()
  if self.last-began>REFRESH_SECONDS:
   raise OperationFailure('refresh deadline exceeded',cleanup_verified=isinstance(receipt,dict) and receipt.get('session_closed') is True,
    may_have_written=isinstance(receipt,dict) and receipt.get('sd_write_attempted') is True)
  proof=validate_swap(self.bundle,direction,receipt)
  self.record('refresh-verified',direction=direction,boot_sha256=proof['boot_sha256'])
  return proof
 def run(self):
  require(not self.retired,'flow retired');self.retired=True
  self.start=self.now();require(type(self.start) in (int,float) and math.isfinite(self.start),'monotonic clock required')
  self.last=self.leg_start=self.start
  with (self.directory/'attempt.json').open('x') as stream:
   json.dump(dict(scope='one-reversible-flow-attempt',plan_sha256=self.bundle.sha256),stream)
   stream.flush();os.fsync(stream.fileno())
  with (self.directory/'journal.jsonl').open('x'):pass
  state=dict(physical_reset_required=False,recovery_safe=True,status='running',diagnostic_verified=False,baseline_restored=False,rollback_attempted=False,
   fit_state='baseline',ordinary_admission=False,hardware_acceptance=False,live_adapter_qualified=False)
  possible_write=False;cleanup=True;recovery_safe=True
  self.native_attempted=False
  try:
   recovery=self.epoch('recovery-before')
   possible_write=True;state['fit_state']='uncertain'
   self.refresh('forward',recovery);state['fit_state']='candidate'
   self.native_attempted=True;recovery_safe=False
   self.epoch('native-diagnostic');state['diagnostic_verified']=True;recovery_safe=True
  except BaseException as error:
   state['error']=str(error)
   cleanup=isinstance(error,OperationFailure) and error.cleanup_verified
   if isinstance(error,OperationFailure):
    possible_write=possible_write and error.may_have_written or state['fit_state']=='candidate'
    if self.native_attempted:recovery_safe=error.recovery_safe
   self.record('forward-stopped',error=str(error),cleanup_verified=cleanup,recovery_safe=recovery_safe)
  state['recovery_safe']=recovery_safe
  state['physical_reset_required']=self.native_attempted and not recovery_safe
  if possible_write and cleanup and recovery_safe:
   state['rollback_attempted']=True
   self.leg_start=self.last
   try:
    recovery=self.epoch('recovery-rollback')
    self.refresh('rollback',recovery);state['fit_state']='baseline'
    self.epoch('native-baseline');state['baseline_restored']=True
   except BaseException as error:
    state['rollback_error']=str(error);state['physical_reset_required']=True;state['recovery_safe']=False
    self.record('rollback-stopped',error=str(error),physical_reset_required=True)
  state['status']='verified-logical-flow' if state['diagnostic_verified'] and state['baseline_restored'] else 'stopped'
  state['roles_consumed']=sorted(self.used,key=ROLES.index);state['plan_sha256']=self.bundle.sha256
  self.record('terminal',status=state['status'],fit_state=state['fit_state'])
  (self.directory/'result.json').write_text(json.dumps(state,indent=2)+'\n')
  return state
