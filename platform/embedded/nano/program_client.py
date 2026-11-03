"""Bounded, single-owner client for the contained native UART service.

The platform owns the console and supplies one synchronous exchange callback.
No retries, device discovery, admission or hardware initialization happen here.
"""
import copy
import json
import threading
import time
from service_trace import startup, response, opening, closed, require


class ProgramClient:
    def __init__(self, initial, *, build_id, owner_seed, atf, package, programs,
                 exchange, owns, now=time.monotonic, record=lambda _:None):
        require(callable(exchange) and callable(owns) and callable(now) and callable(record),'owned exchange and monotonic clock required')
        require(isinstance(programs,list) and 0<len(programs)<=32,'bounded expected program catalogue required')
        self.programs=copy.deepcopy(programs)
        checked=startup(initial,build_id,owner_seed,atf,package,[p['identity'] for p in self.programs])
        self.boot=checked['boot'];self.view=checked['reply'];self.owner=self.view['owner']
        self.exchange,self.owns,self.now,self.record=exchange,owns,now,record
        self.active=threading.Lock();self.retired=False;self.loads=0;self.current=None
        self.configuration_generation=None;self.previous_configuration=0;self.last_counter=0;self.records=[]
        require(self.owns(),'native console claim unavailable')

    def _invalid(self,command):
        op=command.get('op');base={'op','owner','generation'}
        if op=='status':return set(command)!={'op'}
        if op not in ('launch','return','park','hold','run','keyboard','activity'):return True
        fields=base|({'program'} if op=='launch' else {'rows'} if op=='keyboard' else set())
        if set(command)!=fields or command.get('owner')!=self.owner or type(command.get('generation')) is not int or command['generation']!=self.view['generation']:return True
        if op=='launch':return self.view['state']!='ready' or type(command.get('program')) is not int or not 0<=command['program']<len(self.programs)
        if self.view['state']!='running':return True
        if op=='keyboard':return not isinstance(command.get('rows'),list) or len(command['rows'])!=9 or any(type(v) is not int or not 0<=v<=65535 for v in command['rows'])
        return False

    def call(self,command,*,reject=False):
        """Issue one command once; any uncertain exchange/proof retires this client."""
        require(type(reject) is bool and isinstance(command,dict),'explicit command and expected outcome required')
        require(not self.retired and self.view['state']!='parked','native service client is retired or parked')
        require(self._invalid(command)==reject,'expected outcome disagrees with local request/session validity')
        payload=json.dumps(command,separators=(',',':')).encode()+b'\n'
        require(len(payload)-1<=1024,'native command exceeds byte bound')
        require(self.active.acquire(blocking=False),'another native command is in flight')
        row=None
        try:
            require(self.owns(),'native console claim lost')
            bound=100 if command.get('op')=='launch' and not reject else 5
            before=self.now();row={'command':copy.deepcopy(command),'bound_seconds':bound,'status':'issued'}
            self.records.append(row);self.record(copy.deepcopy(row))
            raw=self.exchange(payload,bound)
            after=self.now();row['elapsed_seconds']=after-before
            require(before<=after<before+bound,'native command clock regressed or deadline exceeded')
            require(self.owns(),'native console claim changed during command')
            reply,evidence=response(raw)
            row['reply']=reply;row['raw_bytes']=len(raw)
            require(reply['owner']==self.owner and reply['op']==command.get('op','invalid'),'reply changed boot owner or operation')
            previous=self.view
            if reject:
                require(not reply['ok'] and not evidence.strip(),'rejected command had firmware side effects or was accepted')
                require(all(reply.get(k)==previous.get(k) for k in ('state','generation','held','package_id','build_id','core_id')),'rejected command changed session state')
                require('activity' not in reply,'rejected command supplied activity')
            else:
                require(reply['ok'],'native command rejected: '+reply.get('error',''))
                op=command['op'];expected_generation=previous['generation']+int(op in ('launch','return','park'))
                require(reply['generation']==expected_generation,'reply changed request generation')
                if op=='launch':
                    selected=self.programs[command['program']]
                    require(reply['state']=='running' and reply['held'] is False,'launch did not publish running')
                    require(all(reply.get(k)==v for k,v in selected['identity'].items()),'launched identity differs from retained choice')
                    generation=4+5*self.loads
                    row['opening']=opening(evidence,selected['configuration'],int(self.owner,16),generation,self.previous_configuration)
                    require(row['opening']['configuration_timing']['first_counter']>self.last_counter,'configuration counter restarted between sessions')
                    self.last_counter=row['opening']['configuration_timing']['last_counter']
                    self.current=selected['identity'];self.configuration_generation=generation
                    self.previous_configuration=generation;self.loads+=1
                elif op in ('return','park'):
                    require(reply['state']==('ready' if op=='return' else 'parked') and reply['held'] is True,'owned return did not reach expected state')
                    require(not any(k in reply for k in ('package_id','build_id','core_id')),'return retained a published core identity')
                    row['master_stop']=closed(evidence,int(self.owner,16),self.configuration_generation)
                    self.current=None
                else:
                    require(not evidence.strip(),'control/status command produced unexpected firmware observations')
                    require(reply['state']==previous['state'],'control/status command changed session state')
                    require(all(reply.get(k)==previous.get(k) for k in ('package_id','build_id','core_id')),'control/status changed program identity')
                    require(reply['held']==(op=='hold' if op in ('hold','run') else previous['held']),'control/status changed Hold state')
                if op=='status':
                    require(reply.get('programs')==[{'index':i,'package_id':p['identity']['package_id']} for i,p in enumerate(self.programs)],'catalogue changed after startup')
                require(('activity' in reply)==(op=='activity'),'activity reply shape differs from request')
                require(('programs' in reply)==(op=='status'),'unexpected catalogue in command reply')
                self.view=reply
            row['status']='verified';self.record(copy.deepcopy(row))
            return reply
        except BaseException as error:
            self.retired=True
            if row is not None:
                row.update(status='failed',error=str(error));self.record(copy.deepcopy(row))
            raise
        finally:self.active.release()

    def request(self,op,**values):
        command={'op':op}
        if op!='status':command.update(owner=self.owner,generation=self.view['generation'])
        command.update(values)
        return self.call(command)


