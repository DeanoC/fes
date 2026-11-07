"""Observe the selected cold two-slot menu and independent secure return.

Serial receipts establish driver observations, not physical HDMI qualification.
This decoder performs no device operations and grants no runtime admission.
"""
import re

from hps_context import MENU_PROFILE, digest, validate_menu_read
from window_trace import PATTERNS as WINDOW_PATTERNS, HEX, TEXT, check_route

D = r'[0-9]{1,20}'
H = r'[0-9a-f]{1,16}'
PATTERNS = dict(WINDOW_PATTERNS,
    step=re.compile(rf'FES_NATIVE_FPGA_MENU_PRESENT STEP index=(?P<index>{D}) phase=(?P<phase>[a-z_]{{1,24}}) elapsed_us=(?P<elapsed_us>{D})'),
    frame=re.compile(rf'FES_NATIVE_FPGA_MENU_PRESENT FRAME index=(?P<index>{D}) package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) reader_generation=(?P<reader_generation>{D}) sequence=(?P<sequence>{D}) submitted=(?P<submitted>{D}) slot=(?P<slot>{D}) underflows=(?P<underflows>{D}) memory_retained=1'),
    receipt=re.compile(rf'FES_NATIVE_FPGA_MENU_PRESENT PASS package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) source_generation=(?P<source_generation>{D}) closed_generation=(?P<closed_generation>{D}) frames=(?P<frames>{D}) last_sequence=(?P<last_sequence>{D}) last_slot=(?P<last_slot>{D}) access_map_sha256=(?P<access_map_sha256>[0-9a-f]{{64}}) data_closed=1 memory_retained=1 runtime_admission=unavailable'))


class _Incomplete(Exception): pass
class _Invalid(Exception): pass


def checked_contract(identity, memory, contract):
    if (not isinstance(identity, dict) or set(identity) != {'sha256','package_id','build_id','control_abi','cpu_base','map_sha256'} or
            type(identity.get('control_abi')) is not int or identity['control_abi'] != 0x10203043 or
            type(identity.get('cpu_base')) is not int or identity['cpu_base'] != 0x20010400 or
            any(not isinstance(identity.get(k), str) or not re.fullmatch('[0-9a-f]{'+str(n)+'}', identity[k]) or not int(identity[k],16)
                for k,n in (('sha256',64),('package_id',64),('build_id',32),('map_sha256',64)))):
        raise ValueError('explicit sealed menu identity required')
    context = dict(programming_profile=MENU_PROFILE, control_abi=identity['control_abi'],
                   package_id=identity['package_id'], build_id=identity['build_id'])
    return validate_menu_read(context, memory, identity['map_sha256'], contract)


