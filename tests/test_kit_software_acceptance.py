import copy
from contextlib import contextmanager
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
from pathlib import Path
import secrets
import sys
import tarfile
import tempfile
import threading
import types
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import kit_software_acceptance as accept
from diagnostic_identity import INTERFACES

TARGET = '73dc9f5f-1a12-4a95-a820-a9b4e600769a'
BOOT = 'c7505aea-2519-4355-bb24-7e5dff81f309'
NEW_BOOT = '687e01ff-9ffa-4674-9f8b-d1c0ecb16d40'
BASE = 'd' * 64
SECRET = 'private-canary-do-not-publish'


def manifest(version='good'):
    return {'version': version, 'interfaces': dict(INTERFACES), 'provenance': {'source': 'different-source', 'private': SECRET}}


OLD, GOOD, BAD = (hashlib.sha256(json.dumps(manifest(v)).encode()).hexdigest() for v in ('old','good','bad'))

class State:
    def __init__(self):
        self.lock = threading.RLock()
        self.software = {'selected': OLD, 'active_bundle': OLD, 'previous': '', 'generation': 4,
                         'base_identity': BASE, 'pending': None, 'unreleased': None, 'receipts': {}}
        self.owner = None
        self.held_generation = 'held-generation'
        self.boot = BOOT
        self.calls = []
        self.maintenance = {'state': 'none'}
        self.fault = None
        self.drop = None
        self.install_id = GOOD
        self.status = 'idle'
        self.releases = [{'id': OLD, 'manifest': manifest('old')}]

    def lease(self):
        if self.maintenance['state'] != 'none':
            return {'state': 'maintenance', **{k:v for k,v in self.maintenance.items() if k != 'state'}}
        return {'state': 'held' if self.owner else 'free', 'owner': self.owner or '',
                'generation': self.held_generation, 'expires_in_ms': 90000}

    def grant(self):
        return {'status': self.lease(), 'token': SECRET}

    def complete(self):
        op = self.software['pending']
        if op:
            outcome = 'recovered' if op['candidate'] == BAD else 'accepted'
            self.software['receipts'][op['id']] = dict(op, result=outcome)
            if outcome == 'accepted':
                self.software.update(previous=self.software['selected'], selected=op['candidate'],
                                     active_bundle=op['candidate'], generation=self.software['generation'] + 1)
            self.software['pending'] = None


