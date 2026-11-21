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

    def attempt(self, directory, fault='', fixture=None):
        root = Path(directory); image = root / 'recovery.sof'; image.write_bytes(b'explicit synthetic SOF')
        self.binding = identity(); self.calls = []; self.last_session = None
        payload = b'expected candidate FIT'
        kit = {'fit': b'bootfit', 'manifest': b'manifest', 'fit_sha256': recovery.sha(b'bootfit'),
               'prefix_sha256': recovery.sha(b'prefix'), 'image_sha256':recovery.sha(b'card-image')}
        self.model = Model(kit, payload, fault)
        if fixture is not None:
            self.model, supplied, payload = fixture; kit = supplied.kit
            kit['image_sha256'] = recovery.sha(b'card-image')
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
            raw = self.wire
            if fault == 'partial':raw = raw[:-1]
            if fault == 'foreign':raw = raw.replace(producer.NONCE.encode(), b'b' * 32)
            if fault == 'wrong-kind':raw = raw.replace(b'kind=4', b'kind=2')
            if fault != 'no-new-boot':self.capturer.feed(raw)
            return {'returncode': 1 if fault == 'program' else 0,
                    'output': b'Quartus Prime Programmer was successful. 0 errors, 0 warnings\n'}
        def collect(window, timeout, maximum):
            self.calls.append('capture');self.assertEqual((timeout,maximum),(90,1 << 20))
            if fault == 'capture':raise TimeoutError('no boot')
        def drain():
            if fault == 'ownership-during-drain':self.binding['claim_token'] = 'c' * 16
            return self.wire if fault == 'stale-preprogram' else b''
        self.capturer = recovery.ResetCapture(drain, collect)
        def session_factory(owns):
            self.calls.append('loader')
            selected = dict(kit)
            if fault == 'substituted-prefix':selected['prefix_sha256'] = 'a' * 64
            if fault == 'substituted-image':selected['image_sha256'] = 'b' * 64
            if fixture is None:
                self.last_session = UploadSession(self.model, selected, '192.0.2.1', '02:fe:25:08:cb:01', owns=owns)
            else:
                self.last_session = supplied; supplied.owns = owns; supplied.kit = selected
            return self.last_session
        self.serial_observations = []
        def serial_transport():
            self.serial_observations.append(True)
            value = {k:copy.deepcopy(self.binding[k]) for k in
                     ('serial','source_commit','claim_token','pid','start_ticks','cable','config_sha256')}
            value.update(nonce=producer.NONCE,uart=[1,2,3],usb=[4,5,6])
            return value
        return recovery.ResetRecoveryAttempt(root / 'consumed.json', serial='TRWU8CBL',
            source_commit=self.binding['source_commit'], image={'path':str(image),'sha256':recovery.sha(image.read_bytes())},
            nonce=producer.NONCE, failed_wire=b'original failed native wire, unchanged',expected_payload=payload,
            loader_fit_sha256=recovery.sha(kit['fit']),loader_manifest_sha256=recovery.sha(kit['manifest']),
            loader_prefix_sha256=kit['prefix_sha256'],loader_image_sha256=kit['image_sha256'],
            nonce_registry=root/'used-nonces',
            transport=lambda:copy.deepcopy(self.binding),serial_transport=serial_transport,
            scan=scan,program=program,capture=self.capturer,
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
                      'capture','partial','foreign','wrong-kind','stale-preprogram','no-new-boot',
                      'substituted-prefix','substituted-image','ownership-during-drain','existing-epoch','wrong-card','readback','reserved','old-loader'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as name:
                attempt=self.attempt(name,fault)
                with self.assertRaises((ValueError,TimeoutError)):attempt.run()
                before=self.calls[:]
                with self.assertRaises(ValueError):attempt.run()
                self.assertEqual(self.calls,before);self.assertEqual(self.model.writes,0)
                if self.last_session is not None:self.assertTrue(self.last_session.retired)

    def test_same_nonce_cannot_be_replayed_with_a_different_operation_file(self):
        with tempfile.TemporaryDirectory() as name:
            first=self.attempt(name);first.run()
            second=self.attempt(name);second.path=Path(name)/'new-operation.json'
            with self.assertRaises(FileExistsError):second.run()
            self.assertEqual(self.calls,[])

    def test_explicit_opt_in_precedes_even_operation_reservation(self):
        with tempfile.TemporaryDirectory() as name:
            attempt=self.attempt(name)
            with patch.dict(os.environ,{'FES_EXPERIMENTAL_DE25_EMBEDDED':'0'}), self.assertRaises(ValueError):attempt.run()
            self.assertFalse(attempt.path.exists());self.assertEqual(self.calls,[])


if __name__=='__main__':unittest.main()
