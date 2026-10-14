"""Target-enforced maintenance admission for the FES kit transport.

The private journal retains credentials for explicit recovery after a lost
response. It is not an ownership authority; the target decides admission.
"""

import contextlib
import fcntl
import json
import http.client
import os
from pathlib import Path
import re
import secrets
import stat
import tempfile
import threading
import time
import tomllib
import unicodedata
import urllib.error
import urllib.parse
import urllib.request


MAX_BYTES = 64 * 1024
HEX = re.compile(r'^[0-9a-f]{64}$')
REQUEST_ID = re.compile(r'^[0-9a-f]{32,128}$')
PUBLIC_FIELDS = ('state', 'operation_id', 'generation', 'owner', 'purpose', 'boot_id', 'reason')


class MaintenanceError(ValueError):
    """A sanitized maintenance admission or recovery failure."""


class _Unavailable(MaintenanceError):
    pass


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


def _label(value):
    # Match kitlease.label: Go's len counts UTF-8 bytes and IsControl is Cc.
    if not isinstance(value, str):
        return False
    try:
        size = len(value.encode('utf-8'))
    except UnicodeError:
        return False
    return (0 < size <= 160 and value.strip() == value
            and all(unicodedata.category(c) != 'Cc' for c in value))


def _status(value):
    if not isinstance(value, dict) or value.get('state') not in (
        'none', 'preparing', 'ready', 'blocked', 'finishing', 'completed'
    ):
        raise MaintenanceError('invalid target maintenance status')
    if value['state'] != 'none':
        if (not REQUEST_ID.fullmatch(str(value.get('operation_id', '')))
                or not HEX.fullmatch(str(value.get('generation', '')))
                or not _label(value.get('boot_id'))):
            raise MaintenanceError('invalid target maintenance identity')
        if not _label(value.get('owner')) or not _label(value.get('purpose')):
            raise MaintenanceError('invalid target maintenance owner')
    if 'reason' in value and (not isinstance(value['reason'], str) or len(value['reason']) > 1024):
        raise MaintenanceError('invalid target maintenance reason')
    # Never publish credentials or arbitrary fields from an API response.
    return {key: value[key] for key in PUBLIC_FIELDS if key in value}


