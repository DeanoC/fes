"""Loopback maintenance protocol tests; no kit or hardware is contacted."""

import contextlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import tomllib
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from kit_maintenance import KitMaintenance, MaintenanceError

TARGET_ID = '22222222-2222-4222-8222-222222222222'
BOOT_ID = '11111111-1111-4111-8111-111111111111'
LEASE_TOKEN = 'a' * 64
RESERVATION_TOKEN = 'b' * 64
GENERATION = 'c' * 64


class FakeAuthority:
    """Reusable HTTP authority for kit-manifest's local transport fixture.

`authority(root)` yields this object with .url, .requests and .status_value.
It writes the target's private marker for the real shell executor to validate.
"""
    def __init__(self, root):
        self.root = Path(root)
        self.requests = []
        self.end_states = []
        self.status_value = {'state': 'none'}
        self.claim = None
        self.health_target_id = None
        self.api_version = 'v1'
        self.unsupported = False
        self.disconnect_start = False
        self.disconnect_claim = False
        self.disconnect_end = False
        self.reject_end = False
        self.redirect = False
        self.malformed_status = False
        self.prepare_polls = 0
        self.prepared_state = 'ready'
        self.finish_polls = 0
        self.health_unavailable = 0

    @property
    def marker(self):
        return self.root / 'run/fogcast/kit-maintenance.json'

    def config(self):
        return tomllib.loads((self.root / 'media/fat/fogcast/agent.toml').read_text())

    def observed_target_id(self):
        if self.health_target_id:
            return self.health_target_id
        configured = self.config().get('target_id') or ''
        if isinstance(configured, str) and configured:
            return configured
        path = self.root / 'media/fat/fogcast/target-id'
        try:
            text = path.read_text()
        except OSError:
            return ''
        if text.endswith('\n'):
            text = text[:-1]
        return text

    def boot_id(self):
        return (self.root / 'proc/sys/kernel/random/boot_id').read_text().strip()

    def persist(self):
        self.marker.parent.mkdir(parents=True, exist_ok=True)
        self.marker.write_text(json.dumps({'schema': 1, **self.status_value,
            'token': RESERVATION_TOKEN, 'start_token': LEASE_TOKEN}))
        self.marker.chmod(0o600)

    def public(self):
        value = dict(self.status_value)
        if value['state'] == 'completed':
            value['state'] = 'none'
        return value


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def reply(self, value, code=200):
        raw = json.dumps(value).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def handle_request(self):
        authority = self.server.authority
        config = authority.config()
        body = json.loads(self.rfile.read(int(self.headers['Content-Length']))) if self.command == 'POST' else None
        authority.requests.append({'method': self.command, 'path': self.path, 'body': body})
        if self.headers.get('Authorization') != 'Bearer ' + config['token']:
            self.reply({'error': {'message': config['token']}}, 401)
            return
        if self.path == '/v1/health':
            if authority.health_unavailable:
                authority.health_unavailable -= 1
                self.reply({'error': 'startup'}, 503)
                return
            self.reply({'api_version': authority.api_version,
                        'target_id': authority.observed_target_id(),
                        'boot_id': authority.boot_id(), 'ready': True})
        elif self.path == '/v1/kit/maintenance' and self.command == 'GET':
            if authority.unsupported:
                self.reply({'error': 'unsupported'}, 404)
                return
            if authority.redirect:
                self.send_response(307)
                self.send_header('Location', authority.url + '/forbidden')
                self.end_headers()
                return
            if authority.malformed_status:
                self.reply({'state': 'unexpected', 'token': RESERVATION_TOKEN})
                return
            if authority.status_value['state'] == 'preparing':
                if authority.prepare_polls > 0:
                    authority.prepare_polls -= 1
                else:
                    authority.status_value['state'] = authority.prepared_state
                    authority.persist()
            elif authority.status_value['state'] == 'finishing':
                if authority.finish_polls > 0:
                    authority.finish_polls -= 1
                else:
                    authority.status_value['state'] = 'completed'
                    authority.persist()
            self.reply(authority.public())
        elif self.path == '/v1/kit/claim':
            if (authority.status_value['state'] not in ('none', 'completed')
                    or authority.claim and authority.claim['request_id'] != body['request_id']):
                self.reply({'error': 'foreign grant'}, 409)
                return
            authority.claim = body
            if authority.disconnect_claim:
                authority.disconnect_claim = False
                self.connection.shutdown(socket.SHUT_RDWR)
                self.close_connection = True
                return
            self.reply({'status': {'state': 'held', 'generation': GENERATION}, 'token': LEASE_TOKEN})
        elif self.path == '/v1/kit/maintenance/start':
            if self.headers.get('X-FogCast-Kit-Lease') != LEASE_TOKEN or not authority.claim:
                self.reply({'error': LEASE_TOKEN}, 403)
                return
            if authority.status_value['state'] not in ('none', 'completed'):
                if authority.status_value['operation_id'] != body['request_id']:
                    self.reply({'error': 'different operation'}, 409)
                    return
            else:
                authority.status_value = {'state': 'preparing', 'operation_id': body['request_id'],
                    'generation': GENERATION, 'owner': authority.claim['owner'],
                    'purpose': body['purpose'], 'boot_id': authority.boot_id()}
                authority.persist()
            if authority.disconnect_start:
                authority.disconnect_start = False
                self.connection.shutdown(socket.SHUT_RDWR)
                self.close_connection = True
                return
            self.reply({'status': authority.public(), 'token': RESERVATION_TOKEN})
        elif self.path in ('/v1/kit/maintenance/end', '/v1/kit/maintenance/resume'):
            if (self.headers.get('X-FogCast-Kit-Lease') != RESERVATION_TOKEN
                    or body['operation_id'] != authority.status_value.get('operation_id')
                    or body['expected_generation'] != authority.status_value.get('generation')):
                self.reply({'error': RESERVATION_TOKEN}, 403)
                return
            if self.path.endswith('/resume'):
                authority.status_value['state'] = 'preparing'
                authority.prepared_state = 'ready'
                authority.persist()
                self.reply(authority.public())
                return
            authority.end_states.append(authority.status_value['state'])
            if authority.status_value['state'] == 'preparing':
                self.reply({'error': 'startup cleanup busy'}, 409)
                return
            if authority.reject_end:
                self.reply({'error': {'message': RESERVATION_TOKEN}}, 409)
                return
            authority.status_value['state'] = 'finishing'
            authority.persist()
            authority.claim = None
            if authority.disconnect_end:
                authority.disconnect_end = False
                self.connection.shutdown(socket.SHUT_RDWR)
                self.close_connection = True
                return
            self.reply(authority.public())
        else:
            self.reply({'error': 'unexpected route'}, 404)

    do_GET = handle_request
    do_POST = handle_request


