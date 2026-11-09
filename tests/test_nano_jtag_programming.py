"""Host image programming requires one fresh scan and cannot reuse admission."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import tempfile
import threading
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_jtag_programming', ROOT/'platform/embedded/nano/jtag_programming.py')
jtag = importlib.util.module_from_spec(spec)
spec.loader.exec_module(jtag)
RAW = (ROOT/'tests/fixtures/nano-jtag/controlled-fixed-clock-scan.txt').read_bytes()
CABLE = re.search(rb'^FES_JTAG_SCAN CABLE (.+)$', RAW, re.M)[1].decode()
IMAGE = 'a'*64


def identity():
    return dict(serial='TRWU8CBL', source_commit='1'*40, claim_token='2'*16,
                pid=1234, start_ticks='5678', cable=CABLE, config_sha256='3'*64,
                clock='3M', auto_adjust=0, client_sha256={p:'4'*64 for p in jtag.LIBRARIES}, probe_sha256='5'*64)


class ProgrammingTests(unittest.TestCase):
    def gate(self, directory, *, observe=None, scan=None, program=None, name='native'):
        path = Path(directory)/(name+'.json')
        calls = []
        def scanning(cable):
            calls.append('scan')
            self.assertEqual(cable, CABLE)
            self.assertEqual(json.loads(path.read_text())['state'], 'reserved-before-scan')
            return RAW
        def programming(cable):
            calls.append('program')
            self.assertEqual(cable, CABLE)
            proof = json.loads(path.with_name(path.name+'.scan.json').read_text())
            self.assertEqual(proof['image_sha256'], IMAGE)
            self.assertEqual(proof['state'], 'consumed-before-programming')
            self.assertEqual(proof['live_scan_sha256'], hashlib.sha256(RAW).hexdigest())
            return 'program-result'
        return jtag.ProgramAttempt(path, observe or identity, scan or scanning, program or programming), calls

    def test_controlled_fixed_scan_then_durable_consumption_and_single_program(self):
        self.assertEqual(hashlib.sha256(RAW).hexdigest(), '9c12980e0e559c97abedf814b2d234ee2753a43a6778be0d1c0e68d01ad06f58')
        with tempfile.TemporaryDirectory() as temp:
            gate, calls = self.gate(temp)
            record, result = gate.run('native', IMAGE)
            self.assertEqual(result, 'program-result')
            self.assertEqual(record['transport'], identity())
            self.assertEqual(calls, ['scan', 'program'])
            with self.assertRaises(ValueError): gate.run('native', IMAGE)
            other, other_calls = self.gate(temp)
            with self.assertRaises(FileExistsError): other.run('recovery', IMAGE)
            self.assertEqual(other_calls, [])

    def test_each_programming_phase_requires_its_own_scan(self):
        with tempfile.TemporaryDirectory() as temp:
            for phase in ('recovery', 'native'):
                gate, calls = self.gate(temp, name=phase)
                record, _ = gate.run(phase, IMAGE)
                self.assertEqual(record['phase'], phase)
                self.assertEqual(calls, ['scan', 'program'])

    def test_failed_incomplete_or_listing_only_scan_never_programs_or_replays(self):
        failures = [(ROOT/'tests/fixtures/nano-jtag/vendor-live-scan.txt').read_bytes(),
                    RAW.replace(b'AUTO before=0',b'AUTO before=1'), RAW.replace(b'AUTO after=0',b'AUTO after=1'),
                    RAW.replace(b'CLOCK after=3000000',b'CLOCK after=6000000'), b'', None, RAW.decode(), RAW[:-1], RAW+b'Error (213019): scan failed\n',
                    b'1) '+CABLE.encode()+b'\n  4362C0DD device\n',
                    RAW.replace(b'phase=forced-scan rc=0', b'phase=forced-scan rc=87'),
                    RAW.replace(b'FES_JTAG_SCAN CALL phase=forced-scan rc=0\n', b''),
                    RAW.replace(b'phase=cached-after', b'phase=cached-before'),
                    RAW.replace(b'id=4362C0DD', b'id=12345679'),
                    RAW.replace(b'count=1', b'count=2'), RAW.replace(b'ir=10', b'ir=0'),
                    RAW.replace(b'CLOCK 3000000', b'CLOCK 15000000'),
                    RAW.replace(b'BINDING forced-scan '+jtag.BINDING.encode(), b'BINDING forced-scan /tmp/other.so'),
                    RAW.replace(b'FES_JTAG_SCAN LIBRARY '+jtag.LIBRARIES[0].encode(), b'FES_JTAG_SCAN LIBRARY /tmp/foreign.so'),
                    RAW.replace(b'pass=1', b'pass=0'), RAW.replace(b'cpu_reset=0', b'cpu_reset=1'),
                    RAW+b'\xff', RAW.replace(b'\n', b'\r\n'), RAW+RAW]
        for bad in failures:
            with self.subTest(raw=repr(bad)[:80]), tempfile.TemporaryDirectory() as temp:
                programmed = []
                gate, _ = self.gate(temp, scan=lambda _:bad, program=lambda _:programmed.append(True))
                with self.assertRaises(ValueError): gate.run('native', IMAGE)
                with self.assertRaises(ValueError): gate.run('native', IMAGE)
                other, other_calls = self.gate(temp)
                with self.assertRaises(FileExistsError): other.run('native', IMAGE)
                self.assertEqual(programmed, [])
                self.assertEqual(other_calls, [])

    def test_any_identity_change_during_scan_or_before_programming_rejects(self):
        changes = dict(serial='TRWU8J8G', source_commit='9'*40, claim_token='9'*16,
                       pid=1235, start_ticks='5679', cable=CABLE.replace('45043','45044'),
                       config_sha256='9'*64, clock='15M', auto_adjust=1,
                       client_sha256={p:'9'*64 for p in jtag.LIBRARIES}, probe_sha256='9'*64)
        for field, value in changes.items():
            for changed_call in (2, 3):
                with self.subTest(field=field, call=changed_call), tempfile.TemporaryDirectory() as temp:
                    shared = identity(); observed = []; programmed = []
                    def observe():
                        observed.append(True)
                        if len(observed)==changed_call: shared[field] = value
                        return shared  # Reusing and mutating this dictionary must not change the snapshot.
                    gate, calls = self.gate(temp, observe=observe, program=lambda _:programmed.append(True))
                    with self.assertRaises(ValueError): gate.run('native', IMAGE)
                    self.assertEqual(programmed, [])
                    self.assertEqual(calls, ['scan'])

    def test_incomplete_or_foreign_identity_rejects_before_scan(self):
        bads = [None, {}, dict(identity(), serial='TRWU8J8G'), dict(identity(), pid=True),
                dict(identity(), cable='DE25-Nano'), dict(identity(), cable=CABLE.replace('45043','65536')),
                dict(identity(), clock='15M'), dict(identity(), clock='6M'), dict(identity(), auto_adjust=1),
                dict(identity(), auto_adjust=False), dict(identity(), auto_adjust='0'), dict(identity(), client_sha256={}),
                dict(identity(), probe_sha256=None), dict(identity(), source_commit='1'*64)]
        for bad in bads:
            with self.subTest(bad=bad), tempfile.TemporaryDirectory() as temp:
                gate, calls = self.gate(temp, observe=lambda:bad)
                with self.assertRaises(ValueError): gate.run('native', IMAGE)
                self.assertEqual(calls, [])

    def test_scan_or_program_exception_is_terminal_across_instances(self):
        for failing in ('scan', 'program'):
            with self.subTest(failing=failing), tempfile.TemporaryDirectory() as temp:
                def fail(_): raise RuntimeError('injected '+failing)
                gate, _ = self.gate(temp, **{failing:fail})
                with self.assertRaisesRegex(RuntimeError, 'injected'): gate.run('native', IMAGE)
                other, calls = self.gate(temp)
                with self.assertRaises(FileExistsError): other.run('native', IMAGE)
                self.assertEqual(calls, [])

    def test_persistence_failure_or_foreign_scan_record_prevents_programming(self):
        for call in (1, 2, 3, 4):
            with self.subTest(fsync=call), tempfile.TemporaryDirectory() as temp:
                gate, calls = self.gate(temp)
                fsync = jtag.os.fsync; seen = []
                def fail(fd):
                    seen.append(fd)
                    if len(seen)==call: raise OSError('injected durable write failure')
                    fsync(fd)
                with patch.object(jtag.os, 'fsync', fail), self.assertRaises(OSError): gate.run('native', IMAGE)
                self.assertNotIn('program', calls)
                other, retry_calls = self.gate(temp)
                with self.assertRaises(FileExistsError): other.run('native', IMAGE)
                self.assertEqual(retry_calls, [])
        with tempfile.TemporaryDirectory() as temp:
            gate, calls = self.gate(temp)
            gate.consumed.with_name(gate.consumed.name+'.scan.json').write_text('foreign\n')
            with self.assertRaises(FileExistsError): gate.run('native', IMAGE)
            self.assertEqual(calls, ['scan'])
            self.assertEqual(gate.consumed.with_name(gate.consumed.name+'.scan.json').read_text(), 'foreign\n')

    def test_concurrent_call_cannot_take_over_active_scan(self):
        with tempfile.TemporaryDirectory() as temp:
            entered, release = threading.Event(), threading.Event()
            def scan(_):
                entered.set()
                if not release.wait(3): raise TimeoutError('test scan barrier')
                return RAW
            gate, calls = self.gate(temp, scan=scan)
            errors = []
            def run():
                try:gate.run('native', IMAGE)
                except BaseException as error:errors.append(error)
            thread = threading.Thread(target=run);thread.start()
            try:
                self.assertTrue(entered.wait(3))
                with self.assertRaisesRegex(ValueError, 'active'):gate.run('native', IMAGE)
                other, other_calls = self.gate(temp)
                with self.assertRaises(FileExistsError):other.run('native', IMAGE)
                self.assertEqual(other_calls, [])
            finally:release.set();thread.join(3)
            self.assertFalse(thread.is_alive());self.assertEqual(errors, []);self.assertEqual(calls, ['program'])
