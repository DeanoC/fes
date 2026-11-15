"""Qualification ends at proved candidate Park; rollback never boots the old image."""
import json,math,os
from flow import Flow,OperationFailure,ROLES
from fit_swap import require
class CandidateFlow(Flow):
 def epoch(self,role):
  require(role in ('recovery-before','native-diagnostic','recovery-rollback'),'old native baseline dispatch forbidden')
  return super().epoch(role)
 def run(self):
  require(not self.retired,'flow retired');self.retired=True;self.start=self.now();require(type(self.start) in (int,float) and math.isfinite(self.start),'monotonic clock required');self.last=self.leg_start=self.start
  with (self.directory/'attempt.json').open('x') as f:json.dump(dict(scope='one-candidate-cycle-qualification',plan_sha256=self.bundle.sha256),f);f.flush();os.fsync(f.fileno())
  with (self.directory/'journal.jsonl').open('x'):pass
  state=dict(status='running',fit_state='baseline',candidate_cycle_Park_verified=False,candidate_left_installed=False,original_FIT_restored=False,old_native_baseline_dispatched=False,rollback_attempted=False,physical_reset_required=False,ordinary_admission=False,hardware_acceptance=False)
  possible_write=False;native_attempted=False;recovery_completed=False;cleanup=True;safe=False
  try:
   recovery=self.epoch('recovery-before');recovery_completed=True;safe=True;possible_write=True;state['fit_state']='uncertain';self.refresh('forward',recovery);state['fit_state']='candidate'
   native_attempted=True;safe=False;self.epoch('native-diagnostic');state.update(candidate_cycle_Park_verified=True,candidate_left_installed=True);safe=True
  except BaseException as error:
   state['error']=str(error);cleanup=isinstance(error,OperationFailure) and error.cleanup_verified
   if isinstance(error,OperationFailure):
    possible_write=possible_write and error.may_have_written or state['fit_state']=='candidate'
    if native_attempted:safe=error.recovery_safe
   self.record('candidate-stopped',error=str(error),cleanup_verified=cleanup,recovery_safe=safe)
  state['recovery_safe']=safe;state['physical_reset_required']=(native_attempted or not recovery_completed) and not safe
  if not state['candidate_cycle_Park_verified'] and possible_write and cleanup and safe:
   state['rollback_attempted']=True;self.leg_start=self.last
   try:
    recovery=self.epoch('recovery-rollback');self.refresh('rollback',recovery);state.update(fit_state='baseline',original_FIT_restored=True)
   except BaseException as error:state.update(rollback_error=str(error),physical_reset_required=True,recovery_safe=False);self.record('rollback-stopped',error=str(error))
  state['status']='verified-candidate-cycle-Park' if state['candidate_cycle_Park_verified'] else 'stopped';state['roles_consumed']=sorted(self.used,key=ROLES.index);state['plan_sha256']=self.bundle.sha256
  self.record('terminal',status=state['status'],fit_state=state['fit_state']);(self.directory/'result.json').write_text(json.dumps(state,indent=2)+'\n');return state
