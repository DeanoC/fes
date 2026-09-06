#!/usr/bin/env python3
"""Thin interactive client for FogCast's target-owned kit lease (Python 3.11+)."""
import argparse
import http.client
import json
import os
from pathlib import Path
import secrets
import select
import shlex
import stat
import sys
import threading
import time
import tomllib
from urllib.parse import urlsplit

MAX_RBF = 32 << 20
HEADER = 'X-FogCast-Kit-Lease'


class KitError(Exception):
    def __init__(self, message, status=None):
        super().__init__(message)
        self.status = status


class Client:
    def __init__(self, url, bearer, timeout=15):
        self.url = urlsplit(url)
        if (self.url.scheme not in ('http', 'https') or not self.url.hostname
                or self.url.username or self.url.password or self.url.query
                or self.url.fragment or self.url.path not in ('', '/')):
            raise KitError('target URL must be an http(s) origin without credentials')
        if not bearer or '\n' in bearer or '\r' in bearer:
            raise KitError('a valid FogCast bearer token is required')
        self.bearer, self.timeout = bearer, timeout

    def request(self, path, body=None, token=None, stream=None, size=0):
        headers = {'Authorization': 'Bearer ' + self.bearer}
        if token:
            headers[HEADER] = token
        payload = json.dumps(body).encode() if body is not None else b''
        headers['Content-Length'] = str(size if stream else len(payload))
        if stream:
            headers['Content-Type'] = 'application/octet-stream'
        elif body is not None:
            headers['Content-Type'] = 'application/json'
        connection = (http.client.HTTPSConnection if self.url.scheme == 'https'
                      else http.client.HTTPConnection)(self.url.hostname, self.url.port,
                                                       timeout=self.timeout)
        try:
            connection.putrequest('GET' if path == '/v1/kit/lease' else 'POST', path)
            for key, value in headers.items():
                connection.putheader(key, value)
            connection.endheaders()
            if stream:
                remaining = size
                while remaining:
                    chunk = stream.read(min(65536, remaining))
                    if not chunk:
                        raise KitError('RBF changed or was truncated during upload')
                    connection.send(chunk)
                    remaining -= len(chunk)
            elif payload:
                connection.send(payload)
            response = connection.getresponse()
            # Never echo a remote body: it could include bearer or lease secrets.
            raw = response.read(65537)
            if response.status >= 300:
                raise KitError(f'target rejected request (HTTP {response.status})', response.status)
            if len(raw) > 65536:
                raise KitError('target response exceeds limit')
            result = json.loads(raw)
            if not isinstance(result, dict):
                raise ValueError()
            return result
        except KitError:
            raise
        except (OSError, ValueError, http.client.HTTPException):
            raise KitError('target request failed; inspect target status before retrying') from None
        finally:
            connection.close()

    def load(self, path, token):
        with open(path, 'rb') as source:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= MAX_RBF:
                raise KitError('RBF must be a regular file between 1 byte and 32 MiB')
            return self.request('/v1/development/rbf', token=token,
                                stream=source, size=info.st_size)


class Session:
    def __init__(self, client, owner, purpose, interval=20):
        self.client, self.owner, self.purpose = client, owner, purpose
        self.interval = interval
        self.token = None
        self.generation = None
        self.deadline = 0
        self.failed = threading.Event()
        self.done = threading.Event()
        self.thread = None

    def claim(self, generation=None, reason=None, wait=60):
        body = dict(request_id=secrets.token_hex(32), owner=self.owner, purpose=self.purpose)
        path = '/v1/kit/claim'
        if generation is not None:
            path = '/v1/kit/takeover'
            body.update(expected_generation=generation, reason=reason)
        deadline = time.monotonic() + wait
        while True:
            try:
                request_start = time.monotonic()
                grant = self.client.request(path, body)
                break
            except KitError as error:
                # Same request identity allows recovery of a lost successful response.
                if (error.status is not None and not (generation is not None and error.status == 409)) or time.monotonic() >= deadline:
                    raise
                time.sleep(min(1, max(0, deadline - time.monotonic())))
        token, generation, expires = self.validate_grant(grant, request_start)
        self.token, self.generation, self.deadline = token, generation, expires
        self.thread = threading.Thread(target=self._renew, daemon=True)
        self.thread.start()
        return grant.get('status', {})

    @staticmethod
    def validate_grant(grant, request_start):
        token = grant.get('token')
        status = grant.get('status')
        if not isinstance(status, dict):
            raise KitError('target returned an invalid lease grant')
        generation = status.get('generation')
        remaining = status.get('expires_in_ms')
        if (not isinstance(token, str) or not token or any(ord(c) < 33 or ord(c) > 126 for c in token)
                or status.get('state') != 'held'
                or not isinstance(generation, str) or not generation.strip()
                or type(remaining) is not int or remaining <= 0):
            raise KitError('target returned an invalid lease grant')
        try:
            deadline = request_start + remaining / 1000
        except OverflowError:
            raise KitError('target returned an invalid lease duration') from None
        if time.monotonic() >= deadline:
            raise KitError('lease grant expired in transit')
        return token, generation, deadline

    def _renew(self):
        while not self.done.wait(self.interval):
            try:
                request_start = time.monotonic()
                if request_start >= self.deadline:
                    raise KitError('lease expired locally')
                grant = self.client.request('/v1/kit/renew', token=self.token)
                token, generation, deadline = self.validate_grant(grant, request_start)
                if token != self.token or generation != self.generation or time.monotonic() >= self.deadline:
                    raise KitError('lease renewal identity changed or previous lease expired')
                self.deadline = deadline
            except KitError:
                self.failed.set()
                return

    def mutate(self, command, path=None):
        if not self.token or self.failed.is_set() or time.monotonic() >= self.deadline:
            raise KitError('lease renewal lost; mutations disabled')
        if command == 'load':
            return self.client.load(path, self.token)
        return self.client.request('/v1/stop', token=self.token)

    def close(self):
        self.done.set()
        if self.thread:
            self.thread.join()
        if self.token:
            token, self.token = self.token, None
            return self.client.request('/v1/kit/release', token=token)
        return {}


