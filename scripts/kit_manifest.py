#!/usr/bin/env python3
"""Claim, swap, restore and watch development-kit runtime files.

The root filesystem on a kit is read-only, so the manifest and stock backups
live on the writable FAT volume at /media/fat/fes/. Hold markers stay on the
workstation at ~/tmp/KITA-HELD-BY-<who> (and KITB-...), which is the existing
cooperative lock for disruptive kit work.
"""

import argparse
from collections import namedtuple
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile


REMOTE = Path(__file__).resolve().parent / 'kit_remote.sh'
MANIFEST_PATH = '/media/fat/fes/kit-manifest.json'
BASELINE_PATH = '/media/fat/fes/baseline.json'
CORE_PREFIX = '/usr/share/mister-runtime/core-packages/'
SERVICE_ORDER = ('S40mister-runtime', 'S50mister-agent', 'S60fogcast-kit')
HOSTS = {'A': '192.168.10.84', 'B': '192.168.10.85'}
NAME_RE = re.compile(r'^[A-Za-z0-9._+-]{1,80}$')
SLUG_RE = re.compile(r'^[A-Za-z0-9._-]{1,64}$')
HASH_RE = re.compile(r'^[0-9a-f]{64}$')
Component = namedtuple('Component', 'name path persistence services mode')

RUNTIME = SERVICE_ORDER
AGENT = ('S50mister-agent', 'S60fogcast-kit')
LAUNCHER = ('S60fogcast-kit',)


def component(name, path, persistence, services, mode):
    return Component(name, path, persistence, services, mode)


COMPONENTS = {
    'mister-runtime': component('mister-runtime', '/usr/sbin/mister-runtime', 'tmpfs', RUNTIME, '0755'),
    'mister-agent': component('mister-agent', '/usr/sbin/mister-agent', 'tmpfs', AGENT, '0755'),
    'fogcast-kit': component('fogcast-kit', '/usr/sbin/fogcast-kit', 'tmpfs', LAUNCHER, '0755'),
    'fogcast-tenfoot': component('fogcast-tenfoot', '/usr/sbin/fogcast-tenfoot', 'tmpfs', LAUNCHER, '0755'),
    'launcher.json': component('launcher.json', '/media/fat/fogcast/launcher.json', 'persistent', LAUNCHER, '0644'),
    'agent.toml': component('agent.toml', '/media/fat/fogcast/agent.toml', 'persistent', AGENT, '0644'),
}
TRACKED_PATHS = {item.path: item for item in COMPONENTS.values()}


class KitError(Exception):
    def __init__(self, message, code=1):
        super().__init__(message)
        self.code = code


def utc_stamp():
    return datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


def local_stamp():
    return datetime.datetime.now().astimezone().strftime('%Y-%m-%d %H:%M:%S %Z')


def sha256_bytes(data):
    return hashlib.sha256(data).hexdigest()


def hold_dir():
    path = Path(os.environ.get('FES_KIT_HOLD_DIR', '/home/deano/tmp'))
    path.mkdir(parents=True, exist_ok=True)
    return path


def drift_dir():
    path = Path(os.environ.get('FES_KIT_DRIFT_DIR', '/home/deano/tmp/fes-kit-drift'))
    path.mkdir(parents=True, exist_ok=True)
    return path


def kit_name(value):
    name = value.upper()
    if name not in HOSTS:
        raise KitError(f'unknown kit {value}; use A or B', code=2)
    return name


def kit_prefix(name):
    return f'KIT{kit_name(name)}'


def check_slug(value, label):
    if not SLUG_RE.fullmatch(value or ''):
        raise KitError(f'{label} must match {SLUG_RE.pattern}', code=2)
    return value


def check_reason(value):
    text = (value or '').strip()
    if not text or '\n' in text or len(text) > 200:
        raise KitError('a reason is required (one line, at most 200 characters)', code=2)
    return text


def resolve_component(spec):
    if spec in COMPONENTS:
        return COMPONENTS[spec]
    path = spec
    if spec.startswith('core-packages/'):
        path = '/usr/share/mister-runtime/' + spec
    if not path.startswith('/'):
        raise KitError(f'unknown component {spec}', code=2)
    if not allowed_path(path):
        raise KitError(f'component path is not tracked: {path}', code=2)
    digest = hashlib.sha256(path.encode()).hexdigest()[:16]
    return component('path-' + digest, path, 'tmpfs', RUNTIME, '0755')


def image_id(value):
    text = (value or '').strip().lower()
    if HASH_RE.fullmatch(text):
        return text
    return ''


def allowed_path(path):
    """Core-package destinations stay lexically inside the package directory.

    ``.`` and ``..`` segments are rejected. ``normpath`` collapses them, so a
    path that changes under normalization can leave the tracked directory.
    """
    if path in TRACKED_PATHS:
        return True
    if '\x00' in path or not path.startswith(CORE_PREFIX):
        return False
    normalized = os.path.normpath(path)
    if normalized != path or not normalized.startswith(CORE_PREFIX):
        return False
    parts = normalized[len(CORE_PREFIX):].split('/')
    if len(parts) < 2 or not HASH_RE.fullmatch(parts[0]):
        return False
    return all(part not in ('.', '..') and NAME_RE.fullmatch(part) for part in parts)


def stock_path(name, image_sha):
    image = image_id(image_sha)
    if not image:
        raise KitError('kit image sha256 is missing; cannot place a stock backup')
    return f'/media/fat/fes/stock/{image}/{name}'


