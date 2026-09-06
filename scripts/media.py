#!/usr/bin/env python3
"""Publish and reverify native boot media without accessing a physical device."""
import argparse
from contextlib import contextmanager
from dataclasses import dataclass
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import tempfile
import time
import tomllib

import build as cold_build
from environment import build_environment
from media_container import ensure_media_container
from media_inputs import MediaLock, digest, resolve_payloads
from media_inside import ImageInputs, PART1_OFFSET

PROFILE = 'native-integration-dev'
recipe_fingerprint = cold_build.recipe_fingerprint
CHECKS = dict.fromkeys(('structural_media', 'rootfs_structural', 'rootfs_qemu', 'reproducibility'), 'pass')


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


def snapshot_config(config, scratch):
    if config is None:
        return None, None
    config = Path(config)
    try:
        if not config.is_absolute():
            raise ValueError('agent config must be absolute')
        before = config.lstat()
        if not stat.S_ISREG(before.st_mode):
            raise ValueError('agent config must be a regular non-symlink file')
        descriptor = os.open(config, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as stream:
            opened = os.fstat(stream.fileno())
            if ((opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino)
                    or not stat.S_ISREG(opened.st_mode) or opened.st_mode & 0o077
                    or opened.st_size > 65536):
                raise ValueError('agent config must be owner-only and at most 65536 bytes')
            data = stream.read(65537)
            after = os.fstat(stream.fileno())
            if len(data) > 65536 or (opened.st_size, opened.st_mtime_ns, opened.st_ctime_ns) != (
                    after.st_size, after.st_mtime_ns, after.st_ctime_ns):
                raise ValueError('agent config changed during snapshot')
        destination = scratch / 'agent.toml'
        write_private(destination, data)
        return destination, hashlib.sha256(data).hexdigest()
    except OSError:
        raise ValueError('agent config could not be securely opened') from None


def select(root, profile):
    configuration = tomllib.loads((root / 'profiles' / (profile + '.toml')).read_text())
    revisions = cold_build.validate(root, configuration)
    env = build_environment()
    with tempfile.TemporaryDirectory(prefix='fes-media-go-') as temporary:
        (Path(temporary) / 'go.mod').write_text(cold_build.git(root / 'sources/FogCast', 'show',
                                                           revisions['FogCast'] + ':go.mod') + '\n')
        toolchain = subprocess.check_output(['go', 'version'], cwd=temporary, env=env, text=True).strip()
    fingerprint, _ = cold_build.build_fingerprint(revisions, configuration, toolchain)
    fogcast = cold_build.source_checkout('FogCast', revisions['FogCast'], '-' + profile)
    return fingerprint, fogcast, cold_build.selected_cores(configuration), env


def prepare(root, profile):
    fingerprint, fogcast, cores, env = select(root, profile)
    output = root / 'out' / profile
    cold = cold_build.load_verified_image(output, fingerprint)
    cold['reproducibility_sha256'] = digest(output / 'reproducibility.txt')
    lock = MediaLock.load(root / 'boot-media.lock.toml')
    payloads = resolve_payloads(root, lock, cold_build.run)
    inputs = ImageInputs(output / 'linux.img', fogcast / 'build/cache/target-image/native/idle.rbf',
                         payloads.kernel, payloads.uboot)
    env = dict(env, NATIVE_RUNTIME_SYSTEMS=' '.join(cores),
               TARGET_IMAGE_OUTPUT_VOLUME=cold_build.output_volume(root, profile),
               TARGET_IMAGE_CONTAINER_RUNTIME=os.environ.get('CONTAINER_RUNTIME', 'docker'))
    return cold, fogcast, env, inputs, lock


def media_fingerprint(cold, lock_path, provision_sha):
    data = {'cold': cold, 'boot_lock': digest(lock_path),
            'recipe': recipe_fingerprint(cold_build.MEDIA_RECIPE_FILES),
            'provision_sha256': provision_sha}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data


@contextmanager
def child_scratch(fogcast):
    base = fogcast / 'build/output/target-image'
    base.mkdir(parents=True, exist_ok=True)
    staged = base / 'media-verify'
    log = base / 'native-dev/qemu-smoke.log'
    with tempfile.TemporaryDirectory(prefix='.media-restore-', dir=base) as temporary:
        saved = Path(temporary)
        moved_scratch = saved_log = created_scratch = False
        had_log = log.exists()
        had_log_parent = log.parent.exists()
        try:
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
        finally:
            if created_scratch:
                shutil.rmtree(staged)
            if moved_scratch:
                os.replace(saved / 'scratch', staged)
            if saved_log:
                os.replace(saved / 'log', log)
            elif not had_log:
                log.unlink(missing_ok=True)
            if not had_log_parent and log.parent.exists():
                log.parent.rmdir()


def validate_artifact(generation, inputs, lock, provision_sha, runner, fogcast, env):
    runner.verify(generation, inputs, lock, provision_sha)
    with child_scratch(fogcast) as staged:
        extracted = staged / 'linux.img'
        runner.extract(generation, extracted)
        if (extracted.is_symlink() or not extracted.is_file()
                or extracted.stat().st_size != inputs.rootfs.stat().st_size
                or digest(extracted) != digest(inputs.rootfs)):
            raise ValueError('embedded rootfs differs from verified cold image')
        extracted.chmod(0o600)
        runner.child_verify(fogcast, staged, env)


def receipt_for(generation, fingerprint, info):
    image_sha = digest(generation / 'fes.img')
    return {'format': 1, 'fingerprint': fingerprint, 'inputs': info,
            'image_sha256': image_sha, 'manifest_sha256': digest(generation / 'fes-media.toml'),
            'assembly_sha256': [image_sha, image_sha], 'checks': CHECKS, 'hardware': 'not-run'}


def validate_receipt(generation, cold, root):
    try:
        if generation.is_symlink() or not generation.is_dir() or stat.S_IMODE(generation.stat().st_mode) != 0o700:
            raise ValueError('generation directory differs')
        if set(path.name for path in generation.iterdir()) != {'fes.img', 'fes-media.toml', 'media.json'}:
            raise ValueError('generation files differ')
        for path in generation.iterdir():
            mode = path.lstat().st_mode
            if not stat.S_ISREG(mode) or stat.S_IMODE(mode) != 0o600:
                raise ValueError('generation file permissions differ')
        receipt = json.loads((generation / 'media.json').read_text())
        provision_sha = receipt['inputs']['provision_sha256']
        if provision_sha is not None and (not isinstance(provision_sha, str) or not re.fullmatch('[0-9a-f]{64}', provision_sha)):
            raise ValueError('invalid provision hash')
        fp, info = media_fingerprint(cold, root / 'boot-media.lock.toml', provision_sha)
        if receipt != receipt_for(generation, fp, info) or receipt['image_sha256'] != generation.name:
            raise ValueError('generation evidence differs')
        return provision_sha
    except (OSError, ValueError, KeyError, TypeError):
        raise ValueError('media receipt or artifact digest is missing or stale') from None


def build(root, profile=PROFILE, agent_config=None, runner=None):
    root = Path(root).resolve()
    require_profile(profile)
    with operation(root, profile):
        cold, fogcast, env, inputs, lock = prepare(root, profile)
        media_root = root / 'out' / profile / 'media'
        generations = media_root / 'generations'
        if media_root.is_symlink() or generations.is_symlink():
            raise ValueError('media publication directories must not be symlinks')
        generations.mkdir(parents=True, exist_ok=True, mode=0o700)
        with tempfile.TemporaryDirectory(prefix='.staging-', dir=media_root) as temporary:
            scratch = Path(temporary)
            snapshot, provision_sha = snapshot_config(agent_config, scratch)
            inputs = ImageInputs(inputs.rootfs, inputs.idle, inputs.kernel, inputs.uboot, snapshot)
            runner = runner or Runner(root, lock, env)
            candidate = scratch / 'generation'
            candidate.mkdir(mode=0o700)
            hashes = runner.assemble(candidate, inputs, lock)
            image_sha = digest(candidate / 'fes.img')
            if hashes != [image_sha, image_sha]:
                raise ValueError('independent media assembly hashes differ')
            for path in candidate.iterdir():
                path.chmod(0o600)
            validate_artifact(candidate, inputs, lock, provision_sha, runner, fogcast, env)
            fp, info = media_fingerprint(cold, root / 'boot-media.lock.toml', provision_sha)
            receipt = receipt_for(candidate, fp, info)
            # This is the last file created in a generation, after all checks pass.
            write_private(candidate / 'media.json', (json.dumps(receipt, indent=2, sort_keys=True) + '\n').encode())
            for path in candidate.iterdir():
                with path.open('rb') as stream:
                    os.fsync(stream.fileno())
            fsync_dir(candidate)
            generation = generations / image_sha
            if generation.exists() or generation.is_symlink():
                old_provision = validate_receipt(generation, cold, root)
                if old_provision != provision_sha:
                    raise ValueError('existing generation provision receipt differs')
                validate_artifact(generation, inputs, lock, provision_sha, runner, fogcast, env)
            else:
                os.rename(candidate, generation)
                fsync_dir(generations)
            current = media_root / 'current'
            previous_link = scratch / 'previous'
            if current.is_symlink():
                previous_link.symlink_to(os.readlink(current))
            elif current.exists():
                raise ValueError('media current must be a relative symlink')
            temporary_link = scratch / 'current'
            temporary_link.symlink_to(Path('generations') / image_sha)
            fsync_dir(scratch)
            fsync_dir(media_root)
            os.replace(temporary_link, current)
            try:
                fsync_dir(media_root)
            except OSError:
                if previous_link.is_symlink():
                    os.replace(previous_link, current)
                else:
                    current.unlink()
                fsync_dir(media_root)
                raise
            return Result(generation)


def verify(root, profile=PROFILE, runner=None):
    root = Path(root).resolve()
    require_profile(profile)
    with operation(root, profile):
        cold, fogcast, env, inputs, lock = prepare(root, profile)
        media_root = root / 'out' / profile / 'media'
        try:
            if media_root.is_symlink():
                raise ValueError('invalid media directory')
            target = os.readlink(media_root / 'current')
            if not re.fullmatch(r'generations/[0-9a-f]{64}', target):
                raise ValueError('invalid target')
            # Read current once; every subsequent check uses this fixed generation.
            generation = media_root / target
            if generation.parent.is_symlink():
                raise ValueError('invalid generations directory')
        except (OSError, ValueError):
            raise ValueError('media current is missing or invalid; run make media') from None
        provision_sha = validate_receipt(generation, cold, root)
        runner = runner or Runner(root, lock, env)
        validate_artifact(generation, inputs, lock, provision_sha, runner, fogcast, env)
        return Result(generation)


class Runner:
    """External disk/container operations; publication remains on the host."""
    def __init__(self, root, lock, env):
        self.root = root
        self.runtime = env['TARGET_IMAGE_CONTAINER_RUNTIME']
        self.container = ensure_media_container(root, self.runtime, lock)

    def run(self, command, **kwargs):
        result = subprocess.run(list(map(str, command)), capture_output=True, text=True, **kwargs)
        if result.returncode:
            # A tool could quote provisioned bytes in its diagnostics.
            raise ValueError('media verification or assembly failed; child output withheld')
        return result.stdout

    def disk(self, command):
        return self.run([self.runtime, 'run', '--rm', '--network=none', '--cap-drop=ALL',
                         '--security-opt=no-new-privileges', '--volume', str(self.root) + ':/work',
                         '--workdir', '/work', '--entrypoint', 'sh', self.container,
                         '-c', 'umask 077; exec "$@"', 'media', *command])

    def path(self, path):
        return '/work/' + str(Path(path).relative_to(self.root))

    def arguments(self, inputs):
        return [item for name in ('rootfs', 'idle', 'kernel', 'uboot')
                for item in ('--' + name, self.path(getattr(inputs, name)))]

    def assemble(self, output, inputs, lock):
        command = ['python3', '/work/scripts/media_inside.py', 'assemble', '--output', self.path(output), *self.arguments(inputs)]
        if inputs.agent_config:
            command += ['--agent-config', self.path(inputs.agent_config)]
        return json.loads(self.disk(command))['assembly_sha256']

    def verify(self, generation, inputs, lock, provision_sha):
        command = ['python3', '/work/scripts/media_inside.py', 'verify', '--image', self.path(generation / 'fes.img'),
                   '--manifest', self.path(generation / 'fes-media.toml'), *self.arguments(inputs)]
        if provision_sha:
            command += ['--agent-config-sha256', provision_sha]
        self.disk(command)

    def extract(self, generation, destination):
        self.disk(['mcopy', '-i', self.path(generation / 'fes.img') + '@@' + str(PART1_OFFSET),
                   '::/linux/linux.img', self.path(destination)])

    def child_verify(self, fogcast, staged, env):
        write_private(staged / 'verify.sh', CHILD_VERIFY.encode())
        guard = staged / 'bin'
        guard.mkdir(mode=0o700)
        write_private(guard / 'make', b'#!/bin/sh\necho "run make verify" >&2\nexit 1\n')
        (guard / 'make').chmod(0o700)
        try:
            self.run([fogcast / 'scripts/target-image-container.sh', 'run', 'sh',
                      '/work/build/output/target-image/media-verify/verify.sh'], env=env)
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
    for command in ('build', 'verify'):
        child = subparsers.add_parser(command)
        child.add_argument('--profile', default=PROFILE, choices=[PROFILE])
        if command == 'build':
            child.add_argument('--agent-config', type=Path)
    args = parser.parse_args()
    try:
        if args.command == 'build':
            result = build(cold_build.ROOT, args.profile, args.agent_config)
        else:
            result = verify(cold_build.ROOT, args.profile)
        print(str(result.image))
        print(result.log)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'media: {error}\n')


if __name__ == '__main__':
    main()
