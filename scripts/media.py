#!/usr/bin/env python3
"""Publish and reverify native boot media without accessing a physical device."""
import argparse
from contextlib import contextmanager
from dataclasses import asdict, dataclass, replace
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import shlex
import signal
import socket
import stat
import subprocess
import tempfile
import time
import tomllib
import uuid

import build as cold_build
from environment import build_environment
from media_container import ensure_media_container
from media_inputs import MediaLock, digest, resolve_payloads, verify_file
from media_inside import ImageInputs, PART1_OFFSET, Provenance, load_manifest, manifest_data, write_manifest

PROFILE = 'native-integration-dev'
MAX_CONFIG_BYTES = 65536
DEFAULT_HOST_CONFIG = Path.home() / '.config' / 'fogcast' / 'config.toml'
BEARER_TOKEN_PATTERN = re.compile(r'[A-Za-z0-9._~+/-]+={0,}')
TARGET_NAME_PATTERN = re.compile(r'[a-z0-9]+(?:-[a-z0-9]+)*')
TARGET_ID_PATTERN = re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}')
recipe_fingerprint = cold_build.recipe_fingerprint
CHECKS = dict.fromkeys(('structural_media', 'rootfs_structural', 'rootfs_qemu', 'reproducibility'), 'pass')


TERMINATION_SIGNALS = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)


class ChildShutdownError(Exception):
    pass


class TerminationRequested(Exception):
    def __init__(self, signum):
        self.signum = signum
        super().__init__('terminated by ' + signal.Signals(signum).name)


@contextmanager
def termination_handling():
    previous = {signum: signal.getsignal(signum) for signum in TERMINATION_SIGNALS}
    def terminate(signum, frame):
        # A second ordinary termination must not interrupt restoration.
        for pending in TERMINATION_SIGNALS:
            signal.signal(pending, signal.SIG_IGN)
        raise TerminationRequested(signum)
    try:
        for signum in TERMINATION_SIGNALS:
            signal.signal(signum, terminate)
        yield
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)


@contextmanager
def defer_termination():
    previous = signal.pthread_sigmask(signal.SIG_BLOCK, TERMINATION_SIGNALS)
    try:
        yield
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


@dataclass(frozen=True)
class Result:
    generation: Path

    @property
    def image(self):
        return self.generation / 'fes.img'

    @property
    def manifest_text(self):
        return (self.generation / 'fes-media.toml').read_text()

    @property
    def log(self):
        return 'Media structural, rootfs structural, QEMU and reproducibility checks passed; hardware not run.'


def require_profile(profile):
    if profile != PROFILE:
        raise ValueError('boot media requires native-integration-dev')


