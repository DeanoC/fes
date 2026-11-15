"""Actual UploadSession reader over a controlled recovery console; no board."""
import copy,json,sys,unittest
from pathlib import Path
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from installer_recovery import PURPOSES,CATALOG_PURPOSES,read_staged_sources
import upload
from test_nano_upload import Model

class RecoveryTests(unittest.TestCase):
    def fixture(self,fault='',purposes=PURPOSES):
        kit=dict(manifest=b'manifest',fit=b'bootfit',fit_sha256=upload.sha(b'bootfit'),prefix_sha256=upload.sha(b'prefix'))
        payloads={p:b'expected-'+p.encode() for p in purposes}
        payloads['firmware']=b'x'*8192
        if 'firmware-alt' in payloads:payloads['firmware-alt']=b'y'*8192
        payloads['selection']=b'{"format":1,"programs":[],"scope":"nano-program-selection-v1"}'
        payloads['ui-catalog']=b'{"entries":[],"format":1,"scope":"nano-tenfoot-catalog-v1"}'
        model=Model(kit,b'unused',fault)
        model.files={upload.destination(p):data for p,data in payloads.items()}
        def no_network(*a):raise AssertionError('read-only recovery started a network server')
        session=upload.UploadSession(model,kit,'192.168.10.202','02:fe:25:08:cb:01',no_network)
        return model,session,payloads
    def test_all_sources_and_empty_bootstrap_read_without_writes_or_network(self):
        m,s,p=self.fixture();r={}
        with patch('builtins.print'):self.assertIs(read_staged_sources(s,p,r,purposes=PURPOSES),r)
        self.assertEqual(r['status'],'verified');self.assertEqual(r['purposes'],list(PURPOSES))
        self.assertTrue(r['bootstrap_empty'] and r['session_closed'] and s.retired)
        self.assertFalse(r['persistence_verified'] or r['fresh_recovery_boot_proven'] or r['cid_verified'])
        self.assertFalse(r['sd_write_attempted']);self.assertEqual(m.writes,0)
        self.assertFalse(any(c.startswith(('fatwrite','mmc write','tftpboot','dhcp','setenv ethaddr')) for c in m.commands))
        targets=[c.split()[-1] for c in m.commands if c.startswith('fatload') and c.split()[-1] in upload.DESTINATIONS.values()]
        self.assertEqual(targets,[upload.destination(purpose) for purpose in PURPOSES])
        self.assertEqual(len({v['upload_session'] for v in r['files'].values()}),1)
        self.assertEqual(s.verified,{})
    def test_fourteen_role_catalogue_read_is_explicit_read_only_and_closed(self):
        m,s,p=self.fixture(purposes=CATALOG_PURPOSES);r={}
        with patch('builtins.print'):read_staged_sources(s,p,r,purposes=CATALOG_PURPOSES)
        self.assertEqual(r['purposes'],list(CATALOG_PURPOSES));self.assertEqual(m.writes,0)
        self.assertTrue(r['session_closed'] and s.retired);self.assertFalse(r['persistence_verified'])
        targets=[c.split()[-1] for c in m.commands if c.startswith('fatload') and c.split()[-1] in upload.DESTINATIONS.values()]
        self.assertEqual(targets,[upload.destination(purpose) for purpose in CATALOG_PURPOSES])
    def test_explicit_role_set_cannot_adopt_self_described_payload_subset(self):
        for purposes in (PURPOSES,tuple(reversed(CATALOG_PURPOSES)),list(CATALOG_PURPOSES)):
            m,s,p=self.fixture(purposes=CATALOG_PURPOSES);r={}
            with self.subTest(purposes=purposes),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=purposes)
            self.assertEqual(m.commands,[]);self.assertTrue(s.retired)
    def test_later_alternate_content_failure_stops_before_tape_and_manifest(self):
        m,s,p=self.fixture(purposes=CATALOG_PURPOSES);r={}
        m.files[upload.destination('metadata-alt')]=b'z'*len(p['metadata-alt'])
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=CATALOG_PURPOSES)
        self.assertEqual(list(r['files']),list(CATALOG_PURPOSES[:6]));self.assertEqual(m.writes,0)
        self.assertTrue(s.retired);self.assertNotIn('install-manifest',r['files'])

    def test_bad_or_populated_bootstrap_closes_before_any_io(self):
        changes=[lambda p:p.pop('boot'),lambda p:p.update(extra=b'file'),
                 lambda p:p.update(selection=b'{"format":1,"programs":[{}],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update(selection=b'{"format":true,"programs":[],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update(selection=b'{"format":1,"programs":[{}],"programs":[],"scope":"nano-program-selection-v1"}'),
                 lambda p:p.update({'ui-catalog':b'{"entries":[{}],"format":1,"scope":"nano-tenfoot-catalog-v1"}'}),
                 lambda p:p.update(firmware=b'bad'),lambda p:p.update(boot=bytearray(b'mutable'))]
        for i,change in enumerate(changes):
            m,s,p=self.fixture();change(p);r={}
            with self.subTest(i=i),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
            self.assertEqual(m.commands,[]);self.assertTrue(s.retired and r['session_closed']);self.assertEqual(r['status'],'failed')
    def test_card_or_content_failure_stops_without_reading_later_sources(self):
        for fault in ('old-loader','prefix','readback','wrong-card','reserved'):
            m,s,p=self.fixture(fault);r={}
            with self.subTest(fault=fault),patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
            self.assertLessEqual(len(r['files']),1);self.assertEqual(m.writes,0);self.assertTrue(s.retired)
    def test_later_corrupt_source_keeps_partial_evidence_and_stops(self):
        m,s,p=self.fixture();m.files[upload.destination('metadata')]=b'z'*len(p['metadata']);r={}
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
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
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
        self.assertEqual(list(r['files']),['boot']);self.assertEqual(r['status'],'failed')
        self.assertTrue(r['session_closed'] and s.retired);self.assertEqual(m.writes,0)
    def test_owned_upload_ledger_cannot_be_adopted_as_read_only_boot_proof(self):
        m,s,p=self.fixture();s.verified['boot']=(len(p['boot']),upload.sha(p['boot']));r={}
        with self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
        self.assertEqual(m.commands,[]);self.assertTrue(s.retired)
    def test_close_failure_is_recorded_and_does_not_hide_the_first_error(self):
        for bad_input in (False,True):
            m,s,p=self.fixture();r={}
            if bad_input:p.pop('boot')
            def fail_close():raise RuntimeError('close failed')
            s.close=fail_close
            expected=ValueError if bad_input else RuntimeError
            with self.subTest(bad_input=bad_input),patch('builtins.print'),self.assertRaises(expected) as caught:
                read_staged_sources(s,p,r,purposes=PURPOSES)
            self.assertEqual(r['status'],'failed');self.assertFalse(r['session_closed'])
            self.assertEqual(r['session_close_error'],'close failed')
            if bad_input:self.assertIn('exact staged source set',str(caught.exception))
            self.assertEqual(m.writes,0)
    def test_successful_receipt_cannot_be_replayed_with_new_session(self):
        m,s,p=self.fixture();r={}
        with patch('builtins.print'):read_staged_sources(s,p,r,purposes=PURPOSES)
        before=copy.deepcopy(r);other,session,_=self.fixture()
        with self.assertRaises(ValueError):read_staged_sources(session,p,r,purposes=PURPOSES)
        self.assertEqual(r,before);self.assertEqual(other.commands,[]);self.assertFalse(session.retired)
    def test_previous_recovery_marker_is_not_adopted_by_new_session(self):
        m,s,p=self.fixture();m.marker='1'*32;r={}
        with patch('builtins.print'),self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES)
        self.assertEqual(m.writes,0);self.assertTrue(s.retired);self.assertEqual(r['status'],'failed')

    def retained(self, name='prior'):
        return json.dumps(dict(format=1,scope='nano-installed-program-v1',id=name,
                         title=name,program={'path':'FES/CONTENT/'+('a'*64)+'.BIN'})).encode()

    def test_prior_record_and_fourteen_sources_share_one_fresh_read_only_session(self):
        m,s,p=self.fixture(purposes=CATALOG_PURPOSES);payload=self.retained();digest=upload.sha(payload)
        target='FES/INSTALLED/'+digest+'.JSN';m.files[target]=payload;r={}
        with patch('builtins.print'):
            read_staged_sources(s,p,r,purposes=CATALOG_PURPOSES,retained_records=(payload,))
        self.assertTrue(r['retained_record_bytes_verified'] and r['session_closed'])
        self.assertEqual(list(r['retained_records']),[digest]);self.assertEqual(list(r['files']),list(CATALOG_PURPOSES))
        rows=[*r['retained_records'].values(),*r['files'].values()]
        self.assertEqual(len({v['upload_session'] for v in rows}),1)
        requests=[c.split()[-1] for c in m.commands if c.startswith('fatload') and
                  c.split()[-1] in ({target}|set(upload.DESTINATIONS.values()))]
        self.assertEqual(requests,[target]+[upload.destination(k) for k in CATALOG_PURPOSES])
        self.assertFalse(r['cid_verified'] or r['persistence_verified'] or r['fresh_recovery_boot_proven'])
        self.assertEqual(m.writes,0);self.assertFalse(s.verified)
        self.assertFalse(any(c.startswith(('fatwrite','mmc write','tftpboot','dhcp')) for c in m.commands))

    def test_three_independently_expected_records_precede_staged_reads(self):
        m,s,p=self.fixture();expected=tuple(self.retained(name) for name in ('prior','zx81-trial','zx80-trial'))
        for payload in expected:m.files['FES/INSTALLED/'+upload.sha(payload)+'.JSN']=payload
        r={}
        with patch('builtins.print'):read_staged_sources(s,p,r,purposes=PURPOSES,retained_records=expected)
        self.assertEqual(list(r['retained_records']),[upload.sha(payload) for payload in expected])
        self.assertTrue(all(v['status']=='verified' for v in r['retained_records'].values()))
        self.assertFalse(r['persistence_verified']);self.assertEqual(m.writes,0)

    def test_bad_retained_record_stops_before_all_staged_reads(self):
        for fault in ('missing','corrupt','invalid-json','wrong-scope'):
            with self.subTest(fault=fault),patch('builtins.print'):
                m,s,p=self.fixture();payload=self.retained();target='FES/INSTALLED/'+upload.sha(payload)+'.JSN'
                if fault=='corrupt':m.files[target]=b'x'*len(payload)
                if fault=='invalid-json':payload=b'not json'
                if fault=='wrong-scope':payload=payload.replace(b'nano-installed-program-v1',b'nano-staged-program-install-v1')
                r={}
                with self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES,retained_records=(payload,))
                self.assertEqual(r['files'],{});self.assertEqual(r['status'],'failed')
                self.assertFalse(r['retained_record_bytes_verified']);self.assertTrue(r['session_closed'] and s.retired)
                self.assertFalse(any(c.startswith('fatload') and c.split()[-1] in upload.DESTINATIONS.values() for c in m.commands))
                self.assertEqual(m.writes,0)

    def test_retained_record_bounds_and_duplicates_reject_before_io(self):
        payload=self.retained()
        for records in ([payload],False,(payload,)*33,(b'',),(b'x'*8193,),(bytearray(payload),),(payload,payload)):
            with self.subTest(records_type=type(records),count=len(records) if hasattr(records,'__len__') else None):
                m,s,p=self.fixture();r={}
                with self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES,retained_records=records)
                self.assertEqual(m.commands,[]);self.assertTrue(s.retired and r['session_closed'])

    def test_retained_read_cannot_change_session_or_promote_scope(self):
        for field,value in [('upload_session','0'*32),('destination','FES/UPLOAD.BIN'),('cid_verified',True),
                            ('activation',True),('purpose','boot'),('memory_observations',[])]:
            with self.subTest(field=field),patch('builtins.print'):
                m,s,p=self.fixture();payload=self.retained();m.files['FES/INSTALLED/'+upload.sha(payload)+'.JSN']=payload
                original=s.read_installed_record
                def read(data,receipt):
                    original(data,receipt);receipt[field]=value
                s.read_installed_record=read;r={}
                with self.assertRaises(ValueError):read_staged_sources(s,p,r,purposes=PURPOSES,retained_records=(payload,))
                self.assertEqual(r['files'],{});self.assertTrue(r['session_closed'] and s.retired);self.assertEqual(m.writes,0)

    def test_changed_source_after_retained_read_does_not_promote_partial_result(self):
        m,s,p=self.fixture();payload=self.retained();m.files['FES/INSTALLED/'+upload.sha(payload)+'.JSN']=payload
        m.files[upload.destination('boot')]=b'changed';r={}
        with patch('builtins.print'),self.assertRaises(ValueError):
            read_staged_sources(s,p,r,purposes=PURPOSES,retained_records=(payload,))
        self.assertEqual(r['retained_records'][upload.sha(payload)]['status'],'verified')
        self.assertFalse(r['retained_record_bytes_verified']);self.assertEqual(list(r['files']),['boot'])
        self.assertEqual(r['status'],'failed');self.assertTrue(s.retired);self.assertEqual(m.writes,0)

if __name__=='__main__':unittest.main()
