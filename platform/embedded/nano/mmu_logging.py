"""Bounded late-MMU decode-log lifetime; diagnostic evidence only."""
import hashlib
import json
import re

CONTRACT=dict(format=1,scope='late-mmu-contained-dsu-decode-v1',
    control=0x1c000100,interrupt=0x1c000104,status=0x1c000108,
    field='CAIUUEDR.DecErrDetEn',baseline=0,enabled=8,restore=0,
    state_bytes=8,states=[0,1,2,3,4],status_acknowledgement=False,
    scope_checks='single-secure-cpu-exact-mmu-all-masters-reset-v1',
    hardware_authority=False)
SHA=hashlib.sha256(json.dumps(CONTRACT,sort_keys=True,separators=(',',':')).encode()).hexdigest()
ROW=re.compile(r'FES_NANO_EL3_MMU_LOG phase=(arm-before|armed|restore-before|restored|inactive|interrupted|retired)')
FAIL=re.compile(r'FES_NANO_EL3_MMU_LOG FAIL phase=(admit|enable|restore|status)')


def state():return dict(records=[],valid=True,complete=False,write_attempted=False,
    armed=False,restore_attempted=False,restored=False,failed=False,failure=None,hardware_authority=False)


def observe(s,line,number,*,fault):
    def bad():
        s['valid']=s['complete']=False;s['failed']=True
        return 'invalid/out-of-order MMU decode logging'
    failure=FAIL.fullmatch(line)
    if failure:
        if s['failure'] is None:s['failure']=failure[1]
        s['failed']=True;s['valid']=s['complete']=False
        return 'terminal MMU decode logging failure: '+failure[1]
    match=ROW.fullmatch(line)
    cleanup=s['failure']=='enable' and s['write_attempted'] and not s['restored']
    if not match or not s['valid'] and not cleanup or len(s['records'])>=6:return bad()
    phase=match[1];previous=[r['phase'] for r in s['records']]
    if phase=='arm-before':
        if fault or previous:return bad()
        s['write_attempted']=True
    elif phase=='armed':
        if fault or previous!=['arm-before']:return bad()
        s['armed']=True
    elif phase=='restore-before':
        if previous not in (['arm-before'],['arm-before','armed']):return bad()
        s['restore_attempted']=True
    elif phase=='restored':
        if not previous or previous[-1]!='restore-before':return bad()
        s['restored']=True
        s['complete']=not fault and s['armed'] and not s['failed']
    elif phase=='inactive':
        if not fault or previous not in ([],['arm-before']):return bad()
    elif phase=='interrupted':
        if not fault or not previous or previous[-1] not in ('armed','arm-before','restore-before'):return bad()
        s['restore_attempted']=True;s['failed']=True;s['complete']=False
    else:
        if not fault or not previous or previous[-1] not in ('restore-before','restored'):return bad()
        # A private retired state without its readback receipt is not proof.
        s['complete']=False
    s['records'].append(dict(phase=phase,line=number))
    if fault:s['complete']=False
    return None