def fsync_dir(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def write_private(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


@contextmanager
def lease(root, profile):
    require_profile(profile)
    directory = Path(root) / 'out/locks'
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / ('media-' + profile)
    descriptor = os.open(path.with_suffix('.lock'), os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('another media operation holds this profile lease') from None
        now = time.time()
        record = {'owner': os.getuid(), 'pid': os.getpid(), 'host': socket.gethostname(),
                  'start': now, 'expiry': now + 86400}
        with tempfile.TemporaryDirectory(prefix='.lease-', dir=directory) as temporary:
            candidate = Path(temporary) / 'owner.json'
            write_private(candidate, (json.dumps(record, sort_keys=True) + '\n').encode())
            os.replace(candidate, path.with_suffix('.json'))
            fsync_dir(directory)
        yield
    finally:
        os.close(descriptor)


@contextmanager
def operation(root, profile):
    # The selected child checkout and cold evidence are shared with make verify.
    with lease(root, profile):
        descriptor = os.open(Path(root) / 'out/build.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            try:
                fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise ValueError('another parent build is running in this workspace') from None
            previous = os.umask(0o077)
            try:
                yield
            finally:
                os.umask(previous)
        finally:
            os.close(descriptor)


def _read_private_config(config, description):
    config = Path(config)
    try:
        if not config.is_absolute():
            raise ValueError(f'{description} must be absolute')
        before = config.lstat()
        if not stat.S_ISREG(before.st_mode):
            raise ValueError(f'{description} must be a regular non-symlink file')
        descriptor = os.open(config, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as stream:
            opened = os.fstat(stream.fileno())
            if ((opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino)
                    or not stat.S_ISREG(opened.st_mode) or opened.st_mode & 0o077
                    or opened.st_size > MAX_CONFIG_BYTES):
                raise ValueError(f'{description} must be owner-only and at most {MAX_CONFIG_BYTES} bytes')
            data = stream.read(65537)
            after = os.fstat(stream.fileno())
            if len(data) > MAX_CONFIG_BYTES or (opened.st_size, opened.st_mtime_ns, opened.st_ctime_ns) != (
                    after.st_size, after.st_mtime_ns, after.st_ctime_ns):
                raise ValueError(f'{description} changed during snapshot')
        return data
    except OSError:
        raise ValueError(f'{description} could not be securely opened') from None


def snapshot_config(config, scratch):
    if config is None:
        return None, None
    data = _read_private_config(config, 'agent config')
    destination = scratch / 'agent.toml'
    write_private(destination, data)
    return destination, hashlib.sha256(data).hexdigest()


def _truthy_environment(name):
    return os.environ.get(name, '').strip().lower() in {'1', 'true', 'yes', 'on'}


def _target_id(raw):
    target_id = raw.get('target_id', '')
    if not isinstance(target_id, str) or (target_id and not TARGET_ID_PATTERN.fullmatch(target_id)):
        raise ValueError('target identity must be a canonical lowercase UUID')
    return target_id


def _host_provisioning(raw):
    targets = raw.get('targets')
    if targets:
        selected = raw.get('selected_target')
        if not isinstance(selected, str) or not selected.strip() or not isinstance(targets, list):
            raise ValueError('selected target is required for automatic media provisioning')
        selected = selected.strip()
        normalized = []
        seen = set()
        for target in targets:
            if not isinstance(target, dict):
                raise ValueError('target entries must be tables for automatic media provisioning')
            name = target.get('name')
            if not isinstance(name, str):
                raise ValueError('target names must be strings for automatic media provisioning')
            name = name.strip()
            if not TARGET_NAME_PATTERN.fullmatch(name):
                raise ValueError('target names must be lowercase ASCII slugs for automatic media provisioning')
            if name in seen:
                raise ValueError('target names must be unique for automatic media provisioning')
            seen.add(name)
            enabled = target.get('enabled', False)
            if not isinstance(enabled, bool):
                raise ValueError('target enabled values must be booleans for automatic media provisioning')
            address = target.get('address', '')
            agent = target.get('agent', '')
            if not isinstance(address, str) or not isinstance(agent, str):
                raise ValueError('target address and agent must be strings for automatic media provisioning')
            has_address = bool(address.strip())
            has_agent = bool(agent.strip())
            if has_address != has_agent or (enabled and not has_address):
                raise ValueError('enabled targets must have both address and agent for automatic media provisioning')
            _target_id(target)
            normalized.append((name, target))
        matches = [target for name, target in normalized if name == selected]
        if len(matches) != 1 or matches[0].get('enabled') is not True:
            raise ValueError('selected target must be enabled for automatic media provisioning')
        selected_config = matches[0]
        token = selected_config.get('agent')
    else:
        selected_config = raw
        token = raw.get('token')
    if not isinstance(token, str) or not BEARER_TOKEN_PATTERN.fullmatch(token):
        raise ValueError('host configuration has no usable target token')
    return token, _target_id(selected_config)


def generate_agent_config(host_config, scratch):
    """Derive a target agent config from a private host FogCast config."""
    try:
        data = _read_private_config(host_config, 'host configuration')
        raw = tomllib.loads(data.decode('utf-8'))
        token, target_id = _host_provisioning(raw)
        content = (
            'listen_address = "0.0.0.0:8182"\n'
            f'token = {json.dumps(token, ensure_ascii=True)}\n'
            'mister_process_comm = "MiSTer"\n'
            'command_pipe = "/dev/MiSTer_cmd"\n'
            'core_name_file = "/tmp/CORENAME"\n'
            'menu_rbf = "/media/fat/menu.rbf"\n'
            'mgl_directory = "/tmp/fogcast"\n'
        ).encode('utf-8')
        if target_id:
            content += f'target_id = "{target_id}"\n'.encode('ascii')
        if len(content) > MAX_CONFIG_BYTES:
            raise ValueError('generated target agent configuration exceeds size limit')
        destination = Path(scratch) / 'agent.toml'
        write_private(destination, content)
        return destination, hashlib.sha256(content).hexdigest()
    except (UnicodeDecodeError, tomllib.TOMLDecodeError, TypeError, ValueError):
        raise ValueError('host configuration could not be converted to a target agent configuration') from None


def resolve_agent_config(config, scratch, *, auto=False):
    """Select an explicit, automatically derived, or unprovisioned config."""
    if config is not None:
        return snapshot_config(config, scratch)
    if not auto or _truthy_environment('CI') or _truthy_environment('FES_UNPROVISIONED'):
        return None, None
    configured_path = os.environ.get('FES_HOST_CONFIG', str(DEFAULT_HOST_CONFIG))
    host_config = Path(configured_path)
    if not host_config.is_absolute():
        raise ValueError('automatic media provisioning requires an absolute FES_HOST_CONFIG')
    try:
        host_config.lstat()
    except FileNotFoundError:
        raise ValueError('automatic media provisioning requires private host configuration at ' + str(host_config)) from None
    except OSError:
        raise ValueError('automatic media provisioning could not inspect host configuration') from None
    return generate_agent_config(host_config, scratch)


def validate_launcher_token(token, agent_token):
    """Match the restricted host listener's bounds and separate credentials."""
    if (not isinstance(token, str) or not 32 <= len(token) <= 256
            or not BEARER_TOKEN_PATTERN.fullmatch(token) or token == agent_token):
        raise ValueError('launcher token must be 32–256 bearer characters and distinct from the agent token')


def resolve_launcher_config(agent_config, scratch, *, auto=False):
    """Snapshot prepared companion only for automatic local provisioning."""
    if not auto or agent_config is None or _truthy_environment('CI') or _truthy_environment('FES_UNPROVISIONED'):
        return None, None
    path = Path(os.environ.get('FES_HOST_CONFIG', str(DEFAULT_HOST_CONFIG))).parent / 'launcher.json'
    try:
        path.lstat()
    except FileNotFoundError:
        return None, None
    try:
        data = _read_private_config(path, 'launcher configuration')
        value = json.loads(data)
        agent = tomllib.loads(Path(agent_config).read_text())
        from urllib.parse import urlsplit
        from prepare_launcher import validate_address
        endpoint = urlsplit(value['api'])
        validate_launcher_token(value['token'], agent.get('token'))
        if (set(value) != {'api', 'token', 'target_id'} or endpoint.scheme != 'http'
                or endpoint.port != 8789 or endpoint.path or endpoint.query or endpoint.fragment
                or endpoint.username or endpoint.password or not endpoint.hostname
                or not value['target_id'] or value['target_id'] != agent.get('target_id')):
            raise ValueError()
        validate_address(endpoint.hostname)
    except (ValueError, TypeError, KeyError, UnicodeError):
        raise ValueError('prepared launcher configuration is invalid or differs from the agent target identity') from None
    destination = Path(scratch) / 'launcher.json'
    write_private(destination, data)
    return destination, hashlib.sha256(data).hexdigest()


def select(root, profile):
    configuration = tomllib.loads((root / 'profiles' / (profile + '.toml')).read_text())
    revisions = cold_build.validate(root, configuration)
    env = build_environment()
    with tempfile.TemporaryDirectory(prefix='fes-media-go-') as temporary:
        (Path(temporary) / 'go.mod').write_text(cold_build.git(root / 'sources/FogCast', 'show',
                                                           revisions['FogCast'] + ':go.mod') + '\n')
        toolchain = subprocess.check_output(['go', 'version'], cwd=temporary, env=env, text=True).strip()
    image_fingerprint, _ = cold_build.build_fingerprint(revisions, configuration, toolchain)
    host_fingerprint, _ = cold_build.host_fingerprint(revisions, configuration, toolchain)
    fogcast = cold_build.source_checkout('FogCast', revisions['FogCast'], '-' + profile)
    return image_fingerprint, host_fingerprint, fogcast, cold_build.selected_cores(configuration), env



def provenance_for(root, fogcast, cold):
    try:
        idle = tomllib.loads((fogcast / 'build/native-runtime.inputs.lock.toml').read_text())['idle_rbf']
        if idle['install_path'] != '/usr/share/mister-runtime/idle.rbf':
            raise ValueError('noncanonical idle destination')
        return Provenance(cold['fes_revision'], PROFILE,
            hashlib.sha256(json.dumps(recipe_fingerprint(cold_build.MEDIA_RECIPE_FILES), sort_keys=True).encode()).hexdigest(),
            cold['image_receipt_sha256'], cold['child_manifest_sha256'],
            idle['repository'], idle['commit'], idle['path'], idle['size'], idle['sha256'])
    except (OSError, KeyError, TypeError, ValueError):
        raise ValueError('selected media provenance or native idle lock is invalid') from None


def prepare(root, profile):
    image_fingerprint, host_fingerprint, fogcast, cores, env = select(root, profile)
    output = root / 'out' / profile
    cold = cold_build.load_verified_image(output, image_fingerprint)
    host = cold_build.load_verified_host(output, host_fingerprint)
    # Host and image receipts have independent input keys. Preserve both
    # provenance revisions instead of relabelling a reused host artifact or
    # requiring an unrelated image/runtime change to rebuild it. The nested
    # media receipt keeps host_fes_revision as receipt-only provenance: the
    # manifest's fes.revision identifies the image/rootfs, and the host
    # binaries are not embedded in the disk image. host_receipt_sha256 binds
    # the separately validated host bytes.
    cold.update({key: value for key, value in host.items() if key != 'fes_revision'})
    cold['host_fes_revision'] = host['fes_revision']
    try:
        child_manifest = output / 'manifest.tsv'
        if not stat.S_ISREG(child_manifest.lstat().st_mode):
            raise ValueError('child manifest must be regular')
        cold['child_manifest_sha256'] = digest(child_manifest)
        image_receipt = json.loads((output / 'image.json').read_text())
        if image_receipt['files']['manifest.tsv'] != cold['child_manifest_sha256']:
            raise ValueError('child manifest receipt differs')
    except (OSError, ValueError, KeyError, TypeError):
        raise ValueError('cold child manifest is missing or stale; run make build and make verify') from None
    cold['reproducibility_sha256'] = digest(output / 'reproducibility.txt')
    lock = MediaLock.load(root / 'boot-media.lock.toml')
    payloads = resolve_payloads(root, lock, cold_build.run)
    image = root / 'image'
    inputs = ImageInputs(output / 'linux.img', image / 'build/cache/target-image/native/idle.rbf',
                         payloads.kernel, payloads.uboot, provenance=provenance_for(root, fogcast, cold))
    verify_file(inputs.idle, inputs.provenance.idle_size, inputs.provenance.idle_sha256, "idle provenance")
    env = dict(env, NATIVE_RUNTIME_SYSTEMS=' '.join(cores),
               TARGET_IMAGE_OUTPUT_VOLUME=cold_build.output_volume(root, profile),
               TARGET_IMAGE_CONTAINER_RUNTIME=os.environ.get('CONTAINER_RUNTIME', 'docker'),
               FOGCAST_DIR=str(fogcast))
    return cold, fogcast, image, env, inputs, lock


def media_fingerprint(cold, lock_path, provision_sha, launcher_sha=None):
    data = {'cold': cold, 'boot_lock': digest(lock_path), 'fes_revision': cold['fes_revision'],
            'recipe': recipe_fingerprint(cold_build.MEDIA_RECIPE_FILES),
            'provision_sha256': provision_sha}
    if launcher_sha is not None:
        data['launcher_config_sha256'] = launcher_sha
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data


@contextmanager
def child_scratch(image):
    base = image / 'build/output/target-image'
    base.mkdir(parents=True, exist_ok=True)
    staged = base / 'media-verify'
    log = base / 'native-dev/qemu-smoke.log'
    # Do not use TemporaryDirectory: failed restoration must retain originals.
    saved = Path(tempfile.mkdtemp(prefix='.media-restore-', dir=base))
    moved_scratch = saved_log = created_scratch = False
    safe_to_restore = True
    had_log = log.exists() or log.is_symlink()
    had_log_parent = log.parent.exists()
    try:
        with defer_termination():
            if staged.is_symlink() or log.is_symlink():
                raise ValueError('child scratch and QEMU log must not be symlinks')
            if staged.exists():
                os.replace(staged, saved / 'scratch')
                moved_scratch = True
            log.parent.mkdir(parents=True, exist_ok=True)
            if had_log:
                shutil.copy2(log, saved / 'log')
                saved_log = True
            staged.mkdir(mode=0o700)
            created_scratch = True
        yield staged
    except ChildShutdownError:
        safe_to_restore = False
        raise
    finally:
        with defer_termination():
            if not safe_to_restore:
                raise ValueError('child shutdown could not be confirmed; backup retained at ' + str(saved))
            failures = []
            def restore(action):
                try:
                    action()
                except OSError as error:
                    failures.append(error)
            if created_scratch:
                restore(lambda: shutil.rmtree(staged))
            if moved_scratch:
                restore(lambda: os.replace(saved / 'scratch', staged))
            if saved_log:
                restore(lambda: os.replace(saved / 'log', log))
            elif not had_log:
                restore(lambda: log.unlink(missing_ok=True))
            if not had_log_parent and log.parent.exists():
                restore(log.parent.rmdir)
            if failures:
                raise ValueError('child restoration failed; backup retained at ' + str(saved)) from failures[0]
            shutil.rmtree(saved)


def validate_artifact(generation, inputs, lock, provision_sha, runner, image, env, *, rootfs_verified=True):
    runner.verify(generation, inputs, lock, provision_sha, rootfs_verified=rootfs_verified)
    with child_scratch(image) as staged:
        extracted = staged / 'linux.img'
        runner.extract(generation, extracted)
        if (extracted.is_symlink() or not extracted.is_file()
                or extracted.stat().st_size != inputs.rootfs.stat().st_size
                or digest(extracted) != digest(inputs.rootfs)):
            raise ValueError('embedded rootfs differs from verified cold image')
        extracted.chmod(0o600)
        runner.child_verify(image, staged, env)


def receipt_for(generation, fingerprint, info, assembly_hashes):
    image_sha = digest(generation / 'fes.img')
    return {'format': 1, 'fingerprint': fingerprint, 'inputs': info,
            'image_sha256': image_sha, 'manifest_sha256': digest(generation / 'fes-media.toml'),
            'assembly_sha256': list(assembly_hashes), 'checks': CHECKS, 'hardware': 'not-run'}


def read_receipt(generation):
    """Validate retained evidence without treating it as current acceptance."""
    try:
        if generation.is_symlink() or not generation.is_dir() or stat.S_IMODE(generation.stat().st_mode) != 0o700:
            raise ValueError('generation directory differs')
        files = {'fes.img', 'fes-media.toml', 'media.json'}
        entries = {path.name for path in generation.iterdir()}
        legacy = generation.parent.name == 'generations'
        if not files <= entries or (not legacy and entries != files):
            raise ValueError('generation files differ')
        if legacy:
            for name in entries - files:
                path = generation / name
                if not re.fullmatch('[0-9a-f]{64}', name) or path.is_symlink() or not path.is_dir():
                    raise ValueError('legacy generation entries differ')
        for name in files:
            path = generation / name
            mode = path.lstat().st_mode
            if not stat.S_ISREG(mode) or stat.S_IMODE(mode) != 0o600:
                raise ValueError('generation file permissions differ')
        receipt = json.loads((generation / 'media.json').read_text())
        info = receipt['inputs']
        if set(info) not in ({'cold', 'boot_lock', 'fes_revision', 'recipe', 'provision_sha256'},
                             {'cold', 'boot_lock', 'fes_revision', 'recipe', 'provision_sha256', 'launcher_config_sha256'}):
            raise ValueError('generation input schema differs')
        if 'launcher_config_sha256' in info and (not isinstance(info['launcher_config_sha256'], str)
                or not re.fullmatch('[0-9a-f]{64}', info['launcher_config_sha256'])):
            raise ValueError('invalid launcher config hash')
        provision_sha = info['provision_sha256']
        if provision_sha is not None and (not isinstance(provision_sha, str) or not re.fullmatch('[0-9a-f]{64}', provision_sha)):
            raise ValueError('invalid provision hash')
        image_sha = generation.name if legacy else generation.parent.name
        if digest(generation / 'fes.img') != image_sha:
            raise ValueError('disk identity differs')
        hashes = load_manifest(generation / 'fes-media.toml')['assembly']['sha256']
        fp = hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
        if (type(hashes) is not list or hashes != [image_sha, image_sha]
                or receipt != receipt_for(generation, fp, info, hashes)):
            raise ValueError('generation evidence differs')
        if generation.parent.name != 'generations' and generation.name != digest(generation / 'media.json'):
            raise ValueError('evidence identity differs')
        return receipt
    except (OSError, ValueError, KeyError, TypeError):
        raise ValueError('media receipt or artifact digest is missing or stale') from None


def validate_receipt(generation, cold, root):
    receipt = read_receipt(generation)
    provision_sha = receipt['inputs']['provision_sha256']
    fp, info = media_fingerprint(cold, root / 'boot-media.lock.toml', provision_sha, receipt['inputs'].get('launcher_config_sha256'))
    if receipt['fingerprint'] != fp or receipt['inputs'] != info:
        raise ValueError('media evidence requires current validation')
    return provision_sha


def generation_path(media_root, target):
    # Legacy single-level generations are read-only refresh candidates. New
    # publication always names both immutable disk and immutable evidence.
    if not re.fullmatch(r'[0-9a-f]{64}(?:/[0-9a-f]{64})?', target):
        raise ValueError('invalid media generation selector')
    path = media_root
    for part in ('generations', *target.split('/')):
        if path.is_symlink():
            raise ValueError('media generation directories must not be symlinks')
        path = path / part
    if path.is_symlink():
        raise ValueError('media generation must not be a symlink')
    return path


def current_generation(media_root):
    try:
        target = os.readlink(media_root / 'current')
        if not target.startswith('generations/'):
            raise ValueError('invalid current link')
        return generation_path(media_root, target.removeprefix('generations/'))
    except (OSError, ValueError):
        raise ValueError('media current is missing or invalid; run make media') from None


def assemble_candidate(scratch, cold, root, inputs, lock, provision_sha, runner, image, env, expected_image=None):
    candidate = scratch / 'generation'
    candidate.mkdir(mode=0o700)
    hashes = runner.assemble(candidate, inputs, lock)
    image_sha = digest(candidate / 'fes.img')
    if hashes != [image_sha, image_sha]:
        raise ValueError('independent media assembly hashes differ')
    if expected_image is not None and image_sha != expected_image:
        raise ValueError('retained disk differs from current assembly policy or selected inputs')
    for path in candidate.iterdir():
        path.chmod(0o600)
    validate_artifact(candidate, inputs, lock, provision_sha, runner, image, env, rootfs_verified=False)
    write_manifest(candidate / 'fes-media.toml', manifest_data(candidate / 'fes.img', inputs, lock,
                   provision_sha, hashes, rootfs_verified=True))
    runner.verify(candidate, inputs, lock, provision_sha, rootfs_verified=True)
    fp, info = media_fingerprint(cold, root / 'boot-media.lock.toml', provision_sha, inputs.launcher_config_sha256)
    receipt = receipt_for(candidate, fp, info, hashes)
    # Evidence identity includes the whole receipt and its manifest digest.
    # media.json remains the last file created, after every check succeeds.
    write_private(candidate / 'media.json', (json.dumps(receipt, indent=2, sort_keys=True) + '\n').encode())
    for path in candidate.iterdir():
        with path.open('rb') as stream:
            os.fsync(stream.fileno())
    fsync_dir(candidate)
    return candidate


def retain_candidate(candidate, media_root, cold, root, inputs, lock, runner, image, env):
    image_sha = digest(candidate / 'fes.img')
    evidence_sha = digest(candidate / 'media.json')
    generation = generation_path(media_root, image_sha + '/' + evidence_sha)
    generation.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if stat.S_IMODE(generation.parent.stat().st_mode) != 0o700:
        raise ValueError('media disk identity directory must be owner-only')
    if generation.exists():
        provision_sha = validate_receipt(generation, cold, root)
        validate_artifact(generation, inputs, lock, provision_sha, runner, image, env)
    else:
        os.rename(candidate, generation)
        fsync_dir(generation.parent)
        fsync_dir(generation.parent.parent)
    return generation


def publish_current(media_root, generation, scratch, *, previous_target=None):
    """Called only under operation's lease, through selection and rollback."""
    current = media_root / 'current'
    previous_link = scratch / 'previous'
    if previous_target is not None:
        previous_link.symlink_to(previous_target)
    elif current.is_symlink():
        previous_link.symlink_to(os.readlink(current))
    elif current.exists():
        raise ValueError('media current must be a relative symlink')
    temporary_link = scratch / 'current'
    temporary_link.symlink_to(generation.relative_to(media_root))
    fsync_dir(scratch)
    fsync_dir(media_root)
    try:
        # A pending signal delivered on unmask is part of this transaction too.
        with defer_termination():
            os.replace(temporary_link, current)
            fsync_dir(media_root)
    except BaseException:
        with defer_termination():
            if previous_link.is_symlink():
                os.replace(previous_link, current)
            else:
                current.unlink(missing_ok=True)
            fsync_dir(media_root)
        raise


def build(root, profile=PROFILE, agent_config=None, runner=None, *, auto_agent_config=False):
    root = Path(root).resolve()
    require_profile(profile)
    with operation(root, profile):
        cold, fogcast, image, env, inputs, lock = prepare(root, profile)
        media_root = root / 'out' / profile / 'media'
        if media_root.is_symlink() or (media_root / 'generations').is_symlink():
            raise ValueError('media publication directories must not be symlinks')
        media_root.mkdir(parents=True, exist_ok=True, mode=0o700)
        with tempfile.TemporaryDirectory(prefix='.staging-', dir=media_root) as temporary:
            scratch = Path(temporary)
            snapshot, provision_sha = resolve_agent_config(agent_config, scratch, auto=auto_agent_config)
            launcher, launcher_sha = resolve_launcher_config(snapshot, scratch, auto=auto_agent_config and agent_config is None)
            inputs = replace(inputs, agent_config=snapshot, launcher_config=launcher, launcher_config_sha256=launcher_sha)
            runner = runner or Runner(root, lock, env)
            candidate = assemble_candidate(scratch, cold, root, inputs, lock, provision_sha, runner, image, env)
            generation = retain_candidate(candidate, media_root, cold, root, inputs, lock, runner, image, env)
            publish_current(media_root, generation, scratch)
            return Result(generation)


def verify_selected(root, media_root, generation, scratch, cold, image, env, inputs, lock, runner):
    receipt = read_receipt(generation)
    provision_sha = receipt['inputs']['provision_sha256']
    fp, info = media_fingerprint(cold, root / 'boot-media.lock.toml', provision_sha, receipt['inputs'].get('launcher_config_sha256'))
    inputs = replace(inputs, launcher_config_sha256=receipt['inputs'].get('launcher_config_sha256'))
    if generation.parent.name != 'generations' and receipt['fingerprint'] == fp and receipt['inputs'] == info:
        validate_artifact(generation, inputs, lock, provision_sha, runner, image, env)
        return generation
    # Historical evidence never authorizes current checks. Reassemble twice
    # with the current recipe and require exactly the retained disk bytes.
    snapshot = None
    if provision_sha is not None:
        snapshot = scratch / 'agent.toml'
        runner.extract_config(generation, snapshot)
        if (not stat.S_ISREG(snapshot.lstat().st_mode) or snapshot.stat().st_size > 65536
                or digest(snapshot) != provision_sha):
            raise ValueError('embedded agent config differs from retained evidence')
        snapshot.chmod(0o600)
    launcher = None
    if inputs.launcher_config_sha256:
        launcher = scratch / 'launcher.json'
        runner.extract_launcher_config(generation, launcher)
        if not stat.S_ISREG(launcher.lstat().st_mode) or launcher.stat().st_size > 65536 or digest(launcher) != inputs.launcher_config_sha256:
            raise ValueError('embedded launcher config differs from retained evidence')
        launcher.chmod(0o600)
    inputs = replace(inputs, agent_config=snapshot, launcher_config=launcher)
    candidate = assemble_candidate(scratch, cold, root, inputs, lock, provision_sha, runner, image, env,
                                   expected_image=receipt['image_sha256'])
    return retain_candidate(candidate, media_root, cold, root, inputs, lock, runner, image, env)


def verify(root, profile=PROFILE, runner=None):
    root = Path(root).resolve()
    require_profile(profile)
    with operation(root, profile):
        cold, fogcast, image, env, inputs, lock = prepare(root, profile)
        media_root = root / 'out' / profile / 'media'
        generation = current_generation(media_root)  # Resolve the selector once.
        runner = runner or Runner(root, lock, env)
        with tempfile.TemporaryDirectory(prefix='.staging-', dir=media_root) as temporary:
            scratch = Path(temporary)
            selected = verify_selected(root, media_root, generation, scratch, cold, image, env, inputs, lock, runner)
            if selected != generation:
                publish_current(media_root, selected, scratch, previous_target=generation.relative_to(media_root))
            return Result(selected)


def rollback(root, generation, profile=PROFILE, runner=None):
    root = Path(root).resolve()
    require_profile(profile)
    with operation(root, profile):
        cold, fogcast, image, env, inputs, lock = prepare(root, profile)
        media_root = root / 'out' / profile / 'media'
        candidate = generation_path(media_root, generation)
        runner = runner or Runner(root, lock, env)
        with tempfile.TemporaryDirectory(prefix='.staging-', dir=media_root) as temporary:
            scratch = Path(temporary)
            selected = verify_selected(root, media_root, candidate, scratch, cold, image, env, inputs, lock, runner)
            publish_current(media_root, selected, scratch)
            return Result(selected)


class Runner:
    """External disk/container operations; publication remains on the host."""
    def __init__(self, root, lock, env):
        self.root = root
        self.runtime = env['TARGET_IMAGE_CONTAINER_RUNTIME']
        self.container = ensure_media_container(root, self.runtime, lock)

    def run(self, command, *, container_id=None, **kwargs):
        process = subprocess.Popen(list(map(str, command)), stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True, start_new_session=True, **kwargs)
        try:
            stdout, _ = process.communicate()
        except BaseException:
            # Wait before child_scratch restores paths a child could still write.
            with defer_termination():
                try:
                    if container_id is not None:
                        self.remove_owned_container(container_id)
                finally:
                    try:
                        os.killpg(process.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                    try:
                        process.communicate(timeout=5)
                    except subprocess.TimeoutExpired:
                        try:
                            os.killpg(process.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                        process.communicate()
            raise
        if process.returncode:
            if container_id is not None:
                self.remove_owned_container(container_id)
            # A tool could quote provisioned bytes in its diagnostics.
            raise ValueError('media verification or assembly failed; child output withheld')
        return stdout

    def remove_owned_container(self, identity):
        # The immutable ID (or unique name during create) is known before any
        # attached process starts. Never infer ownership from a late CID file.
        if not re.fullmatch(r'(?:[0-9a-f]{64}|fes-media-[0-9a-f]{32})', identity):
            raise ChildShutdownError('invalid owned container identity')
        try:
            result = subprocess.run([self.runtime, 'rm', '--force', identity],
                                    capture_output=True, text=True, timeout=15)
            if result.returncode and 'no such container' not in result.stderr.lower():
                raise ChildShutdownError('owned container removal failed')
            absent = subprocess.run([self.runtime, 'inspect', '--type', 'container',
                                     '--format', '{{.Id}}', identity],
                                    capture_output=True, text=True, timeout=15)
            if not absent.returncode or 'no such' not in absent.stderr.lower():
                raise ChildShutdownError('owned container absence could not be confirmed')
        except (OSError, subprocess.TimeoutExpired) as error:
            raise ChildShutdownError('owned container shutdown failed') from error

    def create_and_start(self, command, name, **kwargs):
        identity = name
        try:
            # Create cannot execute the payload. Wait for its request to finish
            # even when termination is pending, before removing this exact
            # operation-owned name/ID. A pending signal then prevents start.
            with defer_termination():
                output = self.run(command, **kwargs)
                returned = output.strip().splitlines()[-1] if output.strip() else ''
                if not re.fullmatch('[0-9a-f]{64}', returned):
                    raise ValueError('container create did not return an immutable identity')
                identity = returned
            return self.run([self.runtime, 'start', '--attach', identity],
                            container_id=identity, **kwargs)
        finally:
            with defer_termination():
                self.remove_owned_container(identity)

    def disk(self, command):
        name = 'fes-media-' + uuid.uuid4().hex
        return self.create_and_start([
            self.runtime, 'create', '--name', name, '--label', 'org.fes.media.operation=' + name,
            '--network=none', '--cap-drop=ALL', '--security-opt=no-new-privileges',
            '--volume', str(self.root) + ':/work', '--workdir', '/work',
            '--entrypoint', 'sh', self.container,
            '-c', 'umask 077; exec "$@"', 'media', *command], name)

    def path(self, path):
        return '/work/' + str(Path(path).relative_to(self.root))

    def arguments(self, inputs):
        return [item for name in ('rootfs', 'idle', 'kernel', 'uboot')
                for item in ('--' + name, self.path(getattr(inputs, name)))]

    @contextmanager
    def provenance_file(self, inputs):
        directory = self.root / 'out/tmp'
        directory.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='media-provenance-', dir=directory) as temporary:
            path = Path(temporary) / 'provenance.json'
            write_private(path, json.dumps(asdict(inputs.provenance), sort_keys=True).encode())
            yield self.path(path)

    def assemble(self, output, inputs, lock):
        command = ['python3', '/work/scripts/media_inside.py', 'assemble', '--output', self.path(output), *self.arguments(inputs)]
        if inputs.agent_config:
            command += ['--agent-config', self.path(inputs.agent_config)]
        if inputs.launcher_config:
            command += ['--launcher-config', self.path(inputs.launcher_config)]
        if inputs.launcher_config_sha256:
            command += ['--launcher-config-sha256', inputs.launcher_config_sha256]
        with self.provenance_file(inputs) as provenance:
            return json.loads(self.disk(command + ['--provenance', provenance]))['assembly_sha256']

    def verify(self, generation, inputs, lock, provision_sha, *, rootfs_verified=True):
        command = ['python3', '/work/scripts/media_inside.py', 'verify', '--image', self.path(generation / 'fes.img'),
                   '--manifest', self.path(generation / 'fes-media.toml'), *self.arguments(inputs)]
        if provision_sha:
            command += ['--agent-config-sha256', provision_sha]
        if inputs.launcher_config_sha256:
            command += ['--launcher-config-sha256', inputs.launcher_config_sha256]
        if rootfs_verified:
            command += ['--rootfs-verified']
        with self.provenance_file(inputs) as provenance:
            self.disk(command + ['--provenance', provenance])

    def extract(self, generation, destination):
        self.disk(['mcopy', '-i', self.path(generation / 'fes.img') + '@@' + str(PART1_OFFSET),
                   '::/linux/linux.img', self.path(destination)])

    def extract_config(self, generation, destination):
        self.disk(['mcopy', '-i', self.path(generation / 'fes.img') + '@@' + str(PART1_OFFSET),
                   '::/fogcast/agent.toml', self.path(destination)])

    def extract_launcher_config(self, generation, destination):
        self.disk(['mcopy', '-i', self.path(generation / 'fes.img') + '@@' + str(PART1_OFFSET),
                   '::/fogcast/launcher.json', self.path(destination)])

    def child_verify(self, image, staged, env):
        write_private(staged / 'verify.sh', CHILD_VERIFY.encode())
        guard = staged / 'bin'
        guard.mkdir(mode=0o700)
        write_private(guard / 'make', b'#!/bin/sh\necho "run make verify" >&2\nexit 1\n')
        (guard / 'make').chmod(0o700)
        name = 'fes-media-' + uuid.uuid4().hex
        runtime_shim = staged / 'container-runtime'
        runtime = shlex.quote(self.runtime)
        # The FES image wrapper constructs its pinned container arguments;
        # turn only its final run into create, then start the returned ID here.
        write_private(runtime_shim, ('#!/bin/sh\nif [ "${1:-}" = run ]; then\n'
                      '  shift\n  exec ' + runtime + ' create --name ' + name
                      + ' --label org.fes.media.operation=' + name
                      + ' "$@"\nfi\nexec ' + runtime + ' "$@"\n').encode())
        runtime_shim.chmod(0o700)
        try:
            self.create_and_start([image / 'scripts/target-image-container.sh', 'run', 'sh',
                      '/work/build/output/target-image/media-verify/verify.sh'], name,
                     env=dict(env, TARGET_IMAGE_CONTAINER_RUNTIME=str(runtime_shim)))
        except ValueError:
            raise ValueError('rootfs verification failed; if the pinned QEMU kernel cache is absent or stale, run make verify') from None


# Match the selected child's cache key algorithm, then disallow any fallback
# compiler invocation. The guard remains active when the child rechecks its cache.
CHILD_VERIFY = r'''set -eu
scratch=/work/build/output/target-image/media-verify
export PATH="$scratch/bin:$PATH"
fail() { echo 'QEMU kernel cache is absent or stale; run make verify' >&2; exit 1; }
/work/scripts/verify-target-image-source-cache.sh /work/build/target-image.sources.lock.toml /work/build/cache/target-image || fail
kernel_bare=/work/build/cache/target-image/linux-kernel.git
kernel_source=/target-image-output/qemu-vexpress-source
kernel_output=/target-image-output/qemu-vexpress-kernel
toolchain_root=/target-image-output/work-2-native-dev/host
test -x "$toolchain_root/bin/arm-buildroot-linux-gnueabihf-gcc" || fail
source_head=$(git --git-dir="$kernel_bare" rev-parse refs/target-image/pinned) || fail
test -d "$kernel_source/.git" || fail
test "$(cat "$kernel_source/.target-image-commit")" = "$source_head" || fail
test "$(git -C "$kernel_source" rev-parse HEAD)" = "$source_head" || fail
test -z "$(git -C "$kernel_source" status --porcelain --untracked-files=all -- ':!/.target-image-commit')" || fail
toolchain_sha=$(
  cd "$toolchain_root"
  find . \( -type f -o -type l \) -print | LC_ALL=C sort | while IFS= read -r relative; do
    if [ -L "$relative" ]; then
      printf '%s\tsymlink\t%s\n' "$relative" "$(readlink "$relative")"
    else
      printf '%s\tfile\t%s\n' "$relative" "$(sha256sum "$relative" | awk '{print $1}')"
    fi
  done | sha256sum | awk '{print $1}'
)
script_sha=$(sha256sum /work/scripts/qemu-smoke-target-image.sh | awk '{print $1}')
expected_key=$(printf '%s\n%s\n%s\n' "$source_head" "$toolchain_sha" "$script_sha" | sha256sum | awk '{print $1}')
TARGET_IMAGE_TEST_MODE=1 /work/scripts/qemu-smoke-target-image.sh --verify-kernel-cache "$expected_key" "$kernel_output" || fail
/work/scripts/verify-target-image.sh --inside native-dev "$scratch/linux.img" "$scratch/manifest.tsv" "$scratch/library-report.tsv"
/work/scripts/qemu-smoke-target-image.sh --inside native-dev "$scratch/linux.img"
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest='command', required=True)
    for command in ('build', 'verify', 'rollback'):
        child = subparsers.add_parser(command)
        child.add_argument('--profile', default=PROFILE, choices=[PROFILE])
        if command == 'build':
            config = child.add_mutually_exclusive_group()
            config.add_argument('--agent-config', type=Path,
                                help='private target agent TOML to embed')
            config.add_argument('--auto-agent-config', action='store_true',
                                help='derive target agent TOML from the private host config')
            config.add_argument('--unprovisioned', action='store_true',
                                help='omit target agent configuration')
        if command == 'rollback':
            child.add_argument('--generation', required=True)
    args = parser.parse_args()
    try:
        with termination_handling():
            if args.command == 'build':
                if args.auto_agent_config:
                    result = build(cold_build.ROOT, args.profile, args.agent_config,
                                   auto_agent_config=True)
                else:
                    result = build(cold_build.ROOT, args.profile, args.agent_config)
            elif args.command == 'rollback':
                result = rollback(cold_build.ROOT, args.generation, args.profile)
            else:
                result = verify(cold_build.ROOT, args.profile)
        print(str(result.image))
        print(result.log)
    except TerminationRequested as error:
        parser.exit(128 + error.signum, f'media: {error}; cleanup completed\n')
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'media: {error}\n')


if __name__ == '__main__':
    main()
