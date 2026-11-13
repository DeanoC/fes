"""Two-source installer observer with an independently expected prior library."""
import copy
import json
from pathlib import Path
import sys
import unittest
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import installer_client
from installer_cycle import exercise,replay
import paired_tenfoot_trace as paired
from test_nano_installer_client import Model as SingleModel,closed_panel
from test_nano_installer_cycle import Capture
from test_nano_paired_tenfoot_trace import Model as PairedModel,MENU_OPEN,MENU_CLOSE,CORE_OPEN
from test_nano_program_client import OWNER,SPEC
from test_nano_tenfoot_trace import PROGRAMS

class Model(SingleModel):
    def __init__(self,discovered=False,shared=True):
        self.preinstalled=discovered
        self.mask=[discovered,discovered]
        self.prior=['Existing machine']
        self.visible=self.prior+([p['title'] for p in PROGRAMS] if discovered else [])
        self.origin=0;self.installing=False;self.source_selection=None
        self.panels=dict(settings=closed_panel(),systems=closed_panel())
        self.installer_fault='';self.install_count=0;self.fail_second=''
        self.fail_focus=False;self.transient_library=True;self.bad_second_launch=False;self.reorder_library=False
        PairedModel.__init__(self,shared=shared,protocol='boot-keyboard')
    def state(self,phase=None,gen=None):
        row=PairedModel.state(self,phase,gen);n=row['native']
        selected=n['State'] in ('running','returning') or n['State']=='loading' and not self.installing and (n['Generation']-self.origin)%2==1
        n['Identity']=copy.deepcopy(PROGRAMS[self.selected]['identity'] if selected else paired.app.ZERO)
        n['Held']=n['State'] not in ('running','returning')
        n.pop('rom_receipt_sha256',None)
        if n['Identity']['ActivationReceiptSHA256']:n['rom_receipt_sha256']=n['Identity']['ActivationReceiptSHA256']
        row.update(titles=list(self.visible),focus=self.focus)
        return row
    def client(self):
        return installer_client.InstallerClient(self.initial,programs=PROGRAMS,installed=list(self.mask),
            initial_titles=list(self.visible),build_record=self.profile,build_id=SPEC['build_id'],
            owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],exchange=self.exchange,
            owns=lambda:self.service.owned,now=lambda:self.service.clock,
            shared_usb=self.policy if self.shared else None)
    def install_wire(self):
        before=self.service.generation;self.installing=True;self.install_count+=1
        self.panels['systems']['busy']=True;self.reports+=8;self.polls+=8
        raw=self.input_action('select',before)
        if self.shared:raw+=self.usb('pause','ready',before)
        raw+=self.raw(self.state('loading',before))+self.source_lines(MENU_CLOSE,self.source)+self.menu_wire('return')
        self.service.generation+=1;self.service.phase='loading';raw+=self.raw(self.state())
        self.source+=6;self.menu_index+=1;self.frames=1
        raw+=self.configure(self.source)+self.source_lines(MENU_OPEN,self.source)+self.menu_wire()
        self.service.phase='ready'
        if self.shared:raw+=self.usb('resume','ready',self.service.generation)
        self.panels['systems'].update(busy=False,rows=['library','title','create'],index=0)
        raw+=self.raw(self.state());self.frames+=1;raw+=self.menu_wire()
        self.mask[self.source_selection]=True;self.origin+=1;self.installing=False
        raw=self.decorate(raw)
        if self.install_count==2:
            if self.fail_second=='owner':raw=raw.replace(b'"owner":'+str(int(OWNER,16)).encode(),b'"owner":1')
            if self.fail_second=='resume':raw=b''.join(l for l in raw.splitlines(keepends=True) if b'phase=resume' not in l)
            if self.fail_second=='master':raw=raw.replace(b'cancel=0 complete=1',b'cancel=1 complete=0')
            if self.fail_second=='generation':raw=raw.replace(b'\"Generation\":2',b'\"Generation\":3')
        return raw
    def launch_wire(self):
        before=self.service.generation;title=self.visible[self.focus]
        self.selected=next(i for i,p in enumerate(PROGRAMS) if p['title']==title)
        if self.bad_second_launch and self.selected==1:self.selected=0
        raw=self.input_action('select',before)
        if self.shared:
            self.reports+=8;self.polls+=8;raw+=self.usb('pause','ready',before)
        raw+=self.raw(self.state('loading',before))+self.source_lines(MENU_CLOSE,self.source)+self.menu_wire('return')
        raw+=self.raw(self.state('loading',before+1));self.source+=6
        raw+=self.configure(self.source)+self.source_lines(CORE_OPEN,self.source)
        self.service.phase='running';self.service.generation+=1
        if self.shared:raw+=self.usb('resume','running',self.service.generation)
        raw+=self.fetch()+self.raw(self.state())
        return self.decorate(raw)
    def exchange(self,key,bound,done):
        if self.service.phase=='running' or key in (b's',b'q'):
            raw=self.decorate(PairedModel.exchange(self,key,bound,lambda _:False))
        elif key in (b'\x1b[C',b'\x1b[D'):
            raw=PairedModel.exchange(self,key,bound,lambda _:False) # Real unchanged installer views are deduplicated.
            if self.fail_focus:raw=raw.replace(b'"Owner":'+str(int(OWNER,16)).encode(),b'"Owner":1')
        else:
            self.keys.append(key);raw=b''
            if key==b'o':self.panels['settings']=dict(open=True,index=0,rows=['layout','systems'],busy=False,status='')
            elif key==b'\x1b[B':
                panel=self.panels['systems'] if self.panels['systems']['open'] else self.panels['settings']
                panel['index']=(panel['index']+1)%len(panel['rows'])
            elif not self.panels['settings']['open'] and not self.panels['systems']['open']:
                raw=self.launch_wire()
            elif self.panels['settings']['open']:
                self.panels['settings']=closed_panel()
                self.panels['systems']=dict(open=True,index=0,rows=['refresh','system','system'],busy=False,status='')
            else:
                panel=self.panels['systems'];kind=panel['rows'][panel['index']]
                if kind=='system':
                    self.source_selection=panel['index']-1
                    panel.update(index=0,rows=['library','title','create'] if self.mask[self.source_selection] else ['install'])
                elif kind=='install':raw=self.install_wire()
                elif kind=='library':
                    self.panels['systems']=closed_panel()
                    if self.transient_library:
                        state=self.state();state['loading']=True;self.frames+=1
                        raw=self.raw(state)+self.navigation()+self.menu_wire()
                    self.visible=self.prior+[p['title'] for p,installed in zip(PROGRAMS,self.mask) if installed]
                    if self.reorder_library:self.visible.sort(reverse=True)
                    self.frames+=1;raw+=self.raw(self.state())+self.navigation()+self.menu_wire()
                    raw=self.input_action('select',self.service.generation)+raw
                else:raise AssertionError('unexpected model row')
            if not raw:
                self.frames+=1;raw=self.raw(self.state())+self.navigation()+self.menu_wire()
                if key==b'\r':raw=self.input_action('select',self.service.generation)+raw
            self.service.clock+=.01
        for end in (len(raw)//2,len(raw)-1,len(raw),len(raw)):done(raw[:end])
        return raw

class CatalogCapture(Capture):
    def __init__(self,discovered=False,shared=True):
        self.model=Model(discovered,shared);self.client=self.model.client()
        self.raw=bytearray(self.model.initial);self.windows=[]
        self.startup=copy.deepcopy(self.client.view);self.boot=copy.deepcopy(self.client.boot)
        self.discovered=discovered
        self.options=dict(programs=PROGRAMS,initial_titles=list(self.model.visible),build_record=self.model.profile,
            build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],
            shared_usb=self.model.policy if shared else None)
        self.client.exchange=self.exchange

