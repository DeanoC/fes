"""Strict native Tenfoot serial observations; no I/O or hardware authority."""
import copy
import importlib.util
from pathlib import Path
import json
import re
import threading
import time

from native_trace import decode_native_boot
from shared_input_trace import SharedInput, PREFIX as USB_PREFIX, record as usb_record
from service_trace import _unique, closed, number, opening, require

PREFIX=b'FES_NATIVE_TENFOOT_STATE '
FETCH=b'FES_NATIVE_CORE_FETCH '
BEGIN=b'FES_NATIVE_TENFOOT_BEGIN display=serial-state-only input=uart preferences=ram'
EXIT=re.compile(rb'FES_NATIVE_TENFOOT_EXIT state=ready generation=([0-9]+)')
IDENTITY={'PackageID','CoreID','ABI','BuildID','Platform','Device','ProgrammingProfile','ActivationReceiptSHA256','ProgrammedSHA256','Major','Minor'}
ZERO={k:0 if k in ('Major','Minor') else '' for k in IDENTITY}
MAX_BYTES=1<<20


def fetch_observation(raw):
    def invalid(_):raise ValueError('nonfinite fetch JSON')
    r=json.loads(raw,object_pairs_hook=_unique,parse_constant=invalid)
    require(type(r) is dict and set(r)=={'scope','owner','generation','package_id','build_id','before','held','running','delta','polls','elapsed_ns','limit_ns'},'fetch observation fields differ')
    require(r['scope']=='nano-computer-fetch-v1','fetch observation scope differs')
    require(number(r['owner'],(1<<64)-1) and r['owner']>0 and number(r['generation'],(1<<64)-1) and r['generation']%2==1,'fetch owner/generation differs')
    require(type(r['package_id']) is str and re.fullmatch('[0-9a-f]{64}',r['package_id']) and type(r['build_id']) is str and re.fullmatch('[0-9a-f]{32}',r['build_id']),'fetch identity differs')
    for key in ('before','held','running'):
        a=r[key];require(type(a) is dict and set(a)=={'Cycles','Address'} and number(a['Cycles'],(1<<32)-1) and number(a['Address'],65535),'invalid fetch counter/address pair')
    require(r['before']==r['held'],'fetch counter/address changed under Hold')
    delta=(r['running']['Cycles']-r['held']['Cycles'])&0xffffffff
    require(number(r['delta'],0x7fffffff) and 0<delta==r['delta']<=0x7fffffff,'fetch counter did not advance or reset backwards')
    require(number(r['polls'],64) and r['polls']>0 and number(r['limit_ns'],1000000000) and number(r['elapsed_ns'],1000000000) and r['elapsed_ns']<r['limit_ns'],'fetch deadline/poll bound differs')
    return r