def hash_specs(items, image_sha):
    specs = []
    seen = set()
    image = image_id(image_sha)
    for item in items:
        if item.name in seen:
            continue
        seen.add(item.name)
        specs.append(f'{item.name}={item.path}')
        if image:
            specs.append(f'stock-{item.name}={stock_path(item.name, image)}')
    return specs


def discover_items(probe, extra=()):
    """Manifest entries and core-package mounts beyond the fixed components."""
    items = []
    seen = set(COMPONENTS)

    def add(item):
        if item is None or item.name in seen:
            return
        seen.add(item.name)
        items.append(item)
    for item in extra:
        add(item)
    manifest = (probe.get('manifest') or {}).get('components') or {}
    for name, entry in manifest.items():
        path = entry.get('path')
        if not path:
            continue
        services = tuple(entry.get('services') or RUNTIME)
        add(component(name, path, entry.get('persistence') or 'tmpfs', services,
                      entry.get('mode') or '0755'))
    for mount in probe.get('mounts') or []:
        if mount in TRACKED_PATHS or not allowed_path(mount):
            continue
        add(resolve_component(mount))
    return items


def iter_tracked(probe):
    return list(COMPONENTS.values()) + discover_items(probe)


def trusted_stock(name, entry, probe, fallback):
    """Stock comes from the manifest or the image backup, never from drift.

    ``fallback`` is the named baseline hash. It is used only when neither
    source exists, so a hand-edited file is not saved as the backup.
    """
    if entry and HASH_RE.fullmatch(entry.get('stock_sha256') or ''):
        return entry['stock_sha256']
    backup = probe['hashes'].get(f'stock-{name}', 'MISSING')
    if HASH_RE.fullmatch(backup):
        return backup
    if HASH_RE.fullmatch(fallback or ''):
        return fallback
    raise KitError(f'{name} has no trusted stock backup')


def reference_covers(probe, main_hashes):
    """True when --main-hashes names every present tracked file at its current hash."""
    if not main_hashes:
        return False
    present = False
    for item in COMPONENTS.values():
        current = probe['hashes'].get(item.name, 'MISSING')
        if current == 'MISSING':
            if item.name in main_hashes:
                return False
            continue
        present = True
        if main_hashes.get(item.name) != current:
            return False
    return present


def ordered_services(names):
    chosen = set(names)
    unknown = chosen.difference(SERVICE_ORDER)
    if unknown:
        raise KitError(f'unknown services {", ".join(sorted(unknown))}')
    return [name for name in SERVICE_ORDER if name in chosen]


def service_plan(names):
    ordered = ordered_services(names)
    if not ordered:
        return []
    return [f'STOP {",".join(reversed(ordered))}', f'START {",".join(ordered)}']


def held_files(name):
    prefix = kit_prefix(name)
    return sorted(hold_dir().glob(f'{prefix}-HELD-BY-*'))


def parse_hold(path):
    who = path.name.split('-HELD-BY-', 1)[1]
    why = ''
    for line in path.read_text(encoding='utf-8').splitlines():
        if line.startswith('who='):
            who = line[4:]
        elif line.startswith('why='):
            why = line[4:]
    return who, why


def hold_body(who, why):
    return (f'held by {who} {why} since {local_stamp()}\n'
            f'who={who}\nwhy={why}\nsince={utc_stamp()}\n')


def write_hold(path, who, why):
    path.write_text(hold_body(who, why), encoding='utf-8')


def create_hold(name, who):
    path = hold_dir() / f'{kit_prefix(name)}-HELD-BY-{who}'
    try:
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o644)
    except FileExistsError as exc:
        raise KitError(f'{name} is already held by {who}') from exc
    os.write(fd, b'claiming\n')
    os.close(fd)
    return path


def release_hold(path):
    text = path.read_text(encoding='utf-8')
    if not text.endswith('\n'):
        text += '\n'
    text += f'released {local_stamp()}\n'
    released = Path(str(path).replace('-HELD-BY-', '-RELEASED-BY-'))
    if released.exists():
        released.write_text(released.read_text(encoding='utf-8') + text, encoding='utf-8')
        path.unlink()
    else:
        released.write_text(text, encoding='utf-8')
        path.unlink()


def parse_blocks(text):
    lines = text.splitlines()
    found = {}
    index = 0
    while index < len(lines):
        line = lines[index]
        if line.startswith('--') and line.endswith('--') and not line.startswith('--/'):
            name = line[2:-2]
            index += 1
            body = []
            end = f'--/{name}--'
            while index < len(lines) and lines[index] != end:
                body.append(lines[index])
                index += 1
            if index >= len(lines):
                raise KitError(f'probe is missing the end of {name}')
            found[name] = '\n'.join(body).strip('\n')
            index += 1
        else:
            index += 1
    return found


def load_object(text):
    text = (text or '').strip()
    if not text:
        return {}
    try:
        value = json.loads(text)
    except json.JSONDecodeError as exc:
        raise KitError(f'kit returned invalid JSON: {exc}') from exc
    if not isinstance(value, dict):
        raise KitError('kit returned a JSON value that is not an object')
    return value


