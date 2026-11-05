"""Synthetic App states over captured C observations; no hardware acceptance."""
import copy,json,re,sys,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import tenfoot_trace as trace
from test_nano_program_client import Model as ProgramModel,BOOT,SPEC,OWNER
from service_trace import response

IDENTITY=dict(PackageID=SPEC['identity']['package_id'],CoreID=SPEC['identity']['core_id'],ABI='fes.de25-embedded-diagnostic',BuildID=SPEC['identity']['build_id'],Platform='de25_nano',Device='A5EB013BB23BE4SCS',ProgrammingProfile='de25-embedded-hps-first-ddr-diagnostic-v1',ActivationReceiptSHA256='',ProgrammedSHA256=SPEC['package']['payload_sha256'],Major=1,Minor=0)
SECOND=dict(IDENTITY,PackageID='a'*64,BuildID='b'*32,ProgrammedSHA256='c'*64)
PROGRAMS=[dict(title='ZX81',identity=IDENTITY,configuration=SPEC['configuration']),dict(title='ZX80',identity=SECOND,configuration=dict(SPEC['configuration'],package_id='a'*64,build_id='b'*32,core_sha256='c'*64))]

class Model:
    def __init__(self):
        self.service=ProgramModel();self.focus=0;self.selected=0;self.keys=[];self.fault=''
    def state(self,phase=None,gen=None):
        native=self.service.phase if phase is None else phase
        generation=self.service.generation if gen is None else gen
        return dict(scope='nano-tenfoot-serial-v1',display='serial-state-only',titles=['ZX81','ZX80'],focus=self.focus,loading=False,load_error='',session='active' if native=='running' else 'idle',launch='ok' if native=='running' else 'idle',launch_error='',retry_stop=False,native=dict(State=native,Owner=int(OWNER,16),Generation=generation,Identity=copy.deepcopy(PROGRAMS[self.selected]['identity'] if native in ('running','returning') else trace.ZERO),Held=native not in ('running','returning'),Error=''))
    def raw(self,state):return trace.PREFIX+json.dumps(state,separators=(',',':')).encode()+b'\n'
    def client(self,programs=None):
        return trace.TenfootClient(BOOT+trace.BEGIN+b'\n'+self.raw(self.state()),build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],programs=PROGRAMS if programs is None else programs,exchange=self.exchange,owns=lambda:self.service.owned,now=lambda:self.service.clock)
    def fetch(self):
        r=dict(scope='nano-computer-fetch-v1',owner=int(OWNER,16),generation=self.service.generation,package_id=PROGRAMS[self.selected]['identity']['PackageID'],build_id=PROGRAMS[self.selected]['identity']['BuildID'],before=dict(Cycles=10,Address=0),held=dict(Cycles=10,Address=0),running=dict(Cycles=20,Address=1),delta=10,polls=1,elapsed_ns=1000000,limit_ns=1000000000)
        if self.fault=='fetch-stuck':r['running']['Cycles']=10;r['delta']=0
        if self.fault=='fetch-held':r['before']['Cycles']=9
        if self.fault=='fetch-reset':r['running']['Cycles']=9;r['delta']=0xffffffff
        if self.fault=='fetch-identity':r['build_id']='e'*32
        if self.fault=='fetch-generation':r['generation']+=2
        if self.fault=='fetch-clock':r['elapsed_ns']=1000000000
        if self.fault=='fetch-bool':r['polls']=True
        raw=trace.FETCH+json.dumps(r,separators=(',',':')).encode()+b'\n'
        return b'' if self.fault=='fetch-missing' else raw*2 if self.fault=='fetch-duplicate' else raw
    def exchange(self,key,bound,done):
        self.keys.append(key);before=self.service.generation
        if key in (b'\x1b[C',b'\x1b[D'):
            self.focus+=1 if key==b'\x1b[C' else -1
            raw=self.raw(self.state())
        else:
            op='launch' if key==b'\r' else 'return'
            intermediate=self.state('loading' if op=='launch' else 'returning',before)
            if op=='launch':self.selected=self.focus
            command={'op':op,'owner':OWNER,'generation':before}
            if op=='launch':command['program']=self.focus
            self.service.fault=self.fault if self.fault in ('transfer','smmu','stop','late','clock','claim','raise','activation') else ''
            _,evidence=response(self.service.exchange(json.dumps(command).encode(),bound))
            state=self.state()
            raw=self.raw(intermediate)+evidence+(self.fetch() if op=='launch' else b'')+(b'FES_NATIVE_TENFOOT_EXIT state=ready generation=6\n' if key==b'q' else self.raw(state))
            if self.fault=='early-state':raw=self.raw(state)+evidence+self.raw(state)
            if self.fault=='fetch-early' and op=='launch':raw=self.fetch()+self.raw(intermediate)+evidence+self.raw(state)
            if self.fault=='regress':raw+=self.raw(intermediate)+self.raw(state)
        if self.fault=='owner':raw=raw.replace(str(int(OWNER,16)).encode(),b'1')
        if self.fault=='identity':raw=raw.replace(IDENTITY['BuildID'].encode(),b'd'*32)
        if self.fault=='bool-generation':raw=re.sub(rb'"Generation":\d+',b'"Generation":true',raw)
        if self.fault=='scope':raw=raw.replace(b'serial-state-only',b'hdmi')
        if self.fault=='queue':raw=raw.replace(b'credits=4',b'credits=8')
        if self.fault=='duplicate-field':raw=raw.replace(b'"retry_stop":false',b'"retry_stop":false,"retry_stop":false')
        if self.fault=='truncated':raw=raw[:-1]
        if self.fault=='side-effect':raw=b'NOTICE:  FES_NANO_CONFIG_QUERY command=4 words=6\n'+raw
        done(raw)
        return raw

