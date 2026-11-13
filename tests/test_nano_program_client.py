"""Request client fixtures use recorded C observations and simulated service replies."""
import copy,json,re,sys,unittest
from unittest.mock import patch
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from program_client import ProgramClient,exercise
from service_trace import PREFIX,SCOPE,response,startup
FIX=ROOT/'tests/fixtures/nano-program-service'
SPEC=json.loads((FIX/'expected.json').read_text())
BOOT=(FIX/'boot.txt').read_bytes();LAUNCH=(FIX/'launch.txt').read_bytes();STOP=(FIX/'stop.txt').read_bytes()
OWNER=re.search(rb'FES_NANO_SPL HANDOFF owner=([0-9a-f]+)',BOOT)[1].decode().zfill(16)
PROGRAMS=[{'identity':SPEC['identity'],'configuration':SPEC['configuration']}]*2

class Model:
    def __init__(self):
        self.phase='ready';self.generation=0;self.held=True;self.loads=0;self.fetches=0
        self.commands=[];self.fault='';self.owned=True;self.clock=10.;self.ready=self.reply('status')
    def reply(self,op,ok=True):
        r=dict(scope=SCOPE,op=op,ok=ok,state=self.phase,owner=OWNER,generation=self.generation,held=self.held)
        if self.phase=='running':r.update(SPEC['identity'])
        if op=='status':r['programs']=[{'index':i,'package_id':p['identity']['package_id']} for i,p in enumerate(PROGRAMS)]
        if op=='activity' and ok:r['activity']={'Cycles':self.fetches,'Address':0x3d1}
        if not ok:r['error']='modeled stale or malformed request'
        return r
    def bytes(self,r,prefix=b''):return prefix+PREFIX+json.dumps(r,separators=(',',':')).encode()+b'\n'
    def exchange(self,payload,seconds):
        command=json.loads(payload);self.commands.append(command);op=command['op'];evidence=b''
        fields={'op'} if op=='status' else {'op','owner','generation'}|({'program'} if op=='launch' else {'rows'} if op=='keyboard' else set())
        bad=set(command)!=fields or op!='status' and (command.get('owner')!=OWNER or command.get('generation')!=self.generation)
        bad=bad or (op=='launch' and (self.phase!='ready' or not 0<=command.get('program',-1)<2)) or op not in ('status','launch','return','park','hold','run','keyboard','activity')
        bad=bad or op not in ('status','launch') and self.phase!='running'
        if not bad:
            if op=='launch':
                generation=4+5*self.loads;previous=0 if not self.loads else generation-5
                text=LAUNCH.decode();text=re.sub(r'\bgeneration=4\b',f'generation={generation}',text)
                text=text.replace('previous=0 rearm=0',f'previous={previous} rearm={int(self.loads>0)}')
                if self.loads:text=re.sub(r'\bcounter=([0-9a-f]+)',lambda m:'counter='+format(int(m[1],16)+self.loads*20*400000000,'x'),text)
                evidence=text.encode();self.phase='running';self.generation+=1;self.held=False;self.loads+=1;self.fetches=0
            elif op in ('return','park'):
                evidence=re.sub(rb'\bgeneration=5\b',f'generation={5+5*(self.loads-1)}'.encode(),STOP)
                self.phase='ready' if op=='return' else 'parked';self.generation+=1;self.held=True
            elif op in ('hold','run'):self.held=op=='hold'
            elif op=='activity' and ((not self.held and self.fault!='fetch-stopped') or self.held and self.fault=='fetch-held'):self.fetches+=100
        r=self.reply(op,not bad)
        if self.fault=='owner':r['owner']='0000000000000001'
        if self.fault=='generation':r['generation']+=1
        if self.fault=='boolean-generation':r['generation']=True
        if self.fault=='held':r['held']=not r['held']
        if self.fault=='identity':r['build_id']='a'*32
        if self.fault=='rejected':r.update(ok=False,error='unknown outcome')
        if self.fault=='activation':evidence=re.sub(rb'[^\n]*FES_NANO_FABRIC_ACTIVATION[^\n]*\n',b'',evidence)
        if self.fault=='transfer':evidence=evidence.replace(b'completed=1646592 pending=0',b'completed=1646591 pending=0')
        if self.fault=='smmu':evidence=evidence.replace(b'bypass_controls_clear=1',b'bypass_controls_clear=0')
        if self.fault=='stop':evidence=evidence.replace(b'complete=1 data=held',b'complete=0 data=held')
        if self.fault=='side-effect':evidence+=b'NOTICE:  FES_NANO_CONFIG_QUERY command=4 words=6\n'
        if self.fault=='claim':self.owned=False
        if self.fault=='late':self.clock+=seconds
        if self.fault=='clock':self.clock-=1
        if self.fault=='raise':raise OSError('lost serial exchange')
        raw=self.bytes(r,evidence)
        if self.fault=='duplicate':raw+=self.bytes(r)
        if self.fault=='duplicate-field':raw=raw.replace(b'"ok":true',b'"ok":true,"ok":true')
        if self.fault=='truncated':raw=raw[:-1]
        if self.fault=='panic':raw=b'panic: failure\n'+raw
        return raw
    def client(self):
        return ProgramClient(BOOT+self.bytes(self.ready),build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],programs=PROGRAMS,
            exchange=self.exchange,owns=lambda:self.owned,now=lambda:self.clock)