def packet(data,*,startup=False,input_mode='uart',usb_records=None,fetch_records=None):
    """Separate complete App observations from unchanged firmware evidence."""
    require(isinstance(data,(bytes,bytearray)) and 0<len(data)<=MAX_BYTES and data.endswith(b'\n'),'bounded complete App capture required')
    require(input_mode in ('uart','uart+shared-usb-gamepad','uart+shared-usb-boot-keyboard'),'explicit App input scope required')
    begin=BEGIN if input_mode=='uart' else b'FES_NATIVE_TENFOOT_BEGIN display=serial-state-only input='+input_mode.encode()+b' preferences=ram'
    states=[];evidence=[];exited=None;begins=0;usb_ready=False
    def constant(_):raise ValueError('nonfinite App JSON')
    lines=bytes(data).splitlines(keepends=True)
    for index,line in enumerate(lines):
        raw=line.rstrip(b'\r\n')
        if raw.startswith(FETCH):
            require(not startup and exited is None and fetch_records is not None,'fetch observation outside a live launch')
            r=fetch_observation(raw[len(FETCH):])
            if fetch_records is not None:fetch_records.append(r)
            continue
        if raw.startswith(USB_PREFIX):
            require(input_mode!='uart','shared USB observation in UART-only App')
            row=usb_record(raw,index+1)
            if startup:
                require(row['stage']=='ready' and not usb_ready and begins==0,'unexpected shared USB startup transition')
                require(evidence and evidence[-1].rstrip(b'\r\n').startswith(b'FES_NATIVE_HPS_IO PASS '),'shared Ready precedes sealed pin proof')
                usb_ready=True
            else:require(row['stage']!='ready','shared USB reinitialized during play')
            if usb_records is not None:usb_records.append(row)
            continue
        if raw==begin:
            require(input_mode=='uart' or usb_ready,'App began before shared USB Ready')
            begins+=1;require(startup and begins==1 and not states,'unexpected App begin');continue
        if raw.startswith(PREFIX):
            r=json.loads(raw[len(PREFIX):].decode('utf-8'),object_pairs_hook=_unique,parse_constant=constant)
            fields={'scope','display','titles','focus','loading','load_error','session','launch','launch_error','retry_stop','native'}
            require(type(r) is dict and set(r)==fields,'App fields differ')
            require(r['scope']=='nano-tenfoot-serial-v1' and r['display']=='serial-state-only','App scope/display differs')
            require(type(r['titles']) is list and len(r['titles'])<=32 and all(type(t) is str and 0<len(t)<=256 for t in r['titles']),'invalid App titles')
            require(number(r['focus'],31) and (not r['titles'] or r['focus']<len(r['titles'])),'invalid App focus')
            require(type(r['loading']) is type(r['retry_stop']) is bool,'invalid App flags')
            require(r['load_error']==r['launch_error']=='' and not r['retry_stop'],'App reported a failure')
            require(r['session'] in ('','idle','launching','active','stopping') and r['launch'] in ('idle','launching','ok'),'unknown/failed App phase')
            v=r['native'];require(type(v) is dict and set(v)=={'State','Owner','Generation','Identity','Held','Error'},'native status fields differ')
            require(v['State'] in ('ready','loading','running','returning') and v['Error']=='','native service failed or retired')
            require(number(v['Owner'],(1<<64)-1) and v['Owner']>0 and number(v['Generation'],(1<<64)-1) and type(v['Held']) is bool,'invalid native ownership')
            ident=v['Identity'];require(type(ident) is dict and set(ident)==IDENTITY,'native identity fields differ')
            require(all(type(ident[k]) is str and len(ident[k])<=256 for k in IDENTITY-{'Major','Minor'}) and all(number(ident[k],65535) for k in ('Major','Minor')),'invalid native identity values')
            if v['State'] in ('ready','loading'):
                require(v['Generation']%2==0 and v['Held'] and ident==ZERO,'inactive App has live identity or lost Hold')
            else:
                require(v['Generation']%2==1 and not v['Held'],'active App generation/Hold differs')
                for key,length in (('PackageID',64),('BuildID',32),('ProgrammedSHA256',64)):
                    require(re.fullmatch('[0-9a-f]{'+str(length)+'}',ident[key]) and int(ident[key],16),'invalid live '+key)
            states.append(r);continue
        match=EXIT.fullmatch(raw)
        if match:
            require(not startup and index==len(lines)-1 and exited is None,'nonterminal/duplicate App exit')
            exited=int(match[1]);require(exited<1<<64,'App exit generation overflow');continue
        text=raw.decode('utf-8')
        require(not text.startswith(('panic:','fatal error:')) and not re.search(r'\b(?:FAIL|FAILURE|REJECT|EXCEPTION)\b',text),'native failure before App observation')
        require('FES_NATIVE_CORE_GAMEPAD' not in text and 'FES_NATIVE_CORE_FETCH' not in text and 'FES_NATIVE_SHARED_USB' not in text and 'FES_NATIVE_TENFOOT' not in text and 'FES_NATIVE_PROGRAM_REPLY' not in text and 'FES_NATIVE_WINDOW_REPLY' not in text,'foreign or malformed App observation')
        evidence.append(line)
    require(states or exited is not None,'App observation absent')
    require(begins==int(startup),'App startup marker differs')
    require(lines[-1].rstrip(b'\r\n').startswith(PREFIX) or exited is not None,'unterminated App evidence')
    return states,b''.join(evidence),exited


def startup_checkpoints(data):
    """Require all checkpoints emitted before the selected App is Ready.

    The selected-service main branches before the fixed-load diagnostic's
    retain_begin/retain_end. CPU mappings are not decoder phase arguments.
    """
    spec=importlib.util.spec_from_file_location('fes_tenfoot_startup',Path(__file__).resolve().parents[1]/'entry/startup.py')
    decoder=importlib.util.module_from_spec(spec);spec.loader.exec_module(decoder)
    result=decoder.decode(data,through='fat_mount_end')
    require(result['complete'] and not result['errors'],'selected App CPU startup checkpoints rejected')
    return result


def quiet(data):return all(row.strip() in (b'',b'NOTICE:') for row in data.splitlines())