def parse_probe(text):
    blocks = parse_blocks(text)
    hashes = {}
    for line in (blocks.get('HASHES') or '').splitlines():
        if not line:
            continue
        name, digest = line.split()
        hashes[name] = digest
    mounts = []
    for line in (blocks.get('MOUNTS') or '').splitlines():
        parts = line.split()
        if len(parts) >= 2:
            mounts.append(parts[1])
    procs = []
    for line in (blocks.get('PROCS') or '').splitlines():
        pid, exe, cmd = (line.split('\t', 2) + ['', ''])[:3]
        procs.append({'pid': pid, 'exe': exe, 'cmd': cmd})
    boot = load_object(blocks.get('BOOTJSON'))
    state = load_object(blocks.get('STATEJSON'))
    release = load_object(blocks.get('RELEASEJSON'))
    image_sha = boot.get('image_sha256') or release.get('image_sha256') or ''
    return {
        'boot_id': (blocks.get('BOOT') or '').strip(),
        'image': {
            'boot_id': (blocks.get('BOOT') or '').strip(),
            'image_sha256': image_sha,
            'version': release.get('version') or '',
            'fes_revision': release.get('fes_revision') or '',
            'good': state.get('good') or '',
            'trial': boot.get('trial'),
        },
        'hashes': hashes,
        'mounts': mounts,
        'manifest': load_object(blocks.get('MANIFEST')) or None,
        'baseline': load_object(blocks.get('BASELINE')) or None,
        'procs': procs,
    }


def empty_manifest(name, image, holder):
    return {
        'format': 1,
        'kit': name,
        'boot_id': image['boot_id'],
        'image_sha256': image['image_sha256'],
        'image_version': image['version'],
        'fes_revision': image['fes_revision'],
        'holder': holder,
        'components': {},
    }


def manifest_bytes(document):
    return (json.dumps(document, indent=2, sort_keys=True) + '\n').encode()


def unrecorded_mounts(probe, manifest):
    recorded = {entry.get('path') for entry in (manifest or {}).get('components', {}).values()}
    found = []
    for mount in probe['mounts']:
        tracked = mount in TRACKED_PATHS or mount.startswith(CORE_PREFIX)
        if tracked and mount not in recorded:
            found.append(mount)
    return found


def load_main_hashes(path):
    hashes = {}
    for line in Path(path).read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        parts = line.split()
        if len(parts) != 2:
            raise KitError(f'main hash line must be "sha256 component": {line}', code=2)
        if HASH_RE.fullmatch(parts[0]) and NAME_RE.fullmatch(parts[1]):
            hashes[parts[1]] = parts[0]
        elif HASH_RE.fullmatch(parts[1]) and NAME_RE.fullmatch(parts[0]):
            hashes[parts[0]] = parts[1]
        else:
            raise KitError(f'main hash line is not a component and sha256: {line}', code=2)
    return hashes


class LocalTransport:
    def __init__(self, root):
        self.root = Path(root)

    def run(self, args):
        env = os.environ.copy()
        env['KIT_FAKE'] = '1'
        env['KIT_ROOT'] = str(self.root)
        if os.environ.get('FES_KIT_FAIL_START') == '1':
            env['KIT_FAIL_START'] = '1'
        fail_umount = os.environ.get('FES_KIT_FAIL_UMOUNT')
        if fail_umount:
            env['KIT_FAIL_UMOUNT'] = fail_umount
        fail_stage = os.environ.get('FES_KIT_FAIL_STAGE')
        if fail_stage:
            env['KIT_FAIL_STAGE'] = fail_stage
        proc = subprocess.run(['sh', str(REMOTE), *args], text=True, capture_output=True, env=env, check=False)
        if proc.returncode != 0:
            detail = (proc.stderr or proc.stdout or f'exit {proc.returncode}').strip()
            raise KitError(detail)
        return proc.stdout

    def put(self, remote, data):
        path = self.root / remote.lstrip('/')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)


class SSHTransport:
    def __init__(self, name):
        self.host = os.environ.get(f'FES_KIT_{name}_HOST', HOSTS[name])
        self.user = os.environ.get('FES_KIT_SSH_USER', 'root')
        self.password = os.environ.get('FES_KIT_SSH_PASSWORD', '1')

    def _known_hosts(self):
        handle = tempfile.NamedTemporaryFile(prefix='fes-kit-kh-', delete=False)
        handle.close()
        return handle.name

    def _options(self, known):
        return [
            '-o', 'ConnectTimeout=8', '-o', 'ServerAliveInterval=5', '-o', 'ServerAliveCountMax=2',
            '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={known}',
            '-o', 'LogLevel=ERROR', '-o', 'PubkeyAuthentication=no',
            '-o', 'PreferredAuthentications=password', '-o', 'IdentitiesOnly=yes',
            '-o', 'NumberOfPasswordPrompts=1',
        ]

    def _target(self):
        return f'{self.user}@{self.host}'

    def _run(self, argv, timeout, input_bytes=None):
        if shutil_which('sshpass') is None:
            raise KitError('sshpass is required to reach a kit')
        proc = subprocess.run(
            ['sshpass', '-p', self.password, *argv],
            input=input_bytes, capture_output=True, timeout=timeout, check=False)
        if proc.returncode != 0:
            detail = proc.stderr.decode('utf-8', 'replace').strip() or f'exit {proc.returncode}'
            raise KitError(detail)
        return proc.stdout

    def run(self, args):
        remote = ' '.join(shlex.quote(part) for part in ['sh', '-s', '--', *args])
        known = self._known_hosts()
        try:
            output = self._run(
                ['ssh', *self._options(known), self._target(), remote],
                180, REMOTE.read_bytes())
        finally:
            if os.path.exists(known):
                os.unlink(known)
        return output.decode('utf-8', 'replace')

    def put(self, remote, data):
        parent = remote.rsplit('/', 1)[0]
        known = self._known_hosts()
        try:
            self._run(['ssh', *self._options(known), self._target(), f'mkdir -p {shlex.quote(parent)}'], 60)
        finally:
            if os.path.exists(known):
                os.unlink(known)
        handle = tempfile.NamedTemporaryFile(prefix='fes-kit-put-', delete=False)
        known = self._known_hosts()
        try:
            handle.write(data)
            handle.close()
            self._run(['scp', '-O', '-q', *self._options(known), handle.name, f'{self._target()}:{remote}'], 180)
        finally:
            os.unlink(handle.name)
            if os.path.exists(known):
                os.unlink(known)


