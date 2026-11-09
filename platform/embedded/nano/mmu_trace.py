"""Bounded observer for the selected late EL3 MMU diagnostic; no authority."""
import hashlib
import json
import re
import mmu_status
import mmu_logging
import mmu_mailbox_view

POLICY_SHA='f735c54bf8fd6b9a810a12bead54d34bc124ea14263d38c1c65d326af702ea88'
CACHE_HELPER_SHA='c16b4d1b6931e6d4d92ac017ba23457ff59d6d5ac9e4279b6c9fb031114b305c'
FIELDS=('el','midr','mpidr','mmfr0','daif','scr','sctlr','tcr','root','mair','witness')
ROW=re.compile(r'FES_NANO_EL3_MMU phase=(before|after) '+
    ' '.join(name+r'=(?P<'+name+r'>[0-9a-f]{1,16})' for name in FIELDS)+
    r' writes=(?P<writes>[0-4]) memory=(?P<memory>[01])')
STEP=re.compile(r'FES_NANO_EL3_MMU_STEP phase=([a-z-]{1,20}) operation=([0-3])')
TRANSITION=re.compile(r'FES_NANO_EL3_MMU_TRANSITION phase=(enabled|restored) sctlr=([0-9a-f]{16}) vbar=([0-9a-f]{16})')
FAULT_FIELDS=('esr','elr','far','spsr','sp','sctlr','tcr','root','mair','scr','vbar','sp0')
FAULT=re.compile(r'FES_NANO_EL3_MMU_FAULT '+' '.join(name+r'=(?P<'+name+r'>[0-9a-f]{16})' for name in FAULT_FIELDS))
STEPS=(('admit-before',0),('admit-after',0),
    ('barrier-before',0),('barrier-after',0),
    ('write-before',0),('write-after',0),('write-before',1),('write-after',1),
    ('write-before',2),('write-after',2),('barrier-before',1),('barrier-after',1),
    ('admit-before',0),('admit-after',0),('write-before',3),('write-after',3),
    ('admit-before',0),('admit-after',0),('memory-before',0),
    ('clean-before',0),('clean-after',0),('clean-before',0),('clean-after',0),
    ('memory-after',0),('admit-before',0),('admit-after',0))


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
            layout.get('data_start')!=root+24576 or type(layout.get('data_end')) is not int or
            not root+24576<layout['data_end']<=0x80200000 or
            type(witness) is not int or witness&63 or not root+24576<=witness<=layout['data_end']-64 or
            proof.get('witness_bytes')!=64):
        raise ValueError('MMU trace requires exact linked table/witness placement')
    helper=proof.get('startup_cache_helper') or {};address=helper.get('address')
    if (type(address) is not int or address&3 or not 0x80000000<=address<address+44<=root or
            helper.get('bytes')!=44 or helper.get('sha256')!=CACHE_HELPER_SHA):
        raise ValueError('MMU trace requires the linked SCR-restoring startup helper')
    diagnostic=proof.get('transition_diagnostic') or {}
    if diagnostic.get('status_inventory')!=mmu_status.SHA:
        raise ValueError('MMU trace requires the exact read-only status inventory')
    state_address=diagnostic.get('logging_state');saved_address=diagnostic.get('saved_vbar')
    if (diagnostic.get('decode_logging')!=mmu_logging.SHA or type(state_address) is not int or
            state_address&7 or not root+24576<=state_address<state_address+8<=0x80200000 or
            diagnostic.get('logging_state_bytes')!=8 or type(saved_address) is not int or
            abs(state_address-saved_address)<8):
        raise ValueError('MMU trace requires private contained decode logging')
    if any(witness<a+8 and a<witness+64 for a in (state_address,saved_address)):
        raise ValueError('MMU trace witness overlaps transition lifetime state')
    for name in ('write','restore','capture','vectors','park_vectors','park'):
        code=diagnostic.get(name) or {};address=code.get('address');size=code.get('bytes')
        if (type(address) is not int or address&3 or type(size) is not int or size&3 or
                not 0<size<=16384 or not 0x80000000<=address<address+size<=root or
                not re.fullmatch('[0-9a-f]{64}',str(code.get('sha256',''))) or
                name in ('vectors','park_vectors') and (address&2047 or size!=2048) or
                name=='park' and size!=8):
            raise ValueError('MMU trace requires the linked bounded transition diagnostic')
    formatter=diagnostic.get('hex_formatter') or {};address=formatter.get('address')
    if (formatter.get('contract')!=mmu_mailbox_view.HEX or formatter.get('bytes')!=96 or
            type(address) is not int or address&3 or not 0x80000000<=address<address+96<=root or
            not re.fullmatch('[0-9a-f]{64}',str(formatter.get('sha256',''))) or
            set(formatter.get('calls',{}))!={'write','restore','capture'}):
        raise ValueError('MMU trace requires its exact stack-free UART formatter')
    for name,addresses in formatter['calls'].items():
        parent=diagnostic[name]
        if not addresses or len(addresses)>128 or any(type(a) is not int or a&3 or not parent['address']+8<=a<a+8<=parent['address']+parent['bytes'] for a in addresses):
            raise ValueError('MMU UART formatter call bounds differ')
    views=diagnostic.get('mailbox_views') or {}
    if views.get('contract_sha256')!=mmu_mailbox_view.SHA or views.get('maximum_reads')!=18 or len(views.get('sites',[]))!=3:
        raise ValueError('MMU trace requires the selected bounded mailbox views')
    writer=diagnostic['write'];previous=writer['address']
    for phase,group in enumerate(views['sites']):
        names=('scope_before','setup','reads','loop','scope_after')
        if not isinstance(group,dict) or set(group)!=set(names):
            raise ValueError('MMU trace mailbox view sites differ')
        for name in names:
            site=group[name];address=site.get('address');size=site.get('bytes')
            if (type(address) is not int or address&3 or size!=dict(scope_before=356,setup=28,reads=24,loop=12,scope_after=356)[name] or
                    not previous<=address<address+size<=writer['address']+writer['bytes'] or
                    not re.fullmatch('[0-9a-f]{64}',str(site.get('sha256','')))):
                raise ValueError('MMU trace mailbox view site bounds differ')
            previous=address+size
    runtime=diagnostic.get('runtime_vectors');saved=diagnostic.get('saved_vbar')
    if (type(runtime) is not int or runtime&2047 or not 0x80000000<=runtime<runtime+2048<=root or
            type(saved) is not int or saved&7 or not root+24576<=saved<saved+8<=0x80200000 or
            diagnostic.get('saved_bytes')!=8 or
            any(diagnostic.get(k)!=v for k,v in dict(uart_data=0x10c02100,uart_status=0x10c02114,uart_polls=100000,uart_bytes=384).items())):
        raise ValueError('MMU trace requires exact vector restoration and UART bounds')
    return dict(root=root,witness=witness,mair=policy['mair'],tcr=policy['tcr'],
                vectors=diagnostic['vectors']['address'],runtime_vectors=runtime)


