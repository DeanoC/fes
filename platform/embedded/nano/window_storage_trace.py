"""Require owned frame return plus fresh SD selection/containment; no admission."""
import re
from native_trace import decode_native_configuration

PREFIX=b'FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN '
HEX=r'[0-9a-f]{1,16}'
DEC=r'[0-9]{1,20}'
BEGIN=re.compile(rf'FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN BEGIN owner=(?P<owner>{HEX}) closed_generation=(?P<closed_generation>{DEC})')
PASS=re.compile(rf'FES_NATIVE_FPGA_WINDOW_STORAGE_RETURN PASS package_id=(?P<package_id>[0-9a-f]{{64}}) owner=(?P<owner>{HEX}) closed_generation=(?P<closed_generation>{DEC}) resumed_generation=(?P<resumed_generation>{DEC}) contained_generation=(?P<contained_generation>{DEC}) source=sd fresh_selection=1 data_closed=1 memory_retained=1 runtime_admission=unavailable')


def decode(data,build_id,owner_seed,**options):
    if options.get('window_present') is not True or options.get('from_storage') is not True:
        raise ValueError('window storage return requires the explicitly selected stored presentation path')
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    too_large=len(raw)>1<<20
    rows=[] if too_large else raw.splitlines(keepends=True)
    split=next((i for i,line in enumerate(rows) if line.startswith(PREFIX)),len(rows))
    report=decode_native_configuration(b''.join(rows[:split]),build_id,owner_seed,**options)
    observed=dict(complete=False,errors=[],records=[],memory_released=False,runtime_admission=False)
    report['window_storage_return']=observed
    prefix_complete=report['complete'] and report['last_checkpoint']=='window_present_closed' and not report['errors']
    report['complete']=False
    report['classification']='serial-observed native frames, owned return and fresh SD selection/containment; no normal admission'
    if too_large:
        report['errors'].append('window storage capture exceeds byte bound')
        return report
    if not prefix_complete:return report
    expected=report['window_read']['receipt']
    complete_rows=[(split+i+1,line.decode(errors='replace').rstrip('\r\n')) for i,line in enumerate(rows[split:]) if line.endswith(b'\n') and line.strip()]
    for index,(number,line) in enumerate(complete_rows):
        pattern=BEGIN if index==0 else PASS if index==1 else None
        match=pattern.fullmatch(line) if pattern else None
        if not match:
            observed['errors'].append('missing, reordered, malformed or repeated window SD event at line '+str(number))
            break
        values={k:v if k=='package_id' else int(v,16 if k=='owner' else 10) for k,v in match.groupdict().items()}
        want=dict(owner=expected['owner'],closed_generation=expected['closed_generation'])
        if index==1:
            want.update(package_id=expected['package_id'],resumed_generation=expected['closed_generation']+2,
                        contained_generation=expected['closed_generation']+3)
        if values!=want or expected['closed_generation']>(1<<64)-4:
            observed['errors'].append('window SD event changed package, owner or return generations')
            break
        observed['records'].append(dict(stage='window_storage_begin' if index==0 else 'window_storage_contained',line=number,values=values))
    report['errors'].extend(observed['errors'])
    if observed['records']:
        report['last_checkpoint']=observed['records'][-1]['stage']
    observed['complete']=len(observed['records'])==2 and not report['errors'] and raw.endswith(b'\n')
    if observed['complete']:
        observed['receipt']=observed['records'][-1]['values']
        report['complete']=True
        report['records'].extend(observed['records'])
    return report