def shutil_which(name):
    return __import__('shutil').which(name)


class Session:
    def __init__(self, name, transport, host):
        self.name = name
        self.transport = transport
        self.host = host

    def probe(self, extra=None):
        extra = list(extra or [])
        base = list(COMPONENTS.values())
        first = parse_probe(self.transport.run(['probe', *hash_specs(base + extra, '')]))
        image = image_id(first['image'].get('image_sha256'))
        items = base + discover_items(first, extra)
        specs = hash_specs(items, image)
        if specs == hash_specs(base + extra, ''):
            return first
        return parse_probe(self.transport.run(['probe', *specs]))

    def put_text(self, remote, data):
        payload = data if isinstance(data, bytes) else data.encode()
        self.transport.put(remote, payload)
        return remote

    def apply(self, lines):
        remote = f'/tmp/fes-incoming/{os.getpid()}-plan'
        self.put_text(remote, '\n'.join(lines) + '\n')
        return self.transport.run(['apply', remote]).strip()

    def show(self, path):
        return self.transport.run(['show', path])

    def require_hold(self):
        found = held_files(self.name)
        if not found:
            raise KitError(f'{self.name} is not claimed; run fes kit claim first')
        if len(found) > 1:
            raise KitError(f'{self.name} has more than one hold marker')
        return found[0]

    def claim(self, who, why):
        who = check_slug(who, '--by')
        why = check_reason(why)
        existing = held_files(self.name)
        created = None
        if len(existing) > 1:
            raise KitError(f'{self.name} has more than one hold marker')
        if len(existing) == 1:
            holder, _old = parse_hold(existing[0])
            if holder != who:
                raise KitError(f'{self.name} is held by {holder}')
            path = existing[0]
        else:
            path = create_hold(self.name, who)
            created = path
        try:
            probe = self.probe()
            manifest = probe['manifest'] or empty_manifest(self.name, probe['image'], None)
            holder = manifest.get('holder') or {}
            if manifest.get('components') and holder.get('who') not in (None, '', who):
                raise KitError(f'{self.name} manifest is held by {holder.get("who")}')
            dirty = unrecorded_mounts(probe, manifest)
            if dirty:
                raise KitError('unrecorded mounts: ' + ', '.join(dirty)
                               + '; fes kit restore --force drops abandoned binds')
            manifest = empty_manifest(self.name, probe['image'], {'who': who, 'why': why, 'when': utc_stamp()})
            manifest['components'] = (probe['manifest'] or {}).get('components') or {}
            if probe['boot_id'] and manifest['components']:
                recorded = (probe['manifest'] or {}).get('boot_id')
                if recorded and recorded != probe['boot_id']:
                    raise KitError('kit boot id changed while components are swapped')
            remote = f'/tmp/fes-incoming/{os.getpid()}-manifest.json'
            self.put_text(remote, manifest_bytes(manifest))
            lines = [f'BOOT {probe["boot_id"]}', f'MANIFEST {remote}', 'COMMIT']
            result = self.apply(lines)
            if result not in ('OK', 'BOOT_CHANGED'):
                raise KitError(result or 'claim failed')
            write_hold(path, who, why)
            created = None
        finally:
            if created is not None and created.exists():
                created.unlink()
        return f'claimed {self.name} by {who}'

    def _write_baseline(self, probe):
        hashes = {name: digest for name, digest in probe['hashes'].items()
                  if not name.startswith('stock-') and digest != 'MISSING'}
        document = {'format': 1, 'when': utc_stamp(), 'hashes': hashes}
        remote = f'/tmp/fes-incoming/{os.getpid()}-baseline.json'
        self.put_text(remote, manifest_bytes(document))
        lines = [f'BOOT {probe["boot_id"]}', f'INSTALL {remote} {BASELINE_PATH}', 'COMMIT']
        result = self.apply(lines)
        if result not in ('OK', 'BOOT_CHANGED'):
            raise KitError(result or 'baseline write failed')

    def swap(self, spec, source, why, persistent, temporary):
        if persistent and temporary:
            raise KitError('pass only one of --persistent and --temporary', code=2)
        path = self.require_hold()
        with path.open('r', encoding='utf-8') as handle:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
            return self._swap(path, spec, source, why, persistent, temporary)

    def _swap(self, hold, spec, source, why, persistent, temporary):
        who, claim_why = parse_hold(hold)
        item = resolve_component(spec)
        persistence = item.persistence
        if persistent:
            persistence = 'persistent'
        if temporary:
            persistence = 'tmpfs'
        if persistence == 'persistent' and not why:
            raise KitError('persistent swaps require --why', code=2)
        why = check_reason(why) if why else (claim_why or 'temporary swap')
        source_path = Path(source)
        if not source_path.is_file():
            raise KitError(f'{source} is not a file', code=2)
        payload = source_path.read_bytes()
        new_hash = sha256_bytes(payload)
        probe = self.probe(extra=[item])
        manifest = probe['manifest'] or empty_manifest(self.name, probe['image'], None)
        self._check_boot(probe, manifest)
        current = probe['hashes'].get(item.name, 'MISSING')
        if current == 'MISSING':
            raise KitError(f'{item.path} is not on the kit')
        if current == new_hash:
            raise KitError(f'{item.name} is already {new_hash}')
        entry = (manifest.get('components') or {}).get(item.name)
        if entry:
            stock = entry['stock_sha256']
        else:
            stock = current
        if not HASH_RE.fullmatch(stock):
            raise KitError(f'{item.name} has no stock hash')
        stage = f'/tmp/fes-swap/{item.name}'
        backup = stock_path(item.name, probe['image'].get('image_sha256'))
        incoming = f'/tmp/fes-incoming/{os.getpid()}-{item.name}'
        self.put_text(incoming, payload)
        components = dict(manifest.get('components') or {})
        components[item.name] = {
            'name': item.name,
            'path': item.path,
            'stock_sha256': stock,
            'current_sha256': new_hash,
            'who': who,
            'when': utc_stamp(),
            'why': why,
            'persistence': persistence,
            'backup': backup,
            'stage': stage,
            'mode': item.mode,
            'services': list(item.services),
        }
        updated = empty_manifest(self.name, probe['image'], manifest.get('holder'))
        updated['components'] = components
        manifest_remote = f'/tmp/fes-incoming/{os.getpid()}-manifest.json'
        self.put_text(manifest_remote, manifest_bytes(updated))
        lines = [f'BOOT {probe["boot_id"]}', *service_plan(item.services),
                 place_line(item, incoming, persistence, backup, stage, stock, new_hash),
                 f'MANIFEST {manifest_remote}', 'COMMIT']
        result = self.apply(lines)
        if result != 'OK':
            raise KitError(result or 'swap failed')
        checked = self.probe(extra=[item])
        if checked['hashes'].get(item.name) != new_hash:
            raise KitError(f'{item.name} hash did not change')
        return f'swapped {item.name} on {self.name} {persistence} {new_hash}'

    def restore(self, baseline=None, force=False):
        found = held_files(self.name)
        if len(found) > 1:
            raise KitError(f'{self.name} has more than one hold marker')
        if not found and not force:
            raise KitError(f'{self.name} is not claimed; run fes kit claim first')
        if found:
            with found[0].open('r', encoding='utf-8') as handle:
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
                return self._restore(found[0], baseline)
        return self._restore(None, baseline)

    def _item_for(self, name, entry, info):
        if name in COMPONENTS:
            return COMPONENTS[name]
        path = (entry or {}).get('path') or (info or {}).get('component_path')
        if not path:
            raise KitError(f'{name} is not a tracked component')
        services = tuple((entry or {}).get('services') or (info or {}).get('services') or RUNTIME)
        mode = (entry or {}).get('mode') or (info or {}).get('mode') or '0755'
        persistence = (entry or {}).get('persistence') or (info or {}).get('persistence') or 'tmpfs'
        return component(name, path, persistence, services, mode)

    def _restore(self, hold, baseline):
        probe = self.probe()
        manifest = probe['manifest'] or empty_manifest(self.name, probe['image'], None)
        components = dict(manifest.get('components') or {})
        desired = None
        if baseline:
            check_slug(baseline, '--baseline')
            raw = self.show(f'/media/fat/fes/baselines/{baseline}.json')
            desired = load_object(raw)
            if desired.get('name') != baseline:
                raise KitError(f'baseline {baseline} is not on the kit')
        lines = [f'BOOT {(manifest.get("boot_id") or probe["boot_id"])}']
        services = set()
        checks = []
        extra = []
        image = probe['image'].get('image_sha256')
        if desired:
            who = 'force'
            claim_why = f'baseline {baseline}'
            if hold is not None:
                who, _claim_why = parse_hold(hold)
            files = desired.get('files') or {}
            names = list(dict.fromkeys([*COMPONENTS.keys(), *files.keys(), *components.keys()]))
            for name in names:
                info = files.get(name) or {}
                entry = components.get(name)
                omitted = name not in files
                if info.get('missing') or (omitted and name in COMPONENTS):
                    item = self._item_for(name, entry, info)
                    path = item.path
                    present = (probe['hashes'].get(name, 'MISSING') != 'MISSING'
                               or path in probe['mounts'] or entry)
                    if present:
                        services.update(entry.get('services') if entry else item.services)
                        lines.append(f'REMOVE {item.name} {path}')
                        checks.append({'name': name, 'path': path, 'expect': 'MISSING', 'unmount': True})
                        extra.append(item)
                    components.pop(name, None)
                    continue
                if omitted:
                    if entry:
                        services.update(entry.get('services') or ())
                        lines.append(clear_line(entry))
                        checks.append({'name': name, 'path': entry['path'],
                                       'expect': entry.get('stock_sha256') or '', 'unmount': True})
                        extra.append(self._item_for(name, entry, {}))
                        components.pop(name, None)
                    continue
                item = self._item_for(name, entry, info)
                want = info.get('sha256') or ''
                if not HASH_RE.fullmatch(want):
                    raise KitError(f'{name} baseline hash is missing')
                current = probe['hashes'].get(name, 'MISSING')
                stock = trusted_stock(name, entry, probe, want)
                mounted = item.path in probe['mounts']
                backup_known = bool(entry) or probe['hashes'].get(f'stock-{name}', 'MISSING') != 'MISSING'
                if current == want and not (mounted and want == stock):
                    continue
                services.update(entry.get('services') if entry else item.services)
                backup = stock_path(item.name, image)
                if want == stock and backup_known:
                    lines.append(clear_line(entry or self._synthetic(item, stock, image)))
                    components.pop(name, None)
                    checks.append({'name': name, 'path': item.path, 'expect': want, 'unmount': True})
                else:
                    lines.append(place_line(item, info['path'], item.persistence, backup,
                                            f'/tmp/fes-swap/{name}', stock, want))
                    components[name] = {
                        'name': item.name, 'path': item.path, 'stock_sha256': stock,
                        'current_sha256': want, 'who': who, 'when': utc_stamp(), 'why': claim_why,
                        'persistence': item.persistence, 'backup': backup,
                        'stage': f'/tmp/fes-swap/{name}', 'mode': item.mode,
                        'services': list(item.services),
                    }
                    checks.append({'name': name, 'path': item.path, 'expect': want,
                                   'unmount': item.persistence != 'tmpfs'})
                extra.append(item)
        else:
            handled = set()
            for name, entry in list(components.items()):
                services.update(entry.get('services') or ())
                lines.append(clear_line(entry))
                path = entry['path']
                checks.append({'name': name, 'path': path, 'expect': entry.get('stock_sha256') or '',
                               'unmount': True})
                if name not in COMPONENTS:
                    extra.append(self._item_for(name, entry, {}))
                handled.add(path)
            for item in iter_tracked(probe):
                if item.path in handled or item.path not in probe['mounts']:
                    continue
                stock = probe['hashes'].get(f'stock-{item.name}', 'MISSING')
                services.update(item.services)
                if stock == 'MISSING':
                    lines.append(f'UNMOUNT {item.name} {item.path}')
                    checks.append({'name': item.name, 'path': item.path, 'expect': '', 'unmount': True})
                else:
                    lines.append(clear_line(self._synthetic(item, stock, image)))
                    checks.append({'name': item.name, 'path': item.path, 'expect': stock, 'unmount': True})
                if item.name not in COMPONENTS:
                    extra.append(item)
            components = {}
        updated = empty_manifest(self.name, probe['image'], None)
        updated['boot_id'] = manifest.get('boot_id') or probe['boot_id']
        updated['components'] = components
        remote = f'/tmp/fes-incoming/{os.getpid()}-manifest.json'
        self.put_text(remote, manifest_bytes(updated))
        lines.extend(service_plan(services))
        lines.extend([f'MANIFEST {remote}', 'COMMIT'])
        result = self.apply(lines)
        checked = self.probe(extra=extra)
        for item in checks:
            name = item['name']
            path = item['path']
            if item.get('unmount') and path in checked['mounts']:
                raise KitError(f'{name} is still mounted')
            expect = item.get('expect') or ''
            if expect == 'MISSING':
                if checked['hashes'].get(name, 'MISSING') != 'MISSING':
                    raise KitError(f'{name} is still on the kit')
            elif expect and checked['hashes'].get(name) != expect:
                raise KitError(f'{name} restored to {checked["hashes"].get(name)}, want {expect}')
        if result not in ('OK', 'BOOT_CHANGED'):
            raise KitError(result or 'restore failed')
        if result == 'BOOT_CHANGED':
            if hold is not None:
                release_hold(hold)
            raise KitError('boot id changed; swapped files were restored', code=4)
        if hold is not None:
            release_hold(hold)
        return f'restored {self.name}'

    def _synthetic(self, item, stock, image_sha):
        return {
            'name': item.name, 'path': item.path, 'persistence': item.persistence,
            'backup': stock_path(item.name, image_sha), 'stage': f'/tmp/fes-swap/{item.name}',
            'stock_sha256': stock,
        }

    def _check_boot(self, probe, manifest):
        recorded = (manifest or {}).get('boot_id')
        if recorded and probe['boot_id'] and recorded != probe['boot_id']:
            raise KitError('kit boot id changed since the claim; restore before swapping again')

    def save_baseline(self, name):
        check_slug(name, 'baseline name')
        path = self.require_hold()
        with path.open('r', encoding='utf-8') as handle:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
            probe = self.probe()
            files = {}
            lines = [f'BOOT {probe["boot_id"]}']
            seen = set()
            for item in list(COMPONENTS.values()) + discover_items(probe):
                if item.name in seen:
                    continue
                seen.add(item.name)
                digest = probe['hashes'].get(item.name, 'MISSING')
                if digest == 'MISSING':
                    record = {'missing': True}
                    if item.name not in COMPONENTS:
                        record['component_path'] = item.path
                    files[item.name] = record
                    continue
                dest = f'/media/fat/fes/baselines/{name}/{item.name}'
                lines.append(f'INSTALL {item.path} {dest}')
                record = {'sha256': digest, 'path': dest}
                if item.name not in COMPONENTS:
                    record.update({
                        'component_path': item.path,
                        'persistence': item.persistence,
                        'mode': item.mode,
                        'services': list(item.services),
                    })
                files[item.name] = record
            document = {'format': 1, 'name': name, 'when': utc_stamp(), 'files': files}
            remote = f'/tmp/fes-incoming/{os.getpid()}-baseline-meta.json'
            self.put_text(remote, manifest_bytes(document))
            lines.append(f'INSTALL {remote} /media/fat/fes/baselines/{name}.json')
            lines.append('COMMIT')
            result = self.apply(lines)
            if result not in ('OK', 'BOOT_CHANGED'):
                raise KitError(result or 'baseline save failed')
            return f'saved baseline {name} on {self.name}'

    def status(self, main_hashes=None):
        probe = self.probe()
        found = held_files(self.name)
        hold = None
        if len(found) == 1:
            who, why = parse_hold(found[0])
            hold = {'who': who, 'why': why, 'file': found[0].name}
        elif len(found) > 1:
            hold = {'who': 'conflict', 'why': 'multiple hold markers', 'file': ''}
        report = {
            'kit': self.name,
            'host': self.host,
            'hold': hold,
            'image': probe['image'],
            'manifest_path': MANIFEST_PATH,
            'manifest': probe['manifest'],
            'components': component_rows(probe, main_hashes or {}),
            'mounts': probe['mounts'],
        }
        return report


