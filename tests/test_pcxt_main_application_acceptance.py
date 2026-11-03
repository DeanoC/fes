import copy
import unittest
from types import SimpleNamespace
from unittest import mock

from scripts import pcxt_main_application_acceptance as main


class MainApplicationTest(unittest.TestCase):
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
                     mock.patch.object(main.app,'run_game',side_effect=lambda *_:calls.append('game')), \
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
