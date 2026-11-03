import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import errno
import socket
import time
import types
from urllib.error import URLError

from scripts import pcxt_health_evidence as h


def health(address='http://fes-kit-a:8182', reachable=True):
    return {'ready': True, 'token': 'secret-do-not-log',
            'target': {'reachable': reachable, 'ready': reachable, 'api_version': 'v1',
                       'connection': {'state': 'ready' if reachable else 'disconnected',
                                      'address': address, 'target_id': 'target-id',
                                      'boot_id': 'boot-id', 'message': ''},
                       'software': {'active_bundle': 'bundle-id', 'selected': 'bundle-id',
                                    'base_identity': 'image-id'}}}


class Api:
    def __init__(self, values): self.values = list(values); self.calls = []
    def get(self, path, **kwargs):
        self.calls.append((path, kwargs))
        value = self.values.pop(0)
        if isinstance(value, BaseException): raise value
        return value


class HealthEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.observer = h.HealthEvidence(self.root, 'target-id', 'boot-id', 'bundle-id', 'image-id')
        self.addCleanup(self.observer.close)

    def test_same_response_and_no_extra_request_with_safe_failure_context(self):
        good = health()
        bad = health('http://192.0.2.3:8182', reachable=False)
        bad['target']['connection']['message'] = 'Target status could not be reconciled.'
        api = Api([good, bad]); self.observer.install(api)
        self.assertIs(api.get('/api/v1/health', limit=123), good)
        self.assertTrue((self.root / h.HealthEvidence.FILE).exists())
        self.assertIs(api.get('/api/v1/health'), bad)
        self.assertEqual(len(api.calls), 2)
        path = self.root / h.HealthEvidence.FILE
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        self.assertEqual(len(rows), 2)
        self.assertFalse(rows[0]['anomaly']); self.assertTrue(rows[1]['anomaly'])
        self.assertEqual(rows[0]['endpoint']['kind'], 'hostname')
        self.assertEqual(rows[1]['endpoint']['kind'], 'ipv4')
        self.assertNotEqual(rows[0]['endpoint']['fingerprint'], rows[1]['endpoint']['fingerprint'])
        self.assertEqual(rows[1]['connection_error'], 'target_status')
        self.assertEqual(rows[1]['target_id'], 'match')
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertNotIn('secret-do-not-log', path.read_text())
        self.assertNotIn('192.0.2.3', path.read_text())

    def test_request_exception_is_reraised_once_without_error_text(self):
        error = OSError('private credential in raw message')
        api = Api([error]); self.observer.install(api)
        with self.assertRaises(OSError) as raised: api.get('/api/v1/health')
        self.assertIs(raised.exception, error); self.assertEqual(len(api.calls), 1)
        row = json.loads((self.root / h.HealthEvidence.FILE).read_text())
        self.assertEqual(row['error_category'], 'other_redacted')
        self.assertNotIn('credential', json.dumps(row))

    def test_other_endpoint_is_not_observed_or_changed(self):
        result = {'token': 'leave untouched'}
        api = Api([result]); self.observer.install(api)
        self.assertIs(api.get('/api/v1/session'), result)
        self.assertFalse((self.root / h.HealthEvidence.FILE).exists())

    def test_missing_selected_bundle_is_recorded_without_changing_response(self):
        value = health()
        value['target']['software']['selected'] = ''
        api = Api([value]); self.observer.install(api)
        self.assertIs(api.get('/api/v1/health'), value)
        row = json.loads((self.root / h.HealthEvidence.FILE).read_text())
        self.assertEqual(row['selected_bundle'], 'invalid')
        self.assertTrue(row['anomaly'])
        self.assertEqual(len(api.calls), 1)

    def test_unpinned_transition_fields_are_explicitly_not_checked(self):
        observer = h.HealthEvidence(self.root, 'target-id', None, None, 'image-id')
        self.addCleanup(observer.close)
        value = health(); value['target']['software']['selected'] = ''
        api = Api([value]); observer.install(api)
        self.assertIs(api.get('/api/v1/health'), value)
        self.assertTrue((self.root / h.HealthEvidence.FILE).exists())
        self.assertEqual(h.project(value, 'target-id', None, None, 'image-id')['selected_bundle'], 'not_checked')

    def test_evidence_failure_blocks_before_returning_anomalous_health(self):
        api = Api([health(reachable=False)]); self.observer.install(api)
        (self.root / h.HealthEvidence.FILE).write_text('pre-existing data')
        with self.assertRaises(FileExistsError): api.get('/api/v1/health')
        self.assertEqual(len(api.calls), 0)
        self.assertEqual((self.root / h.HealthEvidence.FILE).read_text(), 'pre-existing data')
        self.assertTrue(self.observer.compromised)
        with self.assertRaisesRegex(h.EvidenceError, 'compromised'): api.get('/api/v1/session')
        self.assertEqual(len(api.calls), 0)

    def test_original_critical_exception_is_preserved_if_logging_also_fails(self):
        class CriticalIdentityError(RuntimeError): pass
        error = CriticalIdentityError('critical type, secret text')
        api = Api([error]); self.observer.install(api)
        with mock.patch.object(self.observer, '_write', side_effect=OSError('disk full')):
            with self.assertRaises(CriticalIdentityError) as raised: api.get('/api/v1/health')
        self.assertIs(raised.exception, error)
        self.assertTrue(self.observer.compromised)
        self.assertIn('claim must be retained', raised.exception.__notes__[0])
        with self.assertRaisesRegex(h.EvidenceError, 'compromised'): api.get('/api/v1/session')
        self.assertEqual(len(api.calls), 1)

    def test_projection_failure_preserves_exception_and_compromises_observer(self):
        error = RuntimeError('projection failure')
        api = Api([health()]); self.observer.install(api)
        with mock.patch.object(h, 'project', side_effect=error):
            with self.assertRaises(RuntimeError) as raised: api.get('/api/v1/health')
        self.assertIs(raised.exception, error)
        self.assertTrue(self.observer.compromised)
        self.assertIn('claim must be retained', raised.exception.__notes__[0])

    def test_nonprivate_directory_and_output_tampering_fail_closed(self):
        self.root.chmod(0o755)
        api = Api([health(reachable=False)]); self.observer.install(api)
        with self.assertRaisesRegex(h.EvidenceError, 'directory is not private'):
            api.get('/api/v1/health')
        self.root.chmod(0o700)
        self.observer = h.HealthEvidence(self.root, 'target-id', 'boot-id', 'bundle-id', 'image-id')
        self.addCleanup(self.observer.close)
        api = Api([health(reachable=False), health(reachable=False)])
        self.observer.install(api); api.get('/api/v1/health')
        with (self.root / h.HealthEvidence.FILE).open('ab') as stream: stream.write(b'tamper')
        with self.assertRaisesRegex(h.EvidenceError, 'file changed'):
            api.get('/api/v1/health')

    def test_safe_categories_and_endpoint_credential_rejection(self):
        self.assertEqual(h.message_category('dial: connection refused; secret'), 'connection_refused')
        self.assertEqual(h.message_category('EOF'), 'eof')
        self.assertEqual(h.message_category('Client.Timeout awaiting headers'), 'timeout')
        self.assertEqual(h.message_category('sensitive-target-token'), 'other_redacted')
        self.assertEqual(h.endpoint('http://user:password@example.test:8182')['kind'], 'invalid')
        self.assertEqual(h.endpoint('http://example.test:8182?token=secret')['kind'], 'invalid')
        self.assertEqual(h.endpoint('http://[2001:db8::1]:8182')['kind'], 'ipv6')

    def test_wrapped_transport_error_category_never_exposes_text(self):
        inner = ConnectionRefusedError(errno.ECONNREFUSED, 'private host and secret')
        wrapped = RuntimeError('wrapper text')
        wrapped.__cause__ = URLError(inner)
        self.assertEqual(h.exception_category(wrapped), 'connection_refused')
        wrapped.__cause__ = URLError(socket.gaierror(socket.EAI_NONAME, 'secret'))
        self.assertEqual(h.exception_category(wrapped), 'dns')

    def test_bounded_records_refuse_overflow(self):
        self.observer.MAX_RECORDS = 1
        api = Api([health(), health(reachable=False)]); self.observer.install(api)
        api.get('/api/v1/health')
        with self.assertRaisesRegex(h.EvidenceError, 'limit reached'):
            api.get('/api/v1/health')
        self.assertTrue((self.root / h.HealthEvidence.FILE).exists())

    def test_parent_prepared_evidence_survives_sequential_forked_observers(self):
        self.observer.prepare()
        for value in (health(), health(reachable=False)):
            pid = os.fork()
            if pid == 0:
                try:
                    api = Api([value]); self.observer.install(api)
                    api.get('/api/v1/health')
                    os._exit(0)
                except BaseException:
                    os._exit(1)
            _, status = os.waitpid(pid, 0)
            self.assertEqual(os.waitstatus_to_exitcode(status), 0)
        rows = [json.loads(line) for line in (self.root / h.HealthEvidence.FILE).read_text().splitlines()]
        self.assertEqual([row['anomaly'] for row in rows], [False, True])

    def test_traced_get_preserves_request_and_redacts_path(self):
        self.observer.begin_trace(); self.observer.tracing = True
        response = {'secret': 'hide'}
        api = Api([response]); self.observer.install(api)
        self.assertIs(api.get('/api/v1/session?token=secret', timeout=3), response)
        self.assertEqual(api.calls, [('/api/v1/session?token=secret', {'timeout': 3})])
        self.observer.end_trace()
        raw = (self.root / h.HealthEvidence.TRACE_FILE).read_text()
        rows = [json.loads(line) for line in raw.splitlines()]
        self.assertEqual([row['stage'] for row in rows], ['target_session_get'] * 2)
        self.assertEqual([row['event'] for row in rows], ['start', 'finish'])
        self.assertEqual(rows[0]['id'], rows[1]['id'])
        self.assertNotIn('secret', raw)

    def test_stage_failure_preserves_exception_and_evidence_failure_compromises(self):
        self.observer.begin_trace()
        original = KeyboardInterrupt('do not log')
        real_event = self.observer._stage_event
        def first_only(row):
            if row['event'] == 'finish':
                self.observer.compromised = True
                raise OSError('pipe full')
            return real_event(row)
        with mock.patch.object(self.observer, '_stage_event', side_effect=first_only):
            with self.assertRaises(KeyboardInterrupt) as error:
                self.observer.run_stage('claim', lambda: (_ for _ in ()).throw(original))
        self.assertIs(error.exception, original)
        self.assertTrue(self.observer.compromised)
        self.assertIn('claim must be retained', error.exception.__notes__[0])
        self.observer.end_trace()
        raw = (self.root / h.HealthEvidence.TRACE_FILE).read_text()
        self.assertNotIn('do not log', raw)

    def test_failed_start_prevents_callback_and_unknown_label_rejected(self):
        called = []
        self.observer.begin_trace()
        with self.assertRaises(h.EvidenceError):
            self.observer.run_stage('private value', lambda: called.append(1))
        with mock.patch.object(h.os, 'write', side_effect=OSError('pipe full')):
            with self.assertRaises(OSError):
                self.observer.run_stage('claim', lambda: called.append(2))
        self.assertEqual(called, [])
        self.assertTrue(self.observer.compromised)

    def test_guard_patch_restores_all_functions_on_failure(self):
        self.observer.begin_trace()
        def function(*args): return 7
        visual = types.SimpleNamespace(load_font=function)
        app = types.SimpleNamespace(visual=visual, check_claim=function,
                                    source_provenance=function, check_private_config=function)
        def callback():
            self.assertEqual(app.visual.load_font(), 7)
            app.check_claim()
            raise ValueError('private')
        with self.assertRaises(ValueError): h.traced_app_validation(app, self.observer, callback)
        for obj, name in ((app, 'check_claim'), (app, 'source_provenance'),
                          (app, 'check_private_config'), (visual, 'load_font')):
            self.assertIs(getattr(obj, name), function)
        self.assertFalse(self.observer.tracing)
        self.observer.end_trace()
        rows = [json.loads(x) for x in (self.root / h.HealthEvidence.TRACE_FILE).read_text().splitlines()]
        self.assertEqual([r['stage'] for r in rows], ['guard', 'font', 'font', 'claim', 'claim', 'guard'])
        self.assertFalse(rows[-1]['ok'])
        self.assertNotIn('private', json.dumps(rows))

    def test_killed_fork_retains_unfinished_stage(self):
        self.observer.begin_trace()
        ready_read, ready_write = os.pipe()
        pid = os.fork()
        if pid == 0:
            os.close(ready_read)
            try:
                def stuck():
                    os.write(ready_write, b'R')
                    time.sleep(60)
                self.observer.run_stage('source_provenance', stuck)
            finally: os._exit(0)
        os.close(ready_write)
        try:
            self.assertEqual(os.read(ready_read, 1), b'R')
        finally:
            os.kill(pid, 9); os.waitpid(pid, 0); os.close(ready_read)
        self.observer.end_trace()
        rows = [json.loads(x) for x in (self.root / h.HealthEvidence.TRACE_FILE).read_text().splitlines()]
        self.assertEqual([(r['stage'], r['event']) for r in rows], [('source_provenance', 'start')])

    def test_stage_limit_fails_closed(self):
        self.observer.MAX_STAGE_RECORDS = 1
        self.observer.begin_trace()
        self.observer.run_stage('claim', lambda: None)
        with self.assertRaisesRegex(h.EvidenceError, 'limit reached'):
            self.observer.end_trace()
        self.assertTrue(self.observer.compromised)

    def test_retained_writer_is_detected_without_blocking(self):
        self.observer.begin_trace()
        leaked = os.dup(self.observer.trace_fd)
        try:
            with self.assertRaisesRegex(h.EvidenceError, 'writer still open'):
                self.observer.end_trace()
            self.assertTrue(self.observer.compromised)
        finally:
            os.close(leaked)

    def test_partial_pipe_write_fails_before_guard_callback(self):
        self.observer.begin_trace()
        with mock.patch.object(h.os, 'write', return_value=0):
            with self.assertRaisesRegex(h.EvidenceError, 'record incomplete'):
                self.observer.run_stage('claim', lambda: self.fail('callback ran'))
        self.assertTrue(self.observer.compromised)

    def test_actual_full_pipe_fails_before_guard_callback(self):
        self.observer.begin_trace()
        # Fill the kernel pipe directly; small stage writes must not block.
        while True:
            try: os.write(self.observer.trace_fd, b'x' * 512)
            except BlockingIOError: break
        with self.assertRaises(BlockingIOError):
            self.observer.run_stage('claim', lambda: self.fail('callback ran'))
        self.assertTrue(self.observer.compromised)
        # Discard deliberately invalid test bytes; never accept them as evidence.
        with self.assertRaises(h.EvidenceError): self.observer.end_trace()

    def test_pipe_full_prevents_guard_callback(self):
        self.observer.begin_trace()
        with mock.patch.object(h.os, 'write', side_effect=BlockingIOError('full')):
            with self.assertRaises(BlockingIOError):
                self.observer.run_stage('claim', lambda: self.fail('callback ran'))
        self.assertTrue(self.observer.compromised)


if __name__ == '__main__': unittest.main()