class TenfootClient:
    """Single-use key exchanges with injectable ownership and deadline hooks.

    Exchange sends once and gathers through a complete matching App observation.
    Its predicate is framing only; this class verifies every state and all C
    evidence afterwards. Any uncertainty permanently retires the client.
    """
    def __init__(self,initial,*,build_id,owner_seed,atf,package,programs,exchange,owns,now=time.monotonic,record=lambda _:None,shared_usb=None):
        require(all(callable(f) for f in (exchange,owns,now,record)),'owned exchange and clock hooks required')
        require(type(programs) is list and len(programs)==2 and len({p['title'] for p in programs})==2 and len({p['identity']['PackageID'] for p in programs})==2,'two distinct titled program choices required')
        self.programs=copy.deepcopy(programs)
        self.usb=SharedInput(shared_usb) if shared_usb is not None else None
        usb_rows=[]
        states,evidence,exited=self._packet(initial,startup=True,usb_records=usb_rows)
        options={} if self.usb is None else dict(usb_control=True,usb_shared=True,hps_context=self.usb.policy['hps_context'],hps_io=self.usb.policy['hps_io'])
        boot=decode_native_boot(evidence,build_id,owner_seed,package=package,atf=atf,**options)
        require(not boot['errors'] and boot['loader_ready'] and boot['last_checkpoint']==('el1' if self.usb is None else 'hps_io_pass'),'matched App boot rejected')
        seal=next(r['values'] for r in boot['records'] if r['stage']=='spl_handoff')
        self.owner=seal['owner'];self.view=states[-1];self.boot=boot
        if self.usb is not None:self.usb.startup(usb_rows,self.owner,boot)
        self.exchange,self.owns,self.now,self.record=exchange,owns,now,record
        self.active=threading.Lock();self.retired=False;self.loads=0;self.previous_configuration=0;self.last_counter=0;self.records=[]
        require(self.owns(),'App console claim absent')
        for state in states:self._state(state,0,('ready',))
        require(self._ready(self.view),'initial App is not ready')

    def _packet(self,data,*,startup=False,usb_records=None,fetch_records=None):
        return packet(data,startup=startup,input_mode='uart' if self.usb is None else self.usb.input_mode,usb_records=usb_records,fetch_records=fetch_records)

    def _ready(self,r):return r['native']['State']=='ready' and r['session']=='idle' and not r['loading'] and len(r['titles'])==2
    def _state(self,r,generation,allowed,identity=None):
        v=r['native'];require(v['Owner']==self.owner and v['Generation']==generation and v['State'] in allowed,'App ownership/generation/state changed')
        require(not r['titles'] or len(r['titles'])==2 and set(r['titles'])=={p['title'] for p in self.programs},'App catalog changed')
        if identity is not None and v['State'] in ('running','returning'):require(v['Identity']==identity,'App published wrong retained identity')

    def key(self,op,selection=None):
        require(not self.retired,'App client retired')
        v=self.view['native'];generation=v['Generation'];identity=v['Identity']
        if op=='launch':
            require(self._ready(self.view) and type(selection) is int and 0<=selection<2,'ready App and selected program required')
            selected=self.programs[selection]
            require(self.view['titles'][self.view['focus']]==selected['title'],'App focus does not select requested identity')
            payload=b'\r';bound=100
            terminal=lambda r,ex:r is not None and r['native']['State']=='running' and r['session']=='active'
        elif op=='focus':
            require(self._ready(self.view) and type(selection) is int and 0<=selection<2,'focus requires ready App and a choice')
            target=self.view['titles'].index(self.programs[selection]['title']);delta=target-self.view['focus']
            require(abs(delta)==1,'one real focus change required')
            payload=b'\x1b[C' if delta>0 else b'\x1b[D';bound=5
            terminal=lambda r,ex:r is not None and self._ready(r) and r['focus']==target
        elif op in ('stop','quit'):
            require(v['State']=='running' and self.view['session']=='active','Stop/Quit requires a live App session')
            payload=b's' if op=='stop' else b'q';bound=5
            terminal=lambda r,ex:ex is not None if op=='quit' else r is not None and self._ready(r)
        else:raise ValueError('unsupported App action')
        require(self.active.acquire(blocking=False),'App key exchange already active')
        row={'action':op,'selection':selection,'payload_hex':payload.hex(),'bound_seconds':bound,'status':'issued'}
        try:
            require(self.owns(),'App console claim lost')
            self.records.append(row);self.record(copy.deepcopy(row));before=self.now()
            def done(raw):
                if not raw.endswith(b'\n'):return False
                last=raw.splitlines()[-1].rstrip(b'\r')
                if not last.startswith(PREFIX) and not EXIT.fullmatch(last):return False
                states,_,exited=self._packet(raw,fetch_records=[])
                for state in states:
                    require(state['native']['Owner']==self.owner,'App owner changed while waiting')
                    require(state['native']['Generation'] in (generation,generation+1),'App generation changed while waiting')
                return terminal(states[-1] if states else None,exited)
            raw=self.exchange(payload,bound,done);after=self.now()
            require(before<=after<before+bound and self.owns(),'App deadline/clock/claim failed')
            usb_rows=[]
            fetch_rows=[]
            states,evidence,exited=self._packet(raw,usb_records=usb_rows,fetch_records=fetch_rows)
            require(terminal(states[-1] if states else None,exited),'App action did not reach its terminal observation')
            rank=-1
            for state in states:
                n=state['native'];g=n['Generation']
                current_rank=({'ready':0,'loading':1,'running':2} if op=='launch' else {'running':0,'returning':1,'ready':2} if op in ('stop','quit') else {'ready':0})[n['State']]
                require(current_rank>=rank,'App native state regressed');rank=current_rank
                if op=='focus':self._state(state,generation,('ready',))
                elif op=='launch':
                    require(g in (generation,generation+1),'launch skipped App generation')
                    self._state(state,g,('ready','loading') if g==generation else ('running',),selected['identity'])
                else:
                    require(g in (generation,generation+1),'return skipped App generation')
                    self._state(state,g,('running','returning') if g==generation else ('ready',),identity)
            row['elapsed_seconds']=after-before
            if op in ('launch','stop','quit'):
                terminal_native='running' if op=='launch' else 'ready'
                reached=False
                for line in raw.splitlines():
                    if line.startswith(PREFIX):
                        n=json.loads(line[len(PREFIX):])['native']
                        reached=reached or (n['Generation']==generation+1 and n['State']==terminal_native)
                    elif reached:require(quiet(line) or EXIT.fullmatch(line) or self.usb is not None and line.startswith(USB_PREFIX),'firmware evidence followed published terminal ownership')
            if op=='launch':
                require(len(fetch_rows)==1,'launch requires one held/run fetch observation')
                fetch=fetch_rows[0]
                require(fetch['owner']==self.owner and fetch['generation']==generation+1 and fetch['package_id']==selected['identity']['PackageID'] and fetch['build_id']==selected['identity']['BuildID'],'fetch observation belongs to another launch')
                complete=raw.find(b'FES_NANO_CORE_TRANSFER phase=complete ')
                require(complete>=0 and raw.find(FETCH)>complete,'fetch observation precedes FPGA configuration completion')
                row['fetch']=fetch
                require(exited is None and states[-1]['native']['Generation']==generation+1,'launch exit/generation differs')
                source=4+5*self.loads
                row['opening']=opening(evidence,selected['configuration'],self.owner,source,self.previous_configuration)
                records=row['opening']['configuration_records']
                engines=[r['values'] for r in records if r['stage']=='configuration_engine']
                phases={r['values']['phase'] for r in records if r['stage']=='configuration_mailbox'}
                require(len(engines)==1 and engines[0]['credits']==4 and engines[0]['block_bytes']==0xffff0 and phases=={1,2,3},'configuration queue contract differs')
                require(b'FES_NATIVE_FPGA_PROGRAMMED' not in evidence,'legacy programmed report in App service')
                timing=row['opening']['configuration_timing'];require(timing['first_counter']>self.last_counter,'configuration counter restarted')
                self.last_counter=timing['last_counter'];self.previous_configuration=source;self.loads+=1
            elif op=='focus':require(not fetch_rows and exited is None and quiet(evidence),'focus performed firmware I/O or exited')
            else:
                require(not fetch_rows,'fetch observation during return')
                row['master_stop']=closed(evidence,self.owner,self.previous_configuration)
                require((exited==generation+1) if op=='quit' else exited is None and states[-1]['native']['Generation']==generation+1,'owned return/exit generation differs')
            if self.usb is not None:row['shared_usb']=self.usb.action(op,generation,usb_rows,self.owner,raw,PREFIX)
            row.update(status='verified',states=states,exit_generation=exited,raw_bytes=len(raw));self.record(copy.deepcopy(row))
            if op=='quit':self.retired=True
            else:self.view=copy.deepcopy(states[-1])
            return copy.deepcopy(row)
        except BaseException as error:
            self.retired=True;row.update(status='failed',error=str(error));self.record(copy.deepcopy(row));raise
        finally:self.active.release()


def exercise(client,settle=lambda _:None,*,during_play=None):
    require(callable(settle),'bounded settle hook required')
    require(during_play is None or callable(during_play),'explicit during-play observer required')
    cycles=[]
    for i,selection in enumerate((0,1,0)):
        title=client.programs[selection]['title']
        if client.view['titles'][client.view['focus']]!=title:client.key('focus',selection)
        if client.usb is not None:settle(0.5)
        launch=client.key('launch',selection)
        if during_play is not None:during_play(i,client)
        if client.usb is not None:settle(0.5)
        returned=client.key('quit' if i==2 else 'stop')
        cycles.append({'selection':selection,'launch_generation':launch['states'][-1]['native']['Generation'],'return_generation':returned['exit_generation'] if i==2 else returned['states'][-1]['native']['Generation']})
    require(client.loads==3 and client.retired and cycles[-1]['return_generation']==6,'App cycle incomplete')
    return dict(passed=True,cycles=cycles,fetch_activity_observed=True,shared_usb_reports=None if client.usb is None else client.usb.reports,input_controls_exercised=False,hardware_acceptance=False,ordinary_admission=False,display='serial-state-only')
