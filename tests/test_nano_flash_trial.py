"""Host models only: no device opens and no claim of physical flash acceptance."""
import copy
import hashlib
import json
from pathlib import Path
import struct
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import flash_trial as flash
import jtag_programming as jtag

RAW = (ROOT/'tests/fixtures/nano-jtag/controlled-fixed-clock-scan.txt').read_bytes()
CABLE = 'DE25-Nano on localhost:45043 [3-11.1-iface0]'
OK = dict(returncode=0, output=b'Info: Quartus Prime Programmer was successful. 0 errors, 0 warnings\n')


def identity():
    return dict(serial='TRWU8CBL', source_commit='1'*40, claim_token='2'*16,
                pid=1234, start_ticks='5678', cable=CABLE, config_sha256='3'*64,
                clock='3M', auto_adjust=0, client_sha256={p:'4'*64 for p in jtag.LIBRARIES}, probe_sha256='5'*64)


def fixture(path, value):
    # Synthetic envelope exercises the independent target/array decoder; these
    # bytes are neither a vendor programming artifact nor admitted to hardware.
    physical = bytes([value])*flash.ARRAY_BYTES
    encoded = bytes([int(f'{value:08b}'[::-1], 2)])*flash.ARRAY_BYTES
    records = ((1, b'test'), (2, b'A5EB013BB23B\0'), (3, b'test'),
               (27, b'MT25QU128\0'), (28, b'\0'*12+encoded), (30, b'test'), (26, b'test'), (8, b'\0\0'))
    raw = b'JIC\0'+struct.pack('<II', 0, 8)+b''.join(struct.pack('<HI', tag, len(data))+data for tag, data in records)
    path.write_bytes(raw)
    return dict(path=str(path), sha256=hashlib.sha256(raw).hexdigest(), array_sha256=hashlib.sha256(physical).hexdigest())


class FlashTrialTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.images = tempfile.TemporaryDirectory()
        base = Path(cls.images.name)
        cls.native, cls.factory = fixture(base/'native.jic', 0x12), fixture(base/'factory.jic', 0xff)

    @classmethod
    def tearDownClass(cls):
        cls.images.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.plan = dict(format=1, serial='TRWU8CBL', source_commit='1'*40,
                         backup_sha256=self.factory['array_sha256'],
                         images={'native':copy.deepcopy(self.native), 'factory-recovery':copy.deepcopy(self.factory)})
        self.approval = dict(format=1, source='direct-user-message', serial='TRWU8CBL',
                             plan_sha256=flash.digest(self.plan), roles=['native', 'factory-recovery'],
                             user_text='Program the specified native trial; restore this backup once if it fails.')
        self.calls = []

    def trial(self, *, approval=None, observe=None, scan=None, program=None):
        def scanning(cable):
            self.calls.append('scan')
            self.assertEqual(cable, CABLE)
            return RAW
        def programming(role, cable, arguments):
            self.calls.append(role)
            request = json.loads((self.directory/(role+'.write-started.json')).read_text())
            self.assertEqual(request['state'], 'consumed-before-nonvolatile-write')
            self.assertEqual(arguments, ['-c', CABLE, '-m', 'jtag', '-o',
                                        'IPV;'+self.plan['images'][role]['path']+';A5EB013BB23B@1'])
            self.assertEqual(cable, CABLE)
            return copy.deepcopy(OK)
        return flash.FlashTrial(self.directory, self.plan, self.approval if approval is None else approval,
                                observe or identity, scan or scanning, program or programming)

    def test_explicit_approval_then_one_fixed_device_program_and_verify(self):
        trial = self.trial()
        receipt = trial.run('native')
        self.assertEqual(self.calls, ['scan', 'native'])
        self.assertTrue(receipt['pass'])
        self.assertTrue(receipt['write_attempted'])
        self.assertTrue(receipt['programmer_reported_success'])
        self.assertFalse(receipt['power_on_boot_qualified'])
        self.assertFalse(receipt['factory_restore_boot_qualified'])
        saved = json.loads((self.directory/'native.result.json').read_text())
        self.assertEqual(saved, receipt)
        for other in (trial, self.trial()):
            with self.assertRaises(FileExistsError): other.run('native')
        self.assertEqual(self.calls, ['scan', 'native'])

    def test_missing_or_wrong_authority_is_rejected_before_any_callback(self):
        for bad in ({}, dict(self.approval, source='goal'), dict(self.approval, serial='TRWU8J8G'),
                    dict(self.approval, roles=['factory-recovery']), dict(self.approval, user_text=' '),
                    dict(self.approval, plan_sha256='f'*64), dict(self.approval, roles=[{}]),
                    dict(self.approval, roles=['native', 'native']), dict(self.approval, format=True)):
            with self.subTest(approval=bad), self.assertRaises(ValueError):
                self.trial(approval=bad).run('native')
        self.assertEqual(list(self.directory.iterdir()), [])
        self.assertEqual(self.calls, [])

    def test_fixed_operation_rejects_ambiguous_ports_foreign_boards_and_injection(self):
        for cable in ('1', '2', CABLE.replace('45043', '65536'), CABLE.replace('45043', '0'),
                      CABLE.replace('3-11.1', '3-11.3'), CABLE+';p;other.jic@2'):
            with self.subTest(cable=cable), self.assertRaises(ValueError): flash.command(cable, self.native)
        for path in (None, 'native.jic', '/tmp/a;R;other.jic', '/tmp/a@2.jic', '/tmp/../native.jic'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                flash.validate_image(dict(self.native, path=path))
        link = self.directory/'link.jic'
        link.symlink_to(self.native['path'])
        with self.assertRaises(ValueError): flash.validate_image(dict(self.native, path=str(link)))

    def test_unmatched_restore_backup_or_changed_native_fails_before_scan(self):
        self.plan['backup_sha256'] = 'f'*64
        self.approval['plan_sha256'] = flash.digest(self.plan)
        with self.assertRaisesRegex(ValueError, 'backup'): self.trial().run('native')
        self.assertEqual(self.calls, [])
        self.assertEqual(list(self.directory.iterdir()), [])

    def test_actual_byte_target_array_and_envelope_rejection(self):
        raw = Path(self.native['path']).read_bytes()
        cases = [raw[:-1], raw+b'extra', raw.replace(b'A5EB013BB23B', b'A5EB013BB24B', 1),
                 raw.replace(b'MT25QU128', b'MT25QU256', 1), b'BAD!'+raw[4:],
                 raw[:8]+struct.pack('<I', 9)+raw[12:]]
        path = self.directory/'bad.jic'
        for value in cases:
            path.write_bytes(value)
            image = dict(self.native, path=str(path), sha256=hashlib.sha256(value).hexdigest())
            with self.subTest(size=len(value)), self.assertRaises(ValueError): flash.validate_image(image)
        with self.assertRaises(ValueError): flash.validate_image(dict(self.native, sha256='0'*64))
        with self.assertRaises(ValueError): flash.validate_image(dict(self.native, array_sha256='0'*64))

    def test_scan_failure_is_terminal_without_a_flash_write_or_restore(self):
        trial = self.trial(scan=lambda _:RAW.replace(b'forced-scan rc=0', b'forced-scan rc=87'))
        with self.assertRaises(ValueError): trial.run('native')
        saved = json.loads((self.directory/'native.result.json').read_text())
        self.assertFalse(saved['write_attempted'])
        with self.assertRaises(ValueError): trial.run('factory-recovery')
        with self.assertRaises(FileExistsError): self.trial().run('native')
        self.assertEqual(self.calls, [])

    def test_source_or_transport_drift_at_each_observation_is_terminal(self):
        for changed_call in (1, 2, 3, 4):
            with self.subTest(call=changed_call), tempfile.TemporaryDirectory() as temp:
                self.directory = Path(temp)
                seen = []
                def observe():
                    seen.append(True)
                    return dict(identity(), source_commit='f'*40) if len(seen) == changed_call else identity()
                with self.assertRaises(ValueError): self.trial(observe=observe).run('native')
                saved = json.loads((self.directory/'native.result.json').read_text())
                self.assertEqual(saved['write_attempted'], changed_call == 4)
        self.directory = Path(self.temp.name)
        seen = []
        def changed_clock():
            seen.append(True)
            return dict(identity(), clock='15M') if len(seen) == 4 else identity()
        with self.assertRaisesRegex(ValueError, 'during programming'): self.trial(observe=changed_clock).run('native')

    def test_programmer_failure_does_not_retry_or_restore_automatically(self):
        def uncertain(*_):
            self.calls.append('uncertain')
            raise TimeoutError('unknown programming completion')
        trial = self.trial(program=uncertain)
        with self.assertRaises(TimeoutError): trial.run('native')
        self.assertEqual(self.calls, ['scan', 'uncertain'])
        saved = json.loads((self.directory/'native.result.json').read_text())
        self.assertTrue(saved['write_attempted'])
        self.assertFalse(saved['pass'])
        self.assertFalse(saved['programmer_reported_success'])
        self.assertFalse((self.directory/'factory-recovery.request.json').exists())
        recovered = self.trial().run('factory-recovery')
        self.assertTrue(recovered['pass'])
        self.assertEqual(self.calls, ['scan', 'uncertain', 'scan', 'factory-recovery'])
        with self.assertRaises(FileExistsError): self.trial().run('factory-recovery')

    def test_successful_program_requires_actual_boot_failure_before_restore(self):
        trial = self.trial()
        trial.run('native')
        with self.assertRaises(FileNotFoundError): trial.run('factory-recovery')
        pending = dict(format=1, phase='native-power-on', trace_sha256='a'*64, boot_started=False, failed=True)
        with self.assertRaises(ValueError): trial.record_boot_failure(pending)
        self.assertFalse((self.directory/'native-boot-failure.json').exists())
        trial.record_boot_failure(dict(pending, boot_started=True))
        self.assertTrue(trial.run('factory-recovery')['pass'])
        self.assertEqual(self.calls, ['scan', 'native', 'scan', 'factory-recovery'])

    def test_foreign_boot_failure_cannot_restore_a_successful_program(self):
        trial = self.trial()
        trial.run('native')
        (self.directory/'native-boot-failure.json').write_text(json.dumps(dict(
            plan_sha256=flash.digest(self.plan), native_result_sha256='f'*64, boot_started=True, failed=True)))
        with self.assertRaises(ValueError): trial.run('factory-recovery')
        self.assertEqual(self.calls, ['scan', 'native'])

    def test_programmer_report_must_be_complete_success_without_errors(self):
        for value in (dict(OK, returncode=True), dict(OK, returncode=3), dict(OK, output=b''),
                      dict(OK, output=OK['output']*2), dict(OK, output=OK['output']+b'Error (87): failed\n'),
                      dict(OK, output=OK['output']+b'Warning (1): incomplete verification\n'),
                      dict(OK, output=OK['output'][:-1]), dict(OK, output=b'programmed'), dict(OK, output=b'\xff')):
            with self.subTest(value=value), self.assertRaises(ValueError): flash.programmer_success(value)

    def test_mutating_the_approved_plan_or_approval_during_scan_cannot_program(self):
        for field in ('plan', 'approval'):
            with self.subTest(field=field), tempfile.TemporaryDirectory() as temp:
                self.directory = Path(temp)
                def scan(_):
                    if field == 'plan': trial.plan['source_commit'] = 'f'*40
                    else: trial.approval['user_text'] = 'changed after admission'
                    return RAW
                trial = self.trial(scan=scan)
                with self.assertRaises(ValueError): trial.run('native')
                saved = json.loads((self.directory/'native.result.json').read_text())
                self.assertFalse(saved['write_attempted'])
        self.assertEqual(self.calls, [])

    def test_fsync_failures_prevent_program_and_retire_reservation(self):
        # Request, scan reservation, scan grant, and write intent each have a
        # file fsync and directory fsync before the callback can write flash.
        for failing in range(1, 9):
            with self.subTest(fsync=failing), tempfile.TemporaryDirectory() as temp:
                self.directory = Path(temp)
                count, calls = [], []
                original = jtag.os.fsync
                def fail(fd):
                    count.append(fd)
                    if len(count) == failing: raise OSError('injected fsync failure')
                    original(fd)
                trial = self.trial(program=lambda *args:calls.append(args))
                with patch.object(jtag.os, 'fsync', fail), self.assertRaises(OSError): trial.run('native')
                self.assertEqual(calls, [])
                with self.assertRaises(FileExistsError): self.trial().run('native')

    def test_failed_result_persistence_releases_lock_and_keeps_replay_fuse(self):
        trial = self.trial()
        original = flash._persist
        def fail(path, record):
            if path.name == 'native.result.json': raise OSError('result disk failure')
            original(path, record)
        with patch.object(flash, '_persist', fail), self.assertRaises(OSError): trial.run('native')
        with self.assertRaises(FileExistsError): trial.run('native')
        self.assertEqual(self.calls, ['scan', 'native'])

    def test_concurrent_caller_cannot_acquire_another_programming_grant(self):
        entered, release = threading.Event(), threading.Event()
        def scanning(_):
            entered.set()
            if not release.wait(5): raise TimeoutError('model barrier')
            return RAW
        trial = self.trial(scan=scanning)
        errors = []
        def run():
            try: trial.run('native')
            except BaseException as error: errors.append(error)
        worker = threading.Thread(target=run)
        worker.start()
        try:
            self.assertTrue(entered.wait(5))
            with self.assertRaisesRegex(ValueError, 'active'): trial.run('native')
            with self.assertRaises(FileExistsError): self.trial().run('native')
        finally:
            release.set()
            worker.join(5)
        self.assertFalse(worker.is_alive())
        self.assertEqual(errors, [])
        self.assertEqual(self.calls, ['native'])


if __name__ == '__main__':
    unittest.main()
