import copy
import hashlib
import json
from pathlib import Path
import tempfile
import threading
import time
import unittest
from types import SimpleNamespace
from unittest import mock

from scripts import pcxt_main_application_acceptance as main
from tests.test_pcxt_dos_application_acceptance import FakeRunner


class MainApplicationTest(unittest.TestCase):
    def test_prestarted_worker_persists_real_health_stage_trace(self):
        with tempfile.TemporaryDirectory() as temp:
            evidence=main.floppy.health_evidence.HealthEvidence(Path(temp),'target','boot','a'*64,'b'*64)
            evidence.prepare()
            def observe(_deadline):
                evidence.begin_trace()
                try:
                    return evidence.run_stage('host_guard', lambda:{'owner':'fixture'})
                finally:
                    evidence.end_trace()
            worker=main.app.PrestartedObserver(observe, seconds=1)
            stop=threading.Event(); capture_thread=threading.Thread(target=stop.wait,daemon=True); capture_thread.start()
            try:
                self.assertEqual(worker.check(),{'owner':'fixture'})
            finally:
                stop.set(); capture_thread.join(timeout=1); worker.close(); evidence.close()
            rows=[json.loads(line) for line in (Path(temp)/evidence.TRACE_FILE).read_text().splitlines()]
            self.assertEqual([row['event'] for row in rows],['start','finish'])
            self.assertTrue(rows[-1]['ok'])

    def test_real_app_main_forwards_game_factories_and_worker_trace(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp); archive=root/'core.fcore'; archive.write_bytes(b'archive')
            template=root/'template.rgb'; template.write_bytes(bytes(32*34*3))
            fake=FakeRunner(); fake.value['input']={'state':'attached','ready':True,'session_id':'input-1'}
            owned=root/'owned.json'; owned.write_text(json.dumps(fake.value))
            evidence=root/'evidence'; source='a'*40
            args=['game','--execute','--kit','A','--owner','owner','--queue-lock-fd','42',
                  '--host-receipt',str(root/'host.json'),'--host-receipt-sha256','1'*64,
                  '--host-binary',str(root/'fogcast-api'),'--software-receipt',str(root/'software.json'),
                  '--software-receipt-sha256','2'*64,'--host-api','http://127.0.0.1:1234',
                  '--config',str(root/'host.toml'),'--owned-session',str(owned),'--archive',str(archive),
                  '--archive-sha256',hashlib.sha256(archive.read_bytes()).hexdigest(),'--rbf-sha256','d'*64,
                  '--package-id',fake.package_id,'--source-commit',source,'--bundle-id','a'*64,
                  '--image-sha256','b'*64,'--target-id','target-1','--expected-boot','boot-1',
                  '--disk',str(root/'disk.img'),'--capture-device','/dev/v4l/by-id/fes-shadowcast-hil',
                  '--evidence',str(evidence),'--source-repository','Example/fes','--source-tool-commit','b'*40,
                  '--host-pid','7','--game-template',str(template),
                  '--game-template-sha256',hashlib.sha256(template.read_bytes()).hexdigest(),
                  '--game-max-host-age-ms','1000']
            manifest={'build':{'id':'f'*32},'abi':{'id':'fes.computer','major':1,'minor':1},
                      'interfaces':[{'id':'fes.keyboard.hid','major':1,'minor':0,'required':True}]}
            provenance={'host':{'source_commit':source},'software':{'source_commit':source},
                        'diagnostic_file_sha256':{'scripts/pcxt_v4l2_capture.c':'c'*64}}
            observer=mock.Mock(compromised=False)
            class StubRunner(FakeRunner):
                def __init__(self,_opts): super().__init__()
            class Delegate:
                def __init__(self,callback): self.callback=callback
                def check(self): return self.callback(time.monotonic()+1)
                def close(self): pass
            called=[]
            def game(_runner,_owned,_args,rec,_input,_font,**kwargs):
                self.assertIsInstance(kwargs['probe'], main.app.gameplay.PlaybackProbe)
                self.assertTrue(callable(kwargs['video_factory']))
                self.assertTrue(callable(kwargs['audio_factory']))
                delegate=kwargs['observer_factory'](lambda _deadline:{'owner':'fixture'})
                self.assertEqual(delegate.check(),{'owner':'fixture'})
                delegate.close(); called.append(True); rec['classification']='review pending'
            disk={'floppy_sha256':main.app.SCENARIOS['game']['floppy']}
            with mock.patch.object(main.floppy,'require_main_head'), \
                 mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
                 mock.patch.object(main.app,'check_claim',return_value={'owner':'fixture'}), \
                 mock.patch.object(main.app,'check_private_config',return_value={}), \
                 mock.patch.object(main.app,'inspect_archive',return_value=manifest), \
                 mock.patch.object(main.app,'canonical_package_id',return_value=fake.package_id), \
                 mock.patch.object(main.app,'verify_disk',return_value=disk), \
                 mock.patch.object(main.app.visual,'load_font',return_value=b'font'), \
                 mock.patch.object(main.app,'source_provenance',return_value=(provenance,b'host',b'software')), \
                 mock.patch.object(main.app,'validate_active'), mock.patch.object(main.app,'input_status'), \
                 mock.patch.object(main.app.shutil,'disk_usage',return_value=SimpleNamespace(free=31*1024**3)), \
                 mock.patch.object(main.app.pa,'Runner',StubRunner), \
                 mock.patch.object(main.app.game_capture,'build_capture_helper',return_value=(root/'helper',{'binary_sha256':'d'*64})), \
                 mock.patch.object(main.app,'PrestartedObserver',side_effect=Delegate), \
                 mock.patch.object(main.app,'run_game',side_effect=game):
                main.main(args)
            self.assertEqual(called,[True])
            self.assertGreaterEqual(observer.begin_trace.call_count,1)
            self.assertEqual(observer.begin_trace.call_count,observer.end_trace.call_count)
            self.assertTrue((evidence/'receipt.json').exists())

    def test_com_and_game_guard_source_receipts_and_owned_host(self):
        for name in ('com', 'game'):
            with self.subTest(scenario=name):
                args=SimpleNamespace(source_repository='Example/fes',source_commit='a'*40,
                    source_tool_commit='b'*40,host_pid=7,host_api='http://127.0.0.1:5',host_binary='/host',
                    evidence='/tmp/unused',target_id='target',expected_boot='boot',bundle_id='c'*64,image_sha256='d'*64)
                class Parser:
                    def add_argument(self,*unused,**kwargs):pass
                    def set_defaults(self,**kwargs):vars(args).update(kwargs)
                    def parse_args(self,unused):return args
                provenance={'host':{'source_commit':args.source_commit},
                            'software':{'source_commit':args.source_commit},'diagnostic_file_sha256':{}}
                calls=[]
                observer=mock.Mock(tracing=False,compromised=False)
                observer.run_stage.side_effect=lambda _name,fn:fn()
                def app_main(unused):
                    parsed=main.app.parser().parse_args([])
                    got,_,_=main.app.source_provenance(parsed,b'font')
                    self.assertEqual(got['main_source']['producer_commit'],'a'*40)
                    self.assertEqual(got['main_source']['tool_commit'],'b'*40)
                    self.assertIn('scripts/pcxt_main_application_acceptance.py',got['diagnostic_file_sha256'])
                    main.app._validate_active_once('runner','owned',parsed,'disk')
                    getattr(main.app,'run_'+name)('runner','owned',parsed,{},'input',b'font')
                def check(_args, *_):calls.append('source')
                with mock.patch.object(main.app,'parser',return_value=Parser()), \
                     mock.patch.object(main.app,'source_provenance',side_effect=lambda *_:(copy.deepcopy(provenance),b'h',b's')), \
                     mock.patch.object(main.app,'_validate_active_once',side_effect=lambda *_:calls.append('active')), \
                     mock.patch.object(main.app,'run_com',side_effect=lambda *_:calls.append('com')), \
                     mock.patch.object(main.app,'run_game',side_effect=lambda *_,**_kw:calls.append('game')), \
                     mock.patch.object(main.app,'main',side_effect=app_main), \
                     mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
                     mock.patch.object(main.floppy,'running_private_host',side_effect=lambda *_:calls.append('host')), \
                     mock.patch.object(main.floppy,'require_main_head',side_effect=check):
                    main.main([])
                self.assertEqual(calls,['source','host','active','source',name,'source'])

    def test_source_admission_failure_prevents_application(self):
        args=SimpleNamespace(source_commit='a'*40)
        class Parser:
            def add_argument(self,*unused,**kwargs):pass
            def parse_args(self,unused):return args
        with mock.patch.object(main.app,'parser',return_value=Parser()), \
             mock.patch.object(main.app,'main') as application, \
             mock.patch.object(main.floppy,'require_main_head',side_effect=main.app.pa.AcceptanceError('drift')):
            with self.assertRaises(main.app.pa.AcceptanceError):main.main([])
            application.assert_not_called()

    def test_historical_mode_records_producer_tip_and_rechecks_each_scenario(self):
        args=SimpleNamespace(source_historical_main=True,source_observed_tip='c'*40,source_minimum_tip='c'*40,
            source_diff_sha256='d'*64,source_repository='Example/fes',source_commit='a'*40,
            source_tool_commit='b'*40,evidence='/tmp/unused',target_id='target',expected_boot='boot',
            bundle_id='e'*64,image_sha256='f'*64)
        class Parser:
            def add_argument(self,*unused,**kwargs):pass
            def set_defaults(self,**kwargs):vars(args).update(kwargs)
            def parse_args(self,unused):return args
        observer=mock.Mock(compromised=False)
        provenance={'host':{'source_commit':args.source_commit},'software':{'source_commit':args.source_commit},
                    'diagnostic_file_sha256':{}}
        def application(_):
            parsed=main.app.parser().parse_args([])
            value,_,_=main.app.source_provenance(parsed,b'font')
            self.assertEqual(value['main_source']['producer_commit'],'a'*40)
            self.assertEqual(value['main_source']['reviewed_tip'],'c'*40)
            self.assertIn('later changes were not hardware-tested',value['main_source']['scope'])
            main.app.run_com(None,None,parsed,{},'input',b'font')
        with mock.patch.object(main.app,'parser',return_value=Parser()), \
             mock.patch.object(main.app,'source_provenance',return_value=(provenance,b'h',b's')), \
             mock.patch.object(main.app,'main',side_effect=application), \
             mock.patch.object(main.app,'run_com'), \
             mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
             mock.patch.object(main.floppy,'require_historical_main') as check:
            def observe(namespace, *_):
                namespace.source_last_observed_tip='c'*40
                namespace.source_tip_observations=getattr(namespace,'source_tip_observations',[])+[{'tip':'c'*40,'monotonic_ns':1,'accepted':True}]
            check.side_effect=observe
            main.main([])
            self.assertEqual(check.call_count,3)

    def test_health_evidence_failure_retains_unsafe_latch(self):
        args=SimpleNamespace(source_commit='a'*40,evidence='/tmp/unused',target_id='target',
                             expected_boot='boot',bundle_id='b'*64,image_sha256='c'*64)
        class Parser:
            def add_argument(self,*unused,**kwargs):pass
            def parse_args(self,unused):return args
        observer=mock.Mock(compromised=False)
        observer.begin_trace.side_effect=OSError('evidence failed')
        with mock.patch.object(main.app,'parser',return_value=Parser()), \
             mock.patch.object(main.app,'main',side_effect=lambda _:main.app.validate_active()), \
             mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
             mock.patch.object(main.floppy,'require_main_head'):
            with self.assertRaises(OSError):main.main([])
            self.assertTrue(main.HEALTH_UNSAFE)
            observer.close.assert_called_once()

    def test_game_worker_failure_retains_unsafe_latch(self):
        args=SimpleNamespace(source_commit='a'*40,evidence='/tmp/unused',target_id='target',
                             expected_boot='boot',bundle_id='b'*64,image_sha256='c'*64)
        class Parser:
            def add_argument(self,*unused,**kwargs): pass
            def set_defaults(self,**kwargs): pass
            def parse_args(self,unused): return args
        observer=mock.Mock(compromised=False)
        delegate=mock.Mock(); delegate.check.side_effect=main.app.pa.AcceptanceError('worker failed')
        def application(_):
            main.app.run_game(None,None,args,{},'input',b'font')
        def game(*_args, **kwargs):
            kwargs['observer_factory'](lambda _deadline:{}).check()
        with mock.patch.object(main.app,'parser',return_value=Parser()), \
             mock.patch.object(main.app,'main',side_effect=application), \
             mock.patch.object(main.app,'run_game',side_effect=game), \
             mock.patch.object(main.app,'PrestartedObserver',return_value=delegate), \
             mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
             mock.patch.object(main.floppy,'require_main_head'):
            with self.assertRaisesRegex(main.app.pa.AcceptanceError,'worker failed'): main.main([])
            self.assertTrue(main.HEALTH_UNSAFE)

    def test_main_drift_after_game_prevents_success(self):
        args=SimpleNamespace(source_commit='a'*40,evidence='/tmp/unused',target_id='target',
                             expected_boot='boot',bundle_id='b'*64,image_sha256='c'*64)
        class Parser:
            def add_argument(self,*unused,**kwargs):pass
            def parse_args(self,unused):return args
        observer=mock.Mock(compromised=False)
        finished=[]
        def application(_):
            main.app.run_game(None,None,args,{},'input',b'font')
            finished.append(True)
        with mock.patch.object(main.app,'parser',return_value=Parser()), \
             mock.patch.object(main.app,'main',side_effect=application), \
             mock.patch.object(main.app,'run_game'), \
             mock.patch.object(main.floppy.health_evidence,'HealthEvidence',return_value=observer), \
             mock.patch.object(main.floppy,'require_main_head',side_effect=[None,None,main.app.pa.AcceptanceError('drift')]):
            with self.assertRaises(main.app.pa.AcceptanceError):main.main([])
            self.assertEqual(finished,[])


if __name__ == '__main__':
    unittest.main()
