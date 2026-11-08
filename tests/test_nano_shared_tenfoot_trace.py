"""Shared-input operator observations join real C fixture traces; no kit I/O."""
import copy,hashlib,re
from pathlib import Path
import unittest
from test_nano_tenfoot_trace import Model,PROGRAMS,trace
from test_nano_program_client import BOOT,SPEC,OWNER
from test_nano_hps_io import io_trace
from test_nano_native_trace import OWNER as IO_OWNER

CONTEXT={'sha256':'d'*64,'package_id':SPEC['package']['package_id'],'build_id':SPEC['package']['build_id']}
IO_RAW,_,MAPPING=io_trace()
IO_TAIL=IO_RAW[IO_RAW.index('NOTICE:  FES_NANO_HPS_IO_BEGIN'):].replace(format(IO_OWNER,'x'),OWNER).encode()
DESCRIPTOR=(Path(__file__).resolve().parents[1]/'tests/fixtures/nano-usb-gamepad/joystick-report.bin').read_bytes()
POLICY={'input':'gamepad','descriptor_sha256':hashlib.sha256(DESCRIPTOR).hexdigest(),'descriptor_bytes':len(DESCRIPTOR),'hps_context':CONTEXT,'hps_io':MAPPING}
BEGIN=b'FES_NATIVE_TENFOOT_BEGIN display=serial-state-only input=uart+shared-usb-gamepad preferences=ram\n'
READY=f"FES_NATIVE_SHARED_USB READY input=gamepad owner={OWNER} generation=2 descriptor_sha256={POLICY['descriptor_sha256']} descriptor_bytes={len(DESCRIPTOR)}\n".encode()
SOURCE=f"FES_NATIVE_HPS_CONTEXT PASS sha256={CONTEXT['sha256']} package_id={CONTEXT['package_id']} build_id={CONTEXT['build_id']} owner={OWNER} generation=1 programming=unavailable\n".encode()
SETUP=b'NOTICE:  FES_NANO_USB_SETUP status=0 board_error=0 platform_error=0 data=held\n'
SHARED_BOOT=BOOT.replace(b'NOTICE:  FES_NANO_OWNED_BOOT',SETUP+b'NOTICE:  FES_NANO_OWNED_BOOT')+SOURCE+IO_TAIL