@contextmanager
def server(state=None):
    state = state or State()
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass
        def do_GET(self):
            self.handle_request('GET')
        def do_POST(self):
            self.handle_request('POST')
        def handle_request(self, method):
            raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
            value = json.loads(raw) if raw and self.headers.get('Content-Type') == 'application/json' else None
            with state.lock:
                state.calls.append((method, self.path, value))
                status, result = 200, {}
                sw = state.software
                if self.path == '/v1/health':
                    result = {'target_id': TARGET, 'api_version': 'v1', 'boot_id': state.boot,
                              'ready': True, 'software': {'active_bundle': sw['active_bundle'], 'base_identity': BASE}}
                elif self.path == '/v1/status':
                    result = {'state': state.status}
                elif self.path == '/v1/kit/lease':
                    result = state.lease()
                elif self.path == '/v1/kit/maintenance':
                    result = state.maintenance
                elif self.path == '/v1/kit/maintenance/start':
                    state.maintenance = {'state': 'ready', 'operation_id': value['request_id'],
                                         'generation': 'e'*64, 'boot_id': state.boot,
                                         'owner': state.owner, 'purpose': value['purpose']}
                    state.owner = None
                    result = {'status': state.maintenance, 'token': 'maintenance-private-canary'}
                elif self.path == '/v1/kit/maintenance/end':
                    state.maintenance = {'state': 'none'}; result = state.maintenance
                elif self.path == '/test/reboot':
                    state.boot = NEW_BOOT; state.maintenance = {'state': 'none'}; state.complete()
                elif self.path == '/v1/update':
                    result = {'image_sha256': BASE, 'boot_id': state.boot, 'trial': False}
                elif self.path == '/v1/core-packages':
                    result = {'packages': [], 'selections': []}
                elif self.path == '/v1/kit/software':
                    result = sw
                elif self.path == '/v1/kit/software/releases':
                    if method == 'GET':
                        result = state.releases
                    else:
                        result = {'id': state.install_id, 'manifest': manifest('bad' if state.install_id == BAD else 'old' if state.install_id == OLD else 'good')}
                        state.releases.append(result)
                elif self.path == '/v1/kit/claim':
                    if state.owner or state.maintenance['state'] != 'none':
                        status = 409
                    else:
                        state.owner = value['owner']; result = state.grant()
                elif self.path == '/v1/kit/renew':
                    result = state.grant()
                elif self.path == '/v1/stop':
                    result = {'state': 'idle'}
                elif self.path == '/v1/kit/release':
                    state.owner = None; state.complete(); result = state.lease()
                elif self.path == '/v1/kit/software/activate':
                    pending = sw['pending']
                    if value['expected_generation'] != sw['generation'] or (pending and any(pending[k] != v for k, v in value.items())):
                        status = 409
                    else:
                        sw['pending'] = dict(value, phase='queued'); result = sw
                elif self.path == '/v1/kit/software/cancel':
                    op = sw['pending']
                    if not op or op['id'] != value['id'] or op['expected_generation'] != value['expected_generation']:
                        status = 409
                    else:
                        sw['receipts'][op['id']] = dict(op, result='cancelled'); sw['pending'] = None; result = sw
                else:
                    status = 404
                if state.fault:
                    status, result = state.fault(method, self.path, status, copy.deepcopy(result))
                encoded = json.dumps(result).encode()
                if state.drop == self.path:
                    state.drop = None
                    self.close_connection = True
                    return
            self.send_response(status); self.send_header('Content-Length', str(len(encoded))); self.end_headers(); self.wfile.write(encoded)
    http = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=http.serve_forever, daemon=True); thread.start()
    try:
        yield state, f'http://127.0.0.1:{http.server_port}'
    finally:
        http.shutdown(); http.server_close(); thread.join()


def bundle(directory, version="good"):
    path = directory / (version+'.tar')
    raw = json.dumps(manifest(version)).encode()
    with tarfile.open(path, 'w') as archive:
        member = tarfile.TarInfo('manifest.json'); member.size = len(raw)
        archive.addfile(member, io.BytesIO(raw))
    return path, hashlib.sha256(path.read_bytes()).hexdigest()