class TenfootTraceTests(unittest.TestCase):
    def test_real_key_sequence_three_sessions_and_owned_quit(self):
        model=Model();client=model.client();result=trace.exercise(client)
        self.assertTrue(result['passed']);self.assertEqual([r['launch_generation'] for r in result['cycles']],[1,3,5])
        self.assertEqual(model.keys,[b'\r',b's',b'\x1b[C',b'\r',b's',b'\x1b[D',b'\r',b'q'])
        self.assertFalse(result['hardware_acceptance']);count=len(model.keys)
        with self.assertRaises(ValueError):client.key('stop')
        self.assertEqual(count,len(model.keys))
    def test_fetch_counter_wrap_and_unsolicited_observations(self):
        model=Model();model.service.generation=1
        record=json.loads(model.fetch()[len(trace.FETCH):])
        record['before']=record['held']=dict(Cycles=0xfffffffe,Address=0)
        record['running']=dict(Cycles=3,Address=1);record['delta']=5
        self.assertEqual(trace.fetch_observation(json.dumps(record))['delta'],5)
        model.service.generation=0
        with self.assertRaises(ValueError):trace.packet(model.fetch()+model.raw(model.state()))
    def test_corrupt_launch_retires_without_another_key(self):
        for fault in ('transfer','smmu','late','clock','claim','raise','activation','owner','identity','bool-generation','scope','queue','duplicate-field','truncated','side-effect','early-state','regress','fetch-stuck','fetch-held','fetch-reset','fetch-identity','fetch-generation','fetch-clock','fetch-bool','fetch-missing','fetch-duplicate','fetch-early'):
            with self.subTest(fault=fault):
                model=Model();client=model.client();model.fault=fault
                with self.assertRaises((ValueError,OSError)):client.key('launch',0)
                self.assertTrue(client.retired);count=len(model.keys)
                with self.assertRaises(ValueError):client.key('stop')
                self.assertEqual(count,len(model.keys))
    def test_failed_return_retires_and_cannot_relaunch(self):
        model=Model();client=model.client();client.key('launch',0);model.fault='stop'
        with self.assertRaises(ValueError):client.key('stop')
        self.assertTrue(client.retired)
    def test_parser_rejects_unknown_nested_fields_nonfinite_and_failed_exit(self):
        model=Model();state=model.state();raw=model.raw(state)
        for bad in (raw.replace(b'"Held":true',b'"Held":true,"extra":0'),raw.replace(b'"Generation":0',b'"Generation":NaN'),raw+b'FES_NATIVE_TENFOOT_EXIT state=running generation=1\n',trace.BEGIN+b'\n'+raw):
            with self.assertRaises(ValueError):trace.packet(bad)
    def test_focus_requires_no_firmware_and_input_catalog_is_owned(self):
        model=Model();client=model.client();model.fault='side-effect'
        with self.assertRaises(ValueError):client.key('focus',1)
        self.assertTrue(client.retired)

    def test_program_catalog_input_is_copied(self):
        model=Model();programs=copy.deepcopy(PROGRAMS);client=model.client(programs)
        programs[0]['title']='changed';programs[0]['identity']['BuildID']='d'*32
        self.assertEqual(client.key('launch',0)['status'],'verified')

    def test_initial_boot_binding_and_catalogue_fail_before_input(self):
        model=Model();initial=BOOT+trace.BEGIN+b'\n'+model.raw(model.state())
        common=dict(build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],programs=PROGRAMS,exchange=model.exchange,owns=lambda:True)
        for bad in (initial.replace(str(int(OWNER,16)).encode(),b'1'),initial.replace(trace.BEGIN+b'\n',b''),initial.replace(trace.BEGIN,trace.BEGIN+b' unexpected')):
            with self.assertRaises(ValueError):trace.TenfootClient(bad,**common)
        duplicates=copy.deepcopy(PROGRAMS);duplicates[1]['identity']=copy.deepcopy(duplicates[0]['identity'])
        with self.assertRaises(ValueError):trace.TenfootClient(initial,**dict(common,programs=duplicates))
        self.assertEqual(model.keys,[])

    def test_selected_app_cpu_gate_requires_every_actual_checkpoint(self):
        raw=(ROOT/'tests/fixtures/nano-tenfoot-startup/cpu.txt').read_bytes()
        checked=trace.startup_checkpoints(raw)
        self.assertEqual(len(checked['records']),14)
        self.assertEqual(checked['last_checkpoint'],'fat_mount_end')
        for line in raw.splitlines(keepends=True):
            with self.subTest(line=line[:60]),self.assertRaises(ValueError):trace.startup_checkpoints(raw.replace(line,b'',1))
        with self.assertRaises(ValueError):trace.startup_checkpoints(raw.replace(b'hz=0000000017d78400',b'hz=0000000017d78401',1))
