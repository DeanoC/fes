"""Caller-bound synthetic reader observations; never hardware admission."""
import copy
import unittest
import test_nano_menu_trace as menu
from test_nano_sdm_input_profile import profile
from test_nano_window_trace import trace

class SecureReaderTraceTests(unittest.TestCase):
    def setUp(self):
        self.fixture=menu.MenuTraceTests();self.fixture.setUp()
        self.route=copy.deepcopy(profile()['sdm_reader_route'])
        self.route['menu_context_sha256']=self.fixture.identity['sha256']
        self.route['menu_access_sha256']=trace.context_digest(self.fixture.contract)
        import window_trace
        self.route_digest=window_trace.reader_route.digest(self.route)
        self.marker=(f'NOTICE:  FES_NANO_READER_ROUTE status=0 owner={menu.OWNER:x} stream=1 secure=1 aruser=3e0 arprot=3 '
                     'bank=secure ns_abort=1 source_retained=1 source_generation=4 generation=5 binding='+self.route_digest+'\n')
        suffix=self.fixture.suffix.replace(' gbpa=0 agbpa=0',' gbpa=100000 agbpa=0').replace('bypass_controls_clear=1','bypass_controls_clear=0')
        self.raw=self.fixture.prefix+''.join(self.marker+line if 'FES_NANO_WINDOW_GATE ' in line else line for line in suffix.splitlines(keepends=True))
        self.assertEqual(self.raw.count('FES_NANO_READER_ROUTE '),2)

    def decode(self,data,**changes):
        return self.fixture.decode(data,retained_reader_route=changes.get('route',self.route))

    def test_explicit_bound_secure_route_preserves_aborted_source_and_menu_return(self):
        result=self.decode(self.raw)
        self.assertTrue(result['complete'],result['errors'])
        self.assertEqual(result['last_checkpoint'],'menu_present_closed')
        self.assertFalse(result['core_load_acceptance'])
        for key in ('memory_released','hdmi_qualified','runtime_admission'):
            self.assertFalse(result['menu_present'][key])
        self.assertFalse(self.fixture.decode(self.raw)['complete'])
        self.assertFalse(self.decode(self.fixture.raw)['complete'])
        self.assertTrue(self.fixture.decode(self.fixture.raw)['complete'])

    def test_missing_stale_foreign_or_reordered_route_cannot_complete(self):
        for changed in (self.raw.replace(self.marker,'',1),self.raw.replace(self.marker,self.marker*2,1),
                        self.raw.replace('binding='+self.route_digest,'binding='+'f'*64,1),
                        self.raw.replace(' gbpa=100000 agbpa=0',' gbpa=0 agbpa=0',1),
                        self.raw.replace('s_cr0=0 s_ack=0','s_cr0=1 s_ack=1',1),
                        self.raw.replace('s_gbpa=0','s_gbpa=100000',1)):
            with self.subTest(changed=changed[-120:]):self.assertFalse(self.decode(changed)['complete'])
        for before,after in (('status=0','status=-1'),('owner='+f'{menu.OWNER:x}','owner=1'),('stream=1','stream=2'),
                             ('secure=1','secure=0'),('aruser=3e0','aruser=e0'),('arprot=3','arprot=1'),
                             ('ns_abort=1','ns_abort=0'),('source_retained=1','source_retained=0'),
                             ('source_generation=4','source_generation=3'),('generation=5','generation=6')):
            changed=self.raw.replace(self.marker,self.marker.replace(before,after),1)
            with self.subTest(field=before):self.assertFalse(self.decode(changed)['complete'])
        for at in (self.raw.index(self.marker),self.raw.index(self.marker)+len(self.marker)-1):
            self.assertFalse(self.decode(self.raw[:at])['complete'])

    def test_route_metadata_cannot_cross_the_selected_menu_or_policy(self):
        for key in ('menu_context_sha256','menu_access_sha256'):
            changed=copy.deepcopy(self.route);changed[key]='f'*64
            with self.subTest(key=key),self.assertRaises(ValueError):self.decode(self.raw,route=changed)
        changed=copy.deepcopy(self.route);changed['contract']['secure_stream']=0
        with self.assertRaises(ValueError):self.decode(self.raw,route=changed)
        changed=copy.deepcopy(self.route);del changed['boot_platform_sha256']
        with self.assertRaises(ValueError):self.decode(self.raw,route=changed)