class SharedModel(Model):
    def __init__(self,protocol='gamepad'):
        super().__init__();self.reports=0;self.polls=0;self.usb_fault='';self.waits=[];self.policy=copy.deepcopy(POLICY)
        if protocol=='boot-keyboard':self.policy.update(input=protocol,descriptor_sha256=hashlib.sha256(b'').hexdigest(),descriptor_bytes=0)
    def initial(self):
        p=self.policy
        ready=f"FES_NATIVE_SHARED_USB READY input={p['input']} owner={OWNER} generation=2 descriptor_sha256={p['descriptor_sha256']} descriptor_bytes={p['descriptor_bytes']}\n".encode()
        begin=b'FES_NATIVE_TENFOOT_BEGIN display=serial-state-only input=uart+shared-usb-'+p['input'].encode()+b' preferences=ram\n'
        return SHARED_BOOT+ready+begin+self.raw(self.state())
    def client(self,initial=None,policy=None):
        return trace.TenfootClient(self.initial() if initial is None else initial,build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],programs=PROGRAMS,exchange=self.exchange,owns=lambda:self.service.owned,now=lambda:self.service.clock,shared_usb=self.policy if policy is None else policy)
    def settle(self,seconds):self.waits.append(seconds);self.service.clock+=seconds
    def exchange(self,key,bound,done):
        before=self.service.generation
        raw=super().exchange(key,bound,lambda _:True)
        if key in (b'\x1b[C',b'\x1b[D'):done(raw);return raw
        self.reports+=8;self.polls+=8
        launch=key==b'\r'
        a=f'FES_NATIVE_SHARED_USB INPUT phase=pause state={"ready" if launch else "running"} native_generation={before} owner={OWNER} generation=2 reports={self.reports} polls={self.polls}\n'.encode()
        b=f'FES_NATIVE_SHARED_USB INPUT phase=resume state={"running" if launch else "ready"} native_generation={before+1} owner={OWNER} generation=2 reports={self.reports} polls={self.polls}\n'.encode()
        lines=raw.splitlines(keepends=True);raw=a+b''.join(lines[:-1])+b+lines[-1]
        if key==b'q':raw=raw.replace(lines[-1],f'FES_NATIVE_SHARED_USB STOP owner={OWNER} generation=3 reports={self.reports+1} polls={self.polls+1}\n'.encode()+lines[-1])
        f=self.usb_fault
        if f=='missing-pause':raw=raw.replace(a,b'')
        if f=='missing-resume':raw=raw.replace(b,b'')
        if f=='duplicate':raw=a+raw
        if f in ('no-reports','idle-no-polls','decreasing-polls','during-pause-poll','missing-polls','overflow-polls'):raw=re.sub(rb'reports=[0-9]+',b'reports=0',raw)
        if f in ('no-polls','idle-no-polls','decreasing-polls'):raw=re.sub(rb'polls=[0-9]+',b'polls=0',raw)
        if f=='during-pause-poll':raw=re.sub(rb'(phase=resume[^\n]* polls=)[0-9]+',lambda m:m[1]+str(self.polls+1).encode(),raw)
        if f=='missing-polls':raw=re.sub(rb' polls=[0-9]+',b'',raw)
        if f=='overflow-polls':raw=re.sub(rb'polls=[0-9]+',b'polls=18446744073709551616',raw)
        if f=='during-pause':raw=raw.replace(b,b.replace(f'reports={self.reports}'.encode(),f'reports={self.reports+1}'.encode()))
        if f=='generation':raw=raw.replace(b'generation=2 reports',b'generation=5 reports')
        if f=='owner':raw=raw.replace(b'owner='+OWNER.encode(),b'owner=1')
        if f=='native-generation':raw=raw.replace(f'native_generation={before+1}'.encode(),b'native_generation=99')
        if f=='early-completion':raw=raw.replace(b,b'')+b
        if f=='late-resume':raw=raw.replace(b,b'')+b+lines[-1]
        if f=='late-pause':raw=raw.replace(a,b'');position=raw.index(b'\n')+1;raw=raw[:position]+a+raw[position:]
        if f=='reinitialize':raw=READY+raw
        if f=='stop-lease':raw=raw.replace(b'STOP owner='+OWNER.encode()+b' generation=3',b'STOP owner='+OWNER.encode()+b' generation=2')
        if f=='truncated':raw=raw[:-1]
        done(raw);return raw