def display(value):
    # Output only public status fields, never arbitrary grants or error bodies.
    fields = ('state', 'generation', 'owner', 'purpose', 'expires_at', 'expires_in_ms', 'reason', 'core')
    print(json.dumps({key: value[key] for key in fields if key in value}), flush=True)


def configuration(args):
    config = {}
    if args.config:
        with open(args.config, 'rb') as source:
            config = tomllib.load(source)
        if config.get('selected_target'):
            targets = [target for target in config.get('targets', [])
                       if target.get('name') == config['selected_target']]
            if len(targets) != 1:
                raise KitError('selected target is not uniquely configured')
            if not targets[0].get('enabled', False):
                raise KitError('selected target is disabled')
            config = {'base_url': targets[0].get('address', ''),
                      'token': targets[0].get('agent', '')}
    return Client(args.url or os.environ.get('FOGCAST_BASE_URL') or config.get('base_url', ''),
                  os.environ.get('FOGCAST_TOKEN') or config.get('token', ''))


def read_commands(fd):
    pending = b''
    while True:
        if b'\n' in pending:
            line, pending = pending.split(b'\n', 1)
            yield line.decode('utf-8')
            continue
        if not select.select([fd], [], [], .25)[0]:
            yield None
            continue
        chunk = os.read(fd, 4096)
        if not chunk:
            if pending:
                yield pending.decode('utf-8')
            return
        pending += chunk
        if len(pending) > 65536:
            raise KitError('session command exceeds limit')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, help='private FogCast host TOML')
    parser.add_argument('--url', help='target agent origin (or FOGCAST_BASE_URL)')
    commands = parser.add_subparsers(dest='command', required=True)
    commands.add_parser('status')
    for name in ('session', 'takeover'):
        command = commands.add_parser(name)
        command.add_argument('--owner', required=True)
        command.add_argument('--purpose', required=True)
        if name == 'takeover':
            command.add_argument('--expected-generation', required=True)
            command.add_argument('--reason', required=True)
    args = parser.parse_args(argv)
    session = None
    code = 0
    try:
        client = configuration(args)
        if args.command == 'status':
            display(client.request('/v1/kit/lease'))
            return 0
        session = Session(client, args.owner, args.purpose)
        display(session.claim(getattr(args, 'expected_generation', None), getattr(args, 'reason', None)))
        print('Commands: load PATH, stop, status, release (EOF also releases).', flush=True)
        for line in read_commands(sys.stdin.fileno()):
            if session.failed.is_set():
                break
            if line is None:
                continue
            command = shlex.split(line)
            if command == ['release']:
                break
            if command == ['status']:
                display(client.request('/v1/kit/lease'))
            elif command == ['stop']:
                display(session.mutate('stop'))
            elif len(command) == 2 and command[0] == 'load':
                display(session.mutate('load', command[1]))
            else:
                raise KitError('expected load PATH, stop, status, or release')
        if session.failed.is_set():
            raise KitError('lease renewal failed; session ended and mutations disabled')
    except KitError as error:
        print(str(error), file=sys.stderr)
        code = 1
    except (OSError, ValueError):
        # Local parsing/IO errors may contain private config data; keep output bounded.
        print('kit operation failed; inspect target status before retrying', file=sys.stderr)
        code = 1
    except KeyboardInterrupt:
        code = 130
    finally:
        if session:
            try:
                display(session.close())
            except KitError:
                print('release not confirmed; target expiry will attempt cleanup', file=sys.stderr)
                code = code or 1
    return code


if __name__ == '__main__':
    sys.exit(main())
