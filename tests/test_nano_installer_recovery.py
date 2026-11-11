"""Actual UploadSession reader over a controlled recovery console; no board."""
import copy,json,sys,unittest
from pathlib import Path
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from installer_recovery import PURPOSES,read_staged_sources
import upload
from test_nano_upload import Model

class RecoveryTests(unittest.TestCase):
    def fixture(self,fault=''):
        kit=dict(manifest=b'manifest',fit=b'bootfit',fit_sha256=upload.sha(b'bootfit'),prefix_sha256=upload.sha(b'prefix'))
        payloads={p:b'expected-'+p.encode() for p in PURPOSES}
        payloads['firmware']=b'x'*8192
        payloads['selection']=b'{"format":1,"programs":[],"scope":"nano-program-selection-v1"}'
        payloads['ui-catalog']=b'{"entries":[],"format":1,"scope":"nano-tenfoot-catalog-v1"}'
        model=Model(kit,b'unused',fault)
        model.files={upload.destination(p):data for p,data in payloads.items()}
        def no_network(*a):raise AssertionError('read-only recovery started a network server')
        session=upload.UploadSession(model,kit,'192.168.10.202','02:fe:25:08:cb:01',no_network)
        return model,session,payloads
    def test_all_sources_and_empty_bootstrap_read_without_writes_or_network(self):
        m,s,p=self.fixture();r={}
        with patch('builtins.print'):self.assertIs(read_staged_sources(s,p,r),r)
        self.assertEqual(r['status'],'verified');self.assertEqual(r['purposes'],list(PURPOSES))
        self.assertTrue(r['bootstrap_empty'] and r['session_closed'] and s.retired)
        self.assertFalse(r['persistence_verified'] or r['fresh_recovery_boot_proven'] or r['cid_verified'])
        self.assertFalse(r['sd_write_attempted']);self.assertEqual(m.writes,0)
        self.assertFalse(any(c.startswith(('fatwrite','mmc write','tftpboot','dhcp','setenv ethaddr')) for c in m.commands))
        targets=[c.split()[-1] for c in m.commands if c.startswith('fatload') and c.split()[-1] in upload.DESTINATIONS.values()]
        self.assertEqual(targets,[upload.destination(purpose) for purpose in PURPOSES])
        self.assertEqual(len({v['upload_session'] for v in r['files'].values()}),1)
        self.assertEqual(s.verified,{})
    def test_bad_or_populated_bootstrap_closes_before_any_io(self):
        changes=[lambda p:p.pop('boot'),lambda p:p.update(extra=b'file'),
                 lambda p:p.update(selection=b'{"format":1,"programs":[{}],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update(selection=b'{"format":true,"programs":[],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update(selection=b'{"format":1,"programs":[{}],"programs":[],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update({'ui-catalog':b'{"entries":[{}],"format":1,"scope":"nano-tenfoot-catalog-v1"}'}),
                 lambda p:p.update(firmware=b'bad'),lambda p:p.update(boot=bytearray(b'mutable'))]
        for i,change in enumerate(changes):
            m,s,p=self.fixture();change(p);r={}
            with self.subTest(i=i),self.assertRaises(ValueError):read_staged_sources(s,p,r)
            self.assertEqual(m.commands,[]);self.assertTrue(s.retired and r['session_closed']);self.assertEqual(r['status'],'failed')
    def test_card_or_content_failure_stops_without_reading_later_sources(self):
        for fault in ('old-loader','prefix','readback','wrong-card','reserved'):
            m,s,p=self.fixture(fault);r={}
            with self.subTest(fault=fault),patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r)
            self.assertLessEqual(len(r['files']),1);self.assertEqual(m.writes,0);self.assertTrue(s.retired)
    def test_later_corrupt_source_keeps_partial_evidence_and_stops(self):
        m,s,p=self.fixture();m.files[upload.destination('metadata')]=b'z'*len(p['metadata']);r={}
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r)
        self.assertEqual(list(r['files']),['boot','package','metadata'])
        self.assertEqual(r['files']['boot']['status'],'verified')
        self.assertEqual(r['files']['metadata']['status'],'failed')
        self.assertEqual(m.writes,0);self.assertTrue(s.retired)
        self.assertNotIn('fatload mmc 0:1 83000000 FES/FIRMWARE.BIN',m.commands)
    def test_foreign_session_receipt_stops_and_closes_without_further_reads(self):
        m,s,p=self.fixture();original=s.read_existing
        def crossed(payload,receipt,*,purpose):
            original(payload,receipt,purpose=purpose)
            receipt['upload_session']='0'*32
        s.read_existing=crossed;r={}
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r)
        self.assertEqual(list(r['files']),['boot']);self.assertEqual(r['status'],'failed')
        self.assertTrue(r['session_closed'] and s.retired);self.assertEqual(m.writes,0)
    def test_owned_upload_ledger_cannot_be_adopted_as_read_only_boot_proof(self):
        m,s,p=self.fixture();s.verified['boot']=(len(p['boot']),upload.sha(p['boot']));r={}
        with self.assertRaises(ValueError):read_staged_sources(s,p,r)
        self.assertEqual(m.commands,[]);self.assertTrue(s.retired)
    def test_close_failure_is_recorded_and_does_not_hide_the_first_error(self):
        for bad_input in (False,True):
            m,s,p=self.fixture();r={}
            if bad_input:p.pop('boot')
            def fail_close():raise RuntimeError('close failed')
            s.close=fail_close
            expected=ValueError if bad_input else RuntimeError
            with self.subTest(bad_input=bad_input),patch('builtins.print'),self.assertRaises(expected) as caught:
                read_staged_sources(s,p,r)
            self.assertEqual(r['status'],'failed');self.assertFalse(r['session_closed'])
            self.assertEqual(r['session_close_error'],'close failed')
            if bad_input:self.assertIn('exact staged source set',str(caught.exception))
            self.assertEqual(m.writes,0)
    def test_successful_receipt_cannot_be_replayed_with_new_session(self):
        m,s,p=self.fixture();r={}
        with patch('builtins.print'):read_staged_sources(s,p,r)
        before=copy.deepcopy(r);other,session,_=self.fixture()
        with self.assertRaises(ValueError):read_staged_sources(session,p,r)
        self.assertEqual(r,before);self.assertEqual(other.commands,[]);self.assertFalse(session.retired)
    def test_previous_recovery_marker_is_not_adopted_by_new_session(self):
        m,s,p=self.fixture();m.marker='1'*32;r={}
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r)
        self.assertEqual(m.writes,0);self.assertTrue(s.retired);self.assertEqual(r['status'],'failed')

if __name__=='__main__':unittest.main()