def exercise(client, pause, *, record=lambda _:None):
    """One bounded three-session diagnostic; never send another command on failure."""
    try:
        require(callable(pause) and len(client.programs)>=2,'pause hook and two catalogue indices required')
        owner=client.owner;cycles=[]
        for command in (
            {'op':'launch','owner':f'{int(owner,16)^2:016x}','generation':0,'program':0},
            {'op':'launch','owner':owner,'generation':0,'program':len(client.programs)},
            {'op':'launch','owner':owner,'generation':1,'program':0},
            {'op':'launch','owner':owner,'generation':0,'program':0,'address':0},
        ):client.call(command,reject=True)
        for i,index in enumerate((0,1,0)):
            client.request('launch',program=index)
            generation=client.view['generation']
            first=client.request('activity')['activity'];pause(.05);last=client.request('activity')['activity']
            delta=(last['Cycles']-first['Cycles'])&0xffffffff
            require(0<delta<1<<31,'real fetch bus did not advance while running')
            client.request('hold')
            held=client.request('activity')['activity'];pause(.05);held_after=client.request('activity')['activity']
            require(held==held_after,'real fetch bus continued while held')
            rows=[0]*9;rows[0]=1<<4
            client.request('keyboard',rows=rows);client.request('keyboard',rows=[0]*9)
            client.request('run')
            resumed=client.request('activity')['activity'];pause(.05);resumed_after=client.request('activity')['activity']
            require(0<(resumed_after['Cycles']-resumed['Cycles'])&0xffffffff<1<<31,'fetch bus did not resume after Run')
            client.request('park' if i==2 else 'return')
            cycle={'cycle':i+1,'selection':index,'session_generation':generation,'fetches':[first,last,held,held_after,resumed,resumed_after],
                   'returned_state':client.view['state'],'return_generation':client.view['generation'],'runtime_admission':False}
            cycles.append(cycle);record(copy.deepcopy(cycle))
            if i<2:
                client.call({'op':'return','owner':owner,'generation':generation},reject=True)
                client.call({'op':'launch','owner':owner,'generation':generation-1,'program':index},reject=True)
        require(client.view['state']=='parked' and client.view['generation']==6 and client.loads==3,'three-session diagnostic did not park')
        return {'passed':True,'cycles':cycles,'configuration_cycles':3,'final_state':client.view,
                'core_load_acceptance':False,'runtime_admission':False,'physical_ddr_drain_qualified':False,
                'identity_evidence':'matched native source checks live identity internally; serial identity is the service publication'}
    except BaseException:
        client.retired=True
        raise
