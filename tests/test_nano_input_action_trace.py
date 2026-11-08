"""Join the actual Go action emitter to serial and paired collectors, off board."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from test_nano_tenfoot_trace import Model, ROOT, trace
from test_nano_paired_tenfoot_trace import Model as PairedModel, paired
from test_nano_shared_tenfoot_trace import SharedModel
from test_nano_program_client import OWNER
import input_action_trace as actions

FIXTURE = ROOT/'tests/fixtures/nano-input-actions'


class InputActionTraceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        compiler = shutil.which('go')
        if compiler is None:
            raise AssertionError('Go is required for the actual producer/collector join')
        with tempfile.TemporaryDirectory(prefix='fes-input-action-producer-') as temp:
            temp = Path(temp)
            output, overlay = temp/'producer.txt', temp/'overlay.json'
            virtual = ROOT/'platform/embedded/tamago/nativefabric/program_action_export_test.go'
            overlay.write_text(json.dumps({'Replace': {str(virtual): str(FIXTURE/'export_test.go')}}))
            import os
            env = dict(os.environ, FES_INPUT_ACTION_EXPORT_PATH=str(output), FES_INPUT_ACTION_EXPORT_OWNER=OWNER)
            result = subprocess.run([compiler, '-C', str(ROOT/'platform/embedded/tamago'),
                'test', '-overlay', str(overlay), '-count=1', '-timeout=90s',
                '-run', '^TestExportActualProgramInputActions$', './nativefabric'],
                env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
            if result.returncode != 0:
                raise AssertionError(result.stdout.decode(errors='replace'))
            cls.produced = output.read_bytes()

    def test_actual_go_emitter_matches_captured_host_fixture(self):
        self.assertEqual(self.produced, (FIXTURE/'producer.txt').read_bytes())
        provenance = json.loads((FIXTURE/'provenance.json').read_text())
        self.assertEqual(hashlib.sha256(self.produced).hexdigest(), provenance['fixture_sha256'])
        self.assertEqual(hashlib.sha256((FIXTURE/'export_test.go').read_bytes()).hexdigest(),
                         provenance['replay_exporter_sha256'])
        self.assertFalse(provenance['physical_UART_capture'])
        self.assertEqual(provenance['firmware_calls'], 0)
        rows = [actions.record(line, i, 'uart') for i, line in enumerate(self.produced.splitlines(), 1)]
        self.assertEqual([row['action'] for row in rows], ['select', 'stop', 'select', 'stop', 'select', 'stop', 'quit'])

    def test_current_producer_records_complete_both_collectors(self):
        for model, paired_mode in ((Model(), False), (SharedModel('boot-keyboard'), False),
                                   (PairedModel(), True), (PairedModel(True, 'boot-keyboard'), True)):
            with self.subTest(paired=paired_mode, shared=hasattr(model, 'policy')):
                client = model.client()
                result = (paired.exercise if paired_mode else trace.exercise)(client)
                self.assertTrue(result['passed'])
                observed = [action for row in client.records for action in row['input_actions']]
                self.assertEqual([row['action'] for row in observed],
                                 ['select', 'stop', 'select', 'stop', 'select', 'stop', 'quit'])
                self.assertTrue(all(row['source'] == 'uart' and row['owner'] == int(OWNER, 16) for row in observed))
                self.assertFalse(result['hardware_acceptance'])

    def test_malformed_or_unselected_sources_are_rejected(self):
        good = self.produced.splitlines()[0]
        for bad in (good+b' extra=1', good.replace(b'action=select', b'action=retry'),
                    good.replace(b'source=uart', b'source=usb-gamepad'),
                    good.replace(b'owner='+OWNER.encode(), b'owner=0'),
                    good.replace(b'owner='+OWNER.encode(), b'owner=0'+OWNER.encode()),
                    good.replace(b'owner='+OWNER.encode(), b'owner=' + b'f'*17),
                    good.replace(b'native_generation=0', b'native_generation=00'),
                    good.replace(b'native_generation=0', b'native_generation=18446744073709551616')):
            with self.subTest(raw=bad), self.assertRaises(ValueError):
                actions.record(bad, 1, 'uart')

    def test_valid_lexical_usb_record_does_not_authorize_a_uart_action(self):
        raw = self.produced.splitlines()[0].replace(b'source=uart', b'source=usb-boot-keyboard')
        row = actions.record(raw, 1, 'uart+shared-usb-boot-keyboard')
        with self.assertRaisesRegex(ValueError, 'uncommanded USB'):
            actions.validate([row], 'launch', int(OWNER, 16), 0)

    def test_absent_duplicate_and_foreign_observations_cannot_complete_an_action(self):
        good = actions.record(self.produced.splitlines()[0], 1, 'uart')
        for rows in ([], [good, good], [dict(good, owner=1)],
                     [dict(good, native_generation=2)], [dict(good, state='running')]):
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                actions.validate(rows, 'launch', int(OWNER, 16), 0)

    def test_startup_action_is_rejected_before_transport(self):
        model = PairedModel(True, 'boot-keyboard')
        with self.assertRaises(ValueError):
            model.client(initial=model.initial+self.produced.splitlines(keepends=True)[0])
        self.assertEqual(model.keys, [])

    def test_actions_never_replace_firmware_or_drain_evidence(self):
        for fault in ('missing-drain', 'missing-core-stop', 'queue', 'bad-ack'):
            model = PairedModel(True, 'boot-keyboard')
            client = model.client()
            model.fault = fault
            with self.subTest(fault=fault), self.assertRaises(ValueError):
                paired.exercise(client)
            self.assertTrue(client.retired)

    def test_missing_action_in_full_exchange_retires_before_another_key(self):
        model = PairedModel(True, 'boot-keyboard')
        original = model.corrupt
        model.corrupt = lambda raw, key: b''.join(line for line in original(raw, key).splitlines(keepends=True)
                                                if not line.startswith(actions.PREFIX))
        client = model.client()
        with self.assertRaisesRegex(ValueError, 'input-action count'):
            client.key('launch', 0)
        sent = list(model.keys)
        with self.assertRaises(ValueError):
            client.key('launch', 0)
        self.assertEqual(model.keys, sent)

    def test_early_quit_and_action_after_pause_are_rejected(self):
        for fault in ('early-quit', 'late-input'):
            model = PairedModel(True, 'boot-keyboard')
            client = model.client()
            if fault == 'early-quit':
                client.key('launch', 0)
            original = model.corrupt
            def corrupt(raw, key):
                raw = original(raw, key)
                target = next(line for line in raw.splitlines(keepends=True)
                    if line.startswith(actions.PREFIX) and (b'action=quit ' if fault == 'early-quit' else b'action=select ') in line)
                raw = raw.replace(target, b'', 1)
                if fault == 'early-quit':
                    return target+raw
                pause_end = raw.index(b'\n', raw.index(b'FES_NATIVE_SHARED_USB INPUT phase=pause'))+1
                return raw[:pause_end]+target+raw[pause_end:]
            model.corrupt = corrupt
            with self.subTest(fault=fault), self.assertRaises(ValueError):
                client.key('quit' if fault == 'early-quit' else 'launch', None if fault == 'early-quit' else 0)
            self.assertTrue(client.retired)
