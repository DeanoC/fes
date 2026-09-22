import importlib.util
import json
import os
from pathlib import Path
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

spec = importlib.util.spec_from_file_location('kit', Path(__file__).parents[1] / 'scripts/kit.py')
kit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(kit)


class KitTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.reject_renew = False
        self.renew_override = None
        self.takeovers = 0
        self.load_status = 200
        self.load_error = None
        self.stop_response = None
        self.boot_id = 'boot-1'
        self.rebooted = False
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.do_POST()

            def do_POST(self):
                body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                outer.calls.append((self.path, dict(self.headers), body))
                status = 200
                response = {'state': 'held', 'generation': 'g1',
                            'expires_in_ms': 90000, 'expires_at': '1970-01-01T00:01:30Z'}
                if self.path == '/v1/health':
                    response = {'ready': True, 'boot_id': outer.boot_id}
                elif self.headers.get('Authorization') != 'Bearer private-bearer':
                    status = 401
                elif self.path in ('/v1/kit/claim', '/v1/kit/takeover'):
                    response = {'status': response, 'token': 'private-lease'}
                    if self.path.endswith('takeover'):
                        outer.takeovers += 1
                        if outer.takeovers == 1:
                            status = 409
                elif self.path != '/v1/kit/lease' and self.headers.get(kit.HEADER) != 'private-lease':
                    status = 409
                elif self.path.endswith('/rbf') and outer.load_status != 200:
                    status = outer.load_status
                    if outer.load_error is not None:
                        response = outer.load_error
                elif self.path == '/v1/stop' and outer.stop_response is not None:
                    response = dict(outer.stop_response)
                elif self.path == '/v1/development/recover-idle':
                    response = {'state': 'idle'}
                elif self.path == '/v1/development/reboot':
                    outer.rebooted = True
                    outer.boot_id = 'boot-2'
                    response = {'state': 'stopping', 'development': True,
                                'recovery': 'reboot_required'}
                elif self.path.endswith('renew') and outer.reject_renew:
                    status = 409
                if self.path == '/v1/kit/lease' and outer.rebooted:
                    response = {'state': 'free', 'generation': 'g2',
                                'expires_in_ms': 0, 'expires_at': '0001-01-01T00:00:00Z'}
                if self.path.endswith('renew') and status == 200:
                    response = outer.renew_override or {'status': response, 'token': 'private-lease'}
                if status != 200 and outer.load_error is None:
                    response = {'error': 'private-bearer private-lease'}
                raw = json.dumps(response).encode()
                self.send_response(status)
                self.send_header('Content-Length', str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever)
        self.thread.start()
        self.client = kit.Client(f'http://127.0.0.1:{self.server.server_port}', 'private-bearer')

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_commands_buffer_multiple_lines_without_waiting_for_more_input(self):
        read_fd, write_fd = os.pipe()
        try:
            os.write(write_fd, b'status\nstop\n')
            commands = kit.read_commands(read_fd)
            self.assertEqual(next(commands), 'status')
            self.assertEqual(next(commands), 'stop')
            os.write(write_fd, b'release')
            os.close(write_fd)
            write_fd = None
            self.assertEqual(next(commands), 'release')
            with self.assertRaises(StopIteration):
                next(commands)
        finally:
            os.close(read_fd)
            if write_fd is not None:
                os.close(write_fd)

    def test_session_renews_loads_stops_releases(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=.02)
        session.claim()
        try:
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'test.rbf'
                payload = bytes(range(256)) * 600
                path.write_bytes(payload)
                session.mutate('load', path)
            time.sleep(.07)
            session.mutate('stop')
        finally:
            session.close()
        upload = next(call for call in self.calls if call[0].endswith('/rbf'))
        self.assertEqual(upload[2], payload)
        self.assertEqual(upload[1]['Content-Length'], str(len(payload)))
        self.assertNotIn('Transfer-Encoding', upload[1])
        self.assertTrue(any(call[0].endswith('/renew') for call in self.calls))
        self.assertEqual(self.calls[-1][0], '/v1/kit/release')
        self.assertFalse(any(call[0].endswith('/reboot') for call in self.calls))

    def test_lost_renewal_disables_mutation(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=.01)
        session.claim()
        self.reject_renew = True
        self.assertTrue(session.failed.wait(1))
        before = len(self.calls)
        with self.assertRaisesRegex(kit.KitError, 'mutations disabled'):
            session.mutate('stop')
        self.assertEqual(len(self.calls), before)
        session.close()

    def test_invalid_renewal_disables_mutation_and_keeps_original_token(self):
        for grant in (
                {'status': {'state': 'held', 'generation': 'g1', 'expires_in_ms': 90000}, 'token': 'changed'},
                {'status': {'state': 'held', 'generation': 'g2', 'expires_in_ms': 90000}, 'token': 'private-lease'},
                {'status': {'state': 'free', 'generation': 'g1', 'expires_in_ms': 90000}, 'token': 'private-lease'},
                {'status': {'state': 'held', 'generation': 'g1', 'expires_in_ms': 0}, 'token': 'private-lease'},
                {'status': {'state': 'held', 'generation': 'g1', 'expires_in_ms': True}, 'token': 'private-lease'},
                {'status': {'state': 'held', 'generation': 'g1'}, 'token': 'private-lease'}):
            with self.subTest(grant=grant):
                session = kit.Session(self.client, 'agent', 'bringup', interval=.01)
                self.renew_override = grant
                session.claim()
                self.assertTrue(session.failed.wait(1))
                self.assertEqual(session.token, 'private-lease')
                with self.assertRaises(kit.KitError):
                    session.mutate('stop')
                session.close()

    def test_monotonic_expiry_ignores_target_1970_clock(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        before = time.monotonic()
        session.claim()
        try:
            self.assertGreater(session.deadline, before + 89)
            session.mutate('stop')
            session.deadline = time.monotonic() - .01
            with self.assertRaises(kit.KitError):
                session.mutate('stop')
        finally:
            session.close()

    def test_development_probe_timeout_keeps_lease(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        self.load_status = 503
        self.load_error = {
            'error': {'code': 'CORE_TIMEOUT', 'message': 'private-probe-secret'},
        }
        try:
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'test.rbf'
                path.write_bytes(b'rbf-bytes')
                result = session.mutate('load', path)
            self.assertEqual(result['state'], 'held')
            self.assertEqual(result['reason'], 'development probe timed out')
            session.mutate('stop')
        finally:
            session.close()
        self.assertTrue(any(call[0].endswith('/rbf') for call in self.calls))
        self.assertEqual(self.calls[-1][0], '/v1/kit/release')
        self.assertNotIn('private-probe-secret', ''.join(str(call) for call in self.calls[-1]))

    def test_development_load_http_503_without_code_keeps_lease(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        self.load_status = 503
        self.load_error = {'state': 'failed'}
        try:
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'test.rbf'
                path.write_bytes(b'rbf-bytes')
                result = session.mutate('load', path)
            self.assertEqual(result['reason'], 'development probe timed out')
            session.mutate('stop')
        finally:
            session.close()

    def test_blocked_development_load_still_fails(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        self.load_status = 503
        self.load_error = {
            'error': {'code': 'KIT_LEASE_BLOCKED', 'message': 'private-blocked-secret'},
        }
        try:
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'test.rbf'
                path.write_bytes(b'rbf-bytes')
                with self.assertRaises(kit.KitError) as caught:
                    session.mutate('load', path)
            self.assertEqual(caught.exception.status, 503)
            self.assertEqual(caught.exception.code, 'KIT_LEASE_BLOCKED')
            self.assertNotIn('private', str(caught.exception))
        finally:
            session.close()

    def test_foreign_token_error_does_not_echo_secrets(self):
        with self.assertRaises(kit.KitError) as caught:
            self.client.request('/v1/stop', token='foreign')
        self.assertNotIn('private', str(caught.exception))
        self.assertEqual(caught.exception.status, 409)

    def test_invalid_size_never_uploads(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'test.rbf'
            for size in (0, kit.MAX_RBF + 1):
                with path.open('wb') as file:
                    file.truncate(size)
                with self.assertRaises(kit.KitError):
                    self.client.load(path, 'private-lease')
        self.assertEqual(self.calls, [])

    def test_stop_recovers_idle_without_board_reboot(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        self.stop_response = {
            'state': 'stopping',
            'development': True,
            'recovery': 'reboot_required',
        }
        try:
            result = session.mutate('stop')
            self.assertEqual(result['state'], 'idle')
            self.assertEqual(result['reason'], 'idle recovery restored')
            self.assertEqual(session.token, 'private-lease')
        finally:
            session.close()
        paths = [call[0] for call in self.calls]
        self.assertIn('/v1/stop', paths)
        self.assertIn('/v1/development/recover-idle', paths)
        self.assertNotIn('/v1/development/reboot', paths)
        self.assertFalse(self.rebooted)
        recover = next(call for call in self.calls if call[0] == '/v1/development/recover-idle')
        self.assertEqual(recover[1].get(kit.HEADER), 'private-lease')

    def test_close_after_development_load_stops_instead_of_raw_release(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        self.load_status = 503
        self.load_error = {'error': {'code': 'CORE_TIMEOUT'}}
        self.stop_response = {
            'state': 'stopping',
            'development': True,
            'recovery': 'reboot_required',
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'test.rbf'
            path.write_bytes(b'rbf-bytes')
            session.mutate('load', path)
        result = session.close()
        self.assertEqual(result.get('state'), 'held')
        paths = [call[0] for call in self.calls]
        self.assertIn('/v1/stop', paths)
        self.assertIn('/v1/development/recover-idle', paths)
        self.assertNotIn('/v1/development/reboot', paths)
        self.assertIn('/v1/kit/release', paths)

    def test_close_without_load_only_releases(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        session.close()
        paths = [call[0] for call in self.calls]
        self.assertIn('/v1/kit/release', paths)
        self.assertNotIn('/v1/stop', paths)
        self.assertFalse(any(call[0].endswith('/reboot') for call in self.calls))

    def test_stop_without_recovery_does_not_reboot(self):
        session = kit.Session(self.client, 'agent', 'bringup', interval=20)
        session.claim()
        try:
            session.mutate('stop')
        finally:
            session.close()
        self.assertFalse(any(call[0].endswith('/reboot') for call in self.calls))
        self.assertFalse(any(call[0] == '/v1/health' for call in self.calls))

    def test_takeover_retains_request_identity_while_busy(self):
        session = kit.Session(self.client, 'operator', 'recovery')
        session.claim('g1', 'previous agent gone', wait=3)
        session.close()
        calls = [json.loads(call[2]) for call in self.calls if call[0].endswith('takeover')]
        self.assertEqual(calls[0], calls[1])
        self.assertEqual(calls[0]['expected_generation'], 'g1')


if __name__ == '__main__':
    unittest.main()
