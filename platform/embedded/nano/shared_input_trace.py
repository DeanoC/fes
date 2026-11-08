"""Reported shared USB observations; no device control or admission authority."""
import copy
import hashlib
import json
import re
from service_trace import number,require

PREFIX=b'FES_NATIVE_SHARED_USB '
RULES={
 'ready':re.compile(rb'FES_NATIVE_SHARED_USB READY input=(?P<input>boot-keyboard|gamepad) owner=(?P<owner>[0-9a-f]{1,16}) generation=(?P<generation>[0-9]{1,20}) descriptor_sha256=(?P<descriptor_sha256>[0-9a-f]{64}) descriptor_bytes=(?P<descriptor_bytes>[0-9]{1,4})'),
 'input':re.compile(rb'FES_NATIVE_SHARED_USB INPUT phase=(?P<phase>pause|resume) state=(?P<state>ready|running) native_generation=(?P<native_generation>[0-9]{1,20}) owner=(?P<owner>[0-9a-f]{1,16}) generation=(?P<generation>[0-9]{1,20}) reports=(?P<reports>[0-9]{1,20}) polls=(?P<polls>[0-9]{1,20})'),
 'stop':re.compile(rb'FES_NATIVE_SHARED_USB STOP owner=(?P<owner>[0-9a-f]{1,16}) generation=(?P<generation>[0-9]{1,20}) reports=(?P<reports>[0-9]{1,20}) polls=(?P<polls>[0-9]{1,20})'),
}

def record(raw,line):
    for stage,pattern in RULES.items():
        match=pattern.fullmatch(raw)
        if match:
            values={k:(v.decode() if k in ('input','phase','state','descriptor_sha256') else int(v,16 if k=='owner' else 10)) for k,v in match.groupdict().items()}
            for k,v in values.items():
                if type(v) is int:require(number(v,(1<<64)-1),'shared USB integer overflow')
            require(values['owner']>0,'shared USB owner absent')
            return {'stage':stage,'line':line,'values':values}
    raise ValueError('malformed shared USB observation')

class SharedInput:
    def __init__(self,policy):
        require(type(policy) is dict and set(policy)=={'input','descriptor_sha256','descriptor_bytes','hps_context','hps_io'},'checked shared input/source policy required')
        self.policy=copy.deepcopy(policy)
        require(policy['input'] in ('boot-keyboard','gamepad'),'unknown shared input protocol')
        digest=policy['descriptor_sha256'];size=policy['descriptor_bytes']
        require(type(digest) is str and re.fullmatch('[0-9a-f]{64}',digest) and number(size,4096),'invalid expected USB descriptor identity')
        require(size>0 if policy['input']=='gamepad' else size==0 and digest==hashlib.sha256(b'').hexdigest(),'descriptor identity differs from selected input protocol')
        self.input_mode='uart+shared-usb-'+policy['input'];self.reports=0;self.polls=0;self.started=False

    def startup(self,rows,owner,boot):
        require(len(rows)==1 and rows[0]['stage']=='ready','shared startup needs exactly one Ready observation')
        v=rows[0]['values'];p=self.policy
        require(v['owner']==owner and v['generation']==2 and all(v[k]==p[k] for k in ('input','descriptor_sha256','descriptor_bytes')),'shared USB readiness identity/lease differs')
        require(boot['records'][-1]['stage']=='hps_io_pass' and sum(r['stage'].startswith('hps_io_row_') for r in boot['records'])==328,'shared startup lacks complete sealed pin observation')
        self.started=True

    def action(self,op,generation,rows,owner,raw,app_prefix):
        require(self.started,'shared USB startup absent')
        if op=='focus':require(not rows,'focus changed shared USB ownership');return {'reports':self.reports,'polls':self.polls}
        require([r['stage'] for r in rows]==(['input','input','stop'] if op=='quit' else ['input','input']),'shared USB transition/stop observations differ')
        a,b=(r['values'] for r in rows[:2])
        require(a['phase']=='pause' and b['phase']=='resume','USB pause/resume order differs')
        initial='ready' if op=='launch' else 'running';final='running' if op=='launch' else 'ready'
        require(a['state']==initial and b['state']==final and a['native_generation']==generation and b['native_generation']==generation+1,'shared USB/native transition mismatch')
        require(all(v['owner']==owner and v['generation']==2 for v in (a,b)),'shared USB owner/generation changed during core transition')
        require(a['polls']>self.polls and b['polls']==a['polls'],'successful polling absent before transition or continued during pause')
        require(self.reports<=a['reports']<=a['polls'] and b['reports']==a['reports'],'report counter regressed, exceeded polls or changed during pause')
        # SET_IDLE(0) permits a keyboard to return no reports until a key
        # changes. Successful polling still has to advance at every boundary.
        if self.policy['input']=='gamepad':
            require(a['reports']>self.reports,'gamepad input absent before transition')
        # The cached App cannot publish the completed operation before Resume.
        for line,data in enumerate(raw.splitlines(),1):
            if not data.startswith(app_prefix):continue
            state=json.loads(data[len(app_prefix):]);v=state['native']
            if v['Generation']==generation+1 and v['State']==final and state['session']==('active' if op=='launch' else 'idle'):
                require(rows[1]['line']<line,'App completion preceded USB resume')
            if v['State'] in ('loading','returning'):
                require(rows[0]['line']<line,'native mutation preceded USB pause')
        delta=a['reports']-self.reports
        poll_delta=a['polls']-self.polls
        self.reports=b['reports']
        self.polls=b['polls']
        result={'before':a,'after':b,'reports_observed_since_previous_boundary':delta,'successful_polls_since_previous_boundary':poll_delta}
        if op=='quit':
            stop=rows[2]['values']
            require(stop['owner']==owner and stop['generation']==3 and self.reports<=stop['reports']<=stop['polls'] and stop['polls']>=self.polls,'shared USB stop receipt/counter differs')
            self.reports=stop['reports'];self.polls=stop['polls'];result['stop']=stop
        return result
