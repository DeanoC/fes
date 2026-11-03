import hashlib
import copy
import json
import os
from pathlib import Path
import socket
import sys
import tempfile
import time
import unittest
from unittest import mock
from types import SimpleNamespace

from scripts import pcxt_floppy_persistence_acceptance as p
from tests.test_pcxt_dos_keyboard_acceptance import frame
from tests.test_pcxt_dos_application_acceptance import FakeRunner


def owned():
    return SimpleNamespace(session_id='session', target_id='target', target_name='kit',
                           game_id='fpga-pcxt-persist-test-1234567890abcdef-0123456789ab', package_id='a'*64,
                           generation=9, flight_id='newflight')


def session(revision='absent'):
    return {'core_package': {'abi': {'id':'fes.computer','major':1,'minor':1},
            'persistence_mode':'persistent',
            'active_interfaces':[{'id':'fes.media.pcxt-floppy','major':1,'minor':0},
                                 {'id':'fes.media.pcxt-floppy-write','major':1,'minor':0}],
            'media_units':[{'unit':0, 'interface':{'id':'fes.media.pcxt-floppy','major':1,'minor':0},
                            'min_bytes':368640,'max_bytes':368640,'chunk_bytes':512,'state':'ready',
                            'persistence':{'mode':'persistent','game_id':owned().game_id,
                                           'base_media_id':p.DISK_SHA,'revision':revision}}]}}