class SharedTenfootTraceTests(unittest.TestCase):
    def test_three_shared_cycles_keep_lease_and_require_live_reports(self):
        m=SharedModel();client=m.client();r=trace.exercise(client,settle=m.settle)
        self.assertTrue(r['passed']);self.assertEqual(r['shared_usb_reports'],49)
        self.assertEqual(len(m.waits),6);self.assertFalse(r['input_controls_exercised']);self.assertFalse(r['hardware_acceptance'])
        self.assertEqual(len([r for r in client.records if r['action']=='launch']),3)
    def test_keyboard_uses_its_explicit_shared_scope(self):
        m=SharedModel('boot-keyboard');c=m.client();r=trace.exercise(c,settle=m.settle)
        self.assertTrue(r['passed']);self.assertEqual(r['shared_usb_reports'],49)

    def test_idle_keyboard_requires_poll_progress_without_inventing_input(self):
        m=SharedModel('boot-keyboard');m.usb_fault='no-reports';c=m.client()
        r=trace.exercise(c,settle=m.settle)
        self.assertTrue(r['passed']);self.assertEqual(r['shared_usb_reports'],0)
        self.assertEqual(r['shared_usb_polls'],49)
        self.assertFalse(r['input_controls_exercised']);self.assertFalse(r['hardware_acceptance'])

    def test_idle_keyboard_missing_or_inconsistent_polls_retire_without_retry(self):
        for fault in ('idle-no-polls','during-pause-poll','missing-polls','overflow-polls'):
            with self.subTest(fault=fault):
                m=SharedModel('boot-keyboard');c=m.client();m.usb_fault=fault
                with self.assertRaises(ValueError):c.key('launch',0)
                self.assertTrue(c.retired);sent=list(m.keys)
                with self.assertRaises(ValueError):c.key('launch',0)
                self.assertEqual(m.keys,sent)
        m=SharedModel('boot-keyboard');m.usb_fault='no-reports';c=m.client();c.key('launch',0)
        m.usb_fault='decreasing-polls'
        with self.assertRaises(ValueError):c.key('stop')
        self.assertTrue(c.retired)

    def test_old_mode_cannot_adopt_shared_startup(self):
        for kwargs in ({'usb_control':True},{'usb_control':False,'usb_shared':True},{'usb_control':True,'usb_shared':1}):
            with self.subTest(kwargs=kwargs),self.assertRaises(ValueError):
                trace.decode_native_boot(SHARED_BOOT,SPEC['build_id'],SPEC['owner_seed'],package=SPEC['package'],atf=SPEC['atf'],hps_context=CONTEXT,hps_io=MAPPING,**kwargs)

    def test_corrupt_shared_transitions_retire_without_retry(self):
        for fault in ('missing-pause','missing-resume','duplicate','no-reports','during-pause','generation','owner','native-generation','early-completion','late-resume','late-pause','reinitialize','truncated'):
            with self.subTest(fault=fault):
                m=SharedModel();c=m.client();m.usb_fault=fault
                with self.assertRaises(ValueError):c.key('launch',0)
                self.assertTrue(c.retired);n=len(m.keys)
                with self.assertRaises(ValueError):c.key('launch',0)
                self.assertEqual(n,len(m.keys))
    def test_shared_observations_cannot_hide_native_failures(self):
        for fault in ('transfer','smmu','late','clock','claim','raise','activation','identity','queue','early-state','regress'):
            with self.subTest(fault=fault):
                m=SharedModel();c=m.client();m.fault=fault
                with self.assertRaises((ValueError,OSError)):c.key('launch',0)
                self.assertTrue(c.retired)
        m=SharedModel();c=m.client();c.key('launch',0);m.fault='stop'
        with self.assertRaises(ValueError):c.key('stop')
        self.assertTrue(c.retired)

    def test_shared_stop_is_required_at_quit(self):
        m=SharedModel();c=m.client();c.key('launch',0);m.usb_fault='stop-lease'
        with self.assertRaises(ValueError):c.key('quit')
        self.assertTrue(c.retired)
    def test_startup_owner_descriptor_source_and_scope_are_bound_before_input(self):
        m=SharedModel();raw=m.initial()
        variants=[raw.replace(SETUP,b''),raw.replace(READY,b''),raw.replace(READY,READY+READY),
                  raw.replace(b'generation=2 descriptor',b'generation=4 descriptor'),
                  raw.replace(POLICY['descriptor_sha256'].encode(),b'e'*64),
                  raw.replace(SOURCE,b''),raw.replace(IO_TAIL,b''),raw.replace(BEGIN,trace.BEGIN+b'\n'),
                  raw.replace(SETUP,SETUP+SETUP),raw.replace(b'board_error=0',b'board_error=-1'),
                  raw.replace(b'pass=1 index=163',b'pass=1 index=162'),raw.replace(b'reads=328',b'reads=327'),
                  raw.replace(CONTEXT['sha256'].encode(),b'f'*64),raw[:-1]]
        for bad in variants:
            with self.subTest(bad=bad[-120:]),self.assertRaises(ValueError):m.client(initial=bad)
        p=copy.deepcopy(POLICY);p['hps_context']['package_id']='a'*64
        with self.assertRaises(ValueError):m.client(policy=p)
        self.assertEqual(m.keys,[])
