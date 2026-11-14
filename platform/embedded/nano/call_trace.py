"""Receipts for the eight selected pre-entry PSCI affinity queries."""
import re

PHASES=('smc-before','affinity-enter','cache-before','cache-after','affinity-return','smc-after')


def state():
    return dict(records=[],valid=True,initial_complete=False,complete=False,
                instruction_may_have_started=False,cache_may_have_started=False,
                instruction_returns_observed=0)


def observe(progress,line,number,*,boot_seen,loader_phase,late_seen,live_seen,prior_valid):
    def invalid():
        progress['valid']=progress['initial_complete']=progress['complete']=False
        return f'invalid affinity call trace at line {number}'
    if (not prior_valid or not boot_seen or live_seen or not progress['valid'] or
            progress['complete'] or len(progress['records'])>=8*len(PHASES)):
        return invalid()
    call,step=divmod(len(progress['records']),len(PHASES))
    match=re.fullmatch(r'FES_NANO_CALL phase=([a-z-]+)',line)
    if (match is None or match[1]!=PHASES[step] or late_seen!=(call>=4) or
            call<4 and loader_phase!=f'cpu{call}-before'):
        return invalid()
    phase=match[1]
    progress['records'].append(dict(phase=phase,line=number,call=call,cpu=call%4,
                                   scope='initial' if call<4 else 'late'))
    if phase=='smc-before':progress['instruction_may_have_started']=True
    if phase=='cache-before':progress['cache_may_have_started']=True
    if phase=='smc-after':progress['instruction_returns_observed']+=1
    progress['initial_complete']=len(progress['records'])>=4*len(PHASES)
    progress['complete']=len(progress['records'])==8*len(PHASES)
    return None
