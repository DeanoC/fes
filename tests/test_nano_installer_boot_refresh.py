"""Boot-only refresh through the actual uploader and a controlled recovery console."""
import copy
import unittest
from unittest.mock import patch
import test_nano_installer_recovery as fixtures
from installer_boot_refresh import refresh_staged_boot
from installer_recovery import CATALOG_PURPOSES
import upload


class BootRefreshTests(unittest.TestCase):
    def fixture(self):
        m,s,p = fixtures.RecoveryTests().fixture(purposes=CATALOG_PURPOSES)
        previous = b'previous-staged-fit'
        retained = fixtures.RecoveryTests().retained()
        m.files['FES/INSTALLED/'+upload.sha(retained)+'.JSN'] = retained
        m.files[upload.destination('boot')] = previous
        m.payload = p['boot']
        servers = []
        class Server:
            complete = True
            thread = type('Thread',(),{'join':lambda self,timeout:None})()
            def __init__(self,host,client,name,payload):
                self.payload = payload;self.closed = False;servers.append(self)
            def start(self):pass
            def close(self):self.closed = True
        s.serve = Server
        return m,s,p,previous,retained,servers

    def run_refresh(self,s,p,r,previous,retained):
        with patch('builtins.print'),patch.object(upload.time,'sleep'):
            return refresh_staged_boot(s,p,r,previous_boot=previous,retained_records=(retained,))

    def test_only_staged_boot_changes_and_every_preserved_file_is_read_twice(self):
        m,s,p,old,record,servers = self.fixture();before = copy.deepcopy(m.files);r = {}
        self.assertIs(self.run_refresh(s,p,r,old,record),r)
        self.assertEqual(r['status'],'verified');self.assertTrue(s.retired and r['session_closed'])
        self.assertEqual(m.writes,1);before[upload.destination('boot')] = p['boot'];self.assertEqual(m.files,before)
        self.assertEqual(len(r['before']),15);self.assertEqual(len(r['after']),15)
        self.assertEqual(len(servers),1);self.assertTrue(servers[0].closed);self.assertEqual(servers[0].payload,p['boot'])
        writes = [c for c in m.commands if c.startswith('fatwrite')]
        self.assertEqual(writes,[f"fatwrite mmc 0:1 82000000 FES/UPLOAD.BIN {len(p['boot']):x}"])
        first_write = m.commands.index(writes[0])
        targets = [v['destination'] for v in r['before'].values()]
        for target in targets:
            self.assertIn('fatload mmc 0:1 83000000 '+target,m.commands[:first_write])
            self.assertIn('fatload mmc 0:1 83000000 '+target,m.commands[first_write+1:])
        self.assertTrue(r['unchanged_sources_verified'] and r['sd_write_attempted'])
        self.assertFalse(r['activation'] or r['cid_verified'] or r['persistence_verified'] or r['fresh_recovery_boot_proven'])
        self.assertFalse(any(c.startswith(('bootm','saveenv','mmc write','reset')) for c in m.commands))

    def test_bad_sources_or_previous_image_stop_before_network_and_writes(self):
        for target in ('boot','metadata-alt','install-manifest','prior'):
            m,s,p,old,record,servers = self.fixture();r = {}
            name = 'FES/INSTALLED/'+upload.sha(record)+'.JSN' if target == 'prior' else upload.destination(target)
            m.files[name] = b'corrupt'
            with self.subTest(target=target),self.assertRaises(ValueError):self.run_refresh(s,p,r,old,record)
            self.assertEqual(m.writes,0);self.assertEqual(servers,[]);self.assertTrue(s.retired and r['session_closed'])
            self.assertEqual(r['status'],'failed');self.assertFalse(r['sd_write_attempted'])

    def test_invalid_independent_input_and_adopted_session_reject_before_io(self):
        for kind in ('missing','mutable','bootstrap','same-boot','mutable-old','no-record','old-ledger','retired'):
            m,s,p,old,record,servers = self.fixture();r = {};records = (record,)
            if kind == 'missing':p.pop('metadata')
            if kind == 'mutable':p['boot'] = bytearray(p['boot'])
            if kind == 'bootstrap':p['selection'] = b'{"format":1,"programs":[{}],"scope":"nano-program-selection-v1"}'
            if kind == 'same-boot':old = p['boot']
            if kind == 'mutable-old':old = bytearray(old)
            if kind == 'no-record':records = ()
            if kind == 'old-ledger':s.verified['boot'] = (len(old),upload.sha(old))
            if kind == 'retired':s.retired = True
            with self.subTest(kind=kind),self.assertRaises(ValueError):
                refresh_staged_boot(s,p,r,previous_boot=old,retained_records=records)
            self.assertEqual(m.commands,[]);self.assertEqual(servers,[]);self.assertTrue(s.retired)

    def test_transfer_write_and_protected_file_faults_are_terminal_without_retry(self):
        for fault,write_count in [('interrupted',0),('download-hash',0),('write-timeout',1),
                                  ('short-write',1),('boot-change',1),('manifest-change',1)]:
            m,s,p,old,record,servers = self.fixture();r = {};m.fault = fault
            with self.subTest(fault=fault),self.assertRaises((ValueError,TimeoutError)):
                self.run_refresh(s,p,r,old,record)
            self.assertEqual(m.writes,write_count);self.assertLessEqual(len(servers),1)
            self.assertTrue(all(server.closed for server in servers));self.assertTrue(s.retired and r['session_closed'])
            self.assertEqual(r['sd_write_attempted'],bool(write_count));self.assertFalse(r['unchanged_sources_verified'])
            commands = list(m.commands)
            with self.assertRaises(ValueError):s.upload(p['boot'],{'commands':[]},purpose='boot')
            self.assertEqual(m.commands,commands)

    def test_changed_preserved_file_after_write_cannot_promote_success(self):
        for target in ('metadata-alt','prior'):
            m,s,p,old,record,servers = self.fixture();r = {};command = m.command
            def changed(value,timeout):
                answer = command(value,timeout)
                if value.startswith('fatwrite'):
                    name = 'FES/INSTALLED/'+upload.sha(record)+'.JSN' if target == 'prior' else upload.destination(target)
                    m.files[name] = b'changed'
                return answer
            m.command = changed
            with self.subTest(target=target),self.assertRaises(ValueError):self.run_refresh(s,p,r,old,record)
            self.assertEqual(m.writes,1);self.assertTrue(r['sd_write_attempted'] and s.retired)
            self.assertFalse(r['unchanged_sources_verified']);self.assertEqual(r['status'],'failed')

    def test_foreign_read_receipt_or_upload_ledger_stops_before_write(self):
        for kind in ('receipt','ledger'):
            m,s,p,old,record,servers = self.fixture();r = {};original = s.read_existing
            def changed(payload,row,*,purpose):
                original(payload,row,purpose=purpose)
                if kind == 'receipt':row['upload_session'] = '0'*32
                else:s.verified['package'] = (1,'a'*64)
            s.read_existing = changed
            with self.subTest(kind=kind),self.assertRaises(ValueError):self.run_refresh(s,p,r,old,record)
            self.assertEqual(m.writes,0);self.assertEqual(servers,[]);self.assertTrue(s.retired)

    def test_claim_loss_after_write_preserves_terminal_failure(self):
        m,s,p,old,record,servers = self.fixture();r = {};command = m.command;owned = True
        def lose(value,timeout):
            nonlocal owned
            answer = command(value,timeout)
            if value.startswith('fatwrite'):owned = False
            return answer
        m.command = lose;s.owns = lambda:owned
        with self.assertRaisesRegex(ValueError,'claim ownership'):self.run_refresh(s,p,r,old,record)
        self.assertEqual(m.writes,1);self.assertTrue(r['sd_write_attempted'] and s.retired and r['session_closed'])
        self.assertEqual(r['after'],{});self.assertEqual(r['status'],'failed')

    def test_close_failure_is_recorded_and_does_not_mask_original_error(self):
        for invalid in (False,True):
            m,s,p,old,record,servers = self.fixture();r = {}
            if invalid:p.pop('boot')
            def bad_close():raise RuntimeError('close failed')
            s.close = bad_close
            with self.subTest(invalid=invalid),self.assertRaises(ValueError if invalid else RuntimeError) as caught:
                self.run_refresh(s,p,r,old,record)
            self.assertEqual(r['status'],'failed');self.assertFalse(r['session_closed'])
            self.assertEqual(r['session_close_error'],'close failed')
            if invalid:self.assertIn('exact staged source set',str(caught.exception))

    def test_replayed_receipt_does_not_touch_or_retire_a_new_session(self):
        m,s,p,old,record,servers = self.fixture();r = {};self.run_refresh(s,p,r,old,record)
        before = copy.deepcopy(r);other,session,_,_,_,_ = self.fixture()
        with self.assertRaises(ValueError):self.run_refresh(session,p,r,old,record)
        self.assertEqual(r,before);self.assertEqual(other.commands,[]);self.assertFalse(session.retired)

if __name__ == '__main__':unittest.main()
