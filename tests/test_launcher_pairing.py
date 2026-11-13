import http.server
import json
from pathlib import Path
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import media
import prepare_launcher

ID = '73dc9f5f-1a12-4a95-a820-a9b4e600769a'


class LauncherPairingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.calls = []
        self.target_id = ID
        self.refuse = False
        fixture = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.handle_request()

            def do_POST(self):
                self.handle_request()

            def handle_request(self):
                fixture.calls.append(self.path)
                fixture.assertEqual(self.headers.get('Authorization'), 'Bearer agent-secret')
                body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                code = 200
                if self.path == '/v1/health':
                    result = dict(ready=True, target_id=fixture.target_id, boot_id='boot')
                elif self.path == '/v1/status':
                    result = dict(state='idle')
                elif self.path == '/v1/kit/claim':
                    result = dict(token='lease-secret', status=dict(state='held', generation='generation', expires_in_ms=60000))
                else:
                    fixture.assertEqual(self.headers.get('X-FogCast-Kit-Lease'), 'lease-secret')
                    if self.path == '/v1/kit/release':
                        fixture.assertEqual(body, b'')
                        result = dict(state='free')
                    elif self.path == '/v1/launcher/pairing':
                        pairing = json.loads(body)
                        fixture.assertEqual(pairing, json.loads(fixture.kit.read_text()))
                        result = dict(api=pairing['api'], target_id=ID, restart_required=True, sha256='a' * 64)
                        if fixture.refuse:
                            code, result = 409, dict(error=dict(code='PAIRING_NOT_IDLE', message='private-remote-text'))
                    else:
                        code, result = 404, {}
                self.send_response(code)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(result).encode())

        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join)
        self.addCleanup(server.shutdown)
        self.host = self.root / 'config.toml'
        media.write_private(self.host, (
            'selected_target="kit"\n[[targets]]\nname="kit"\nenabled=true\n'
            f'address="http://127.0.0.1:{server.server_port}"\nagent="agent-secret"\ntarget_id="{ID}"\n').encode())
        _, self.kit = prepare_launcher.prepare(self.host, '192.168.10.2', 18789)

    def test_apply_claims_saves_once_and_releases_without_stop_or_reboot(self):
        receipt = prepare_launcher.apply(self.host, self.kit)
        self.assertTrue(receipt['restart_required'])
        self.assertNotIn('token', receipt)
        self.assertEqual(self.calls, ['/v1/health', '/v1/kit/claim', '/v1/health', '/v1/status',
                                      '/v1/launcher/pairing', '/v1/kit/release'])

    def test_refused_save_releases_and_does_not_retry_or_disclose(self):
        self.refuse = True
        with self.assertRaisesRegex(ValueError, 'PAIRING_NOT_IDLE') as error:
            prepare_launcher.apply(self.host, self.kit)
        self.assertNotIn('private-remote-text', str(error.exception))
        self.assertEqual(self.calls[-1], '/v1/kit/release')
        self.assertEqual(self.calls.count('/v1/launcher/pairing'), 1)

    def test_wrong_target_refused_before_claim(self):
        self.target_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
        with self.assertRaisesRegex(ValueError, 'identity'):
            prepare_launcher.apply(self.host, self.kit)
        self.assertEqual(self.calls, ['/v1/health'])
