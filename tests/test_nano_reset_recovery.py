"""Full entry C witness + fresh JTAG gate + real UploadSession, injected devices."""
import copy
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'platform/embedded/nano'))
import reset_recovery as recovery
import test_nano_recovery_reset_probe as producer
from test_nano_jtag_programming import identity, RAW, CABLE
from test_nano_upload import Model
from upload import UploadSession


@patch.dict(os.environ, {'FES_EXPERIMENTAL_DE25_EMBEDDED': '1'})
class ResetRecoveryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        producer.RecoveryResetProbeTests.setUpClass()
        cls.wire = producer.RecoveryResetProbeTests().run_probe()

    @classmethod
    def tearDownClass(cls):
        producer.RecoveryResetProbeTests.tearDownClass()

    def attempt(self, directory, fault=''):
        root = Path(directory); image = root / 'recovery.sof'; image.write_bytes(b'explicit synthetic SOF')
        self.binding = identity(); self.calls = []; self.last_session = None
        payload = b'expected candidate FIT'
        kit = {'fit': b'bootfit', 'manifest': b'manifest', 'fit_sha256': recovery.sha(b'bootfit'),
               'prefix_sha256': recovery.sha(b'prefix')}
        self.model = Model(kit, payload, fault)
        if fault == 'existing-epoch':self.model.marker = 'old-owner'
        def scan(cable):
            self.calls.append('scan'); self.assertEqual(cable, CABLE)
            return RAW if fault != 'scan' else b'listing only'
        def program(argv, timeout):
            self.calls.append('program')
            self.assertEqual(argv, ['-c', CABLE, '-m', 'jtag', '-o', 'p;' + str(image) + '@1'])
            self.assertEqual(timeout, 90)
            if fault == 'program-uncertain': raise TimeoutError('uncertain programming')
            if fault == 'ownership-after-program': self.binding['claim_token'] = 'b' * 16
            if fault == 'image-after-program':image.write_bytes(b'changed')
            return {'returncode': 1 if fault == 'program' else 0,
                    'output': b'Quartus Prime Programmer was successful. 0 errors, 0 warnings\n'}
        def capture(timeout, maximum):
            self.calls.append('capture');self.assertEqual((timeout,maximum),(90,1 << 20))
            if fault == 'capture':raise TimeoutError('no boot')
            if fault == 'partial':return self.wire[:-1]
            if fault == 'foreign':return self.wire.replace(producer.NONCE.encode(), b'b' * 32)
            if fault == 'wrong-kind':return self.wire.replace(b'kind=4', b'kind=2')
            return self.wire
        def session_factory(owns):
            self.calls.append('loader')
            self.last_session = UploadSession(self.model, kit, '192.0.2.1', '02:fe:25:08:cb:01', owns=owns)
            return self.last_session
        return recovery.ResetRecoveryAttempt(root / 'consumed.json', serial='TRWU8CBL',
            source_commit=self.binding['source_commit'], image={'path':str(image),'sha256':recovery.sha(image.read_bytes())},
            nonce=producer.NONCE, failed_wire=b'original failed native wire, unchanged',expected_payload=payload,
            loader_fit_sha256=recovery.sha(kit['fit']),loader_manifest_sha256=recovery.sha(kit['manifest']),
            transport=lambda:copy.deepcopy(self.binding),scan=scan,program=program,capture=capture,
            session_factory=session_factory)

    def test_live_scan_complete_new_entry_and_actual_protected_readback_without_writes(self):
        with tempfile.TemporaryDirectory() as name:
            attempt=self.attempt(name);result=attempt.run()
            self.assertEqual(result['status'],'verified-read-only-recovery-bootstrap')
            self.assertTrue(result['recovery_loader_admitted'])
            self.assertEqual(self.calls,['scan','program','capture','loader'])
            self.assertEqual(self.model.writes,0)
            self.assertTrue(self.last_session.retired)
            for key in ('sd_write_authorized','old_source_reuse_authorized','ordinary_runtime_resume',
                        'sdm_reset_verified','power_cycle_requested','transport_cleanup_verified','hardware_acceptance','cid_verified'):
                self.assertIs(result[key],False,key)
            with self.assertRaises(ValueError):attempt.run()
            self.assertEqual(self.calls,['scan','program','capture','loader'])
            other=self.attempt(name)
            with self.assertRaises(FileExistsError):other.run()
            self.assertEqual(self.calls,[])

    def test_failure_at_each_boundary_retires_attempt_and_never_writes_or_retries(self):
        for fault in ('scan','program','program-uncertain','ownership-after-program','image-after-program',
                      'capture','partial','foreign','wrong-kind','existing-epoch','wrong-card','readback','reserved','old-loader'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as name:
                attempt=self.attempt(name,fault)
                with self.assertRaises((ValueError,TimeoutError)):attempt.run()
                before=self.calls[:]
                with self.assertRaises(ValueError):attempt.run()
                self.assertEqual(self.calls,before);self.assertEqual(self.model.writes,0)
                if self.last_session is not None:self.assertTrue(self.last_session.retired)

    def test_explicit_opt_in_precedes_even_operation_reservation(self):
        with tempfile.TemporaryDirectory() as name:
            attempt=self.attempt(name)
            with patch.dict(os.environ,{'FES_EXPERIMENTAL_DE25_EMBEDDED':'0'}), self.assertRaises(ValueError):attempt.run()
            self.assertFalse(attempt.path.exists());self.assertEqual(self.calls,[])


if __name__=='__main__':unittest.main()