class CatalogTests(unittest.TestCase):
    def test_two_installs_and_discovery_preserve_prior_library(self):
        for discovered in (False,True):
            with self.subTest(discovered=discovered):
                c=CatalogCapture(discovered);r=c.run()
                self.assertEqual(c.client.installed,[True,True]);self.assertEqual(c.client.loads,2)
                self.assertEqual(c.client.view['titles'],['Existing machine','ZX81','ZX80'])
                self.assertEqual(c.model.install_count,0 if discovered else 2)
                self.assertEqual(sum(x['action']=='install' for x in c.client.records),0 if discovered else 2)
                self.assertEqual(r['service']['exit_generation'],5 if discovered else 7)
                self.assertEqual(r['service']['menu']['menu_sessions'],3 if discovered else 5)
                self.assertIn(b'\x1b[C',c.model.keys)
                replay(bytes(c.raw),r,discovered=discovered,**c.options)
    def test_sorted_library_refresh_keeps_exact_prior_view_during_second_install(self):
        c=CatalogCapture();c.model.reorder_library=True;r=c.run()
        self.assertEqual(c.client.view['titles'],['ZX81','ZX80','Existing machine'])
        self.assertEqual(c.client.installed,[True,True]);self.assertEqual(c.client.loads,2)
        replay(bytes(c.raw),r,discovered=False,**c.options)
    def test_second_install_failure_keeps_first_and_stops_without_launch_or_retry(self):
        for fault in ('owner','resume','master','generation'):
            with self.subTest(fault=fault):
                c=CatalogCapture();c.model.fail_second=fault
                with self.assertRaises(ValueError):c.run()
                self.assertTrue(c.client.retired);self.assertEqual(c.client.installed,[True,False]);self.assertEqual(c.client.loads,0)
                self.assertEqual(c.model.install_count,2)
                before=list(c.model.keys)
                with self.assertRaises(ValueError):c.run()
                self.assertEqual(c.model.keys,before)
    def test_focus_failure_retires_before_launching_any_core(self):
        c=CatalogCapture(True);c.model.fail_focus=True
        with self.assertRaises(ValueError):c.run()
        self.assertTrue(c.client.retired);self.assertEqual(c.client.loads,0)
        self.assertIn(c.model.keys[-1],(b'\x1b[C',b'\x1b[D'))
    def test_wrong_second_activation_is_not_accepted_as_a_completed_cycle(self):
        c=CatalogCapture(True);c.model.bad_second_launch=True
        with self.assertRaises(ValueError):c.run()
        self.assertTrue(c.client.retired);self.assertEqual(c.client.loads,1)
        self.assertEqual(c.client.records[-1]['action'],'launch')
        before=list(c.model.keys)
        with self.assertRaises(ValueError):c.client.key('quit')
        self.assertEqual(c.model.keys,before)
    def test_invalid_source_selection_or_open_controls_issue_no_core_key(self):
        c=CatalogCapture(True)
        for index in (-1,2,True,None):
            with self.subTest(index=index),self.assertRaises(ValueError):c.client.open_source(index)
        self.assertEqual(c.model.keys,[])
        c.client.open_source(0);before=list(c.model.keys)
        for op in ('focus','launch'):
            with self.subTest(op=op),self.assertRaises(ValueError):c.client.key(op,0)
        self.assertEqual(c.model.keys,before)

    def test_ambiguous_or_unexpected_prior_library_rejects_before_any_key(self):
        m=Model()
        options=dict(programs=PROGRAMS,installed=[False,False],initial_titles=['ZX81'],
            build_record=m.profile,build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],exchange=m.exchange,owns=lambda:True)
        with self.assertRaises(ValueError):installer_client.InstallerClient(m.initial,**options)
        self.assertEqual(m.keys,[])
    def test_uart_only_has_no_synthetic_usb_report(self):
        c=CatalogCapture(shared=False);r=c.run()
        self.assertIsNone(r['service']['shared_usb_reports']);replay(bytes(c.raw),r,discovered=False,**c.options)

if __name__=='__main__':unittest.main()
