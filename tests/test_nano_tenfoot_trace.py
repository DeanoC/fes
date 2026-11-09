"""Synthetic App states over captured C observations; no hardware acceptance."""
import copy,json,re,sys,unittest
from unittest.mock import patch
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import tenfoot_trace as trace
from test_nano_program_client import Model as ProgramModel,BOOT,SPEC,OWNER
from service_trace import response
from test_nano_native_trace import firmware_progress

IDENTITY=dict(PackageID=SPEC['identity']['package_id'],CoreID=SPEC['identity']['core_id'],ABI='fes.de25-embedded-diagnostic',BuildID=SPEC['identity']['build_id'],Platform='de25_nano',Device='A5EB013BB23BE4SCS',ProgrammingProfile='de25-embedded-hps-first-ddr-diagnostic-v1',ActivationReceiptSHA256='',ProgrammedSHA256=SPEC['package']['payload_sha256'],Major=1,Minor=0)
SECOND=dict(IDENTITY,PackageID='a'*64,BuildID='b'*32,ProgrammedSHA256='c'*64)
PROGRAMS=[dict(title='ZX81',identity=IDENTITY,configuration=SPEC['configuration']),dict(title='ZX80',identity=SECOND,configuration=dict(SPEC['configuration'],package_id='a'*64,build_id='b'*32,core_sha256='c'*64))]

class TapeObservationTests(unittest.TestCase):
    def test_selected_tape_receipt_is_bound_to_live_program_and_absent_after_return(self):
        m=Model();client=m.client()
        state=m.state('running',1)
        state['native']['Identity'].update(ProgrammingProfile='de25-native-computer-v1',ABI='fes.computer')
        client.programs[0]['identity']=copy.deepcopy(state['native']['Identity'])
        client.programs[0]['tape']=dict(sha256='d'*64,bytes=1025)
        state['native'].update(tape_sha256='d'*64,tape_bytes=1025)
        parsed,_,_=trace.packet(m.raw(state))
        client._state(parsed[0],1,('running',),client.programs[0]['identity'])
        for fault in ('missing','digest','size','unexpected'):
            bad=copy.deepcopy(state)
            if fault=='missing':bad['native'].pop('tape_sha256');bad['native'].pop('tape_bytes')
            if fault=='digest':bad['native']['tape_sha256']='e'*64
            if fault=='size':bad['native']['tape_bytes']=1024
            expected=copy.deepcopy(client.programs)
            if fault=='unexpected':client.programs[0].pop('tape')
            with self.subTest(fault=fault),self.assertRaises(ValueError):
                trace.packet(m.raw(bad));client._state(bad,1,('running',),client.programs[0]['identity'])
            client.programs=expected
        returned=m.state('ready',2)
        returned['native'].update(tape_sha256='d'*64,tape_bytes=1025)
        with self.assertRaises(ValueError):trace.packet(m.raw(returned))

    def test_partial_or_invalid_tape_receipt_rejects_before_state_acceptance(self):
        m=Model();base=m.state('running',1)
        base['native']['Identity'].update(ProgrammingProfile='de25-native-computer-v1',ABI='fes.computer')
        for value in (dict(tape_sha256='d'*64),dict(tape_bytes=1),dict(tape_sha256='d'*64,tape_bytes=True),dict(tape_sha256='d'*64,tape_bytes=16376),dict(tape_sha256='0'*64,tape_bytes=1)):
            row=copy.deepcopy(base);row['native'].update(value)
            with self.subTest(value=value),self.assertRaises(ValueError):trace.packet(m.raw(row))

    def test_bad_expected_tape_is_rejected_as_content_assertion(self):
        for value in (dict(sha256='d'*64,bytes=True),dict(sha256='d'*64,bytes=16376),dict(sha256='d'*64,bytes=0),dict(sha256='d'*64),dict(sha256='d'*64,bytes=1,unit=1)):
            with self.subTest(value=value),self.assertRaises(ValueError):trace.program_tape(dict(tape=value))

