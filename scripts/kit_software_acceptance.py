#!/usr/bin/env python3
"""Explicit, leased kit-software API diagnostic. Successful releases stay selected."""
from __future__ import annotations

import argparse
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import threading
import stat
import subprocess
import select
import sys
import tarfile
import time
import tomllib
from urllib.parse import urlsplit

from diagnostic_identity import active_manifest

ROOT = Path(__file__).resolve().parents[1]
DIGEST = re.compile(r'[0-9a-f]{64}\Z')
UUID = re.compile(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z')
REQUEST_KEYS = ('id', 'candidate', 'requester', 'expected_generation')
GETS = frozenset(('/v1/health', '/v1/status', '/v1/kit/lease', '/v1/kit/maintenance',
                  '/v1/kit/software', '/v1/kit/software/releases', '/v1/update', '/v1/core-packages'))
POSTS = frozenset(('/v1/kit/claim', '/v1/kit/renew', '/v1/kit/release', '/v1/stop',
                   '/v1/kit/maintenance/start', '/v1/kit/maintenance/end',
                   '/v1/kit/software/releases', '/v1/kit/software/activate', '/v1/kit/software/cancel'))
MAX_BUNDLE = 512 << 20
MAX_RESPONSE = 32 << 20


def kit_module():
    path = ROOT / 'sources/misteross/scripts/kit.py'
    spec = importlib.util.spec_from_file_location('fes_acceptance_kit', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


kit = kit_module()


class CheckError(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise CheckError(message)


def project(value, fields):
    return {key: value[key] for key in fields if key in value}


def exact(value, request):
    return isinstance(value, dict) and all(value.get(k) == v for k, v in request.items())


def private_file(path, maximum):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    stream = os.fdopen(fd, 'rb')
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or info.st_size > maximum:
        stream.close()
        raise ValueError('input must be a bounded regular non-symlink file')
    return stream


def load_target(path, target_id):
    with private_file(path, 65536) as stream:
        require(os.fstat(stream.fileno()).st_mode & 0o077 == 0, 'configuration must be private')
        config = tomllib.load(stream)
    matches = [v for v in config.get('targets', []) if v.get('target_id') == target_id]
    require(len(matches) == 1 and matches[0].get('enabled') is True, 'expected target must be uniquely enabled')
    target = matches[0]
    require(target.get('name') == config.get('selected_target'), 'expected target must be explicitly selected')
    # kit.Client validates URL origins and bearer bytes without printing them.
    return Client(target['address'], target['agent'])


class Client(kit.Client):
    """Bounded HTTP, explicit methods, no redirects and no mutation retries."""
    def __init__(self, url, bearer, timeout=15):
        super().__init__(url, bearer, timeout)
        self.deadline = None

    def request(self, path, body=None, token=None, stream=None, size=0, timeout=None):
        require(path in GETS | POSTS, 'unsupported diagnostic route')
        remaining = self.deadline - time.monotonic() if self.deadline else 3600
        require(remaining > 0, 'diagnostic deadline expired')
        limit = min(timeout or self.timeout, remaining)
        cls = http.client.HTTPSConnection if self.url.scheme == 'https' else http.client.HTTPConnection
        conn = cls(self.url.hostname, self.url.port, timeout=limit)
        timer = None
        started = time.monotonic()
        payload = json.dumps(body).encode() if body is not None else b''
        try:
            conn.connect()
            connected = conn.sock
            def interrupt():
                try: connected.shutdown(socket.SHUT_RDWR)
                except OSError: pass
            timer = threading.Timer(max(.001, limit-(time.monotonic()-started)), interrupt)
            timer.daemon = True
            timer.start()
            conn.putrequest('POST' if stream is not None or body is not None or path not in GETS else 'GET', path)
            conn.putheader('Authorization', 'Bearer ' + self.bearer)
            if token:
                conn.putheader(kit.HEADER, token)
            conn.putheader('Content-Length', str(size if stream else len(payload)))
            if stream or body is not None:
                conn.putheader('Content-Type', 'application/octet-stream' if stream else 'application/json')
            conn.endheaders()
            if stream:
                left = size
                while left:
                    chunk = stream.read(min(left, 65536))
                    require(bool(chunk), 'bundle was truncated')
                    conn.send(chunk)
                    left -= len(chunk)
            elif payload:
                conn.send(payload)
            response = conn.getresponse()
            raw = response.read(MAX_RESPONSE + 1)
            if response.status >= 300:
                raise kit.KitError('target rejected diagnostic request', response.status, kit.error_code(raw))
            require(len(raw) <= MAX_RESPONSE, 'response exceeds diagnostic limit')
            result = json.loads(raw)
            require(isinstance(result, (dict, list)), 'response is not a JSON object or array')
            return result
        except (OSError, http.client.HTTPException, json.JSONDecodeError):
            raise kit.KitError('request outcome uncertain; inspect current state') from None
        finally:
            if timer:
                timer.cancel()
            conn.close()


def command_file(path):
    if path is None:
        return None
    with private_file(path, 16384) as stream:
        require(os.fstat(stream.fileno()).st_mode & 0o077 == 0, 'operator command file must be private')
        raw = stream.read()
    argv = json.loads(raw)
    require(isinstance(argv, list) and 0 < len(argv) <= 64 and all(isinstance(x, str) and x and '\0' not in x for x in argv),
            'operator command must be a bounded JSON argv array')
    return {'argv': argv, 'sha256': hashlib.sha256(raw).hexdigest()}


def external(command, timeout, *, output=False):
    # No shell or arbitrary command output in public receipts.
    process = None
    try:
        process = subprocess.Popen(command['argv'], stdin=subprocess.DEVNULL,
                                   stdout=subprocess.PIPE if output else subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, shell=False)
        until = time.monotonic() + timeout
        raw = b''
        if output:
            os.set_blocking(process.stdout.fileno(), False)
            while True:
                remaining = until - time.monotonic()
                if remaining <= 0:
                    raise subprocess.TimeoutExpired(command['argv'][0], timeout)
                ready, _, _ = select.select([process.stdout], [], [], min(.05, remaining))
                if not ready:
                    continue
                chunk = os.read(process.stdout.fileno(), 4097 - len(raw))
                if not chunk:
                    break
                raw += chunk
                require(len(raw) <= 4096, 'local probe response exceeds limit')
        code = process.wait(timeout=max(.001, until-time.monotonic()))
        result = {'outcome': 'exited', 'exit_code': code, 'command_sha256': command['sha256']}
        if output:
            value = json.loads(raw)
            require(isinstance(value, dict) and value.get('http_status') == 409 and
                    value.get('error') in ('in_use', 'blocked'), 'local probe did not report a busy launch')
            result['probe'] = {'http_status': 409, 'error': value['error']}
        return result
    except subprocess.TimeoutExpired:
        return {'outcome': 'timeout', 'command_sha256': command['sha256']}
    except OSError:
        return {'outcome': 'spawn-failed', 'command_sha256': command['sha256']}
    finally:
        if process is not None:
            if process.poll() is None:
                process.kill(); process.wait()
            if process.stdout is not None:
                process.stdout.close()


class Evidence:
    def __init__(self, root):
        self.root = Path(root)
        self.root.mkdir(mode=0o700)
        self.sequence = 0

    def save(self, label, value):
        self.sequence += 1
        path = self.root / f'{self.sequence:03d}-{label}.json'
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            json.dump(value, stream, indent=2, sort_keys=True); stream.write('\n')
            stream.flush(); os.fsync(stream.fileno())

    def private_record(self, name, value):
        private = self.root / 'private'
        private.mkdir(mode=0o700, exist_ok=True)
        fd = os.open(private/name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            json.dump(value, stream); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
        for directory in (private, self.root):
            fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try: os.fsync(fd)
            finally: os.close(fd)

    def freeze(self, path, expected, expected_id, label):
        destination = self.root / (label + '.tar')
        digest = hashlib.sha256()
        with private_file(path, MAX_BUNDLE) as source, destination.open('xb') as target:
            os.chmod(destination, 0o600)
            size = 0
            while chunk := source.read(65536):
                size += len(chunk); require(size <= MAX_BUNDLE, 'bundle exceeds limit')
                digest.update(chunk); target.write(chunk)
            target.flush(); os.fsync(target.fileno())
        require(size > 0 and digest.hexdigest() == expected, 'bundle archive SHA-256 does not match expectation')
        destination.chmod(0o400)
        self.save('frozen-'+label, {'archive_sha256': expected, 'size': size, 'file': destination.name})
        with tarfile.open(destination, 'r:') as archive:
            first = archive.next()
            require(first and first.name == 'manifest.json' and first.isfile() and first.size <= 1 << 20,
                    'bundle must start with a bounded manifest.json')
            raw_manifest = archive.extractfile(first).read((1 << 20) + 1)
            require(hashlib.sha256(raw_manifest).hexdigest() == expected_id,
                    'canonical FES manifest identity does not match expected candidate')
            manifest = json.loads(raw_manifest)
        return destination, size, manifest


class Runner:
    STABLE_SECONDS = 10
    POLL_SECONDS = 1

    def __init__(self, args, client, evidence):
        self.args, self.client, self.evidence = args, client, evidence
        self.owner = 'software-accept-' + secrets.token_hex(6)
        self.session = kit.Session(client, self.owner, 'kit software API acceptance')
        self.phase = 'preflight'
        self.request = None
        self.requests = {}
        self.cancel_attempted = set()
        self.maintenance = None
        self.maintenance_token = None
        self.reboot_attempted = False
        self.boot = None
        self.selected = args.expected_active_bundle
        self.generation = None
        self.stages = {name: {'status': 'skipped'} for name in
                       ('held_install', 'competing_client', 'queue_cancel', 'requester_exit',
                        'retained_stop', 'activation', 'local_busy', 'reboot', 'recovery',
                        'core_inventory', 'input', 'hdmi')}
        self.client.deadline = time.monotonic() + args.timeout

    def health(self):
        h = self.client.request('/v1/health')
        require(h.get('target_id') == self.args.expected_target_id and h.get('api_version') == 'v1',
                'target identity or API contract changed')
        require(isinstance(h.get('boot_id'), str) and UUID.fullmatch(h['boot_id']), 'boot identity is missing')
        return h

    def snapshot(self, label):
        h = self.health()
        sw = self.client.request('/v1/kit/software')
        lease = self.client.request('/v1/kit/lease')
        native = self.client.request('/v1/status')
        h2 = self.health()
        if h['boot_id'] != h2['boot_id']:
            raise kit.KitError('boot changed during observation')
        require(DIGEST.fullmatch(sw.get('selected', '')) and DIGEST.fullmatch(sw.get('base_identity', '')),
                'software identities are invalid')
        require(not sw.get('active_bundle') or (isinstance(sw['active_bundle'], str) and DIGEST.fullmatch(sw['active_bundle'])),
                'active bundle identity is invalid')
        require(type(sw.get('generation')) is int and sw['generation'] >= 0, 'software generation is invalid')
        pending = sw.get('pending')
        safe_pending = None
        if pending:
            safe_pending = {'own_request': pending.get('id') in self.requests,
                            'phase': pending.get('phase') if pending.get('phase') in ('queued', 'applying', 'recovering', 'blocked') else 'unknown'}
        self.evidence.save(label, {'health': {'target_id': h['target_id'], 'boot_id': h['boot_id'], 'ready': h.get('ready') is True},
                                  'software': project(sw, ('selected', 'generation', 'active_bundle', 'base_identity')),
                                  'pending': safe_pending, 'unreleased_present': bool(sw.get('unreleased')),
                                  'fault_present': bool(sw.get('service_fault') or sw.get('storage_error')),
                                  'lease_state': lease.get('state') if lease.get('state') in ('held', 'free', 'revoking', 'blocked', 'maintenance', 'recovery-required') else 'unknown',
                                  'runtime_state': native.get('state') if native.get('state') in ('idle', 'active', 'launching', 'stopping', 'failed') else 'unknown'})
        return sw, lease, h, native

    def same_machine(self, sw, h):
        require(h['boot_id'] == self.boot and sw['base_identity'] == self.args.expected_base,
                'boot or base changed unexpectedly')

    def owned(self, lease):
        return (lease.get('state') == 'held' and lease.get('owner') == self.owner and
                lease.get('generation') == self.session.generation)

    def held(self):
        require(self.session.token and not self.session.failed.is_set() and time.monotonic() < self.session.deadline,
                'owned lease renewal was lost')
        sw, lease, h, _ = self.snapshot('held')
        self.same_machine(sw, h)
        require(self.owned(lease), 'owned execution generation changed')
        require(sw['selected'] == self.selected and sw['active_bundle'] == self.selected and sw['generation'] == self.generation,
                'software changed while execution was held')
        require(not sw.get('unreleased'), 'unexpected unreleased operation')
        if self.request:
            require(exact(sw.get('pending'), self.request) and sw['pending']['phase'] == 'queued',
                    'exact request did not remain queued')
        else:
            require(not sw.get('pending'), 'another software request appeared')
        return sw

    def request_for(self, candidate):
        request = dict(id=secrets.token_hex(32), candidate=candidate,
                       requester=self.owner, expected_generation=self.generation)
        self.requests[request['id']] = request
        self.evidence.save('activation-request', request)
        return request

    def queue(self, request):
        self.request = request  # Preserve intent even if the response is lost.
        reply = self.client.request('/v1/kit/software/activate', request)
        require(exact(reply.get('pending'), request), 'queue response does not match the exact request')
        self.request = request

    def expect_conflict(self, path, request, client=None):
        try:
            (client or self.client).request(path, request)
        except kit.KitError as error:
            require(error.status == 409, 'competing request did not return conflict')
            return
        raise CheckError('competing request unexpectedly succeeded')

    def cancel(self):
        request = self.request
        require(request is not None, 'no own queue exists')
        reply = self.client.request('/v1/kit/software')
        if request['id'] not in self.cancel_attempted:
            require(exact(reply.get('pending'), request) and reply['pending']['phase'] == 'queued',
                    'cancellation refuses a different or applying request')
            self.cancel_attempted.add(request['id'])
            self.evidence.save('cancel-attempt', {'id': request['id'], 'expected_generation': request['expected_generation']})
            try:
                reply = self.client.request('/v1/kit/software/cancel', {'id': request['id'], 'expected_generation': request['expected_generation']})
            except kit.KitError:
                # Read-only reconciliation; cleanup cannot transmit this mutation again.
                reply = self.client.request('/v1/kit/software')
        receipt = reply.get('receipts', {}).get(request['id'])
        require(exact(receipt, request) and receipt.get('result') == 'cancelled' and not reply.get('pending') and
                reply.get('generation') == request['expected_generation'], 'own cancellation was not confirmed')
        self.request = None

    def release(self):
        sw, lease, h, _ = self.snapshot('before-release')
        self.same_machine(sw, h)
        require(self.owned(lease), 'release refuses a different owner or generation')
        require(sw['selected'] == self.selected and sw['active_bundle'] == self.selected and sw['generation'] == self.generation,
                'release refuses a changed software selection')
        if self.request:
            require(exact(sw.get('pending'), self.request) and sw['pending'].get('phase') == 'queued',
                    'release refuses a different or applying queued request')
        else:
            require(not sw.get('pending'), 'release refuses a foreign pending request')
        # Never call Session.mutate/needs_stop: its development recovery can reboot.
        self.session.needs_stop = False
        self.session.close()  # clears credential before transmission, no retry

    def install(self, frozen, expected):
        self.held()
        path, size, _ = frozen
        with path.open('rb') as stream:
            release = self.client.request('/v1/kit/software/releases', stream=stream, size=size, timeout=180)
        require(release.get('id') == expected, 'installed bundle identity differs from expectation')
        active_manifest([release], expected)
        self.held()

    def cores(self):
        try:
            value = self.client.request('/v1/core-packages')
        except kit.KitError as error:
            if error.status == 404:
                return None
            raise
        packages = sorted(row.get('package_id', '') for row in value['packages'])
        selections = sorted((row.get('core_id', ''), row.get('package_id', '')) for row in value['selections'])
        require(all(DIGEST.fullmatch(v) for v in packages) and all(re.fullmatch(r'[a-z][a-z0-9_.-]{0,95}', v[0]) and DIGEST.fullmatch(v[1]) for v in selections),
                'core inventory contains invalid identities')
        return {'package_ids': packages, 'selections': selections}

    def accepted(self, request, previous, generation, *, recovery=False, reboot=False):
        stable = None
        original_boot = self.boot
        observed_boot = None
        phases = []
        while time.monotonic() < self.client.deadline:
            try:
                sw, lease, h, native = self.snapshot('transition')
            except kit.KitError:
                time.sleep(self.POLL_SECONDS)
                continue
            require(sw['base_identity'] == self.args.expected_base, 'base changed during transition')
            if reboot:
                if h['boot_id'] == original_boot:
                    time.sleep(self.POLL_SECONDS); continue
                require(observed_boot in (None, h['boot_id']), 'boot changed twice')
                observed_boot = h['boot_id']
            else:
                require(h['boot_id'] == original_boot, 'software transition unexpectedly rebooted')
            if recovery:
                require(sw['selected'] == previous and sw['generation'] == generation and sw.get('previous') == self.previous,
                        'failed startup changed committed software selection')
            pending = sw.get('pending')
            if pending:
                require(exact(pending, request), 'another software operation appeared')
                phase = pending.get('phase')
                require(phase in ('queued', 'applying', 'recovering'), 'software transition became blocked')
                if not phases or phase != phases[-1]:
                    require(not phases or ('queued', 'applying', 'recovering').index(phase) > ('queued', 'applying', 'recovering').index(phases[-1]),
                            'software transition replayed a phase')
                    phases.append(phase)
            receipt = sw.get('receipts', {}).get(request['id'])
            if receipt:
                require(exact(receipt, request) and receipt.get('result') == ('recovered' if recovery else 'accepted'),
                        'operation receipt has the wrong outcome')
                target = previous if recovery else request['candidate']
                target_generation = generation if recovery else generation + 1
                require(sw['selected'] == target and sw['active_bundle'] == target and sw['generation'] == target_generation,
                        'completed selection/generation differs from the operation')
                ready = (not pending and not sw.get('unreleased') and not sw.get('service_fault') and
                         not sw.get('storage_error') and lease.get('state') == 'free' and h.get('ready') is True and native.get('state') == 'idle')
                hs = h.get('software') or {}
                require(hs.get('active_bundle') == target and hs.get('base_identity') == self.args.expected_base,
                        'authenticated health identifies different software')
                if ready:
                    stable = stable or time.monotonic()
                    if time.monotonic() - stable >= self.STABLE_SECONDS:
                        self.boot, self.selected, self.generation = h['boot_id'], target, target_generation
                        if not recovery:
                            require(sw.get('previous') == previous, 'accepted previous selection differs')
                            self.previous = previous
                        self.request = None
                        return {'status': 'passed', 'operation_id': request['id'], 'result': receipt['result'],
                                'selected': target, 'generation': target_generation, 'boot_id': h['boot_id'],
                                'stable_seconds': self.STABLE_SECONDS, 'observed_phases': phases,
                                'attempt_evidence': 'one matching durable receipt; no process-start count claim'}
                else:
                    stable = None
            time.sleep(self.POLL_SECONDS)
        raise CheckError('transition did not stabilize before the deadline')

    def requester_child(self, request):
        self.request = request  # A child may queue successfully then lose its reply.
        wire = {'url': self.client.url.geturl(), 'bearer': self.client.bearer,
                'target_id': self.args.expected_target_id, 'boot_id': self.boot,
                'request': request}
        process = subprocess.run([sys.executable, str(Path(__file__).resolve()), '--_requester-child', '--execute'],
                                 input=json.dumps(wire), stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                 text=True, timeout=min(30, self.client.deadline-time.monotonic()), shell=False)
        require(process.returncode == 0 and len(process.stdout) <= 4096, 'requester process failed; inspect operation status')
        observed = json.loads(process.stdout)
        require(observed == {'queued_id': request['id']}, 'requester process did not acknowledge the exact request')
        self.request = request
        self.held()
        self.stages['requester_exit'] = {'status': 'passed', 'process_exit': 0, 'request_persisted_after_exit': True}

    def reboot(self, command):
        self.phase = 'reboot-preparation'
        self.session = kit.Session(self.client, self.owner, 'explicit queue reboot acceptance')
        self.session.claim(wait=0)
        self.held()
        promotion = {'request_id': secrets.token_hex(32), 'purpose': 'software queue reboot diagnostic'}
        self.evidence.private_record('maintenance-promotion.json',
                                     {'request': promotion, 'execution_token': self.session.token,
                                      'owner': self.owner, 'generation': self.session.generation,
                                      'boot_id': self.boot, 'target_id': self.args.expected_target_id})
        self.evidence.save('maintenance-promotion', promotion)
        grant = self.client.request('/v1/kit/maintenance/start', promotion, token=self.session.token)
        self.maintenance, self.maintenance_token = grant['status'], grant['token']
        require(isinstance(self.maintenance_token, str) and self.maintenance_token and
                self.maintenance.get('boot_id') == self.boot and self.maintenance.get('owner') == self.owner and
                self.maintenance.get('purpose') == 'software queue reboot diagnostic' and
                self.maintenance.get('operation_id') == promotion['request_id'] and
                DIGEST.fullmatch(self.maintenance.get('generation', '')), 'maintenance grant identity is invalid')
        self.evidence.private_record('maintenance-grant.json', grant)
        self.session.done.set()
        if self.session.thread:
            self.session.thread.join(timeout=20)
            require(not self.session.thread.is_alive(), 'ordinary renewal did not retire after maintenance promotion')
        self.session.token = None
        while True:
            current = self.client.request('/v1/kit/maintenance')
            require(all(current.get(k) == self.maintenance.get(k) for k in ('operation_id', 'generation', 'boot_id', 'owner')),
                    'maintenance ownership changed')
            if current['state'] == 'ready':
                break
            require(current['state'] == 'preparing', 'maintenance did not become ready')
            time.sleep(self.POLL_SECONDS)
        request = self.request_for(self.selected)
        self.queue(request)
        sw = self.client.request('/v1/kit/software')
        self.same_machine(sw, self.health())
        require(exact(sw.get('pending'), request) and sw['pending']['phase'] == 'queued', 'reboot queue changed')
        self.reboot_attempted = True
        self.evidence.save('reboot-attempt', {'operation_id': request['id'], 'old_boot_id': self.boot,
                                            'command_sha256': command['sha256'], 'invocation_attempt_recorded': True})
        # Every outcome beyond this boundary, even spawn failure, is read-only.
        self.evidence.save('reboot-command', external(command, max(.001, min(30, self.client.deadline-time.monotonic()))))
        result = self.accepted(request, self.selected, self.generation, reboot=True)
        result['activation_kind'] = 'reselect_current'
        self.stages['reboot'] = result
        self.maintenance_token = None
        self.maintenance = None
        self.reboot_attempted = False  # Old credentials are discarded; a later optional stage may claim anew.

    def cleanup(self):
        self.client.deadline = time.monotonic() + 30
        try:
            if self.reboot_attempted:
                return  # Never replay preboot credentials or cleanup after a reboot attempt.
            if not self.session.token and not self.maintenance_token:
                return
            sw = self.client.request('/v1/kit/software')
            self.same_machine(sw, self.health())
            if self.request:
                pending = sw.get('pending')
                if exact(pending, self.request) and pending.get('phase') == 'queued':
                    self.cancel()
                else:
                    receipt = sw.get('receipts', {}).get(self.request['id'])
                    require(not pending and not sw.get('unreleased') and exact(receipt, self.request) and
                            receipt.get('result') == 'cancelled', 'queue outcome is unresolved; cleanup will not release into activation')
                    self.request = None
            if self.maintenance_token:
                current = self.client.request('/v1/kit/maintenance')
                require(current.get('state') == 'ready' and all(current.get(k) == self.maintenance.get(k)
                        for k in ('operation_id', 'generation', 'boot_id', 'owner')), 'cleanup maintenance changed')
                self.client.request('/v1/kit/maintenance/end',
                                    {'operation_id': current['operation_id'], 'expected_generation': current['generation']},
                                    token=self.maintenance_token)
                self.maintenance_token = None
            elif self.session.token:
                self.release()
        except (ValueError, kit.KitError, OSError):
            self.evidence.save('cleanup-unresolved', {'status': 'inspect-required',
                                                     'own_queue_may_remain': self.request is not None,
                                                     'automatic_restore': False})
        finally:
            self.session.done.set()
            if self.session.thread:
                self.session.thread.join(timeout=20)

    def run(self, frozen, recovery_frozen, reboot_command, local_command):
        try:
            sw, lease, h, native = self.snapshot('baseline')
            self.boot = h['boot_id']
            self.same_machine(sw, h)
            require(sw['selected'] == self.selected and sw['active_bundle'] == self.selected,
                    'active software differs from explicitly expected bundle')
            require(lease.get('state') == 'free' and h.get('ready') is True and native.get('state') == 'idle',
                    'kit must already be free, ready and idle; no takeover')
            require(not sw.get('pending') and not sw.get('unreleased') and not sw.get('service_fault') and not sw.get('storage_error'),
                    'software supervisor is busy or faulted')
            update = self.client.request('/v1/update')
            require(update.get('image_sha256') == self.args.expected_base and update.get('boot_id') == self.boot and
                    not update.get('trial') and not update.get('corrupt') and not update.get('pending') and not update.get('candidate'),
                    'appliance gate or base identity is incompatible')
            maintenance = self.client.request('/v1/kit/maintenance')
            require(maintenance.get('state') == 'none', 'maintenance is already owned')
            active_manifest(self.client.request('/v1/kit/software/releases'), self.selected)
            self.generation = sw['generation']
            self.previous = sw.get('previous')
            before_cores = self.cores()
            self.phase = 'claim'
            self.session.claim(wait=0)
            self.held()
            competitor = Client(self.client.url.geturl(), self.client.bearer)
            competitor.deadline = self.client.deadline
            self.expect_conflict('/v1/kit/claim', {'request_id': secrets.token_hex(32),
                                 'owner': self.owner+'-competitor', 'purpose': 'independent competing client'}, competitor)
            self.stages['competing_client'] = {'status': 'passed', 'claim_http_status': 409,
                                                'scope': 'independent HTTP client request; not two physical workstations'}
            self.phase = 'held-install'
            self.install(frozen, self.args.candidate)
            self.stages['held_install'] = {'status': 'passed', 'candidate': self.args.candidate,
                                           'selection_unchanged_while_held': True}
            if local_command:
                self.phase = 'local-busy'
                self.held()
                probe = external(local_command, max(.001, min(20, self.client.deadline-time.monotonic())), output=True)
                require(probe.get('exit_code') == 0 and probe.get('probe', {}).get('http_status') == 409 and
                        probe['probe'].get('error') in ('in_use', 'blocked'), 'local probe did not report busy')
                self.held()
                self.stages['local_busy'] = {'status': 'passed', 'scope': 'external busy-only probe; no local launch claim',
                                             'command_sha256': local_command['sha256']}
            self.phase = 'queue-cancel'
            first = self.request_for(self.args.candidate)
            self.queue(first)
            self.queue(first)  # Intentional idempotence check, never a transport retry.
            self.expect_conflict('/v1/kit/software/activate', dict(first, requester=self.owner+'-conflict'))
            self.expect_conflict('/v1/kit/software/activate', dict(first, id=secrets.token_hex(32)))
            self.held()
            self.phase = 'retained-stop'
            stopped = self.client.request('/v1/stop', token=self.session.token)
            require(stopped.get('state') == 'idle' and not stopped.get('recovery'), 'Stop did not remain clean idle')
            self.held()
            self.stages['retained_stop'] = {'status': 'passed', 'execution_lease_retained': True,
                                             'scope': 'idle Stop under an owned grant; no game launch claim'}
            self.cancel()
            self.expect_conflict('/v1/kit/software/activate', dict(first, id=secrets.token_hex(32), expected_generation=self.generation+1))
            self.held()
            self.stages['queue_cancel'] = {'status': 'passed', 'duplicate_idempotent': True,
                                           'conflicting_requests_rejected': True, 'stale_generation_rejected': True}
            self.phase = 'requester-exit'
            request = self.request_for(self.args.candidate)
            self.requester_child(request)
            self.phase = 'release-activation'
            previous = self.selected
            self.release()
            self.stages['activation'] = self.accepted(request, previous, self.generation)
            self.stages['activation']['activation_kind'] = 'reselect_current' if previous == self.selected else 'change_selected'
            if reboot_command:
                self.reboot(reboot_command)
            if recovery_frozen:
                self.phase = 'recovery'
                self.session = kit.Session(self.client, self.owner, 'explicit failed startup recovery acceptance')
                self.session.claim(wait=0)
                self.install(recovery_frozen, self.args.recovery_candidate)
                request = self.request_for(self.args.recovery_candidate)
                self.queue(request); self.held(); self.release()
                self.stages['recovery'] = self.accepted(request, self.selected, self.generation, recovery=True)
            after_cores = self.cores()
            if before_cores is not None:
                require(after_cores == before_cores, 'core inventory or selections changed; never automatically restored')
                self.stages['core_inventory'] = {'status': 'passed', 'scope': 'supported API inventory identities and selections; no byte/launch proof',
                                                'inventory': after_cores}
            self.evidence.save('result', {'success': True, 'evidence_class': 'kit-software API lifecycle diagnostic',
                                          'target_id': self.args.expected_target_id, 'base_identity': self.args.expected_base,
                                          'selected': self.selected, 'generation': self.generation, 'boot_id': self.boot,
                                          'stages': self.stages, 'successful_selection_retained': True,
                                          'overall_hardware_acceptance': False})
            return True
        except (Exception, KeyboardInterrupt) as error:
            self.evidence.save('failure', {'success': False, 'phase': self.phase, 'error_type': type(error).__name__,
                                           'http_status': error.status if isinstance(error, kit.KitError) else None,
                                           'check': str(error) if isinstance(error, CheckError) else None,
                                           'stages': self.stages, 'inspect_required': True})
            return False
        finally:
            self.cleanup()


def requester_child():
    try:
        raw = sys.stdin.read(65537)
        require(len(raw) <= 65536, 'requester input exceeds limit')
        wire = json.loads(raw)
        client = Client(wire['url'], wire['bearer'])
        h = client.request('/v1/health')
        require(h.get('target_id') == wire['target_id'] and h.get('boot_id') == wire['boot_id'] and h.get('api_version') == 'v1',
                'requester target changed')
        reply = client.request('/v1/kit/software/activate', wire['request'])
        require(exact(reply.get('pending'), wire['request']), 'requester queue response mismatch')
        print(json.dumps({'queued_id': wire['request']['id']}))
        return 0
    except Exception:
        return 1


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--config', required=True, type=Path)
    p.add_argument('--expected-target-id', required=True)
    p.add_argument('--expected-base-image-sha256', dest='expected_base', required=True)
    p.add_argument('--expected-bundle-id', dest='expected_active_bundle', required=True)
    p.add_argument('--candidate', required=True)
    p.add_argument('--bundle', required=True, type=Path)
    p.add_argument('--expected-bundle-sha256', required=True)
    p.add_argument('--evidence-dir', required=True, type=Path)
    p.add_argument('--recovery-candidate')
    p.add_argument('--recovery-bundle', type=Path)
    p.add_argument('--expected-recovery-sha256')
    p.add_argument('--reboot-command-file', type=Path)
    p.add_argument('--local-command-file', type=Path)
    p.add_argument('--timeout', type=float, default=600, help='total seconds before bounded cleanup (default 600)')
    p.add_argument('--execute', action='store_true')
    return p


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    if argv == ['--_requester-child', '--execute']:
        return requester_child()
    args = parser().parse_args(argv)
    try:
        require(args.execute, '--execute is required; no compatibility dry-run is implied')
        require(UUID.fullmatch(args.expected_target_id), 'expected target ID must be a UUID')
        for value in (args.expected_base, args.expected_active_bundle, args.candidate, args.expected_bundle_sha256):
            require(DIGEST.fullmatch(value), 'artifact expectations must be SHA-256 identities')
        recovery = (args.recovery_candidate, args.recovery_bundle, args.expected_recovery_sha256)
        require(all(recovery) or not any(recovery), 'recovery requires an explicit candidate, bundle and archive SHA-256')
        if all(recovery):
            require(DIGEST.fullmatch(args.recovery_candidate) and DIGEST.fullmatch(args.expected_recovery_sha256), 'invalid recovery identities')
            require(args.recovery_candidate != args.candidate, 'failing recovery candidate must differ from accepted candidate')
        require(0 < args.timeout <= 1800, 'timeout must be between zero and 1800 seconds')
        reboot = command_file(args.reboot_command_file); local = command_file(args.local_command_file)
        evidence = Evidence(args.evidence_dir)
        bundle = evidence.freeze(args.bundle, args.expected_bundle_sha256, args.candidate, 'candidate')
        active_manifest([{'id': args.candidate, 'manifest': bundle[2]}], args.candidate)
        bad = evidence.freeze(args.recovery_bundle, args.expected_recovery_sha256, args.recovery_candidate, 'recovery') if all(recovery) else None
        if bad:
            active_manifest([{'id': args.recovery_candidate, 'manifest': bad[2]}], args.recovery_candidate)
        client = load_target(args.config, args.expected_target_id)
        success = Runner(args, client, evidence).run(bundle, bad, reboot, local)
        if not success:
            print('Diagnostic failed: inspect failure/cleanup records and current ownership. Private maintenance recovery material, if created, is under the evidence directory/private. No automatic restore or mutation retry.', file=sys.stderr)
        return 0 if success else 1
    except (Exception, KeyboardInterrupt):
        print('Kit software diagnostic refused or failed; inspect private evidence and current state. No automatic restore or mutation retry.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupted)
    raise SystemExit(main())