def place_line(item, source, persistence, backup, stage, stock, desired):
    return (f'PLACE {item.name} {item.path} {source} {persistence} {item.mode} '
            f'{backup} {stage} {stock} {desired}')


def clear_line(entry):
    component_name = entry.get('name')
    path = entry['path']
    if not component_name:
        item = TRACKED_PATHS.get(path)
        if item:
            component_name = item.name
        elif allowed_path(path):
            component_name = resolve_component(path).name
    if not component_name:
        raise KitError(f'cannot restore untracked path {path}')
    return (f'CLEAR {component_name} {entry["path"]} {entry["persistence"]} '
            f'{entry["backup"]} {entry["stage"]} {entry["stock_sha256"]}')


def component_rows(probe, main_hashes):
    manifest = (probe['manifest'] or {}).get('components') or {}
    baseline = (probe['baseline'] or {}).get('hashes') or {}
    rows = []
    for item in iter_tracked(probe):
        current = probe['hashes'].get(item.name, 'MISSING')
        entry = manifest.get(item.name)
        if entry:
            stock = entry.get('stock_sha256', '')
            persistence = entry.get('persistence', '')
            who = entry.get('who', '')
            why = entry.get('why', '')
        elif item.name in baseline:
            stock = baseline[item.name]
            persistence = 'stock'
            who = ''
            why = ''
        else:
            stock = 'unrecorded'
            persistence = 'stock'
            who = ''
            why = ''
        main = main_hashes.get(item.name, '')
        rows.append({
            'component': item.name,
            'path': item.path,
            'current': current,
            'stock': stock,
            'main': main,
            'persistence': persistence,
            'who': who,
            'why': why,
            'mounted': item.path in probe['mounts'],
        })
    return rows


