"""Audit a complete owned window/SD return followed by one fresh window load."""
import re
from configuration_trace import extract
from native_trace import RULES
from window_storage_trace import decode as decode_storage
from window_trace import validate

PREFIX=b'FES_NATIVE_FPGA_WINDOW_RELOAD '
H=r'[0-9a-f]{1,16}'
D=r'[0-9]{1,20}'
ID=r'[0-9a-f]{64}'
BEGIN=re.compile(rf'FES_NATIVE_FPGA_WINDOW_RELOAD BEGIN package_id=(?P<package_id>{ID}) owner=(?P<owner>{H}) returned_generation=(?P<returned_generation>{D}) source_generation=(?P<source_generation>{D}) cycles=2 memory_retained=1')
PASS=re.compile(rf'FES_NATIVE_FPGA_WINDOW_RELOAD PASS package_id=(?P<package_id>{ID}) owner=(?P<owner>{H}) source_generation=(?P<source_generation>{D}) closed_generation=(?P<closed_generation>{D}) cycles=2 frames=2 data_closed=1 sd_contained=1 memory_retained=1 runtime_admission=unavailable')
CONTEXT=re.compile(rf'FES_NATIVE_HPS_CONTEXT PASS sha256=(?P<sha256>{ID}) package_id=(?P<package_id>{ID}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) runtime_programming=unavailable')


def values(match):
    return {k:v if k in ('package_id','build_id','sha256') else int(v,16 if k=='owner' else 10) for k,v in match.groupdict().items()}


def decode(data,build_id,owner_seed,*,window_reload=False,**options):
    if window_reload is not True:
        raise ValueError('window reload requires explicit checked selection')
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    too_large=len(raw)>1<<20
    rows=[] if too_large else raw.splitlines(keepends=True)
    split=next((i for i,row in enumerate(rows) if row.startswith(PREFIX)),len(rows))
    report=decode_storage(b''.join(rows[:split]),build_id,owner_seed,**options)
    first_complete=report['complete'] and report['last_checkpoint']=='window_storage_contained' and not report['errors']
    report['complete']=False
    observed=dict(complete=False,errors=[],memory_released=False,runtime_admission=False)
    report['window_reload']=observed
    report['classification']='serial-observed two stored native window loads and independent owned returns; ordinary admission unavailable'
    def reject(reason):observed['errors'].append(reason);report['errors'].append(reason)
    if too_large:reject('window reload capture exceeds byte bound');return report
    if not first_complete or split==len(rows):return report
    if not rows[split].endswith(b'\n'):return report
    match=BEGIN.fullmatch(rows[split].decode(errors='replace').rstrip('\r\n'))
    if not match:reject('missing or malformed window reload begin');return report
    prior=report['window_storage_return']['receipt']
    owner,generation=prior['owner'],prior['contained_generation']
    if values(match)!=dict(package_id=prior['package_id'],owner=owner,returned_generation=prior['closed_generation'],source_generation=generation):
        reject('window reload changed its original store return or source');return report
    report['last_checkpoint']='window_reload_begin'
    body=b''.join(rows[split+1:])
    parsed=extract(body,options['configuration'],generation=generation,previous=report['window_read']['receipt']['source_generation'],
                   rearm=True,require_activation=True,expected_owner=owner)
    if parsed['errors']:
        for reason in parsed['errors']:reject(reason)
        return report
    parts=parsed['parts'];contexts=[];final=[];query=None
    for i,part in enumerate(parts[:-1]):
        line=part.decode(errors='replace').rstrip('\r')
        m=CONTEXT.fullmatch(line)
        if m:contexts.append((i+1,values(m)));parts[i]=b''
        if re.fullmatch(dict(RULES)['fpga_query'],line):query=i+1
        if line.startswith('FES_NATIVE_FPGA_WINDOW_RELOAD '):
            m=PASS.fullmatch(line)
            if not m:reject('malformed or replayed window reload event');return report
            final.append((i+1,values(m)));parts[i]=b''
    complete_rows=[r for r in body.split(b'\n')[:-1] if r.strip()]
    if not contexts:
        if complete_rows:reject('window reload lacks its unchanged HPS source context')
        return report
    expected=dict(sha256=options['fpga_identity']['sha256'],package_id=prior['package_id'],
                  build_id=options['fpga_identity']['build_id'],owner=owner,generation=generation)
    if len(contexts)!=1 or contexts[0][1]!=expected:
        reject('window reload changed or repeated its closed HPS source');return report
    if parsed['records'] and contexts[0][0]>=parsed['records'][0]['line']:
        reject('window configuration preceded its source context');return report
    if len(final)>1:reject('window reload repeated its final receipt');return report
    if final and final[0][1]!=dict(package_id=prior['package_id'],owner=owner,source_generation=generation,closed_generation=generation+3):
        reject('window reload final receipt changed package or closure');return report
    if query is not None and (not parsed['programmed'] or parsed['programmed']['line']>=query or any(r['line']>=query for r in parsed['records'])):
        reject('window readiness preceded its complete configuration');return report
    window=validate(b'\n'.join(parts),owner=owner,source_generation=generation,identity=options['fpga_identity'],
        memory=options['reader_memory'],contract=options['window_read'],publication=report['window_memory_observation'],
        rules=dict(RULES),present=True,frames_count=2,prepare=True)
    observed.update(configuration={k:v for k,v in parsed.items() if k!='parts'},window=window)
    for reason in window['errors']:reject(reason)
    if parsed['timing'] is not None:observed['configuration_timing']=parsed['timing']
    if parsed['complete'] and (parsed['timing'] is None or not 0<=parsed['timing']['interval_ticks']<20*400000000):
        reject('window reload lacks bounded current configuration timing')
    transfer_complete=parsed['complete'] and parsed['programmed'] is not None and parsed['activation'] is not None
    if final and not (transfer_complete and window['complete']):reject('window reload receipt preceded its complete new configuration and return')
    # Removing the terminal receipt must leave only blank rows after the window.
    # A receipt inserted before the last window event cannot qualify it.
    if final:
        last_window=max((i+1 for i,p in enumerate(parts[:-1]) if p.strip() and p.strip()!=b'NOTICE:'),default=0)
        if final[0][0]<=last_window:reject('window reload final receipt preceded the new window events')
    observed['complete']=bool(transfer_complete and window['complete'] and final and raw.endswith(b'\n') and not report['errors'])
    if observed['complete']:
        observed['receipt']=final[0][1]
        report['complete']=True;report['last_checkpoint']='window_reload_closed'
    return report
