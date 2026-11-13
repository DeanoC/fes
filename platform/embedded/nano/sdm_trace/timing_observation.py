"""Observe existing clock samples; timing supplies no retirement authority."""
import importlib.util,re
from pathlib import Path
_s=importlib.util.spec_from_file_location('timing_bound_retirement',Path(__file__).with_name('retire_observation.py'))
_retire=importlib.util.module_from_spec(_s);_s.loader.exec_module(_retire)
FIELDS='build owner epoch generation bytes sha384 phase timed mask start_us last_us observed_us guards samples owners sources validated reads writes polls first_us latest_us retained reuse activated'.split()
U32=set('bytes phase timed guards samples owners sources validated reads writes polls retained reuse activated'.split())
def require(ok,message):
    if not ok:raise ValueError(message)
def parse_timing(line):
    """Typed record only. Parsing alone establishes no source or boot proof."""
    require(type(line) is bytes and len(line)<=2048 and b'\0' not in line,'bounded timing line required')
    line=re.sub(rb'^NOTICE:[ \t]+',b'',line.rstrip(b'\r\n'))
    require(line.startswith(b'FES_NANO_RETIRE_TIMING '),'timing prefix required')
    pairs=[p.split('=',1) for p in line.decode('ascii').split()[1:]]
    require(all(len(p)==2 for p in pairs) and [p[0] for p in pairs]==FIELDS,'complete ordered timing fields required')
    r={}
    for key,value in pairs:
        if key in ('build','sha384'):
            require(re.fullmatch('[0-9a-f]{'+str(16 if key=='build' else 96)+'}',value) is not None,'bounded timing identity required');r[key]=value
        elif key in ('first_us','latest_us'):
            parts=value.split(',');require(len(parts)==7 and all(re.fullmatch('[0-9a-f]{1,16}',p) for p in parts),'seven bounded phase samples required')
            r[key]=[int(p,16) for p in parts]
        else:
            hexed=key in ('owner','epoch','mask')
            require(re.fullmatch('[0-9a-f]{1,16}' if hexed else '[0-9]{1,20}',value) is not None,'bounded timing integer required')
            r[key]=int(value,16 if hexed else 10)
            require(r[key]<(1<<(32 if key in U32 else 64)),'timing integer overflow')
    return r
def timing_errors(t,source,retirement,*,retirement_verified):
    errors=[]
    def check(ok,message):
        if not ok:errors.append(message)
    for k in ('build','owner','epoch','generation','bytes','sha384'):
        check(t[k]==source[k],'timing/source '+k+' differs')
    check(t['phase']==retirement['phase'] and t['writes']==retirement['writes'],'timing/result phase or writes differs')
    check(t['phase']<=6 and t['timed'] in (0,1),'timing phase/state invalid')
    mask=t['mask'];phase=min(t['phase'],6)
    check(mask in ((1<<phase)-1,(1<<(phase+1))-1),'timing phases not ordered')
    check(t['guards']>=t['samples']==t['owners'] and t['guards']-t['samples']<=1,'timing guard/sample counts invalid')
    check(t['validated']<=t['sources']<=5,'timing source counts invalid')
    check((t['retained'],t['reuse'],t['activated'])==(1,0,0),'timing authority fields invalid')
    seen=[]
    for i,(first,last) in enumerate(zip(t['first_us'],t['latest_us'])):
        if mask&(1<<i):
            check(first<=last,'phase sample clock reversed');seen.append(i)
        else:check(first==last==0,'unseen phase has samples')
    check(t['samples']>=len(seen) and bool(t['samples'])==bool(seen),'sample count/mask invalid')
    for a,b in zip(seen,seen[1:]):check(t['latest_us'][a]<=t['first_us'][b],'phase clock order reversed')
    check(t['observed_us']==(t['latest_us'][seen[-1]] if seen else 0),'latest observation differs')
    if t['timed']:
        check(bool(mask&1) and t['start_us']==t['first_us'][0] and t['start_us']<=t['last_us']<=t['observed_us'],'accepted clock interval invalid')
    else:check(t['start_us']==t['last_us']==0,'unstarted clock has accepted samples')
    if retirement_verified:
        check((t['phase'],t['timed'],mask)==(6,1,127),'retirement lacks complete timing phases')
        check(t['guards']==t['samples']==t['owners'] and t['sources']==t['validated']==5,'successful timing counts incomplete')
        check(t['last_us']==t['observed_us'] and 0<t['observed_us']-t['start_us']<1000000,'successful retirement clock invalid')
        check(retirement['elapsed_us']==t['last_us']-t['start_us'],'timing/retirement elapsed differs')
        check(t['writes']==5 and t['reads']>0 and t['polls']>=4,'successful timing operations incomplete')
    return errors
def decode(raw,*,boot,build,configuration,_terminal=None):
    require(type(raw) is bytes and 0<len(raw)<=1<<20 and raw.endswith(b'\n') and b'\0' not in raw,'complete bounded timing capture required')
    parts=raw.splitlines(keepends=True);lines=[re.sub(rb'^NOTICE:[ \t]+',b'',v.rstrip(b'\r\n')) for v in parts]
    rows=[i for i,v in enumerate(lines) if b'FES_NANO_RETIRE_TIMING' in v]
    require(len(rows)==1,'one timing record required');at=rows[0]
    require(at>=1 and at==len(parts)-2 and lines[at-1].startswith(b'FES_NANO_RETIRE_RESULT '),'timing must follow retirement before final Park')
    t=parse_timing(lines[at]);joined=_retire.decode(b''.join(parts[:at]+parts[at+1:]),boot=boot,build=build,configuration=configuration,_terminal=_terminal)
    extra=timing_errors(t,joined['input_observation']['result'],joined['result'],retirement_verified=joined['retirement_verified'])
    valid=joined['source_flow_verified'] and not extra
    return dict(passed=joined['passed'] and valid,source_flow_verified=joined['source_flow_verified'],
        retirement_verified=joined['retirement_verified'] and valid,timing_validated=valid,
        retirement_observation=joined,timing=t,errors=joined['errors']+extra,
        recorded_DATA_requests=joined['recorded_DATA_requests'],recorded_DATA_bytes=joined['recorded_DATA_bytes'],
        ordinary_runtime_activation=False,buffer_reuse_authority=False,DMA_authority=False)