def format_status(report):
    image = report['image']
    hold = report['hold']
    if hold:
        hold_text = f'{hold["who"]} ({hold["file"]}) {hold["why"]}'.strip()
    else:
        hold_text = 'free'
    lines = [
        f'kit {report["kit"]} {report["host"]}',
        f'hold: {hold_text}',
        (f'image: version {image.get("version") or "-"} '
         f'sha256 {image.get("image_sha256") or "-"} '
         f'boot_id {image.get("boot_id") or "-"}'),
        f'manifest: {report["manifest_path"]}',
        '',
    ]
    for row in report['components']:
        main = row['main'] or 'unknown'
        who = row['who'] or '-'
        lines.append(
            f'{row["component"]} current={row["current"]} stock={row["stock"]} '
            f'main={main} persistence={row["persistence"]} who={who}')
    return '\n'.join(lines) + '\n'


def drift_findings(probe, main_hashes, previous):
    manifest = (probe['manifest'] or {}).get('components') or {}
    baseline = (probe['baseline'] or {}).get('hashes') or {}
    previous = previous or {}
    rows = []
    for item in iter_tracked(probe):
        current = probe['hashes'].get(item.name, 'MISSING')
        entry = manifest.get(item.name)
        if entry:
            if current != entry.get('current_sha256'):
                rows.append(('DRIFT', item.name, 'manifest-mismatch', current, entry.get('current_sha256', '')))
            if entry.get('persistence') == 'tmpfs' and item.path not in probe['mounts']:
                rows.append(('DRIFT', item.name, 'missing-mount', current, entry.get('current_sha256', '')))
        else:
            if item.path in probe['mounts']:
                rows.append(('DRIFT', item.name, 'unrecorded-mount', current, baseline.get(item.name, '')))
            elif item.name in baseline and current != baseline[item.name]:
                rows.append(('DRIFT', item.name, 'unrecorded', current, baseline[item.name]))
            elif item.name not in baseline and previous.get(item.name) not in (None, '', current):
                rows.append(('DRIFT', item.name, 'unrecorded', current, previous[item.name]))
        main = (main_hashes or {}).get(item.name)
        if main and current != main:
            rows.append(('STALE', item.name, 'main', current, main))
    return rows


