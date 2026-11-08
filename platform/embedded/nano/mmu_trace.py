"""Bounded observer for the selected late EL3 MMU diagnostic; no authority."""
import hashlib
import json
import re

POLICY_SHA='764e07f5ba6601027ccb061b60e3e3f88ca6167185fcd317293097cd94ac72fa'
FIELDS=('el','midr','mpidr','mmfr0','daif','scr','sctlr','tcr','root','mair','witness')
ROW=re.compile(r'FES_NANO_EL3_MMU phase=(before|after) '+
    ' '.join(name+r'=(?P<'+name+r'>[0-9a-f]{1,16})' for name in FIELDS)+
    r' writes=(?P<writes>[0-4]) memory=(?P<memory>[01])')


def parameters(proof):
    if proof is None:return None
    if not isinstance(proof,dict):raise ValueError('complete MMU ELF proof required')
    policy=proof.get('selected_policy')
    if hashlib.sha256(json.dumps(policy,sort_keys=True,separators=(',',':')).encode()).hexdigest()!=POLICY_SHA:
        raise ValueError('MMU trace policy differs from selected producer')
    layout=proof.get('layout') or {};tables=layout.get('tables') or {}
    root=tables.get('root');witness=proof.get('witness')
    if (type(root) is not int or root&4095 or not 0x80000000<root<=0x80200000-24576 or
            tables!={name:root+i*4096 for i,name in enumerate(('root','low','ocram','gic','ddr','firmware'))} or
            layout.get('tables_start')!=root or layout.get('tables_end')!=root+24576 or
            type(witness) is not int or witness&63 or not 0x58000<=witness<=0x60000-64 or
            proof.get('witness_bytes')!=64):
        raise ValueError('MMU trace requires exact linked table/witness placement')
    return dict(root=root,witness=witness,mair=policy['mair'],tcr=policy['tcr'])


def state():return dict(records=[],valid=True,complete=False,hardware_authority=False)


def observe(s,line,number,expected,*,startup_complete,exit_cache_complete,uboot_seen,prior_valid):
    match=ROW.fullmatch(line)
    if (not s['valid'] or expected is None or not match or not prior_valid or
            not startup_complete or not exit_cache_complete or uboot_seen or len(s['records'])>=2):
        s['valid']=s['complete']=False;return 'invalid/unselected/out-of-order EL3 MMU record'
    phase=match[1];r={name:int(match[name],16) for name in FIELDS}
    r.update(phase=phase,line=number,writes=int(match['writes']),memory=int(match['memory']))
    ok=(r['el']==12 and r['midr']==0x412fd050 and r['mpidr']==0x81000000 and
        not(r['mmfr0']>>28&15) and r['mmfr0']&15<=5 and
        r['daif']&0x2c0==0x2c0 and not r['daif']&~0x3c0 and not r['scr']&1 and
        r['witness']==expected['witness'])
    if phase=='before':
        ok=ok and not s['records'] and r['sctlr']&0x3081005==0x1000 and not r['writes'] and not r['memory']
    else:
        before=s['records'][0] if s['records'] else {}
        ok=(ok and len(s['records'])==1 and before.get('phase')=='before' and
            all(r[name]==before.get(name) for name in ('el','midr','mpidr','mmfr0','daif','scr')) and
            r['sctlr']==before.get('sctlr',0)|0x80005 and r['mair']==expected['mair'] and
            r['tcr']==expected['tcr'] and r['root']==expected['root'] and r['writes']==4 and r['memory']==1)
    s['records'].append(r)
    if not ok:s['valid']=False
    s['complete']=s['valid'] and [v['phase'] for v in s['records']]==['before','after']
    return None if ok else 'EL3 MMU CPU/register/writeback receipt mismatch'