class ProgramClientTests(unittest.TestCase):
    def test_rom_receipt_is_expected_on_launch_stable_on_controls_and_cleared_on_return(self):
        class ROMModel(Model):
            receipt='1'*64
            def reply(self,op,ok=True):
                r=super().reply(op,ok)
                if self.phase=='running' and self.receipt is not None:r['rom_receipt_sha256']=self.receipt
                return r
        programs=copy.deepcopy(PROGRAMS)
        for row in programs:row['rom_receipt_sha256']='1'*64
        with patch.dict(globals(),PROGRAMS=programs):
            model=ROMModel();client=model.client()
            self.assertEqual(client.request('launch',program=0)['rom_receipt_sha256'],'1'*64)
            self.assertEqual(client.request('hold')['rom_receipt_sha256'],'1'*64)
            self.assertNotIn('rom_receipt_sha256',client.request('return'))
            for receipt in (None,'2'*64,'0'*64,True):
                model=ROMModel();client=model.client();model.receipt=receipt
                with self.subTest(receipt=receipt),self.assertRaises(ValueError):client.request('launch',program=0)
                self.assertTrue(client.retired);count=len(model.commands)
                with self.assertRaises(ValueError):client.request('status')
                self.assertEqual(len(model.commands),count)
            model=ROMModel();client=model.client();client.request('launch',program=0);model.receipt='2'*64
            with self.assertRaises(ValueError):client.request('status')
            self.assertTrue(client.retired)

    def test_three_request_sessions_fetch_hold_return_and_terminal_park(self):
        model=Model();client=model.client();result=exercise(client,lambda _:None)
        self.assertTrue(result['passed']);self.assertEqual(result['configuration_cycles'],3)
        self.assertEqual(result['final_state']['state'],'parked');self.assertFalse(result['core_load_acceptance'])
        self.assertEqual([r['session_generation'] for r in result['cycles']],[1,3,5])
        count=len(model.commands)
        with self.assertRaises(ValueError):client.request('run')
        self.assertEqual(len(model.commands),count)
    def test_failed_fetch_observation_stops_the_sequence_without_another_command(self):
        for fault in ('fetch-stopped','fetch-held'):
            with self.subTest(fault=fault):
                model=Model();client=model.client();model.fault=fault
                with self.assertRaises(ValueError):exercise(client,lambda _:None)
                self.assertTrue(client.retired);count=len(model.commands)
                with self.assertRaises(ValueError):client.request('return')
                self.assertEqual(len(model.commands),count)
    def test_real_uart_crlf_boot_and_reply_are_accepted(self):
        model=Model()
        initial=BOOT.replace(b'\n',b'\r\n')+model.bytes(model.ready).replace(b'\n',b'\r\n')
        checked=startup(initial,SPEC['build_id'],SPEC['owner_seed'],SPEC['atf'],SPEC['package'],[p['identity'] for p in PROGRAMS])
        self.assertEqual(checked['reply']['state'],'ready')
    def test_invalid_local_calls_never_issue_a_command_or_retire_client(self):
        model=Model();client=model.client()
        for command,reject in [({'op':'launch','owner':OWNER,'generation':0,'program':0},True),({'op':'return','owner':OWNER,'generation':0},False)]:
            with self.assertRaises(ValueError):client.call(command,reject=reject)
        self.assertEqual(model.commands,[]);self.assertFalse(client.retired)
    def test_bad_exchange_or_proof_retires_without_another_command(self):
        for fault in ('owner','generation','boolean-generation','held','identity','rejected','activation','transfer','smmu','side-effect','claim','late','clock','raise','duplicate','duplicate-field','truncated','panic'):
            with self.subTest(fault=fault):
                model=Model();client=model.client();model.fault=fault
                with self.assertRaises((ValueError,OSError)):client.request('launch',program=0)
                self.assertTrue(client.retired);count=len(model.commands)
                with self.assertRaises(ValueError):client.request('launch',program=0)
                self.assertEqual(len(model.commands),count)
    def test_changed_or_missing_return_proof_never_advances_to_next_load(self):
        model=Model();client=model.client();client.request('launch',program=0);model.fault='stop'
        with self.assertRaises(ValueError):client.request('return')
        self.assertTrue(client.retired);count=len(model.commands)
        with self.assertRaises(ValueError):client.request('launch',program=1)
        self.assertEqual(len(model.commands),count)
    def test_firmware_output_on_a_rejected_request_is_not_accepted(self):
        model=Model();client=model.client();model.fault='side-effect'
        with self.assertRaises(ValueError):client.call({'op':'launch','owner':OWNER,'generation':1,'program':0},reject=True)
        self.assertTrue(client.retired)
    def test_matched_boot_prefix_is_required_even_with_an_eligible_ready_reply(self):
        model=Model();initial=BOOT+model.bytes(model.ready)
        changes=(initial.replace(SPEC['build_id'].encode(),b'0'*16),b''.join(row for row in initial.splitlines(keepends=True) if not row.startswith(b'FES_ENTRY EL1 HANDOFF')),initial.replace(b'valid=1',b'valid=0'),initial+model.bytes(model.ready))
        for raw in changes:
            with self.subTest(raw=raw[-120:]),self.assertRaises(ValueError):startup(raw,SPEC['build_id'],SPEC['owner_seed'],SPEC['atf'],SPEC['package'],[p['identity'] for p in PROGRAMS])
    def test_reply_json_shape_bounds_and_duplicate_fields_are_strict(self):
        model=Model()
        for field,value in [('held',1),('generation',True),('owner','0'*16),('scope','other'),('programs',[])]:
            r=copy.deepcopy(model.ready);r[field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):response(model.bytes(r))
        for raw in (b'',b'x'*(1<<20|1),model.bytes(model.ready).replace(b'"ok":true',b'"ok":true,"ok":true'),model.bytes(model.ready)[:-1]):
            with self.assertRaises(ValueError):response(raw)
    def test_rejected_commands_preserve_session_state_and_do_not_retire(self):
        model=Model();client=model.client()
        client.call({'op':'launch','owner':OWNER,'generation':1,'program':0},reject=True)
        self.assertEqual(client.view['generation'],0);self.assertFalse(client.retired)
        client.request('launch',program=0)
        self.assertEqual(client.view['generation'],1)