def decode(data, *, identity, memory, contract, base_decode, rules):
    checked_contract(identity, memory, contract)
    if not isinstance(data, (str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw = data.encode() if isinstance(data,str) else bytes(data)
    if len(raw) > 1<<20:
        return dict(complete=False, loader_ready=False, records=[], errors=['menu capture exceeds byte bound'], core_load_acceptance=False)
    lines = raw.splitlines(keepends=True)
    # The native open marker precedes the secure control/data gate records.
    # Never search past a malformed or partial marker to manufacture a prefix.
    split = next((i for i,line in enumerate(lines) if b'FES_NATIVE_FPGA_MENU_PRESENT' in line), None)
    prefix = raw if split is None else b''.join(lines[:split])
    report = base_decode(prefix)
    publication = report.get('menu_memory_observation')
    source = next((r['values'] for r in report['records'] if r['stage']=='hps_context'), None)
    prefix_complete = bool(report['complete'] and report['last_checkpoint']=='ddr_firewall_end' and
                           report.get('configuration_transfer',{}).get('complete') and source and publication)
    observed = dict(complete=False, records=[], progress=[], frames=[], memory_released=False,
                    hdmi_qualified=False, runtime_admission=False)
    report['menu_present'] = observed
    report['core_load_acceptance'] = False
    report['classification'] = 'serial-observed native two-slot menu and secure return; physical HDMI and menu/core/menu unqualified'
    report['complete'] = False
    if split is None:
        return report
    rows = b''.join(lines[split:]).decode(errors='replace').split('\n')[:-1]
    cursor = 0
    elapsed = -1
    owner = source['owner'] if source else 0

    def require(ok, reason):
        if not ok:
            report['errors'].append(reason)
            raise _Invalid()

    def take(kind):
        nonlocal cursor
        while cursor < len(rows) and rows[cursor].strip() in ('','NOTICE:'):
            cursor += 1
        if cursor == len(rows): raise _Incomplete()
        match = (PATTERNS[kind] if kind in PATTERNS else rules[kind]).fullmatch(rows[cursor].rstrip('\r'))
        cursor += 1
        require(match is not None, 'missing, reordered or malformed menu '+kind+' at suffix line '+str(cursor))
        value = {k:v if k in TEXT else int(v,16 if k in HEX else 10) for k,v in match.groupdict().items()}
        observed['records'].append(dict(stage=kind,suffix_line=cursor,values=value))
        return value

    def step(index, phase):
        nonlocal elapsed
        v = take('step')
        require(v['index']==index and v['phase']==phase and elapsed<=v['elapsed_us']<=240000000,
                'menu progress changed order, frame or bounded monotonic time')
        elapsed = v['elapsed_us']
        observed['progress'].append(v)
        report['last_checkpoint'] = 'menu_'+phase+'_'+str(index)

    def owned(v, generation, reset):
        require(v['owner']==owner and v['generation']==generation and v['reset']==reset,
                'menu changed owner, generation or bridge reset scope')

    def scope(phase, generation, reset):
        v = take('scope'); owned(v,generation,reset)
        require(v['phase']==phase, 'menu secure scope phase differs')

    try:
        step(1,'open_begin')
        # Readiness runs inside the firmware's first control-opening request.
        # Validate these real records before expecting an opened control scope.
        take('fpga_query'); take('fpga_status')
        check_route(take,require,owner=owner,generation=4,reset=15,memory=memory,with_gate=False)
        # Keep the original line numbers by replacing only the already checked
        # native progress line with a blank. All actual secure records retain
        # their order in the shared boot/configuration decoder.
        boot = base_decode(prefix+b'\n'+b''.join(lines[split+1:split+cursor]))
        report.update(boot)
        prefix_complete = bool(boot['complete'] and boot['last_checkpoint']=='ddr_firewall_end' and
                               boot.get('configuration_transfer',{}).get('complete') and source and publication)
        report['complete'] = False
        report['last_checkpoint'] = 'menu_open_begin_1'
        require(prefix_complete, 'menu presentation preceded a complete owned boot/configuration prefix')
        scope('control',5,13)
        for _ in range(2):
            check_route(take,require,owner=owner,generation=5,reset=13,memory=memory,with_gate=True)
        scope('data',6,5)
        step(1,'open_end')
        for index in range(1,4):
            step(index,'render_begin'); step(index,'render_end'); step(index,'present_begin')
            frame = take('frame')
            require(frame['index']==index and frame['package_id']==identity['package_id'] and frame['build_id']==identity['build_id'] and
                    frame['reader_generation']==1 and frame['sequence']==frame['submitted']==index and frame['slot']==(index-1)%2 and
                    frame['underflows']==0, 'menu frame changed identity, acknowledgement, slot or reported underflow')
            observed['frames'].append(frame)
            step(index,'present_end')
        step(4,'return_begin')
        scope('close',6,5)
        stop = take('master'); owned(stop,6,5)
        require(stop['status']==0 and stop['complete']==1 and stop['disable']==0 and stop['reader_reads']==88 and
                stop['reader_polls']==1 and stop['probe_polls']==2 and stop['cancel']==0,
                'menu secure return lacks the complete already-drained master stop')
        closure = take('closure'); owned(closure,7,15)
        require(closure['hardware_drained']==8 and closure['data_remained_contained']==0 and
                closure['producers_quiescent']==1 and closure['fabric_acknowledged']==0,
                'menu secure return lacks the exact active DDR closure')
        step(4,'return_end')
        receipt = take('receipt')
        require(receipt==dict(package_id=identity['package_id'],build_id=identity['build_id'],owner=owner,
                source_generation=4,closed_generation=7,frames=3,last_sequence=3,last_slot=0,access_map_sha256=digest(contract)),
                'menu native return differs from its sealed frame/session scope')
        require(all(not line.strip() for line in rows[cursor:]), 'menu capture contains extra or replayed events')
        observed.update(complete=raw.endswith(b'\n'),master_stop=stop,closure=closure,receipt=receipt)
        report['complete'] = bool(prefix_complete and observed['complete'] and not report['errors'])
        if report['complete']: report['last_checkpoint'] = 'menu_present_closed'
    except (_Incomplete,_Invalid):
        pass
    return report
