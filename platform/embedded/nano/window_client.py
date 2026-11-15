"""Single-owner bounded window-service client. No retries or device access."""
import copy
import hashlib
import json
import threading
import time

from service_trace import require
from window_service_trace import startup, response, transition, quiet


class WindowClient:
    def __init__(self,initial,*,build_id,owner_seed,identity,configuration,memory,contract,cpu_mapping,atf=None,
                 exchange,owns,now=time.monotonic,record=lambda _:None):
        require(all(callable(f) for f in (exchange,owns,now,record)),'bounded window client hooks required')
        checked=startup(initial,build_id,owner_seed,identity=identity,configuration=configuration,memory=memory,contract=contract,cpu_mapping=cpu_mapping,atf=atf)
        self.boot=checked['boot'];self._view=checked['reply'];self.owner=self._view['owner']
        self.identity,self.configuration,self.memory=copy.deepcopy((identity,configuration,memory))
        self.exchange,self.owns,self.now,self.record=exchange,owns,now,record
        self.active=threading.Lock();self.retired=False;self.records=[]
        self.last_counter=self.boot['configuration_timing']['last_counter']
        require(self.owns(),'native window claim unavailable')

    @property
    def view(self):return copy.deepcopy(self._view)

    def invalid(self,command):
        op=command.get('op')
        if op=='status':return set(command)!={'op'}
        return (op not in ('launch','present','return','park') or set(command)!={'op','owner','generation'} or
                command.get('owner')!=self.owner or type(command.get('generation')) is not int or command['generation']!=self._view['generation'] or
                self._view['state']!=('ready' if op=='launch' else 'running'))

    def call(self,command,*,reject=False):
        require(type(command) is dict and type(reject) is bool,'explicit window request/outcome required')
        require(not self.retired and self._view['state']!='parked','window client retired or parked')
        require(self.invalid(command)==reject,'window request differs from expected local state')
        payload=json.dumps(command,separators=(',',':')).encode()+b'\n';require(len(payload)-1<=1024,'window command exceeds byte bound')
        require(self.active.acquire(blocking=False),'another window request is active')
        row=None
        try:
            require(self.owns(),'window claim lost before request')
            bound=100 if command['op']=='launch' and not reject else 5
            before=self.now();row=dict(command=copy.deepcopy(command),bound_seconds=bound,status='issued');self.records.append(row);self.record(copy.deepcopy(row))
            raw=self.exchange(payload,bound);after=self.now();row['elapsed_seconds']=after-before
            require(before<=after<before+bound,'window request deadline exceeded or clock regressed')
            require(self.owns(),'window claim changed during request')
            r,prefix=response(raw);row.update(reply=r,raw_bytes=len(raw),raw_sha256=hashlib.sha256(raw).hexdigest())
            require(r['owner']==self.owner and r['op']==command.get('op','invalid'),'window response owner/operation differs')
            if reject:
                require(not r['ok'] and quiet(prefix),'rejected window request had side effects or succeeded')
                require({k:v for k,v in r.items() if k not in ('op','ok','error')}=={k:v for k,v in self._view.items() if k not in ('op','ok','error')},'rejected window request changed cached ownership')
            else:
                proof=transition(raw,self._view,command['op'],identity=self.identity,configuration=self.configuration,memory=self.memory)
                if 'configuration' in proof:
                    timing=proof['configuration']['timing']
                    require(timing['first_counter']>self.last_counter,'window configuration clock restarted across requests')
                    self.last_counter=timing['last_counter']
                row['proof']=proof;self._view=r
            row['status']='verified';self.record(copy.deepcopy(row));return copy.deepcopy(r)
        except BaseException as error:
            self.retired=True
            if row is not None:row.update(status='failed',error=str(error));self.record(copy.deepcopy(row))
            raise
        finally:self.active.release()

    def request(self,op,*,reject=False,**values):
        command={'op':op}
        if op!='status':command.update(owner=self.owner,generation=self._view['generation'])
        command.update(values);return self.call(command,reject=reject)


def exercise(client):
    """Three modeled/requested sessions, two fresh SD cycles, terminal Park."""
    client.request('status');cycles=[]
    for cycle in range(3):
        if cycle:
            client.request('launch')
            client.request('return',generation=1,reject=True)
            client.request('status')
        presented=client.request('present')
        returned=client.request('park' if cycle==2 else 'return')
        cycles.append(dict(frame=presented['last_frame'],returned=returned['last_return'],storage=returned['storage']))
        if cycle<2:
            client.request('return',generation=presented['generation'],reject=True)
            client.request('launch',generation=presented['generation'],reject=True)
            client.request('status')
    require(client.view['state']=='parked' and client.view['generation']==6,'window service did not park after three sessions')
    return dict(cycles=cycles,commands=len(client.records),final=client.view,ordinary_admission=False,memory_released=False)
