"""Synthetic installer UI over captured boot/C/menu records, without kit I/O."""
import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import installer_client
import installer_trace
import paired_tenfoot_trace as paired
from test_nano_paired_tenfoot_trace import Model as PairedModel, MENU_OPEN, MENU_CLOSE, wire
from test_nano_program_client import OWNER, SPEC
from test_nano_tenfoot_trace import PROGRAMS


def closed_panel():
    return dict(open=False,index=0,rows=[],busy=False,status='')


class Model(PairedModel):
    def __init__(self,installed=False,shared=True):
        self.preinstalled = self.installed = installed
        self.library_visible = installed
        self.origin = 0
        self.installing = False
        self.panels = dict(settings=closed_panel(),systems=closed_panel())
        self.installer_fault = ''
        super().__init__(shared=shared,protocol='boot-keyboard')

    def state(self,phase=None,gen=None):
        r = super().state(phase,gen)
        n = r['native']
        selected = n['State'] in ('running','returning') or (
            n['State'] == 'loading' and not self.installing and (n['Generation']-self.origin)%2 == 1)
        n['Identity'] = copy.deepcopy(PROGRAMS[0]['identity'] if selected else paired.app.ZERO)
        n['Held'] = n['State'] not in ('running','returning')
        r['titles'] = [PROGRAMS[0]['title']] if self.library_visible else []
        r['focus'] = 0
        return r

    def navigation(self,n=None):
        n = self.state()['native'] if n is None else n
        return wire(installer_trace.PREFIX,dict(scope='nano-installer-view-v1',owner=n['Owner'],
            generation=n['Generation'],state=n['State'],**copy.deepcopy(self.panels)))

    def _initial(self):
        raw = super()._initial()
        at = raw.rfind(paired.MENU)
        return raw[:at]+self.navigation()+raw[at:]

    def client(self,initial=None):
        return installer_client.InstallerClient(self.initial if initial is None else initial,
            programs=[PROGRAMS[0]],installed=[self.preinstalled],
            initial_titles=[PROGRAMS[0]['title']] if self.preinstalled else [],build_record=self.profile,
            build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],
            exchange=self.exchange,owns=lambda:self.service.owned,now=lambda:self.service.clock,
            shared_usb=self.policy if self.shared else None)

    def decorate(self,raw):
        result = b''
        for line in raw.splitlines(keepends=True):
            result += line
            if line.startswith(paired.app.PREFIX):
                result += self.navigation(json.loads(line[len(paired.app.PREFIX):])['native'])
        return result

    def install_wire(self):
        before = self.service.generation
        self.installing = True
        self.panels['systems']['busy'] = True
        self.reports += 8;self.polls += 8
        raw = self.input_action('select',before)
        if self.shared:
            raw += self.usb('pause','ready',before)
        raw += self.raw(self.state('loading',before))
        raw += self.source_lines(MENU_CLOSE,self.source)+self.menu_wire('return')
        self.service.generation += 1
        self.service.phase = 'loading'
        raw += self.raw(self.state())
        self.source += 6;self.menu_index += 1;self.frames = 1
        raw += self.configure(self.source)+self.source_lines(MENU_OPEN,self.source)+self.menu_wire()
        self.service.phase = 'ready'
        if self.shared:
            raw += self.usb('resume','ready',self.service.generation)
        self.panels['systems'].update(busy=False,rows=['library','title','create'],index=0)
        raw += self.raw(self.state())
        self.frames += 1;raw += self.menu_wire()
        self.installed = True;self.origin = 1;self.installing = False
        return self.decorate(raw)

    def exchange(self,key,bound,done):
        if self.service.phase == 'running' or key in (b's',b'q') or key == b'\r' and self.library_visible and not self.panels['systems']['open'] and not self.panels['settings']['open']:
            raw = self.decorate(super().exchange(key,bound,lambda _:True))
        else:
            self.keys.append(key)
            raw = b''
            if key == b'o':
                self.panels['settings'] = dict(open=True,index=0,rows=['section-display','section-updates'],busy=False,status='')
            elif key == b'\x1b[B':
                p = self.panels['systems'] if self.panels['systems']['open'] else self.panels['settings']
                p['index'] = (p['index']+1)%len(p['rows'])
            elif self.panels['settings']['open']:
                p = self.panels['settings']
                if p['rows'][p['index']] == 'section-updates':
                    p.update(index=0,rows=['systems'])
                else:
                    self.panels['settings'] = closed_panel()
                    self.panels['systems'] = dict(open=True,index=0,rows=['refresh','system'],busy=False,status='')
            else:
                p = self.panels['systems'];kind = p['rows'][p['index']]
                if kind == 'system':
                    p.update(index=0,rows=['library','title','create'] if self.installed else ['install'])
                elif kind == 'install':
                    raw = self.install_wire()
                elif kind == 'library':
                    self.library_visible = True
                    self.panels['systems'] = closed_panel()
                else:
                    raise AssertionError('unexpected synthetic row action')
            if not raw:
                self.frames += 1
                # Real navigation often changes no ordinary App fields.
                raw = (self.raw(self.state()) if self.library_visible else b'')+self.navigation()+self.menu_wire()
                if key == b'\r':
                    raw = self.input_action('select',self.service.generation)+raw
            self.service.clock += .01
        f = self.installer_fault
        if f == 'owner':raw = raw.replace(b'"owner":'+str(int(OWNER,16)).encode(),b'"owner":1')
        if f == 'frame':raw = raw.replace(b'"Underflows":0',b'"Underflows":1')
        if f == 'no-usb-resume':raw = b''.join(l for l in raw.splitlines(keepends=True) if b'phase=resume' not in l)
        if f == 'no-usb-pause':raw = b''.join(l for l in raw.splitlines(keepends=True) if b'phase=pause' not in l)
        if f == 'master':raw = raw.replace(b'cancel=0 complete=1',b'cancel=1 complete=0')
        if f in ('no-menu-close','no-menu-open'):
            result=[]
            for line in raw.splitlines(keepends=True):
                if line.startswith(paired.MENU):
                    row=json.loads(line[len(paired.MENU):])
                    if f=='no-menu-close' and row['event']=='return' or f=='no-menu-open' and row['menu']==2 and row['frames']==1:
                        continue
                result.append(line)
            raw=b''.join(result)
        if f == 'view-before-resume':
            lines=raw.splitlines(keepends=True)
            at=next(i for i,l in enumerate(lines) if b'INPUT phase=resume' in l)
            resume=lines.pop(at);lines.insert(len(lines)-1,resume);raw=b''.join(lines)
        if f == 'duplicate-select':
            line=raw.splitlines(keepends=True)[0];raw=line+raw
        if f == 'fetch':raw=raw.replace(self.raw(self.state()),self.fetch()+self.raw(self.state()))
        if f in ('skipped-native','live-install'):
            lines=[]
            for line in raw.splitlines(keepends=True):
                if line.startswith(paired.app.PREFIX):
                    row=json.loads(line[len(paired.app.PREFIX):]);n=row['native']
                    if n['Generation']==1:
                        if f=='skipped-native':n['Generation']=2
                        else:n['Identity']=copy.deepcopy(PROGRAMS[0]['identity'])
                        line=self.raw(row)
                lines.append(line)
            raw=b''.join(lines)
        if f == 'claim':self.service.owned=False
        if f == 'clock':self.service.clock-=1
        if f == 'late':self.service.clock+=bound
        if f == 'partial':raw=raw[:-1]
        for end in (len(raw)//2,len(raw)-1,len(raw),len(raw)):
            done(raw[:end])
        return raw


class InstallerClientTests(unittest.TestCase):
    def test_empty_install_library_launch_return_and_final_park(self):
        m=Model();c=m.client();c.open_source();r=c.install();c.open_library()
        self.assertEqual(r['status'],'verified');self.assertFalse(r['persistence_verified'])
        self.assertEqual(c.view['native']['Generation'],1)
        c.key('launch',0);c.key('quit')
        self.assertTrue(c.retired);self.assertEqual(c.loads,1)
        self.assertEqual(c.records[-1]['exit_generation'],4)
        self.assertEqual(c.menu.finish()['menu_sessions'],3)
        self.assertFalse(r['hardware_acceptance'])

    def test_separately_discovered_boot_never_installs_again(self):
        m=Model(installed=True);c=m.client();c.open_source();c.open_library()
        before=list(m.keys)
        with self.assertRaises(ValueError):c.install()
        self.assertEqual(m.keys,before)
        c.key('launch',0);c.key('quit')
        self.assertEqual(c.records[-1]['exit_generation'],3)
        self.assertEqual(c.menu.finish()['menu_sessions'],2)
        self.assertFalse(any(r['action']=='install' for r in c.records))

    def test_installed_core_can_return_and_launch_again_without_reinstall(self):
        m=Model();c=m.client();c.open_source();c.install()
        before=list(m.keys)
        with self.assertRaises(ValueError):c.key('launch',0)
        self.assertEqual(m.keys,before)
        c.open_library();c.key('launch',0);c.key('stop');c.key('launch',0);c.key('quit')
        self.assertEqual(c.loads,2);self.assertEqual(c.records[-1]['exit_generation'],6)
        self.assertEqual(c.menu.finish()['menu_sessions'],4)
        self.assertEqual(sum(r['action']=='install' for r in c.records),1)

    def test_rejected_operation_before_install_issues_no_key(self):
        m=Model();c=m.client()
        for op in ('launch','stop','quit'):
            with self.subTest(op=op),self.assertRaises(ValueError):c.key(op,0)
        with self.assertRaises(ValueError):c.install()
        self.assertEqual(m.keys,[])

    def test_navigation_failure_retires_without_another_key(self):
        for fault in ('owner','frame','claim','clock','late','partial'):
            with self.subTest(fault=fault):
                m=Model();c=m.client();m.installer_fault=fault
                with self.assertRaises(ValueError):c.open_source()
                self.assertTrue(c.retired)
                before=list(m.keys)
                with self.assertRaises(ValueError):c.open_source()
                self.assertEqual(m.keys,before)

    def test_install_failures_never_allow_launch_or_replay(self):
        for fault in ('no-usb-resume','no-usb-pause','master','frame','owner','claim','clock','late','partial',
                      'no-menu-close','no-menu-open','view-before-resume','duplicate-select','fetch','skipped-native','live-install'):
            with self.subTest(fault=fault):
                m=Model();c=m.client();c.open_source();m.installer_fault=fault
                with self.assertRaises(ValueError):c.install()
                self.assertTrue(c.retired)
                before=list(m.keys)
                with self.assertRaises(ValueError):c.install()
                with self.assertRaises(ValueError):c.key('launch',0)
                self.assertEqual(m.keys,before)

    def test_uart_only_procedure_uses_no_usb_observations(self):
        m=Model(shared=False);c=m.client();c.open_source();r=c.install();c.open_library();c.key('launch',0);c.key('quit')
        self.assertNotIn('shared_usb',r)

    def test_every_complete_line_boundary_is_framing_only(self):
        m=Model();original=m.exchange
        def exchange(key,bound,done):
            raw=original(key,bound,lambda _:False)
            for end in range(1,len(raw)+1):
                if raw[end-1:end]==b'\n':
                    done(raw[:end]);done(raw[:end])
            return raw
        m.exchange=exchange
        c=m.client();c.open_source();c.install();c.open_library();c.key('launch',0);c.key('quit')
        self.assertEqual(c.menu.finish()['menu_sessions'],3)
        self.assertEqual(c.loads,1)

    def test_mode_and_discovered_catalogue_are_explicit(self):
        for supplied in (False,None,1):
            m=Model(installed=True)
            with self.subTest(installed=supplied),self.assertRaises(ValueError):
                installer_client.InstallerClient(m.initial,programs=[PROGRAMS[0]],installed=[supplied],initial_titles=[],
                    build_record=m.profile,build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],
                    atf=SPEC['atf'],package=SPEC['package'],exchange=m.exchange,owns=lambda:True)
            self.assertEqual(m.keys,[])
        m=Model()
        with self.assertRaises(ValueError):paired.observations(m.initial,'uart+shared-usb-boot-keyboard')

    def test_ordinary_two_title_observer_preserves_catalogue_order_tolerance(self):
        m=PairedModel();lines=[]
        for line in m.initial.splitlines(keepends=True):
            if line.startswith(paired.app.PREFIX):
                row=json.loads(line[len(paired.app.PREFIX):]);row['titles'].reverse();line=m.raw(row)
            lines.append(line)
        c=m.client(initial=b''.join(lines))
        self.assertTrue(c._ready(c.view));self.assertFalse(c.installer)

    def test_existing_phase_deadlines_remain_separate(self):
        for phase in ('work','restore','view'):
            with self.subTest(phase=phase):
                m=Model();c=m.client();c.open_source();original=m.exchange
                def exchange(key,bound,done):
                    raw=original(key,bound,lambda _:False)
                    if phase=='work':
                        lines=raw.splitlines(keepends=True)
                        pause=next(i for i,l in enumerate(lines) if b'INPUT phase=pause' in l)
                        done(b''.join(lines[:pause+1]))
                        m.service.clock+=100
                    else:
                        lines=raw.splitlines(keepends=True)
                        arm=next(i for i,l in enumerate(lines) if paired.ARM.match(l))
                        end=arm if phase=='restore' else next(i for i,l in enumerate(lines) if i>arm and l.startswith(paired.MENU))
                        done(b''.join(lines[:end+1]))
                        m.service.clock+=100 if phase=='restore' else 5
                    done(raw)
                    return raw
                m.exchange=exchange;c.exchange=exchange
                with self.assertRaisesRegex(ValueError,'bound|restoration'):c.install()
                self.assertTrue(c.retired)

    def test_a_replaced_growing_capture_retires_before_another_key(self):
        m=Model();c=m.client();original=m.exchange
        def exchange(key,bound,done):
            raw=original(key,bound,lambda _:False)
            done(raw[:len(raw)//2])
            done(raw[:1])
            return raw
        c.exchange=exchange
        with self.assertRaisesRegex(ValueError,'growing capture'):c.open_source()
        self.assertTrue(c.retired);self.assertEqual(len(m.keys),1)


if __name__=='__main__':unittest.main()