def format_finding(name, kind, component, detail, current, expected):
    return (f'{utc_stamp()} kit {name} {kind} {component} {detail} '
            f'current={current} expected={expected}')


def watch_kit(session, main_hashes, fail_stale):
    probe = session.probe()
    previous_path = drift_dir() / f'{session.name}.json'
    previous = {}
    if previous_path.exists():
        previous = load_object(previous_path.read_text(encoding='utf-8')).get('hashes') or {}
    findings = drift_findings(probe, main_hashes, previous)
    drifted = [row for row in findings if row[0] == 'DRIFT']
    manifest = probe['manifest'] or {}
    dirty = unrecorded_mounts(probe, manifest)
    # A matching operator reference is the only automatic stock baseline.
    # The first observation of a kit is unverified, so a pre-existing edit
    # is not recorded as stock.
    if reference_covers(probe, main_hashes) and not dirty and not drifted and not probe['baseline']:
        session._write_baseline(probe)
        findings.append(('BASELINE', '-', 'recorded', '', ''))
    elif not probe['baseline']:
        findings.append(('UNVERIFIED', '-', 'no-stock-baseline', '', ''))
    snapshot = {item.name: probe['hashes'].get(item.name, 'MISSING') for item in iter_tracked(probe)}
    previous_path.write_text(json.dumps({'format': 1, 'when': utc_stamp(), 'hashes': snapshot}, indent=2) + '\n',
                             encoding='utf-8')
    lines = [format_finding(session.name, *row) for row in findings]
    if not findings:
        lines.append(f'{utc_stamp()} kit {session.name} OK')
    stale = [row for row in findings if row[0] == 'STALE']
    code = 1 if drifted or (fail_stale and stale) else 0
    return code, lines