class PersistenceAdmissionTest(unittest.TestCase):
    def test_requires_isolated_game_and_exact_writable_binding(self):
        self.assertEqual(p.unit_binding(session(), owned(), 'absent'), 'absent')
        for mutate in (
            lambda s: s['core_package']['active_interfaces'].pop(),
            lambda s: s['core_package'].update(persistence_mode='volatile'),
            lambda s: s['core_package']['media_units'][0].update(saveable=False),
            lambda s: s['core_package']['media_units'][0].update(saveable=0),
            lambda s: s['core_package']['media_units'][0].update(saveable='yes'),
            lambda s: s['core_package']['media_units'][0]['persistence'].update(game_id='regular-game'),
            lambda s: s['core_package']['media_units'][0]['persistence'].update(base_media_id='b'*64),
            lambda s: s['core_package']['media_units'][0]['persistence'].update(revision='broken'),
            lambda s: s['core_package']['media_units'][0].update(state='empty'),
            lambda s: s['core_package']['media_units'][0].update(unit=False),
            lambda s: s['core_package']['abi'].update(major=True),
            lambda s: s['core_package']['abi'].update(minor=True),
            lambda s: s['core_package']['media_units'][0]['interface'].update(major=True),
            lambda s: s['core_package']['active_interfaces'][0].update(major=True),
        ):
            item=session();mutate(item)
            with self.assertRaises(p.pa.AcceptanceError):
                p.unit_binding(item, owned())
        with self.assertRaises(p.pa.AcceptanceError):
            p.unit_binding(session('c'*64), owned(), 'absent')

    def test_each_observation_rechecks_private_host_and_writable_media(self):
        args=SimpleNamespace(host_pid=1,host_api='http://127.0.0.1:1234',host_binary='/host')
        runner=SimpleNamespace(api=SimpleNamespace(timeout=20),session=mock.Mock(return_value=session()))
        claim={'owner':'owner'}
        outcomes=iter([(claim, False), (claim, True)])
        def observe(r,o,a,disk,*,session_guard):
            session_guard(r.session(),r.session(o.target_name))
            return next(outcomes)
        original=mock.Mock(side_effect=observe)
        def guarded(r, o, a, disk): return p.guarded_observation(original, r, o, a, disk)
        with mock.patch.object(p, 'running_private_host') as host, \
             mock.patch.object(p, 'unit_binding', wraps=p.unit_binding) as binding, \
             mock.patch.object(p.app, '_validate_active_once', side_effect=guarded), \
             mock.patch.object(p.app.time, 'sleep') as sleep:
            result=p.app._observe_until(runner, owned(), args, p.DISK_SHA, p.app.time.monotonic()+5)
        self.assertEqual(result, claim)
        self.assertEqual(host.call_count, 2)
        self.assertEqual(binding.call_count, 4)
        sleep.assert_called_once()

    def test_changed_media_during_unknown_selection_fails_before_wait(self):
        args=SimpleNamespace(host_pid=1,host_api='http://127.0.0.1:1234',host_binary='/host')
        invalid=session(); invalid['core_package']['persistence_mode']='volatile'
        runner=SimpleNamespace(api=SimpleNamespace(timeout=20),session=mock.Mock(return_value=invalid))
        def observe(r,o,a,disk,*,session_guard):
            session_guard(r.session(),r.session(o.target_name))
            return ({'owner':'owner'},False)
        original=mock.Mock(side_effect=observe)
        def guarded(r, o, a, disk): return p.guarded_observation(original, r, o, a, disk)
        with mock.patch.object(p, 'running_private_host'), \
             mock.patch.object(p.app, '_validate_active_once', side_effect=guarded), \
             mock.patch.object(p.app.time, 'sleep') as sleep:
            with self.assertRaises(p.pa.AcceptanceError):
                p.app._observe_until(runner, owned(), args, p.DISK_SHA, p.app.time.monotonic()+5)
        self.assertEqual(original.call_count,1)
        sleep.assert_not_called()

    def test_real_identity_guard_reuses_both_session_snapshots(self):
        owner = owned()
        runner = FakeRunner()
        value = {'id':owner.session_id, 'target':owner.target_name, 'target_id':owner.target_id,
                 'flight_id':owner.flight_id, 'game_id':owner.game_id, 'state':'active',
                 'execution':'fpga_native', 'core_package':session()['core_package']}
        value['core_package'].update(core_id='fes.pcxt-dos',package_id=owner.package_id,generation=owner.generation)
        value['core_package']['active_interfaces'].append({'id':'fes.keyboard.hid','major':1,'minor':0})
        runner.entry.update(game_id=owner.game_id,package_id=owner.package_id,media_id=p.DISK_SHA)
        runner.package_id=owner.package_id
        runner.api.settings={'selected_target':owner.target_name,'targets':[{'name':owner.target_name,'target_id':owner.target_id,'enabled':True}]}
        runner.api.health['target']['connection'].update(target_id=owner.target_id,boot_id='boot-1')
        args=SimpleNamespace(host_pid=1,host_api='fixture',host_binary='fixture',kit='A',owner='owner',
                             target_id=owner.target_id,queue_lock_fd=1,config='fixture',expected_boot='boot-1',
                             bundle_id='a'*64,image_sha256='b'*64)
        expected=runner.session_identity(value,'fixture')
        self.assertEqual(expected.session_id,owner.session_id)
        original=p.app._validate_active_once
        def execute(first,second):
            runner.session=mock.Mock(side_effect=[first,second])
            with mock.patch.object(p,'running_private_host'), mock.patch.object(p.app,'check_claim',return_value={'owned':True}) as claim, \
                 mock.patch.object(p.app,'source_provenance'),mock.patch.object(p.app,'check_private_config'), \
                 mock.patch.object(p.app.visual,'load_font',return_value=b'font'):
                try:
                    return p.guarded_observation(original,runner,expected,args,p.DISK_SHA),claim.call_count
                finally:
                    self.assertEqual(runner.session.call_count,2)
        self.assertEqual(execute(copy.deepcopy(value),copy.deepcopy(value)),(({'owned':True},True),2))
        for index in (0,1):
            invalid=[copy.deepcopy(value),copy.deepcopy(value)]
            invalid[index]['core_package']['media_units'][0]['persistence']['base_media_id']='f'*64
            with self.subTest(invalid_snapshot=index),self.assertRaises(p.pa.AcceptanceError):execute(*invalid)
        divergent=copy.deepcopy(value)
        divergent['core_package']['media_units'][0]['persistence']['revision']='c'*64
        with self.assertRaisesRegex(p.pa.AcceptanceError,'revision changed'):
            execute(copy.deepcopy(value),divergent)
        wrong_target=copy.deepcopy(value); wrong_target['flight_id']='different-flight'
        with self.assertRaisesRegex(p.pa.AcceptanceError,'session identity'):
            execute(copy.deepcopy(value),wrong_target)
        # Exercise the real hard-deadline fork with scaled independent I/O
        # delays and prove that the revised guard performs only two sessions.
        calls=[]
        def timed_session(target=None):
            calls.append(target)
            time.sleep(0.1118 if target is None else 0.0522)
            return copy.deepcopy(value)
        runner.session=timed_session
        original_get=runner.api.get
        def timed_get(path):
            if path == '/api/v1/health': time.sleep(0.2424)
            return original_get(path)
        runner.api.get=timed_get
        def timed_claim(*_args): return {'owned':True,'session_count':len(calls)}
        with mock.patch.object(p,'running_private_host'), mock.patch.object(p.app,'check_claim',side_effect=timed_claim), \
             mock.patch.object(p.app,'source_provenance'),mock.patch.object(p.app,'check_private_config'), \
             mock.patch.object(p.app.visual,'load_font',return_value=b'font'):
            result=p.app._hard_deadline_observation(
                lambda _deadline:p.guarded_observation(original,runner,expected,args,p.DISK_SHA)[0],seconds=5.0)
        self.assertEqual(result,{'owned':True,'session_count':2})

    def test_current_pr_and_local_checkout_must_match_explicit_source(self):
        args=SimpleNamespace(source_repository='Example/fes',source_pr=42,source_base='main',source_commit='a'*40)
        good={'state':'open','head':{'sha':'a'*40,'repo':{'full_name':'Example/fes'}},'base':{'ref':'main','repo':{'full_name':'Example/fes'}}}
        def command(argv, deadline, cwd=None):
            if argv[:3] == ['git','rev-parse','HEAD']: return b'a'*40+b'\n'
            if argv[:2] == ['git','show']:
                return (Path(p.__file__).resolve().parent.parent / argv[2].split(':',1)[1]).read_bytes()
            self.assertEqual(argv,['gh','api','repos/Example/fes/pulls/42'])
            return json.dumps(good).encode()
        with mock.patch.object(p,'bounded_source_command',side_effect=command):
            p.require_pr_head(args)
            args.source_commit='b'*40
            with self.assertRaises(p.pa.AcceptanceError): p.require_pr_head(args)
            args.source_commit='a'*40
            good['state']='closed'
            with self.assertRaises(p.pa.AcceptanceError): p.require_pr_head(args)
            good['state']='open'; good['head']['repo']['full_name']='fork/fes'
            p.require_pr_head(args)
            good['base']['repo']['full_name']='other/fes'
            with self.assertRaises(p.pa.AcceptanceError): p.require_pr_head(args)
            good['base']['repo']['full_name']='Example/fes'; args.source_repository='../injected'
            with self.assertRaises(p.pa.AcceptanceError): p.require_pr_head(args)
        args.source_repository='Example/fes'
        def altered(argv, deadline, cwd=None):
            if argv[1]=='rev-parse': return b'a'*40
            if argv[1]=='show':
                data=(Path(p.__file__).resolve().parent.parent / argv[2].split(':',1)[1]).read_bytes()
                return data+b'diff' if argv[2].endswith('pcxt_health_evidence.py') else data
            return json.dumps(good).encode()
        # Byte comparison fails even if Git status would hide a changed file.
        with mock.patch.object(p,'bounded_source_command',side_effect=altered):
            with self.assertRaises(p.pa.AcceptanceError): p.require_pr_head(args)

    def test_source_receipts_must_match_exact_package(self):
        good={'host':{'source_commit':'a'*40},'software':{'source_commit':'a'*40}}
        p.check_matching_sources(good,'a'*40)
        good['software']['source_commit']='b'*40
        with self.assertRaises(p.pa.AcceptanceError):
            p.check_matching_sources(good,'a'*40)

    def test_exact_private_process_must_own_loopback_listener(self):
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',0));sock.listen()
            origin=f'http://127.0.0.1:{sock.getsockname()[1]}'
            self.assertEqual(p.running_private_host(os.getpid(),origin,sys.executable)['pid'],os.getpid())
            for bad in (origin+'/other',origin+'?q=1',origin.replace('127.0.0.1','localhost')):
                with self.assertRaises(p.pa.AcceptanceError):
                    p.running_private_host(os.getpid(),bad,sys.executable)
            with self.assertRaises(p.pa.AcceptanceError):
                p.running_private_host(os.getpid(),origin,'/dev/null')

    def test_restart_receipt_requires_new_flight_generation_and_revision(self):
        args=SimpleNamespace(source_commit='c'*40,archive_sha256='d'*64,package_id='a'*64,
                             rbf_sha256='e'*64,bundle_id='f'*64,image_sha256='1'*64,
                             target_id='target',expected_boot='boot')
        rec={'classification':'physical diagnostic: saved; restart pending','scenario':'com',
             'source_commit':args.source_commit,'archive_sha256':args.archive_sha256,
             'package_id':args.package_id,'rbf_sha256':args.rbf_sha256,'bundle_id':args.bundle_id,
             'base_image_sha256':args.image_sha256,'target_id':'target','boot_id':'boot',
             'disk':{'floppy_sha256':p.DISK_SHA},'saved_revision':'9'*64,'provenance':{'same':True},
             'session':{'id':'oldsession','target':'kit','target_id':'target','game_id':owned().game_id,
                        'package_id':'a'*64,'generation':8,'flight_id':'oldflight'}}
        with tempfile.TemporaryDirectory() as tmp:
            file=Path(tmp)/'receipt.json'
            def check():
                data=json.dumps(rec).encode();file.write_bytes(data)
                return p.previous_receipt(file,hashlib.sha256(data).hexdigest(),args,owned(),{'same':True})
            self.assertEqual(check()[0]['saved_revision'],'9'*64)
            for name,value in (('generation',9),('generation',True),('id','session'),('flight_id','newflight'),('target_id','other')):
                old=rec['session'][name];rec['session'][name]=value
                with self.assertRaises(p.pa.AcceptanceError): check()
                rec['session'][name]=old
            rec['saved_revision']='absent'
            with self.assertRaises(p.pa.AcceptanceError): check()

    def test_save_uses_exact_binding_once_and_checks_revision(self):
        expected=owned(); after=session('9'*64)
        class API:
            timeout=20
            calls=[]
            def request(self,*args):
                self.calls.append(args);return after
        class Runner:
            api=API()
            def session(self):return session('absent') if not self.api.calls else after
            def session_identity(self,value,context):return expected
        runner=Runner()
        with mock.patch.object(p.app,'validate_active'), mock.patch.object(p,'require_pr_head'):
            self.assertEqual(p.save_once(runner,expected,SimpleNamespace(source_commit='a'*40,phase_deadline_monotonic=p.time.monotonic()+120),{}),'9'*64)
        self.assertEqual(len(runner.api.calls),1)
        method,path,body,headers=runner.api.calls[0]
        self.assertEqual((method,path,body),('POST','/api/v1/session/disk/save',b''))
        self.assertEqual(headers['X-FogCast-Session-ID'],'session')
        self.assertEqual(headers['X-FogCast-Core-Generation'],'9')
        self.assertEqual(headers['X-FogCast-Package-ID'],'a'*64)
        self.assertEqual(headers['X-FogCast-Target-ID'],'target')
        self.assertEqual(headers['X-FogCast-Media-Unit'],'0')

    def test_rejected_save_keeps_only_unverified_bounded_observation(self):
        expected=owned(); after=session('9'*64)
        after.update({'state':'active','execution':'fpga_development', 'id':expected.session_id,
                      'target_id':expected.target_id,'target':expected.target_name,
                      'flight_id':expected.flight_id,'game_id':expected.game_id,
                      'untrusted':'fixture-secret'})
        after['core_package'].update(package_id=expected.package_id,generation=expected.generation)
        class API:
            timeout=20
            def __init__(self):self.calls=[]
            def request(self,*args):self.calls.append(args);return after
        class Runner:
            def __init__(self):self.api=API()
            def session(self):return session('absent')
            def session_identity(self,value,context):return p.pa._session_identity(value,context)
        runner=Runner();rec={}
        with mock.patch.object(p.app,'validate_active') as active, mock.patch.object(p,'require_pr_head'):
            with self.assertRaisesRegex(p.pa.AcceptanceError,'failed owned session identity validation'):
                p.save_once(runner,expected,SimpleNamespace(source_commit='a'*40,phase_deadline_monotonic=p.time.monotonic()+120),rec)
        self.assertEqual(len(runner.api.calls),1)
        active.assert_called_once()
        observation=rec['save_response_observation']
        self.assertEqual(observation['trust'],'unverified')
        self.assertEqual(observation['execution'],'fpga_development')
        self.assertEqual(observation['unverified_revision'],'9'*64)
        self.assertTrue(observation['session_id_matches'])
        self.assertTrue(observation['unit_identity_matches'])
        self.assertTrue(observation['binding_game_matches'])
        self.assertNotIn('saved_revision',rec)
        self.assertNotIn('save_response',rec)
        self.assertNotIn('fixture-secret',json.dumps(rec))
        self.assertEqual(runner.api.timeout,20)

    def test_save_identity_rejection_redacts_exception_and_never_accepts_revision(self):
        expected=owned(); after=session('8'*64);after['execution']='fpga_native'
        class API:
            timeout=20
            def __init__(self):self.calls=[]
            def request(self,*args):self.calls.append(args);return after
        class Runner:
            def __init__(self):self.api=API()
            def session(self):return session('absent')
            def session_identity(self,value,context):raise p.pa.AcceptanceError('untrusted fixture-secret')
        rec={};runner=Runner()
        with mock.patch.object(p.app,'validate_active'),mock.patch.object(p,'require_pr_head'):
            with self.assertRaises(p.pa.AcceptanceError) as error:
                p.save_once(runner,expected,SimpleNamespace(source_commit='a'*40,phase_deadline_monotonic=p.time.monotonic()+120),rec)
        self.assertNotIn('fixture-secret',repr(error.exception))
        self.assertNotIn('saved_revision',rec)
        self.assertEqual(rec['save_response_observation']['unverified_revision'],'8'*64)
        self.assertEqual(len(runner.api.calls),1)

    def test_save_observation_redacts_malformed_and_untrusted_values(self):
        expected=owned()
        self.assertEqual(p.save_response_observation(['secret'],expected),
                         {'trust':'unverified','response_object':False})
        for secret in ('fixture-secret','bad\\nvalue','9'*63):
            item=session(secret)
            item.update(execution=secret,id=secret,target_id=secret,target=secret,
                        flight_id=secret,game_id=secret,state=secret)
            item['core_package'].update(package_id=secret,generation=secret)
            observation=p.save_response_observation(item,expected)
            self.assertEqual(observation['execution'],'other_or_missing')
            self.assertEqual(observation['revision_kind'],'other_or_missing')
            self.assertNotIn('unverified_revision',observation)
            self.assertNotIn(secret,json.dumps(observation))
            self.assertFalse(observation['session_id_matches'])
            self.assertFalse(observation['generation_matches'])

    def test_observation_never_attributes_another_or_malformed_unit_revision(self):
        for mutate in (
            lambda s:s['core_package']['media_units'].append(s['core_package']['media_units'][0].copy()),
            lambda s:s['core_package']['media_units'][0].update(unit=1),
            lambda s:s['core_package']['media_units'][0].update(unit=False),
            lambda s:s['core_package']['media_units'][0]['interface'].update(major=True),
            lambda s:s['core_package']['media_units'][0]['interface'].update(minor=False),
            lambda s:s['core_package']['abi'].update(major=True),
            lambda s:s['core_package']['abi'].update(minor=True),
            lambda s:s['core_package']['media_units'][0].update(interface={'id':'other'}),
            lambda s:s['core_package']['media_units'][0].update(state='empty'),
            lambda s:s['core_package'].update(media_units=[None]),
            lambda s:s['core_package'].update(media_units=[]),
            lambda s:s['core_package'].update(abi={'id':'other'}),
        ):
            item=session('9'*64);mutate(item)
            observation=p.save_response_observation(item,owned())
            self.assertFalse(observation['unit_identity_matches'])
            self.assertFalse(observation['binding_game_matches'])
            self.assertFalse(observation['binding_base_matches'])
            self.assertEqual(observation['revision_kind'],'other_or_missing')
            self.assertNotIn('unverified_revision',observation)

    def test_guest_control_requires_marker_and_later_prompt(self):
        font=p.app.visual.load_font()
        start=frame(font,{2:'A:\\>'})
        anchor=p.app.visual.find_prompt(*start,font)
        output=frame(font,{2:'A:\\>TYPE FESXT.TXT',3:'FES PC/XT DOS BOOT',4:'A:\\>'})
        lines=p.app.read_anchored(output,font,anchor,start[:2])
        self.assertTrue(any(p.MARKER in line.upper() for line in lines))
        with tempfile.TemporaryDirectory() as tmp:
            args=SimpleNamespace(evidence=Path(tmp))
            rec={'captures':[]}
            with mock.patch.object(p.app,'capture_owned',return_value=(output,{'phase':'test'})):
                proof=p.wait_for_output(None,None,args,rec,font,anchor,start[:2],'TYPE FESXT.TXT',p.MARKER,'test')
            self.assertLess(proof['command_row'],proof['marker_row'])
            self.assertLess(proof['marker_row'],proof['prompt_row'])

    def test_output_requires_exact_command_marker_and_prompt(self):
        for command in ('TYPE FESXT.TXT', 'TYPE FESSAVE.TXT'):
            good=['A:\\>'+command, p.MARKER, 'A:\\>']
            with self.subTest(command=command):
                proof=p.output_proof(good,command,p.MARKER)
                self.assertEqual((proof['command_row'],proof['marker_row'],proof['prompt_row']),(0,1,2))
                for bad in (
                    [p.MARKER, 'A:\\>'+command, 'A:\\>'],
                    ['A:\\>TYPE WRONG.TXT', p.MARKER, 'A:\\>'],
                    ['A:\\>'+command+'X', p.MARKER, 'A:\\>'],
                    ['A:\\>'+command+' '+p.MARKER, 'A:\\>'],
                    ['A:\\>'+command, 'prefix '+p.MARKER, 'A:\\>'],
                    ['A:\\>'+command, p.MARKER, 'OTHER A:\\>'],
                    ['A:\\>'+command, 'A:\\>TYPE WRONG.TXT', p.MARKER, 'A:\\>'],
                    ['A:\\>'+command, 'A:\\>', p.MARKER],
                ):
                    with self.subTest(bad=bad): self.assertIsNone(p.output_proof(bad,command,p.MARKER))

    def test_copy_requires_exact_command_mapping_and_later_prompt(self):
        command='A:\\>COPY FESXT.TXT FESSAVE.TXT'
        for arrow in ('=>', '=>>'):
            lines=['A:\\>TYPE FESXT.TXT', 'FES PC/XT DOS BOOT', command,
                   'fesxt.txt '+arrow+' fessave.txt', 'A:\\>']
            proof=p.copy_output_proof(lines)
            self.assertEqual((proof['command_row'],proof['mapping_row'],proof['prompt_row']),(2,3,4))
        for lines in (
            ['A:\\>', 'FESXT.TXT =>> FESSAVE.TXT', command],
            [command, 'FESXT.TXT =>> FESSAVE.TXT'],
            [command, 'FESXT.TXT =>> WRONG.TXT', 'A:\\>'],
            [command+'X', 'FESXT.TXT =>> FESSAVE.TXT', 'A:\\>'],
            [command, 'OTHERFESXT.TXT =>> FESSAVE.TXT', 'A:\\>'],
            [command, 'FESXT.TXT =>>> FESSAVE.TXT', 'A:\\>'],
            [command, 'FESXT.TXT =>> FESSAVE.TXT', 'A:\\>ERROR'],
        ):
            with self.subTest(lines=lines): self.assertIsNone(p.copy_output_proof(lines))

    def test_copy_glyph_capture_is_guarded_and_requires_later_prompt(self):
        font=p.app.visual.load_font()
        start=frame(font,{2:'A:\\>'})
        anchor=p.app.visual.find_prompt(*start,font)
        output=frame(font,{2:'A:\\>COPY FESXT.TXT FESSAVE.TXT',
                           3:'FESXT.TXT =>> FESSAVE.TXT',4:'A:\\>'})
        with tempfile.TemporaryDirectory() as tmp:
            args=SimpleNamespace(evidence=Path(tmp)); rec={'captures':[]}
            with mock.patch.object(p.app,'capture_owned',return_value=(output,{'phase':'copy'})) as capture:
                proof=p.wait_for_copy(None,None,args,rec,font,anchor,start[:2])
            self.assertLess(proof['command_row'],proof['mapping_row'])
            self.assertLess(proof['mapping_row'],proof['prompt_row'])
            capture.assert_called_once()
            incomplete=frame(font,{2:'A:\\>COPY FESXT.TXT FESSAVE.TXT',
                                   3:'FESXT.TXT =>> FESSAVE.TXT'})
            with mock.patch.object(p.app,'capture_owned',return_value=(incomplete,{'phase':'copy'})), \
                 mock.patch.object(p.time,'monotonic',side_effect=[0,121]):
                with self.assertRaisesRegex(p.pa.AcceptanceError,'later prompt'):
                    p.wait_for_copy(None,None,args,{'captures':[]},font,anchor,start[:2])

    def test_commands_are_real_underscore_free_dos_names(self):
        self.assertEqual(p.usages('TYPE FESXT.TXT\n')[-1],40)
        self.assertEqual(p.usages('COPY FESXT.TXT FESSAVE.TXT\n')[-1],40)
        with self.assertRaises(ValueError): p.usages('Bad!')