def state():return dict(records=[],steps=[],transitions=[],faults=[],status_before=mmu_status.state(),logging=mmu_logging.state(),
                        status_fault=mmu_status.state(),mailbox_views=mmu_mailbox_view.state(),valid=True,complete=False,hardware_authority=False)


def observe(s,line,number,expected,*,startup_complete,exit_cache_complete,uboot_seen,prior_valid):
    match=ROW.fullmatch(line);step=STEP.fullmatch(line);transition=TRANSITION.fullmatch(line);fault=FAULT.fullmatch(line)
    if line.startswith('FES_NANO_EL3_MMU_MAILBOX'):
        phase=re.search(r' phase=([0-2])(?: |$)',line)
        permitted=(expected is not None and s['valid'] and prior_valid and startup_complete and exit_cache_complete and
                   not uboot_seen and len(s['records'])==1 and len(s['steps'])==15 and
                   s['status_before']['complete'] and s['logging']['armed'] and not s['logging']['restore_attempted'] and
                   phase is not None and len(s['transitions'])==(0 if int(phase[1])==0 else 1))
        error=mmu_mailbox_view.observe(s['mailbox_views'],line,number) if permitted else 'out-of-phase MMU mailbox view'
        if error:s['valid']=s['complete']=False
        return error
    if line.startswith('FES_NANO_EL3_MMU_LOG'):
        fatal=bool(s['faults'])
        permitted=(expected is not None and startup_complete and exit_cache_complete and not uboot_seen and
            len(s['records'])==1 and
            ((fatal and s['status_fault']['complete']) or
             (not fatal and s['valid'] and prior_valid and s['status_before']['complete'] and
              (len(s['steps'])==15 and not s['transitions'] or len(s['steps'])==26 and len(s['transitions'])==1)) or
             (not fatal and s['logging']['failure']=='enable' and s['logging']['write_attempted'] and
              len(s['steps'])==15 and not s['transitions'])))
        log_row=mmu_logging.ROW.fullmatch(line)
        if not fatal and log_row and len(s['steps'])==26 and log_row[1] not in ('restore-before','restored'):
            permitted=False
        error=mmu_logging.observe(s['logging'],line,number,fault=fatal) if permitted else 'out-of-phase MMU decode logging'
        if error:s['valid']=s['complete']=False
        return error
    if line.startswith('FES_NANO_EL3_MMU_STATUS'):
        before=line.startswith('FES_NANO_EL3_MMU_STATUS phase=before ')
        phase='before' if before else 'fault'
        permitted=(expected is not None and startup_complete and exit_cache_complete and not uboot_seen and
                   len(s['records'])==1 and
                   ((before and len(s['steps'])==15 and s['valid'] and prior_valid and not s['transitions'] and not s['faults']) or
                    (not before and 15<=len(s['steps'])<=26 and len(s['faults'])==1)))
        error=mmu_status.observe(s['status_'+phase],line,number,phase) if permitted else 'out-of-phase MMU status inventory'
        if error:s['valid']=s['complete']=False
        return error
    if fault:
        # An asynchronous fault can interrupt a diagnostic value mid-line.
        # Preserve one complete fault frame even after that malformed line;
        # neither the frame nor later status reads can restore admission.
        if (expected is not None and startup_complete and exit_cache_complete and not uboot_seen and
                len(s['records'])==1 and 15<=len(s['steps'])<=26 and
                len(s['transitions'])<=1 and not s['faults']):
            r={name:int(fault[name],16) for name in FAULT_FIELDS};r.update(line=number,ec=r['esr']>>26&63)
            r['interrupted_sp']=r['sp0'] if r['spsr']&15==12 else r['sp'] if r['spsr']&15==13 else None
            s['faults'].append(r)
        s['valid']=s['complete']=False;return 'terminal EL3 MMU transition fault'
    if (not s['valid'] or expected is None or not prior_valid or
            not startup_complete or not exit_cache_complete or uboot_seen or len(s['records'])>=2):
        s['valid']=s['complete']=False;return 'invalid/unselected/out-of-order EL3 MMU record'
    if transition:
        phase=transition[1];sctlr=int(transition[2],16);vbar=int(transition[3],16)
        before=s['records'][0] if s['records'] else {}
        ok=(len(s['records'])==1 and sctlr==before.get('sctlr',0)|0x80005 and
            ((phase=='enabled' and len(s['steps'])==15 and not s['transitions'] and vbar==expected['vectors'] and
              s['status_before']['valid'] and s['status_before']['complete'] and s['logging']['valid'] and
              s['logging']['armed'] and not s['logging']['restore_attempted'] and
              len(s['mailbox_views']['records'])==1 and s['mailbox_views']['pending'] is None) or
             (phase=='restored' and len(s['steps'])==26 and len(s['transitions'])==1 and vbar==expected['runtime_vectors'] and
              s['logging']['valid'] and s['logging']['complete'] and s['logging']['restored'] and s['mailbox_views']['complete'])))
        if not ok:s['valid']=s['complete']=False;return 'invalid/out-of-order EL3 MMU transition'
        s['transitions'].append(dict(phase=phase,sctlr=sctlr,vbar=vbar,line=number));return None
    if step:
        index=len(s['steps']);value=(step[1],int(step[2]))
        if (len(s['records'])!=1 or index>=len(STEPS) or value!=STEPS[index] or
                len(s['transitions'])!=(0 if index<15 else 1) or index==15 and not s['mailbox_views']['complete']):
            s['valid']=s['complete']=False;return 'invalid/out-of-order EL3 MMU step'
        s['steps'].append(dict(phase=value[0],operation=value[1],line=number))
        return None
    if not match:
        s['valid']=s['complete']=False;return 'malformed EL3 MMU record'
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
        ok=(ok and len(s['records'])==1 and before.get('phase')=='before' and len(s['steps'])==len(STEPS) and
            [v['phase'] for v in s['transitions']]==['enabled','restored'] and
            all(r[name]==before.get(name) for name in ('el','midr','mpidr','mmfr0','daif','scr')) and
            r['sctlr']==before.get('sctlr',0)|0x80005 and r['mair']==expected['mair'] and
            r['tcr']==expected['tcr'] and r['root']==expected['root'] and r['writes']==4 and r['memory']==1 and s['mailbox_views']['complete'])
    s['records'].append(r)
    if not ok:s['valid']=False
    s['complete']=s['valid'] and [v['phase'] for v in s['records']]==['before','after']
    return None if ok else 'EL3 MMU CPU/register/writeback receipt mismatch'