def open_session(name):
    name = kit_name(name)
    root = os.environ.get(f'FES_KIT_{name}_ROOT')
    if os.environ.get('FES_KIT_TRANSPORT') == 'local' or root:
        if not root:
            raise KitError(f'FES_KIT_{name}_ROOT is required for the local transport', code=2)
        return Session(name, LocalTransport(root), 'local')
    return Session(name, SSHTransport(name), os.environ.get(f'FES_KIT_{name}_HOST', HOSTS[name]))


def build_parser():
    parser = argparse.ArgumentParser(prog='fes', description='FES kit runtime manifest')
    sub = parser.add_subparsers(dest='command', required=True)
    kit = sub.add_parser('kit')
    commands = kit.add_subparsers(dest='kit_command', required=True)

    claim = commands.add_parser('claim')
    claim.add_argument('kit')
    claim.add_argument('--by', required=True)
    claim.add_argument('--for', dest='reason', required=True)

    swap = commands.add_parser('swap')
    swap.add_argument('kit')
    swap.add_argument('component')
    swap.add_argument('file')
    swap.add_argument('--why')
    mode = swap.add_mutually_exclusive_group()
    mode.add_argument('--persistent', action='store_true')
    mode.add_argument('--temporary', action='store_true')

    restore = commands.add_parser('restore')
    restore.add_argument('kit')
    restore.add_argument('--baseline')
    restore.add_argument('--force', action='store_true')

    baseline = commands.add_parser('baseline')
    baseline.add_argument('kit')
    baseline.add_argument('name')

    status = commands.add_parser('status')
    status.add_argument('kit')
    status.add_argument('--json', action='store_true')
    status.add_argument('--main-hashes')

    watch = commands.add_parser('watch')
    watch.add_argument('--once', action='store_true', help='run one cycle (the only mode)')
    watch.add_argument('--kits', default='A,B')
    watch.add_argument('--main-hashes')
    watch.add_argument('--fail-stale', action='store_true')
    return parser


def main(argv=None):
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        if args.command != 'kit':
            parser.error('unknown command')
        if args.kit_command == 'claim':
            print(open_session(args.kit).claim(args.by, args.reason))
        elif args.kit_command == 'swap':
            print(open_session(args.kit).swap(args.component, args.file, args.why, args.persistent, args.temporary))
        elif args.kit_command == 'restore':
            print(open_session(args.kit).restore(args.baseline, args.force))
        elif args.kit_command == 'baseline':
            print(open_session(args.kit).save_baseline(args.name))
        elif args.kit_command == 'status':
            main_hashes = load_main_hashes(args.main_hashes) if args.main_hashes else {}
            report = open_session(args.kit).status(main_hashes)
            if args.json:
                print(json.dumps(report, indent=2, sort_keys=True))
            else:
                print(format_status(report), end='')
        elif args.kit_command == 'watch':
            main_hashes = load_main_hashes(args.main_hashes) if args.main_hashes else {}
            code = 0
            log = drift_dir() / 'watch.log'
            for name in [part.strip() for part in args.kits.split(',') if part.strip()]:
                try:
                    kit_code, lines = watch_kit(open_session(name), main_hashes, args.fail_stale)
                except KitError as exc:
                    lines = [f'{utc_stamp()} kit {kit_name(name)} UNREACHABLE {exc}']
                    kit_code = 2
                text = '\n'.join(lines)
                print(text)
                with log.open('a', encoding='utf-8') as handle:
                    handle.write(text + '\n')
                if kit_code == 1 or (kit_code != 0 and code == 0):
                    code = kit_code
            return code
        else:
            parser.error('unknown kit command')
    except KitError as exc:
        print(f'fes: {exc}', file=sys.stderr)
        return exc.code
    return 0


if __name__ == '__main__':
    sys.exit(main())