class CursorProof(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.font=p.app.visual.load_font()
  cls.original=frame(cls.font,{2:'A:\\>'})
  cls.anchor=p.app.visual.find_prompt(*cls.original,cls.font)
  cls.command='A:\\>COPY FESXT.TXT FESSAVE.TXT'
  cls.mapping='FESXT.TXT =>> FESSAVE.TXT'
 def cursor_frame(self,extra=None):
  f=frame(self.font,{2:self.command,3:self.mapping,4:'A:\\>'})
  w,h,data=f;rgb=bytearray(data);a=self.anchor
  for gy in (6,7):
   for gx in range(8):
    sx,sy=4*8+gx,4*8+gy
    x=round((160+(sx+.5)*1.5)*w/1280)+a['alignment_dx'];y=round((sy+.5)*h/200)+a['alignment_dy']
    rgb[(y*w+x)*3:(y*w+x)*3+3]=bytes(a['foreground_rgb'])
  if extra:
   gy,gx,color=extra;sx,sy=4*8+gx,4*8+gy
   x=round((160+(sx+.5)*1.5)*w/1280)+a['alignment_dx'];y=round((sy+.5)*h/200)+a['alignment_dy']
   rgb[(y*w+x)*3:(y*w+x)*3+3]=bytes(color)
  return w,h,bytes(rgb)
 def proof(self,f):
  lines=p.app.read_anchored(f,self.font,self.anchor,f[:2])
  return lines,p.copy_output_proof(lines,frame=f,anchor=self.anchor)
 def test_cursor_pixels_accept_but_literal_question_mark_rejected(self):
  f=self.cursor_frame();lines,proof=self.proof(f)
  self.assertEqual(lines[4].strip(),'A:\\>?');self.assertIsNotNone(proof)
  literal=frame(self.font,{2:self.command,3:self.mapping,4:'A:\\>?'})
  lines,proof=self.proof(literal);self.assertEqual(lines[4].strip(),'A:\\>?');self.assertIsNone(proof)
 def test_output_prompt_cursor_requires_actual_underline(self):
  command='TYPE FESSAVE.TXT'
  lines=['','', 'A:\\>'+command, p.MARKER, 'A:\\>?']
  self.assertIsNotNone(p.output_proof(lines,command,p.MARKER,frame=self.cursor_frame(),anchor=self.anchor))
  literal=frame(self.font,{2:'A:\\>'+command,3:p.MARKER,4:'A:\\>?'})
  self.assertIsNone(p.output_proof(lines,command,p.MARKER,frame=literal,anchor=self.anchor))
 def test_cursor_shape_must_match_exactly(self):
  for extra in ((0,0,self.anchor['foreground_rgb']),(6,0,self.anchor['background_rgb'])):
   with self.subTest(extra=extra):
    f=self.cursor_frame(extra);self.assertFalse(p.underline_cursor(f,self.anchor,4,4))
    lines=p.app.read_anchored(f,self.font,self.anchor,f[:2]);self.assertIsNone(p.copy_output_proof(lines,frame=f,anchor=self.anchor))
 def test_command_mapping_and_order_are_still_required(self):
  f=self.cursor_frame();lines,_=self.proof(f)
  for mutate in (lambda x:x.__setitem__(2,self.command+'X'),lambda x:x.__setitem__(3,'FESXT.TXT =>> WRONG.TXT'),
                 lambda x:x.__setitem__(3,'FESXT.TXT =>>> FESSAVE.TXT'),lambda x:x.__setitem__(4,'A:\\>? ERROR'),
                 lambda x:(x.__setitem__(2,self.mapping),x.__setitem__(3,self.command))):
   bad=list(lines);mutate(bad);self.assertIsNone(p.copy_output_proof(bad,frame=f,anchor=self.anchor))
  self.assertIsNone(p.copy_output_proof(lines))
 def test_plain_prompt_still_works_and_malformed_pixels_fail_closed(self):
  lines=[self.command,self.mapping,'A:\\>'];self.assertIsNotNone(p.copy_output_proof(lines))
  lines[-1]='A:\\>?'
  self.assertIsNone(p.copy_output_proof(lines,frame=(1,1,b''),anchor=self.anchor))
  self.assertIsNone(p.copy_output_proof(lines,frame=self.cursor_frame(),anchor=None))
 def test_wait_uses_only_guarded_capture_and_does_not_save(self):
  f=self.cursor_frame()
  with tempfile.TemporaryDirectory() as tmp:
   args=SimpleNamespace(evidence=Path(tmp));rec={'captures':[]}
   with mock.patch.object(p.app,'capture_owned',return_value=(f,{'phase':'copy'})) as capture, mock.patch.object(p,'save_once') as save:
    proof=p.wait_for_copy(None,None,args,rec,self.font,self.anchor,f[:2])
   capture.assert_called_once();save.assert_not_called()
   self.assertLess(proof['command_row'],proof['mapping_row']);self.assertLess(proof['mapping_row'],proof['prompt_row'])


class HealthObserverIntegration(unittest.TestCase):
 def fixture(self, evidence):
  return SimpleNamespace(evidence=Path(evidence),target_id='target',expected_boot='boot',bundle_id='a'*64,image_sha256='b'*64)
 def execute_fixture(self,args,*,fail=False):
  class Parser:
   def add_argument(self,*a,**k):pass
   def parse_args(self,argv):return args
  response={'ready':True,'target':{'reachable':False,'ready':False}}
  class API:
   def get(self,path,**kwargs):return response
  class Runner:
   def __init__(self,_):self.api=API()
  def validate(runner,*unused):
   pid=os.fork()
   if pid==0:
    try: runner.api.get('/api/v1/health');os._exit(0)
    except BaseException:os._exit(19)
   _,status=os.waitpid(pid,0)
   if status:raise p.pa.AcceptanceError('forked observation failed')
  def appmain(_):
   runner=p.app.pa.Runner(None)
   p.app.validate_active(runner)
  patches=(mock.patch.object(p.app,'parser',return_value=Parser()),mock.patch.object(p.app.pa,'Runner',Runner),
           mock.patch.object(p.app,'validate_active',validate),mock.patch.object(p.app,'main',side_effect=appmain),
           mock.patch.object(p.app,'install_signal_handler'))
  with patches[0],patches[1],patches[2],patches[3],patches[4]:
   if fail:
    with mock.patch.object(p.health_evidence.HealthEvidence,'record',side_effect=OSError('fixture-private-failure')):
     with self.assertRaises(p.pa.AcceptanceError):p.main([])
   else:p.main([])
 def test_nested_forked_observation_is_retained_without_extra_request(self):
  with tempfile.TemporaryDirectory() as d:
   self.execute_fixture(self.fixture(d));rows=(Path(d)/'health-guard-observations.jsonl').read_text().splitlines()
   self.assertEqual(len(rows),1);row=json.loads(rows[0]);self.assertIs(row['target_reachable'],False);self.assertFalse(p.HEALTH_UNSAFE)
 def test_nested_forked_evidence_failure_latches_unsafe(self):
  with tempfile.TemporaryDirectory() as d:
   self.execute_fixture(self.fixture(d),fail=True);self.assertTrue(p.HEALTH_UNSAFE)
 def test_nested_cli_propagates_reserved_unsafe_exit(self):
  def fail():p.HEALTH_UNSAFE=True;raise p.pa.AcceptanceError('fixture')
  with mock.patch.object(p,'main',side_effect=fail):
   with self.assertRaises(SystemExit) as e:p.cli()
  self.assertEqual(e.exception.code,74)


class FloppyStageTrace(unittest.TestCase):
 def test_real_guard_deadline_keeps_unfinished_host_stage(self):
  import time
  with tempfile.TemporaryDirectory() as d:
   observer=p.health_evidence.HealthEvidence(Path(d),'target','boot','bundle','image')
   observer.begin_trace()
   args=SimpleNamespace(host_pid=1,host_api='fixture',host_binary='fixture')
   runner=SimpleNamespace(session=lambda:session())
   with mock.patch.object(p,'running_private_host',side_effect=lambda *_:time.sleep(2)):
    with self.assertRaisesRegex(p.pa.AcceptanceError,'exceeded hard phase deadline'):
     p.app._hard_deadline_observation(lambda _deadline:p.guarded_observation(lambda *_:None,runner,owned(),args,p.DISK_SHA,observer),seconds=0.15)
   observer.end_trace();observer.close()
   rows=[json.loads(x) for x in (Path(d)/observer.TRACE_FILE).read_text().splitlines()]
   self.assertEqual([(r['stage'],r['event']) for r in rows],[('host_guard','start')])
 def test_media_binding_is_traced_and_identity_guards_remain(self):
  with tempfile.TemporaryDirectory() as d:
   observer=p.health_evidence.HealthEvidence(Path(d),'target','boot','bundle','image');observer.begin_trace()
   args=SimpleNamespace(host_pid=1,host_api='fixture',host_binary='fixture');runner=SimpleNamespace(session=lambda:session())
   with mock.patch.object(p,'running_private_host'),mock.patch.object(p.health_evidence,'traced_app_validation',side_effect=lambda _a,_o,fn:fn()):
    def observe(*_,session_guard):
     session_guard(session(),session())
     return 'pass'
    self.assertEqual(p.guarded_observation(observe,runner,owned(),args,p.DISK_SHA,observer),'pass')
   observer.end_trace();observer.close()
   rows=[json.loads(x) for x in (Path(d)/observer.TRACE_FILE).read_text().splitlines()]
   self.assertEqual([(r['stage'],r['event']) for r in rows],[('host_guard','start'),('host_guard','finish'),('media_binding','start'),('media_binding','finish')])
 def test_drain_failure_latches_unsafe(self):
  fixture=HealthObserverIntegration()
  with tempfile.TemporaryDirectory() as d,mock.patch.object(p.health_evidence.HealthEvidence,'end_trace',side_effect=p.health_evidence.EvidenceError('fixture')):
   with self.assertRaises(p.health_evidence.EvidenceError):fixture.execute_fixture(fixture.fixture(d))
   self.assertTrue(p.HEALTH_UNSAFE)


if __name__ == '__main__': unittest.main()
