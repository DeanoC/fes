"""Actual fresh gate and uploader perform one fixed restoration or fail closed."""
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import test_nano_reset_recovery as recovery_fixtures
import test_nano_installer_boot_refresh as refresh_fixtures
from reset_restoration import ResetRestorationAttempt
from upload import destination


@patch.dict(os.environ, {'FES_EXPERIMENTAL_DE25_EMBEDDED': '1'})
class ResetRestorationTests(unittest.TestCase):
    setUpClass = classmethod(recovery_fixtures.ResetRecoveryTests.setUpClass.__func__)
    tearDownClass = classmethod(recovery_fixtures.ResetRecoveryTests.tearDownClass.__func__)
    attempt = recovery_fixtures.ResetRecoveryTests.attempt

    def setup_restore(self, root, fault=''):
        model, session, payloads, previous, record, servers = refresh_fixtures.BootRefreshTests().fixture()
        if fault.startswith('upload:'):model.fault = fault.split(':', 1)[1]
        gate = self.attempt(root, fault, fixture=(model, session, previous))
        operation = ResetRestorationAttempt(gate, payloads, previous_boot=previous,
                                           retained_records=(record,))
        return operation, model, session, payloads, servers

    def test_complete_fresh_boot_preservation_one_write_and_no_session_adoption(self):
        with tempfile.TemporaryDirectory() as name:
            op,m,s,p,servers=self.setup_restore(name);before=dict(m.files)
            with patch('builtins.print'),patch('upload.time.sleep'):r=op.run()
            self.assertEqual(r['status'],'verified-boot-restoration');self.assertTrue(s.retired)
            self.assertEqual(m.writes,1);before[destination('boot')]=p['boot'];self.assertEqual(m.files,before)
            self.assertEqual(r['restoration']['status'],'verified')
            self.assertTrue(r['recovery']['capture_boundary']['armed_before_dispatch'])
            self.assertEqual(len(servers),1);self.assertTrue(servers[0].closed)
            with self.assertRaises(ValueError):op.run()
            self.assertEqual(m.writes,1)

    def test_reset_and_readback_failures_never_write_or_retry(self):
        for fault in ('scan','stale-preprogram','no-new-boot','foreign','substituted-prefix',
                      'substituted-image','wrong-card','readback'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as name:
                op,m,s,p,servers=self.setup_restore(name,fault)
                if fault in ('wrong-card','readback'):m.fault=fault
                with patch('builtins.print'),patch('upload.time.sleep'),self.assertRaises((ValueError,TimeoutError)):op.run()
                self.assertEqual(m.writes,0);self.assertEqual(servers,[])
                calls=m.commands[:]
                with self.assertRaises(ValueError):op.run()
                self.assertEqual(m.commands,calls)

    def test_transfer_and_uncertain_write_failures_keep_failed_verdict(self):
        for fault,count in (('download-hash',0),('write-timeout',1),('boot-change',1),('manifest-change',1)):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as name:
                op,m,s,p,servers=self.setup_restore(name)
                # Arm the fault when the only selected Ethernet transfer starts.
                command=m.command
                def change(value,timeout):
                    if value.startswith('tftpboot'):m.fault=fault
                    return command(value,timeout)
                m.command=change
                with patch('builtins.print'),patch('upload.time.sleep'),self.assertRaises((ValueError,TimeoutError)):op.run()
                self.assertEqual(m.writes,count);self.assertTrue(s.retired)
                self.assertEqual(op.receipt['status'],'failed')
                with self.assertRaises(ValueError):op.run()
                self.assertEqual(m.writes,count)

    def test_programming_audits_are_constant_across_full_preservation_transaction(self):
        with tempfile.TemporaryDirectory() as name:
            op,m,s,p,servers=self.setup_restore(name)
            observe=op.recovery.transport;program_checks=[]
            def counted():
                program_checks.append(True)
                return observe()
            op.recovery.transport=counted
            with patch('builtins.print'),patch('upload.time.sleep'):op.run()
            self.assertEqual(len(program_checks),9)
            self.assertGreater(len(self.serial_observations),1000)
            self.assertEqual(m.writes,1)

    def test_live_serial_replacement_retires_before_write_and_never_retries(self):
        for field,value in (('claim_token','b'*16),('pid',99999),('start_ticks','999'),
                            ('cable','another cable'),('config_sha256','c'*64),
                            ('nonce','d'*32),('uart',[1,2,99]),('usb',[4,5,99])):
            with self.subTest(field=field),tempfile.TemporaryDirectory() as name:
                op,m,s,p,servers=self.setup_restore(name)
                observe=op.recovery.serial_transport;calls=[]
                def changed():
                    row=observe();calls.append(True)
                    if len(calls)>=4:row[field]=value
                    return row
                op.recovery.serial_transport=changed
                with patch('builtins.print'),self.assertRaises(ValueError):op.run()
                self.assertEqual(m.writes,0);self.assertTrue(s.retired)
                count=len(calls)
                with self.assertRaises(ValueError):op.run()
                self.assertEqual(len(calls),count)

    def test_final_programming_audit_failure_invalidates_restoration_receipt(self):
        with tempfile.TemporaryDirectory() as name:
            op, model, session, payloads, _ = self.setup_restore(name)
            transport = op.recovery.transport
            observations = []

            def changed_clock():
                current = transport()
                observations.append(current)
                if len(observations) == 9:
                    self.assertEqual(model.writes, 1)
                    self.assertEqual(model.files[destination('boot')], payloads['boot'])
                    self.assertEqual(op.receipt['status'], 'checking')
                    return dict(current, clock='6M')
                return current

            op.recovery.transport = changed_clock
            with patch('builtins.print'), patch('upload.time.sleep'):
                with self.assertRaisesRegex(ValueError, 'transport identity differs'):
                    op.run()
            self.assertEqual(op.receipt['status'], 'failed')
            self.assertIn('transport identity differs', op.receipt['error'])
            self.assertFalse(op.receipt['unchanged_sources_verified'])
            self.assertTrue(op.receipt['sd_write_attempted'])
            self.assertTrue(op.receipt['boot_upload']['sd_readback_verified'])
            self.assertTrue(session.retired)
            with self.assertRaisesRegex(ValueError, 'restoration attempt is retired'):
                op.run()
            self.assertEqual(model.writes, 1)

    def test_serial_loss_after_write_keeps_failure_and_does_not_rewrite(self):
        with tempfile.TemporaryDirectory() as name:
            op,m,s,p,servers=self.setup_restore(name)
            observe=op.recovery.serial_transport
            def changed():
                row=observe()
                if m.writes:row['uart']=[1,2,99]
                return row
            op.recovery.serial_transport=changed
            with patch('builtins.print'),patch('upload.time.sleep'),self.assertRaises(ValueError):op.run()
            self.assertEqual(m.writes,1);self.assertTrue(s.retired)
            with self.assertRaises(ValueError):op.run()
            self.assertEqual(m.writes,1)