class AcceptanceTests(unittest.TestCase):
    def run_suite(self, directory, url, state, *, candidate=GOOD, recovery=False, reboot=None, local=None, timeout=10, configure=None):
        args = types.SimpleNamespace(expected_target_id=TARGET, expected_base=BASE,
                                     expected_active_bundle=OLD, candidate=candidate,
                                     recovery_candidate=BAD, timeout=timeout)
        evidence = accept.Evidence(directory / 'evidence')
        path, sha = bundle(directory, 'old' if candidate == OLD else 'good'); frozen = evidence.freeze(path, sha, candidate, 'candidate')
        recovery_frozen = None
        if recovery:
            bad_path, bad_sha = bundle(directory, 'bad')
            recovery_frozen = evidence.freeze(bad_path, bad_sha, BAD, 'recovery')
        runner = accept.Runner(args, accept.Client(url, SECRET, timeout=.5), evidence)
        runner.STABLE_SECONDS = .02; runner.POLL_SECONDS = .005; runner.session.interval = .01
        if recovery:
            original = runner.install
            def install(frozen, expected):
                state.install_id = expected
                return original(frozen, expected)
            runner.install = install
        if configure:
            configure(runner)
        ok = runner.run(frozen, recovery_frozen, reboot, local)
        return ok, runner, list(evidence.root.glob('*.json'))

    def test_real_http_default_requester_process_exit_and_no_restore(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            ok, runner, records = self.run_suite(Path(temporary), url, state)
            self.assertTrue(ok)
            self.assertEqual((state.software['selected'], state.software['generation']), (GOOD, 5))
            result = next(json.loads(p.read_text()) for p in records if p.name.endswith('-result.json'))
            self.assertTrue(result['success']); self.assertFalse(result['overall_hardware_acceptance'])
            for name in ('held_install', 'competing_client', 'queue_cancel', 'requester_exit', 'retained_stop', 'activation'):
                self.assertEqual(result['stages'][name]['status'], 'passed')
            for name in ('reboot', 'recovery', 'local_busy', 'input', 'hdmi'):
                self.assertEqual(result['stages'][name]['status'], 'skipped')
            self.assertEqual(result['stages']['activation']['activation_kind'], 'change_selected')
            self.assertGreater(sum(path == '/v1/kit/renew' for _, path, _ in state.calls), 0)
            self.assertNotIn(SECRET, ''.join(p.read_text() for p in records))
            self.assertEqual(sum(path == '/v1/kit/release' for _, path, _ in state.calls), 1)

    def test_public_cli_inputs_execute_real_http_and_requester_process(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state,url):
            root=Path(temporary);archive,sha=bundle(root);config=root/'config.toml'
            config.write_text('selected_target = "dev"\n[[targets]]\nname="dev"\nenabled=true\n'+
                              'target_id='+json.dumps(TARGET)+'\naddress='+json.dumps(url)+'\nagent='+json.dumps(SECRET)+'\n')
            config.chmod(0o600)
            argv=['--config',str(config),'--expected-target-id',TARGET,
                  '--expected-base-image-sha256',BASE,'--expected-bundle-id',OLD,
                  '--candidate',GOOD,'--bundle',str(archive),'--expected-bundle-sha256',sha,
                  '--evidence-dir',str(root/'run'),'--execute']
            with patch.object(accept.Runner,'STABLE_SECONDS',.02), patch.object(accept.Runner,'POLL_SECONDS',.005):
                self.assertEqual(accept.main(argv),0)
            self.assertEqual(state.software['selected'],GOOD)
            self.assertTrue(list((root/'run').glob('*-result.json')))

    def test_same_release_reselection_is_truthful(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.install_id = OLD
            ok, runner, _ = self.run_suite(Path(temporary), url, state, candidate=OLD)
            self.assertTrue(ok)
            self.assertEqual(runner.stages['activation']['activation_kind'], 'reselect_current')

    def test_recovery_uses_explicit_candidate_and_retains_selection(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            ok, runner, _ = self.run_suite(Path(temporary), url, state, recovery=True)
            self.assertTrue(ok)
            self.assertEqual(state.software['selected'], GOOD)
            self.assertEqual(state.software['generation'], 5)
            self.assertEqual(runner.stages['recovery']['result'], 'recovered')

    def test_wrong_identity_refuses_before_any_post(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.fault = lambda method, path, status, value: (status, dict(value, target_id=NEW_BOOT)) if path == '/v1/health' else (status, value)
            ok, _, _ = self.run_suite(Path(temporary), url, state)
            self.assertFalse(ok)
            self.assertFalse(any(method == 'POST' for method, _, _ in state.calls))

    def test_lost_queue_reply_is_not_retried_and_exact_queue_cancelled_before_release(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.drop = '/v1/kit/software/activate'
            ok, _, _ = self.run_suite(Path(temporary), url, state)
            self.assertFalse(ok)
            self.assertEqual(state.software['selected'], OLD)
            paths = [path for method, path, _ in state.calls if method == 'POST']
            self.assertEqual(paths.count('/v1/kit/software/activate'), 1)
            self.assertLess(paths.index('/v1/kit/software/cancel'), paths.index('/v1/kit/release'))

    def test_failed_stop_cancels_queue_before_release_never_reboots(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.fault = lambda method, path, status, value: (200, {'state': 'failed', 'recovery': 'reboot_required'}) if path == '/v1/stop' else (status, value)
            ok, _, _ = self.run_suite(Path(temporary), url, state)
            self.assertFalse(ok); self.assertEqual(state.software['selected'], OLD)
            self.assertFalse(any('reboot' in path for _, path, _ in state.calls))

    def test_real_requester_child_lost_reply_does_not_activate(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            def fault(method, path, status, value):
                if path == '/v1/kit/software/activate' and status == 200 and state.software['receipts']:
                    state.drop = path
                return status, value
            state.fault = fault
            ok, _, _ = self.run_suite(Path(temporary), url, state)
            self.assertFalse(ok)
            self.assertEqual(state.software['selected'], OLD)
            self.assertIsNone(state.software['pending'])
            self.assertEqual(sum(p == '/v1/kit/software/activate' for _, p, _ in state.calls), 6)
            self.assertEqual(sum(p == '/v1/kit/release' for _, p, _ in state.calls), 1)

    def test_lost_cancel_reply_reconciles_read_only_before_release(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.drop = '/v1/kit/software/cancel'
            ok, _, _ = self.run_suite(Path(temporary), url, state)
            self.assertTrue(ok)
            self.assertEqual(sum(p == '/v1/kit/software/cancel' for _, p, _ in state.calls), 1)

    def test_uncertain_cancel_not_committed_is_not_retried_or_released(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            def fault(method, path, status, value):
                if path == '/v1/kit/software/cancel':
                    op = next(v for v in state.software['receipts'].values() if v['result'] == 'cancelled')
                    state.software['pending'] = {k:v for k,v in op.items() if k != 'result'}
                    del state.software['receipts'][op['id']]
                    return 503, {'error': SECRET}
                return status, value
            state.fault = fault
            ok, _, records = self.run_suite(Path(temporary), url, state)
            self.assertFalse(ok)
            self.assertEqual(sum(p == '/v1/kit/software/cancel' for _, p, _ in state.calls), 1)
            self.assertEqual(sum(p == '/v1/kit/release' for _, p, _ in state.calls), 0)
            self.assertTrue(any('cleanup-unresolved' in p.name for p in records))
            self.assertNotIn(SECRET, ''.join(p.read_text() for p in records))

    def test_foreign_queue_before_release_is_never_cancelled_or_activated(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            def configure(runner):
                original = runner.release
                def release():
                    if runner.phase == 'release-activation':
                        state.software['pending'] = {'id':'f'*64,'candidate':'9'*64,'requester':'other-host',
                                                     'expected_generation':4,'phase':'queued'}
                    return original()
                runner.release = release
            ok, _, _ = self.run_suite(Path(temporary), url, state, configure=configure)
            self.assertFalse(ok)
            self.assertEqual(state.software['pending']['requester'], 'other-host')
            self.assertEqual(sum(p == '/v1/kit/release' for _, p, _ in state.calls), 0)
            self.assertEqual(sum(p == '/v1/kit/software/cancel' for _, p, _ in state.calls), 1)

    def test_unresolved_missing_queue_and_receipt_prevents_release(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            def configure(runner):
                def requester(request):
                    runner.request = request
                    raise accept.kit.KitError('uncertain requester')
                runner.requester_child = requester
            ok, _, _ = self.run_suite(Path(temporary), url, state, configure=configure)
            self.assertFalse(ok)
            self.assertEqual(sum(p == '/v1/kit/release' for _, p, _ in state.calls), 0)

    def test_optional_reboot_runs_once_and_persists_private_grant(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            command = {'argv':[sys.executable,'-c',
                              'import urllib.request;urllib.request.urlopen('+repr(url+'/test/reboot')+').read()'],
                       'sha256':'f'*64}
            ok, runner, records = self.run_suite(Path(temporary), url, state, reboot=command)
            self.assertTrue(ok)
            self.assertEqual(runner.stages['reboot']['status'], 'passed')
            self.assertEqual((state.boot,state.software['generation']), (NEW_BOOT,6))
            self.assertEqual(sum(p == '/test/reboot' for _, p, _ in state.calls),1)
            private = Path(temporary)/'evidence/private'
            self.assertEqual((private/'maintenance-grant.json').stat().st_mode & 0o777,0o600)
            self.assertEqual(json.loads((private/'maintenance-promotion.json').read_text())['execution_token'],SECRET)
            self.assertNotIn(SECRET,''.join(p.read_text() for p in records))
            self.assertNotIn('maintenance-private-canary',''.join(p.read_text() for p in records))

    def test_reboot_spawn_failure_retains_grant_without_cleanup_mutations(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            command = {'argv':['/no-such-explicit-operator-command'],'sha256':'f'*64}
            ok, _, records = self.run_suite(Path(temporary), url, state, reboot=command, timeout=.8)
            self.assertFalse(ok)
            self.assertEqual(state.maintenance['state'],'ready')
            self.assertTrue((Path(temporary)/'evidence/private/maintenance-grant.json').exists())
            self.assertEqual(sum(p == '/v1/kit/maintenance/end' for _, p, _ in state.calls),0)
            self.assertEqual(sum(p == '/v1/kit/software/cancel' for _, p, _ in state.calls),1)
            self.assertTrue(any('reboot-attempt' in p.name for p in records))

    def test_lost_maintenance_promotion_reply_retains_original_private_material(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            state.drop = '/v1/kit/maintenance/start'
            command={'argv':['/must-not-run'],'sha256':'f'*64}
            with patch.object(accept,'external') as external:
                ok, _, _ = self.run_suite(Path(temporary),url,state,reboot=command)
            self.assertFalse(ok); external.assert_not_called()
            private=Path(temporary)/'evidence/private'
            promotion=json.loads((private/'maintenance-promotion.json').read_text())
            self.assertEqual(promotion['request']['request_id'],state.maintenance['operation_id'])
            self.assertEqual(promotion['execution_token'],SECRET)
            self.assertFalse((private/'maintenance-grant.json').exists())
            self.assertEqual(sum(p == '/v1/kit/maintenance/start' for _,p,_ in state.calls),1)

    def test_maintenance_cleanup_preserves_foreign_queue_before_and_after_own_cancel(self):
        for when in ('before_own_queue', 'after_cancel', 'no_foreign_queue'):
            with self.subTest(when=when), tempfile.TemporaryDirectory() as temporary, server() as (state, url):
                foreign = {'id':'f'*64, 'candidate':'9'*64, 'requester':'other-host',
                           'expected_generation':5, 'phase':'queued'}
                def configure(runner):
                    original = runner.queue
                    def queue(request):
                        if runner.phase != 'reboot-preparation':
                            return original(request)
                        if when == 'before_own_queue':
                            state.software['pending'] = dict(foreign)
                        else:
                            original(request)
                        raise accept.CheckError('injected pre-reboot failure')
                    runner.queue = queue
                def fault(method, path, status, value):
                    cancelled = any(r['result'] == 'cancelled' and r['expected_generation'] == 5
                                    for r in state.software['receipts'].values())
                    if when == 'after_cancel' and path == '/v1/kit/maintenance' and cancelled:
                        state.software['pending'] = dict(foreign)
                    return status, value
                state.fault = fault
                command = {'argv':['/must-not-run'], 'sha256':'f'*64}
                with patch.object(accept, 'external') as external:
                    ok, _, records = self.run_suite(Path(temporary), url, state,
                                                    reboot=command, configure=configure)
                self.assertFalse(ok); external.assert_not_called()
                ends = sum(p == '/v1/kit/maintenance/end' for _,p,_ in state.calls)
                if when == 'no_foreign_queue':
                    self.assertEqual(ends, 1)
                    self.assertEqual(state.maintenance['state'], 'none')
                else:
                    self.assertEqual(ends, 0)
                    self.assertEqual(state.software['pending'], foreign)
                    self.assertEqual(state.maintenance['state'], 'ready')
                    self.assertTrue(any('cleanup-unresolved' in p.name for p in records))
                self.assertFalse(any(p == '/v1/kit/software/cancel' and v['id'] == foreign['id']
                                     for _,p,v in state.calls))

    def test_bundle_hash_and_existing_evidence_refused_offline(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); path, _ = bundle(root); evidence = accept.Evidence(root / 'evidence')
            with self.assertRaises(ValueError): evidence.freeze(path, '0'*64, GOOD, 'candidate')
            with self.assertRaises(FileExistsError): accept.Evidence(root / 'evidence')

    def test_wrong_candidate_manifest_binding_fails_offline(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); path,sha=bundle(root); evidence=accept.Evidence(root/'evidence')
            with self.assertRaisesRegex(ValueError,'manifest identity'):
                evidence.freeze(path,sha,BAD,'candidate')

    def test_external_command_is_argv_only_and_probe_output_is_bounded(self):
        command = {'argv': [sys.executable, '-c', 'print("x"*10000)'], 'sha256': 'e'*64}
        with self.assertRaises(ValueError): accept.external(command, 2, output=True)
        good = {'argv': [sys.executable, '-c', 'print(\'{"http_status":409,"error":"in_use","ignored":"secret"}\')'], 'sha256': 'f'*64}
        self.assertEqual(accept.external(good, 2, output=True)['probe'], {'http_status': 409, 'error': 'in_use'})

    def test_optional_local_probe_is_busy_only_and_keeps_selection(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state, url):
            command={'argv':[sys.executable,'-c','print(\'{"http_status":409,"error":"blocked"}\')'],'sha256':'f'*64}
            ok,runner,_=self.run_suite(Path(temporary),url,state,local=command)
            self.assertTrue(ok)
            self.assertEqual(runner.stages['local_busy']['status'],'passed')
            self.assertEqual(state.software['selected'],GOOD)

    def test_incompatible_contracts_refuse_before_claim(self):
        with tempfile.TemporaryDirectory() as temporary, server() as (state,url):
            state.releases[0]['manifest']['interfaces']['runtime-protocol']='999'
            ok,_,_=self.run_suite(Path(temporary),url,state)
            self.assertFalse(ok)
            self.assertFalse(any(m=='POST' for m,_,_ in state.calls))

    def test_total_request_deadline_interrupts_trickled_body(self):
        class Slow(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_GET(self):
                raw=b'{"state":"idle"}'
                self.send_response(200);self.send_header('Content-Length',str(len(raw)));self.end_headers()
                try:
                    for byte in raw:
                        self.wfile.write(bytes([byte]));self.wfile.flush();time.sleep(.01)
                except (BrokenPipeError,ConnectionResetError):pass
        http=ThreadingHTTPServer(('127.0.0.1',0),Slow)
        thread=threading.Thread(target=http.serve_forever,daemon=True);thread.start()
        try:
            client=accept.Client('http://127.0.0.1:'+str(http.server_port),SECRET,timeout=.04)
            started=time.monotonic()
            with self.assertRaises(accept.kit.KitError):client.request('/v1/status')
            self.assertLess(time.monotonic()-started,.15)
        finally:
            http.shutdown();http.server_close();thread.join()

    def test_command_file_private_no_symlink_or_shell_string(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)/'command.json';path.write_text(json.dumps([sys.executable, '-V']));path.chmod(0o600)
            self.assertEqual(accept.command_file(path)['argv'][0], sys.executable)
            link=path.with_name('link');link.symlink_to(path)
            with self.assertRaises(OSError): accept.command_file(link)
            path.write_text(json.dumps('echo hello'))
            with self.assertRaises(ValueError): accept.command_file(path)

    def test_execute_opt_in_refuses_without_touching_target(self):
        self.assertEqual(accept.main(['--config','/missing','--expected-target-id',TARGET,'--expected-base-image-sha256',BASE,
                                     '--expected-bundle-id',OLD,'--candidate',GOOD,'--bundle','/missing',
                                     '--expected-bundle-sha256',GOOD,'--evidence-dir','/missing']),1)


if __name__ == '__main__':
    unittest.main()