class KitMaintenance:
    def __init__(self, name, transport, host, journal: Path, *, timeout=30.0, poll_interval=.1):
        self.name, self.transport, self.host = name, transport, host
        self.journal = Path(journal)
        self.timeout, self.poll_interval = timeout, poll_interval
        self._transaction_owner = None
        self._transaction_depth = 0
        self.lock_path = self.journal.with_suffix(self.journal.suffix + '.lock')
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), _NoRedirect())
        if not 0 < timeout <= 300 or not 0 < poll_interval <= 5:
            raise MaintenanceError('maintenance timeouts must be positive and bounded')

    @contextlib.contextmanager
    def transaction(self):
        """Serialize this workstation's journal across begin/apply/finish.

        This lock grants no kit ownership; target admission remains required.
        Reentrancy is limited to this instance and the owning process/thread.
        """
        owner = (os.getpid(), threading.get_ident())
        if self._transaction_owner == owner:
            self._transaction_depth += 1
            try:
                yield self
            finally:
                self._transaction_depth -= 1
            return
        fd = None
        acquired = False
        try:
            self.journal.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            parent = self.journal.parent.lstat()
            if (not stat.S_ISDIR(parent.st_mode) or parent.st_mode & 0o022
                    or parent.st_uid != os.getuid()):
                raise MaintenanceError('maintenance journal directory must be owned and not shared writable')
            fd = os.open(self.lock_path,
                         os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077
                    or info.st_uid != os.getuid()):
                raise MaintenanceError('maintenance transaction lock must be an owned private regular file')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise MaintenanceError('another local maintenance transaction is running') from None
            acquired = True
            self._transaction_owner, self._transaction_depth = owner, 1
            yield self
        except OSError:
            raise MaintenanceError('cannot access private maintenance transaction lock') from None
        finally:
            if acquired:
                self._transaction_owner, self._transaction_depth = None, 0
                fcntl.flock(fd, fcntl.LOCK_UN)
            if fd is not None:
                os.close(fd)

    def _config(self):
        try:
            raw = self.transport.run(['show', '/media/fat/fogcast/agent.toml'])
            if len(raw.encode()) > MAX_BYTES:
                raise ValueError()
            config = tomllib.loads(raw)
            token, target_id = config['token'], config['target_id']
            if not _label(token) or not _label(target_id):
                raise ValueError()
            override = os.environ.get(f'FES_KIT_{self.name}_API')
            if override:
                origin = override
            else:
                listen = config.get('listen_address', '0.0.0.0:8182')
                port = int(listen.rsplit(':', 1)[1])
                if not 1 <= port <= 65535:
                    raise ValueError()
                host = self.host
                if ':' in host and not host.startswith('['):
                    host = '[' + host + ']'
                origin = f'http://{host}:{port}'
            parsed = urllib.parse.urlsplit(origin)
            if (parsed.scheme not in ('http', 'https') or not parsed.hostname
                    or parsed.username or parsed.password or parsed.path not in ('', '/')
                    or parsed.query or parsed.fragment or not parsed.port):
                raise ValueError()
        except Exception:
            raise MaintenanceError('cannot read valid target agent connection settings') from None
        self.origin, self.token, self.target_id = origin.rstrip('/'), token, target_id

    def _request(self, path, body=None, lease=None):
        headers = {'Authorization': 'Bearer ' + self.token}
        if lease:
            headers['X-FogCast-Kit-Lease'] = lease
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        request = urllib.request.Request(self.origin + path, data=data, headers=headers,
                                         method='POST' if body is not None else 'GET')
        try:
            with self.opener.open(request, timeout=min(self.timeout, 5)) as response:
                raw = response.read(MAX_BYTES + 1)
        except urllib.error.HTTPError as exc:
            code = exc.code
            exc.close()
            if code in (502, 503, 504):
                raise _Unavailable('target maintenance API temporarily unavailable') from None
            if code == 404:
                raise MaintenanceError('target does not support maintenance admission; upgrade required') from None
            raise MaintenanceError(f'target maintenance request rejected (HTTP {code})') from None
        except (OSError, urllib.error.URLError, http.client.HTTPException):
            raise _Unavailable('target maintenance API unavailable; reservation retained') from None
        if len(raw) > MAX_BYTES:
            raise MaintenanceError('target maintenance response exceeds size limit')
        try:
            value = json.loads(raw)
            if not isinstance(value, dict):
                raise ValueError()
            return value
        except (ValueError, UnicodeError):
            raise MaintenanceError('invalid target maintenance response') from None

    def _health(self):
        value = self._request('/v1/health')
        if value.get('target_id') != self.target_id or value.get('api_version') != 'v1':
            raise MaintenanceError('target identity or API version does not match configuration')
        boot_id = value.get('boot_id')
        if not _label(boot_id):
            raise MaintenanceError('target health has no boot identity')
        return boot_id

    def _read(self):
        try:
            fd = os.open(self.journal, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        except FileNotFoundError:
            return None
        except OSError:
            raise MaintenanceError('cannot read private maintenance journal') from None
        try:
            with os.fdopen(fd, 'rb') as handle:
                info = os.fstat(handle.fileno())
                if (not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077
                        or info.st_uid != os.getuid()):
                    raise MaintenanceError('maintenance journal must be an owned private regular file')
                raw = handle.read(MAX_BYTES + 1)
            value = json.loads(raw) if len(raw) <= MAX_BYTES else None
            if (not isinstance(value, dict) or value.get('format') != 1
                    or value.get('kit') != self.name or not _label(value.get('owner'))
                    or not _label(value.get('purpose'))
                    or not HEX.fullmatch(str(value.get('request_id', '')))
                    or not HEX.fullmatch(str(value.get('claim_id', '')))
                    or not _label(value.get('target_id')) or not _label(value.get('boot_id'))):
                raise MaintenanceError('invalid private maintenance journal')
            for key in ('lease_token', 'token'):
                if value.get(key) and not HEX.fullmatch(str(value[key])):
                    raise MaintenanceError('invalid private maintenance credentials')
            if value.get('status'):
                _status(value['status'])
            return value
        except (ValueError, OSError):
            raise MaintenanceError('invalid private maintenance journal') from None

    def _write(self, value):
        temporary = None
        try:
            self.journal.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            parent = self.journal.parent.lstat()
            if (not stat.S_ISDIR(parent.st_mode) or parent.st_mode & 0o022
                    or parent.st_uid != os.getuid()):
                raise MaintenanceError('maintenance journal directory must be owned and not shared writable')
            # Reject an unfamiliar or publicly readable journal before replacement.
            existing = self._read()
            if existing and existing['request_id'] != value['request_id']:
                raise MaintenanceError('private maintenance journal changed; refusing replacement')
            with tempfile.NamedTemporaryFile(mode='w', dir=self.journal.parent,
                                             prefix='.maintenance-', delete=False) as handle:
                temporary = handle.name
                os.chmod(temporary, 0o600)
                json.dump(value, handle)
                handle.write('\n')
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(temporary, self.journal)
            temporary = None
            fd = os.open(self.journal.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        except OSError:
            raise MaintenanceError('cannot persist private maintenance journal') from None
        finally:
            if temporary:
                os.unlink(temporary)

    def status(self):
        self._config()
        self._health()
        return self._public(_status(self._request('/v1/kit/maintenance')), self._read())

    def _public(self, value, record=None):
        result = dict(value)
        credentials = [self.token]
        if record:
            credentials.extend(record.get(key) for key in ('lease_token', 'token'))
        for key, text in result.items():
            if isinstance(text, str):
                for credential in credentials:
                    if credential:
                        text = text.replace(credential, '<redacted>')
                result[key] = text
        return result

    def _ours(self, current, record):
        if (current.get('operation_id') != record['request_id']
                or current.get('owner') != record['owner']
                or current.get('purpose') != record['purpose']
                or current.get('boot_id') != record['boot_id']
                or (record.get('status') and
                    current.get('generation') != record['status']['generation'])):
            raise MaintenanceError('target maintenance operation differs from private journal')

    def _wait(self, record, *, ready=False, terminal=False, settled=False):
        deadline = time.monotonic() + self.timeout
        while True:
            try:
                self._config()
                boot_id = self._health()
                if self.target_id != record['target_id']:
                    raise MaintenanceError('target configuration identity changed during maintenance')
                if ready and boot_id != record['boot_id']:
                    raise MaintenanceError('target rebooted during maintenance; explicit recovery required')
                current = _status(self._request('/v1/kit/maintenance'))
                if current['state'] != 'none' or (current.get('operation_id') and record.get('token')):
                    self._ours(current, record)
                if (terminal and current['state'] == 'finishing'
                        or settled and current['state'] == 'preparing'):
                    pass
                elif not ready or current['state'] == 'ready':
                    return current
                if current['state'] == 'blocked':
                    raise MaintenanceError('target maintenance is blocked; reservation retained')
                if current['state'] in ('none', 'completed'):
                    raise MaintenanceError('target maintenance reservation is no longer active')
            except _Unavailable:
                pass
            if time.monotonic() >= deadline:
                raise MaintenanceError('target maintenance readiness timed out; reservation retained')
            time.sleep(self.poll_interval)

    def begin(self, owner, purpose):
        if not _label(owner) or not _label(purpose):
            raise MaintenanceError('maintenance owner and purpose must be bounded labels')
        with self.transaction():
            return self._begin(owner, purpose)

    def _begin(self, owner, purpose):
        record = self._read()
        recovering = record is not None
        if record:
            # A retained operation may outlive the agent process. Waiting here
            # only observes health/status; no claim or start is replayed yet.
            current = self._wait(record, settled=True)
            boot_id = self._health()
        else:
            self._config()
            boot_id = self._health()
            current = _status(self._request('/v1/kit/maintenance'))
        if record:
            if (record['owner'] != owner or record['purpose'] != purpose
                    or record['target_id'] != self.target_id or record['boot_id'] != boot_id):
                raise MaintenanceError('private maintenance journal requires its original owner and identity')
            if current['state'] != 'none' or (current.get('operation_id') and record.get('token')):
                self._ours(current, record)
            if record.get('token'):
                if current['state'] == 'blocked':
                    self._request('/v1/kit/maintenance/resume', {
                        'operation_id': record['request_id'],
                        'expected_generation': record['status']['generation'],
                    }, record['token'])
                result = self._wait(record, ready=True)
                record['status'] = result
                self._write(record)
                return self._public(result, record)
        else:
            if current['state'] not in ('none', 'completed'):
                raise MaintenanceError('kit already has a maintenance owner')
            record = {'format': 1, 'kit': self.name, 'owner': owner, 'purpose': purpose,
                      'target_id': self.target_id, 'boot_id': boot_id,
                      'claim_id': secrets.token_hex(32), 'request_id': secrets.token_hex(32),
                      'phase': 'claiming'}
            self._write(record)
        if not record.get('lease_token'):
            grant = self._request('/v1/kit/claim', {
                'request_id': record['claim_id'], 'owner': owner, 'purpose': purpose,
            })
            if not HEX.fullmatch(str(grant.get('token', ''))):
                raise MaintenanceError('target returned an invalid execution grant')
            record['lease_token'], record['phase'] = grant['token'], 'starting'
            self._write(record)
        grant = self._request('/v1/kit/maintenance/start', {
            'request_id': record['request_id'], 'purpose': record['purpose'],
        }, record['lease_token'])
        result = _status(grant.get('status'))
        self._ours(result, record)
        if not HEX.fullmatch(str(grant.get('token', ''))):
            raise MaintenanceError('target returned an invalid maintenance grant')
        record.update(token=grant['token'], status=result, phase='reserved')
        self._write(record)
        if recovering and result['state'] == 'blocked':
            self._request('/v1/kit/maintenance/resume', {
                'operation_id': record['request_id'],
                'expected_generation': result['generation'],
            }, record['token'])
        result = self._wait(record, ready=True)
        record['status'] = result
        self._write(record)
        return self._public(result, record)

    def finish(self):
        with self.transaction():
            return self._finish()

    def _finish(self):
        record = self._read()
        if not record or not record.get('token'):
            raise MaintenanceError('no private maintenance grant; explicitly resume begin first')
        # Startup can expose the retained preparing marker before idle cleanup
        # finishes. Observe it settling before attempting End; never replay End
        # because startup temporarily refused an otherwise valid operation.
        current = self._wait(record, settled=True)
        if current['state'] == 'finishing':
            current = self._wait(record, terminal=True)
            if current['state'] not in ('none', 'completed'):
                raise MaintenanceError('maintenance end incomplete; reservation retained')
        if current['state'] not in ('none', 'completed'):
            record['phase'] = 'finishing'
            self._write(record)
            try:
                current = _status(self._request('/v1/kit/maintenance/end', {
                    'operation_id': record['request_id'],
                    'expected_generation': record['status']['generation'],
                }, record['token']))
            except MaintenanceError:
                # Observe once after an ambiguous end; never replay the mutation.
                current = self._wait(record, terminal=True)
                if current['state'] not in ('none', 'completed'):
                    raise MaintenanceError('maintenance end unconfirmed; reservation and journal retained') from None
            if current['state'] != 'none' or current.get('operation_id'):
                self._ours(current, record)
            if current['state'] == 'finishing':
                current = self._wait(record, terminal=True)
        if current['state'] not in ('none', 'completed'):
            raise MaintenanceError('maintenance end incomplete; reservation retained')
        retained = self._read()
        if not retained or retained['request_id'] != record['request_id']:
            raise MaintenanceError('private maintenance journal changed; refusing removal')
        self.journal.unlink()
        return self._public(current, record)