@contextlib.contextmanager
def authority(root):
    value = FakeAuthority(root)
    server = ThreadingHTTPServer(('127.0.0.1', 0), _Handler)
    server.authority = value
    value.url = f'http://127.0.0.1:{server.server_port}'
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield value
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


class _Transport:
    def __init__(self, root):
        self.root = root

    def run(self, args):
        if len(args) != 2 or args[0] != 'show':
            raise AssertionError(args)
        path = self.root / args[1].lstrip('/')
        if not path.is_file():
            raise FileNotFoundError(args[1])
        return path.read_text()


class KitMaintenanceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.config = self.root / 'media/fat/fogcast/agent.toml'
        self.config.parent.mkdir(parents=True)
        self.config.write_text(f'token = "secret"\ntarget_id = "{TARGET_ID}"\n')
        boot = self.root / 'proc/sys/kernel/random/boot_id'
        boot.parent.mkdir(parents=True)
        boot.write_text(BOOT_ID)
        self.journal = self.root / 'private/journal.json'
        self.api = self.enterContext(authority(self.root))
        self.enterContext(mock.patch.dict(os.environ, {'FES_KIT_A_API': self.api.url}))
        self.client = KitMaintenance('A', _Transport(self.root), '192.0.2.1', self.journal,
                                     timeout=.3, poll_interval=.01)

    def posts(self, path):
        return [request for request in self.api.requests if request['method'] == 'POST'
                and request['path'] == path]

    def test_invalid_owner_or_purpose_rejected_before_journal_or_request(self):
        for bad in ('x' * 161, ' leading', 'trailing ', 'x\x7f', 'x\x85', 'é' * 81):
            for field in ('owner', 'purpose'):
                with self.subTest(field=field, value=bad):
                    kwargs = {'owner': 'developer', 'purpose': 'replace runtime', field: bad}
                    with self.assertRaisesRegex(MaintenanceError, 'bounded labels'):
                        self.client.begin(**kwargs)
                    self.assertFalse(self.journal.exists())
                    self.assertFalse(self.client.lock_path.exists())
                    self.assertEqual(self.api.requests, [])

    def test_transaction_is_reentrant_and_fences_other_clients_and_processes(self):
        contender = KitMaintenance('A', _Transport(self.root), '192.0.2.1', self.journal,
                                    timeout=.3, poll_interval=.01)
        with self.client.transaction():
            self.client.begin('developer', 'replace runtime')
            original = self.journal.read_bytes()
            for operation in (lambda: contender.begin('developer', 'replace runtime'), contender.finish):
                with self.assertRaisesRegex(MaintenanceError, 'another local'):
                    operation()
                self.assertEqual(self.journal.read_bytes(), original)
            # A separate process shares the file lock, without contacting HTTP.
            command = '''
import sys
sys.path.insert(0, sys.argv[2])
from kit_maintenance import KitMaintenance, MaintenanceError
c = KitMaintenance('A', None, '127.0.0.1', sys.argv[1])
for call in (lambda: c.begin('developer', 'replace runtime'), c.finish):
    try:
        call()
    except MaintenanceError as e:
        if str(e) != 'another local maintenance transaction is running':
            raise
    else:
        raise AssertionError('concurrent transaction admitted')
'''
            result = subprocess.run([sys.executable, '-c', command, str(self.journal),
                                     str(ROOT / 'scripts')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(self.journal.read_bytes(), original)
            self.assertEqual(len(self.posts('/v1/kit/claim')), 1)
            self.assertEqual(self.posts('/v1/kit/maintenance/end'), [])
            self.client.finish()
            self.assertFalse(self.journal.exists())
        self.assertEqual(self.client.lock_path.stat().st_mode & 0o777, 0o600)
        # Ownership transfers only after the outer apply transaction releases.
        contender.begin('developer', 'replace runtime')
        contender.finish()

    def test_transaction_lock_rejects_symlink_or_shared_permissions(self):
        self.client.lock_path.parent.mkdir(mode=0o700)
        self.client.lock_path.write_text('')
        self.client.lock_path.chmod(0o644)
        with self.assertRaisesRegex(MaintenanceError, 'private regular'):
            self.client.begin('developer', 'replace runtime')
        retained = self.client.lock_path.with_suffix('.retained')
        self.client.lock_path.rename(retained)
        self.client.lock_path.symlink_to(retained)
        with self.assertRaises(MaintenanceError):
            self.client.finish()
        self.assertFalse(self.journal.exists())
        self.assertEqual(self.api.requests, [])

    def test_begin_waits_ready_and_finish_waits_completed_without_exposing_tokens(self):
        self.api.prepare_polls = 2
        ready = self.client.begin('developer', 'replace runtime')
        self.assertEqual(ready['state'], 'ready')
        self.assertNotIn('token', ready)
        self.assertNotIn(LEASE_TOKEN, json.dumps(self.client.status()))
        self.api.status_value['reason'] = 'secret ' + RESERVATION_TOKEN + ' ' + LEASE_TOKEN
        public = json.dumps(self.client.status())
        for credential in ('secret', RESERVATION_TOKEN, LEASE_TOKEN):
            self.assertNotIn(credential, public)
        record = json.loads(self.journal.read_text())
        self.assertEqual(record['token'], RESERVATION_TOKEN)
        self.assertEqual(self.journal.stat().st_mode & 0o777, 0o600)
        self.api.finish_polls = 2
        finished = self.client.finish()
        self.assertEqual(finished['state'], 'none')
        self.assertFalse(self.journal.exists())
        self.assertEqual(len(self.posts('/v1/kit/maintenance/end')), 1)

    def test_lost_start_response_explicit_rerun_reuses_journal_and_original_grant(self):
        self.api.disconnect_start = True
        with self.assertRaises(MaintenanceError):
            self.client.begin('developer', 'replace runtime')
        before = json.loads(self.journal.read_text())
        self.assertNotIn('token', before)
        ready = self.client.begin('developer', 'replace runtime')
        self.assertEqual(ready['operation_id'], before['request_id'])
        self.assertEqual(len(self.posts('/v1/kit/claim')), 1)
        starts = self.posts('/v1/kit/maintenance/start')
        self.assertEqual(starts[0]['body'], starts[1]['body'])
        self.client.finish()

    def test_lost_claim_reply_explicit_rerun_reuses_claim_request_id(self):
        self.api.disconnect_claim = True
        with self.assertRaises(MaintenanceError):
            self.client.begin('developer', 'replace runtime')
        before = json.loads(self.journal.read_text())
        self.assertNotIn('lease_token', before)
        self.client.begin('developer', 'replace runtime')
        claims = self.posts('/v1/kit/claim')
        self.assertEqual(len(claims), 2)
        self.assertEqual(claims[0]['body'], claims[1]['body'])
        self.client.finish()

    def test_restart_blocked_operation_requires_explicit_resume(self):
        first = self.client.begin('developer', 'replace runtime')
        self.api.status_value['state'] = 'blocked'
        self.api.persist()
        self.api.health_unavailable = 2
        ready = self.client.begin('developer', 'replace runtime')
        self.assertEqual(ready['operation_id'], first['operation_id'])
        self.assertEqual(len(self.posts('/v1/kit/maintenance/resume')), 1)
        self.assertEqual(len(self.posts('/v1/kit/claim')), 1)
        self.client.finish()

    def test_recovery_waits_for_startup_prepare_to_blocked_before_explicit_resume(self):
        first = self.client.begin('developer', 'replace runtime')
        self.api.status_value['state'] = 'preparing'
        self.api.prepare_polls = 2
        self.api.prepared_state = 'blocked'
        self.api.persist()
        ready = self.client.begin('developer', 'replace runtime')
        self.assertEqual(ready['operation_id'], first['operation_id'])
        self.assertEqual(ready['state'], 'ready')
        self.assertEqual(len(self.posts('/v1/kit/maintenance/resume')), 1)
        self.assertEqual(len(self.posts('/v1/kit/maintenance/start')), 1)
        self.client.finish()

    def test_finish_waits_for_startup_prepare_to_blocked_before_single_end(self):
        self.client.begin('developer', 'replace runtime')
        self.api.status_value['state'] = 'preparing'
        self.api.prepare_polls = 2
        self.api.prepared_state = 'blocked'
        self.api.persist()
        self.assertEqual(self.client.finish()['state'], 'none')
        self.assertEqual(len(self.posts('/v1/kit/maintenance/end')), 1)
        self.assertEqual(self.posts('/v1/kit/maintenance/resume'), [])
        self.assertEqual(self.api.end_states, ['blocked'])
        self.assertFalse(self.journal.exists())

    def test_readiness_timeout_retains_grant_for_explicit_recovery(self):
        self.api.prepare_polls = 1000
        with self.assertRaisesRegex(MaintenanceError, 'timed out'):
            self.client.begin('developer', 'replace runtime')
        record = json.loads(self.journal.read_text())
        self.assertEqual(record['token'], RESERVATION_TOKEN)
        self.assertEqual(self.api.status_value['state'], 'preparing')
        self.api.prepare_polls = 0
        self.assertEqual(self.client.begin('developer', 'replace runtime')['state'], 'ready')
        self.assertEqual(len(self.posts('/v1/kit/maintenance/start')), 1)
        self.client.finish()

    def test_lost_end_response_observed_without_replaying_end(self):
        self.client.begin('developer', 'replace runtime')
        self.api.disconnect_end = True
        self.api.finish_polls = 2
        self.assertEqual(self.client.finish()['state'], 'none')
        self.assertEqual(len(self.posts('/v1/kit/maintenance/end')), 1)
        self.assertFalse(self.journal.exists())

    def test_rejected_end_keeps_private_credentials_and_sanitizes_errors(self):
        self.client.begin('developer', 'replace runtime')
        self.api.reject_end = True
        with self.assertRaises(MaintenanceError) as error:
            self.client.finish()
        self.assertNotIn(RESERVATION_TOKEN, str(error.exception))
        self.assertNotIn('secret', str(error.exception))
        self.assertTrue(self.journal.exists())
        self.assertEqual(self.api.status_value['state'], 'ready')

    def test_changed_target_or_api_fails_before_claim_or_journal(self):
        for target, api_version in ((BOOT_ID, 'v1'), (TARGET_ID, 'v2')):
            with self.subTest(target=target, api_version=api_version):
                self.api.health_target_id, self.api.api_version = target, api_version
                with self.assertRaises(MaintenanceError):
                    self.client.begin('developer', 'replace runtime')
                self.assertFalse(self.journal.exists())
                self.assertEqual(self.posts('/v1/kit/claim'), [])

    def test_unknown_maintenance_api_and_redirect_do_not_claim_or_bypass(self):
        for mode in ('unsupported', 'redirect', 'malformed_status'):
            with self.subTest(mode=mode):
                setattr(self.api, mode, True)
                with self.assertRaises(MaintenanceError):
                    self.client.begin('developer', 'replace runtime')
                setattr(self.api, mode, False)
                self.assertEqual(self.posts('/v1/kit/claim'), [])
                self.assertFalse(self.journal.exists())
                self.assertFalse(any(r['path'] == '/forbidden' for r in self.api.requests))

    def test_private_journal_rejects_symlink_or_public_permissions_before_mutation(self):
        self.client.begin('developer', 'replace runtime')
        self.journal.chmod(0o644)
        with self.assertRaises(MaintenanceError):
            self.client.begin('developer', 'replace runtime')
        self.journal.chmod(0o600)
        retained = self.journal.with_suffix('.retained')
        self.journal.rename(retained)
        self.journal.symlink_to(retained)
        with self.assertRaises(MaintenanceError):
            self.client.finish()
        self.assertEqual(len(self.posts('/v1/kit/claim')), 1)
        self.assertTrue(retained.exists())

    def test_stale_boot_and_foreign_operation_never_apply_or_end(self):
        self.client.begin('developer', 'replace runtime')
        boot = self.root / 'proc/sys/kernel/random/boot_id'
        boot.write_text('33333333-3333-4333-8333-333333333333')
        with self.assertRaises(MaintenanceError):
            self.client.begin('developer', 'replace runtime')
        self.api.status_value['operation_id'] = 'd' * 64
        self.api.status_value['generation'] = 'e' * 64
        with self.assertRaises(MaintenanceError):
            self.client.finish()
        self.assertEqual(self.posts('/v1/kit/maintenance/end'), [])
        self.assertTrue(self.journal.exists())

    def test_finish_refreshes_current_agent_credentials(self):
        self.client.begin('developer', 'replace runtime')
        self.config.write_text(self.config.read_text().replace('secret', 'new-secret'))
        self.api.status_value['state'] = 'blocked'
        self.api.persist()
        self.assertEqual(self.client.finish()['state'], 'none')

    def test_omitted_target_id_binds_matching_health_and_persisted_identity(self):
        self.config.write_text('token = "secret"\n')
        persisted = self.root / 'media/fat/fogcast/target-id'
        persisted.write_text(TARGET_ID + '\n')
        ready = self.client.begin('developer', 'replace runtime')
        self.assertEqual(ready['state'], 'ready')
        record = json.loads(self.journal.read_text())
        self.assertEqual(record['target_id'], TARGET_ID)
        self.assertNotIn('next_target_id', record)
        self.assertEqual(self.client.finish()['state'], 'none')

    def test_omitted_target_id_rejects_health_that_disagrees_with_persisted_identity(self):
        self.config.write_text('token = "secret"\n')
        (self.root / 'media/fat/fogcast/target-id').write_text(TARGET_ID + '\n')
        self.api.health_target_id = '33333333-3333-4333-8333-333333333333'
        with self.assertRaises(MaintenanceError):
            self.client.begin('developer', 'replace runtime')
        self.assertFalse(self.journal.exists())
        self.assertEqual(self.posts('/v1/kit/claim'), [])

    def test_identity_transition_finish_confirms_original_boot_and_reservation(self):
        new_id = '33333333-3333-4333-8333-333333333333'
        self.client.begin('developer', 'replace runtime')
        self.client.declare_identity_transition(new_id)
        record = json.loads(self.journal.read_text())
        self.assertEqual(record['next_target_id'], new_id)
        self.assertEqual(record['target_id'], TARGET_ID)
        self.config.write_text(f'token = "secret"\ntarget_id = "{new_id}"\n')
        self.api.status_value['operation_id'] = 'd' * 64
        with self.assertRaisesRegex(MaintenanceError, 'differs from private journal'):
            self.client.finish()
        self.assertEqual(self.posts('/v1/kit/maintenance/end'), [])
        self.assertTrue(self.journal.exists())
        self.api.status_value['operation_id'] = record['request_id']
        boot = self.root / 'proc/sys/kernel/random/boot_id'
        boot.write_text('44444444-4444-4444-8444-444444444444')
        with self.assertRaisesRegex(MaintenanceError, 'identity changed'):
            self.client.finish()
        self.assertEqual(self.posts('/v1/kit/maintenance/end'), [])
        boot.write_text(BOOT_ID)
        self.assertEqual(self.client.finish()['state'], 'none')
        self.assertFalse(self.journal.exists())

    def test_undeclared_identity_change_keeps_the_journal(self):
        self.client.begin('developer', 'replace runtime')
        new_id = '33333333-3333-4333-8333-333333333333'
        self.config.write_text(f'token = "secret"\ntarget_id = "{new_id}"\n')
        with self.assertRaisesRegex(MaintenanceError, 'identity changed'):
            self.client.finish()
        self.assertEqual(self.posts('/v1/kit/maintenance/end'), [])
        self.assertTrue(self.journal.exists())


if __name__ == '__main__':
    unittest.main()