class Model:
    def __init__(self):
        self.service=ProgramModel();self.focus=0;self.selected=0;self.keys=[];self.fault=''
    def state(self,phase=None,gen=None):
        native=self.service.phase if phase is None else phase
        generation=self.service.generation if gen is None else gen
        row=dict(scope='nano-tenfoot-serial-v1',display='serial-state-only',titles=['ZX81','ZX80'],focus=self.focus,loading=False,load_error='',session='active' if native=='running' else 'idle',launch='ok' if native=='running' else 'idle',launch_error='',retry_stop=False,native=dict(State=native,Owner=int(OWNER,16),Generation=generation,Identity=copy.deepcopy(PROGRAMS[self.selected]['identity'] if native in ('running','returning') else trace.ZERO),Held=native not in ('running','returning'),Error=''))
        if row['native']['Identity']['ActivationReceiptSHA256']:
            row['native']['rom_receipt_sha256']=row['native']['Identity']['ActivationReceiptSHA256']
        return row
    def raw(self,state):return trace.PREFIX+json.dumps(state,separators=(',',':')).encode()+b'\n'
    def input_action(self,action,generation):
        # The Go formatter's captured record, with this synthetic model's
        # generation. Neither these model rows nor the capture are board data.
        rows=(ROOT/'tests/fixtures/nano-input-actions/producer.txt').read_bytes().splitlines(keepends=True)
        template=next(row for row in rows if b'action='+action.encode()+b' ' in row)
        return re.sub(rb'native_generation=[0-9]+',b'native_generation='+str(generation).encode(),template)
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
        if key not in (b'\x1b[C',b'\x1b[D'):
            raw=self.input_action('select' if key==b'\r' else 'stop',before)+raw
            if key==b'q':raw=raw.replace(b'FES_NATIVE_TENFOOT_EXIT ',self.input_action('quit',before+1)+b'FES_NATIVE_TENFOOT_EXIT ',1)
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
    def test_selected_ram_trace_is_required_before_any_app_input(self):
        model=Model();initial=BOOT+trace.BEGIN+b'\n'+model.raw(model.state())
        options=dict(build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],
                     package=SPEC['package'],programs=PROGRAMS,exchange=model.exchange,
                     owns=lambda:True,firmware_ram_trace=True)
        with self.assertRaises(ValueError):trace.TenfootClient(initial,**options)
        before=b'NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        self.assertIn(before,initial)
        updated=firmware_progress(initial.decode()).encode()
        client=trace.TenfootClient(updated,**options)
        self.assertTrue(client.boot['firmware_ram_trace']['complete'])
        self.assertTrue(client.boot['firmware_ram_trace']['vendor_complete'])
        self.assertEqual(model.keys,[])

    def test_same_package_different_rom_receipts_complete_owned_cycles(self):
        programs=copy.deepcopy(PROGRAMS)
        programs[1]['identity']=copy.deepcopy(programs[0]['identity'])
        programs[1]['configuration']=copy.deepcopy(programs[0]['configuration'])
        for index,row in enumerate(programs):row['identity']['ActivationReceiptSHA256']=str(index+1)*64
        with patch.dict(globals(),PROGRAMS=programs):
            model=Model();client=model.client();result=trace.exercise(client)
            self.assertTrue(result['passed'])
            active=[r['states'][-1]['native'] for r in client.records if r['action']=='launch']
            self.assertEqual([r['Identity']['ActivationReceiptSHA256'] for r in active],['1'*64,'2'*64,'1'*64])
            self.assertEqual(len({r['Identity']['PackageID'] for r in active}),1)
            duplicate=copy.deepcopy(programs);duplicate[1]['identity']=copy.deepcopy(duplicate[0]['identity'])
            with self.assertRaises(ValueError):model.client(duplicate)

    def test_missing_foreign_and_invalid_rom_status_receipts_reject(self):
        model=Model();model.service.phase='running';model.service.generation=1
        good=model.state();good['native']['Identity']['ActivationReceiptSHA256']='1'*64
        good['native']['rom_receipt_sha256']='1'*64
        self.assertEqual(trace.packet(model.raw(good))[0][-1],good)
        variants=[]
        missing=copy.deepcopy(good);missing['native'].pop('rom_receipt_sha256');variants.append(missing)
        for receipt in ('2'*64,'0'*64,'A'*64,True):
            row=copy.deepcopy(good);row['native']['rom_receipt_sha256']=receipt;variants.append(row)
            row=copy.deepcopy(good);row['native']['Identity']['ActivationReceiptSHA256']=receipt;variants.append(row)
        row=copy.deepcopy(good);row['native']['Identity']['ActivationReceiptSHA256']='';variants.append(row)
        row=copy.deepcopy(good);row['native']['State']='ready';variants.append(row)
        for row in variants:
            with self.subTest(receipt=row['native'].get('rom_receipt_sha256')),self.assertRaises(ValueError):
                trace.packet(model.raw(row))

    def test_real_key_sequence_three_sessions_and_owned_quit(self):
        model=Model();client=model.client();result=trace.exercise(client)
        self.assertTrue(result['passed']);self.assertEqual([r['launch_generation'] for r in result['cycles']],[1,3,5])
        self.assertEqual(model.keys,[b'\r',b's',b'\x1b[C',b'\r',b's',b'\x1b[D',b'\r',b'q'])
        self.assertFalse(result['hardware_acceptance']);count=len(model.keys)
        with self.assertRaises(ValueError):client.key('stop')
        self.assertEqual(count,len(model.keys))
    def test_tape_timing_has_a_size_budget_and_a_separate_control_limit(self):
        model=Model();model.service.generation=1
        old=json.loads(model.fetch()[len(trace.FETCH):])
        row=dict(old,scope='nano-computer-fetch-tape-v1',tape_bytes=917,tape_elapsed_ns=2000000000,tape_limit_ns=6000000000,control_elapsed_ns=1000000,elapsed_ns=2001000000,limit_ns=7000000000)
        self.assertEqual(trace.fetch_observation(json.dumps(row)),row)
        for change in (dict(tape_bytes=16376),dict(tape_limit_ns=7000000000),dict(tape_elapsed_ns=6000000000),dict(control_elapsed_ns=1000000000),dict(elapsed_ns=True),dict(tape_bytes=True),dict(tape_limit_ns=6000000000.0),dict(control_elapsed_ns=1000000.0),dict(scope='nano-computer-fetch-v1'),dict(limit_ns=8000000000)):
            with self.subTest(change=change),self.assertRaises(ValueError):trace.fetch_observation(json.dumps(dict(row,**change)))

    def test_fetch_counter_wrap_and_unsolicited_observations(self):
        model=Model();model.service.generation=1
        record=json.loads(model.fetch()[len(trace.FETCH):])
        record['before']=record['held']=dict(Cycles=0xfffffffe,Address=0)
        record['running']=dict(Cycles=3,Address=1);record['delta']=5
        self.assertEqual(trace.fetch_observation(json.dumps(record))['delta'],5)
        model.service.generation=0
        with self.assertRaises(ValueError):trace.packet(model.fetch()+model.raw(model.state()))

    def test_fetch_address_is_a_copied_package_expectation_not_a_selection_index(self):
        programs=copy.deepcopy(PROGRAMS);programs[0]['fetch_address']=1
        model=Model();client=model.client(programs)
        programs[0]['fetch_address']=0
        # Both real machines may fetch at nonzero addresses. Only an explicit
        # fixture expectation restricts the selected address, including zero.
        self.assertTrue(trace.exercise(client)['passed'])
        model=Model();programs[0]['fetch_address']=0;client=model.client(programs)
        with self.assertRaisesRegex(ValueError,'selected fetch address'):client.key('launch',0)
        self.assertTrue(client.retired);keys=list(model.keys)
        with self.assertRaises(ValueError):client.key('stop')
        self.assertEqual(model.keys,keys)
        model=Model();original=model.fetch
        def at_zero():
            r=json.loads(original()[len(trace.FETCH):]);r['running']['Address']=0
            return trace.FETCH+json.dumps(r).encode()+b'\n'
        model.fetch=at_zero
        self.assertTrue(trace.exercise(model.client(programs))['passed'])

    def test_invalid_fetch_expectations_reject_before_any_command(self):
        for address in (True,False,None,-1,65536,1.0,'0'):
            model=Model();programs=copy.deepcopy(PROGRAMS);programs[1]['fetch_address']=address
            with self.subTest(address=address),self.assertRaisesRegex(ValueError,'16-bit integer'):
                model.client(programs)
            self.assertEqual(model.keys,[])
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
