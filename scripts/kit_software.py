#!/usr/bin/env python3
"""Build a complete software release against a reviewed base image; never deploy."""
import argparse
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import platform
import shutil
import subprocess
import tempfile
import tomllib

import bundled_firmware
from environment import build_environment
from recipes import shared_cache_root

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def run(args, **kwargs):
    return subprocess.check_output([str(x) for x in args], text=True, **kwargs).strip()


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def build_go(snapshot, version, revision, env):
    fogcast = snapshot / 'sources/FogCast'
    destination = fogcast / 'bin'
    destination.mkdir(exist_ok=True)
    flags = ('-s -w -X github.com/DeanoC/FogCast/internal/version.Version=' + version +
             ' -X github.com/DeanoC/FogCast/internal/version.Revision=' + revision)
    for command, architecture, filename in (
        ('mister-agent', 'arm', 'mister-agent-linux-armv7'),
        ('fogcast-kit', 'arm', 'fogcast-kit-linux-armv7'),
        ('fogcast-tenfoot', 'arm', 'fogcast-tenfoot-linux-armv7'),
        ('fes-kit-bundle', 'amd64', 'fes-kit-bundle-linux-amd64'),
    ):
        run(['go', 'build', '-buildvcs=false', '-trimpath', '-ldflags', flags,
             '-o', destination / filename, './cmd/' + command], cwd=fogcast,
            env=dict(env, CGO_ENABLED='0', GOOS='linux', GOARCH=architecture, GOARM='7'))
    (destination / 'kit-software.revision').write_text(revision + '\n')
    goroot = Path(run(['go', 'env', 'GOROOT'], cwd=fogcast, env=env))
    return {'version': run(['go', 'version'], cwd=fogcast, env=env),
            'go_sha256': sha(goroot / 'bin/go'),
            'compile_sha256': sha(goroot / 'pkg/tool/linux_amd64/compile'),
            'flags': ['-buildvcs=false', '-trimpath', '-ldflags', flags],
            'target': 'linux/arm GOARM=7 CGO_ENABLED=0', 'helper': 'linux/amd64 CGO_ENABLED=0'}


def pinned_kernel(tree):
    """The kernel source pin, bound to the kernel the boot media install."""
    lock = tomllib.loads((tree / 'image/build/target-image.sources.lock.toml').read_text())
    kernel = lock['kernel']
    if not re.fullmatch('[0-9a-f]{40}', kernel['commit']) or not re.fullmatch('[0-9A-Za-z.+-]+', kernel['release']):
        raise ValueError('invalid pinned kernel in source lock')
    # A base image carries no kernel. Every card boots the boot-media kernel and
    # appliance releases must keep it, so the module can only target that kernel.
    # The source lock pairs the kernel source with that upstream image.
    boot = tomllib.loads((tree / 'boot-media.lock.toml').read_text())['kernel']['sha256']
    if not re.fullmatch('[0-9a-f]{64}', boot) or lock['image_creator']['kernel_sha256'] != boot:
        raise ValueError('pinned kernel source is not paired with the boot-media kernel')
    return {'kernel_commit': kernel['commit'], 'kernel_release': kernel['release'], 'kernel_sha256': boot}


def prepare_kernel_module(root, snapshot, env):
    """Stage the pinned kernel tree for the container's fes-window build."""
    image = snapshot / 'image'
    kernel = pinned_kernel(snapshot)
    commit = kernel['kernel_commit']
    # kit-sdk publishes the SDK to the shared cache, but fetches source into the
    # worktree cache. Prefer a shared kernel repo when present; otherwise use
    # the worktree repo. Both paths are checked against the source lock below.
    shared = env.get('FES_TARGET_IMAGE_SHARED_CACHE')
    local_bare = Path(root) / 'image/build/cache/target-image/linux-kernel.git'
    shared_bare = Path(shared) / 'linux-kernel.git' if shared else None
    bare = shared_bare if shared_bare and shared_bare.exists() else local_bare
    # The provenance verify-target-image-source-cache.sh requires: the fetch pin
    # names the locked commit, and that commit object is present.
    pinned = subprocess.run(['git', '--git-dir', str(bare), 'rev-parse', '--verify', '--quiet',
                             'refs/target-image/pinned'], capture_output=True, text=True, env=env,
                            check=False).stdout.strip()
    if pinned != commit:
        raise ValueError(f'{bare} pin is {pinned or "missing"}, expected {commit}; '
                         'point refs/target-image/pinned at it, as fetch-target-image-sources.sh does')
    present = subprocess.run(['git', '--git-dir', str(bare), 'cat-file', '-e', commit + '^{commit}'],
                             capture_output=True, env=env, check=False)
    if present.returncode != 0:
        raise ValueError(f'pinned kernel commit {commit} missing from {bare}; fetch it into that bare repository')
    source = image / 'build/software/kernel-source'
    source.mkdir(parents=True)
    archive = subprocess.Popen(['git', '--git-dir', str(bare), 'archive', commit],
                               stdout=subprocess.PIPE, env=env)
    extract = subprocess.run(['tar', '-x', '-C', str(source)], stdin=archive.stdout, env=env, check=False)
    archive.stdout.close()
    if archive.wait() != 0 or extract.returncode != 0:
        raise ValueError('could not extract the pinned kernel source')
    return kernel


def runtime_script(revision, driver):
    # All paths are fixed inside the private snapshot. No shell input comes from
    # user paths/version. Interpolated values are a validated Git SHA and the
    # pinned kernel release.
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('invalid runtime revision')
    if not re.fullmatch('[0-9A-Za-z.+-]+', driver['kernel_release']):
        raise ValueError('invalid pinned kernel release')
    return '''set -eu
export SOURCE_DATE_EPOCH=1751459412
python3 /work/scripts/toolchain_cache.py extract /work/build/software/sdk
prefix=/work/build/software/sdk/host/bin/arm-buildroot-linux-gnueabihf
# fes-window builds before SDK relocation; see build-fes-window.sh.
/work/scripts/build-fes-window.sh /work/build/software/kernel-source /work/build/software/fes-window-build \\
 /work/build/software/fes-window "$prefix-" /work/build/software/sdk/host ''' + driver['kernel_release'] + '''
/work/build/software/sdk/host/relocate-sdk.sh
make -C /work/build/software/runtime -j2 all \\
 CXX="$prefix-g++" AR="$prefix-ar" NM="$prefix-nm" CXXFILT="$prefix-c++filt" \\
 MISTER_RUNTIME_VERSION=git-''' + revision + '''
"$prefix-strip" --strip-unneeded /work/build/software/runtime/build/mister-runtime
mkdir -p /work/build/software/base/etc /work/build/software/base/usr/share/fes /work/build/software/base/usr
# Only base ABI and immutable factory trees are required. Avoid device inodes.
debugfs -R 'rdump /lib /work/build/software/base' /work/build/software/base.img
debugfs -R 'rdump /usr/lib /work/build/software/base/usr' /work/build/software/base.img
debugfs -R 'rdump /etc/fes /work/build/software/base/etc' /work/build/software/base.img
debugfs -R 'rdump /usr/share/fes/software /work/build/software/base/usr/share/fes' /work/build/software/base.img
'''


def build_runtime(snapshot, cache, revision, env, driver):
    image = snapshot / 'image'
    work = image / 'build/software'
    work.mkdir(parents=True, exist_ok=True)
    shutil.copytree(snapshot / 'sources/libmister-runtime', work / 'runtime')
    # Reuse only the authenticated archive, never a shared relocated SDK.
    sdk = load(image / 'scripts/toolchain_cache.py', 'kit_sdk')
    sdk_key = sdk.key(image)
    sdk_sha = sdk.validated_sha(cache, sdk_key)
    if sdk_sha is None:
        raise ValueError('compatible cached SDK missing or corrupt; run make kit-sdk')
    local = image / 'build/cache/target-image/toolchains' / sdk_key
    local.mkdir(parents=True)
    shutil.copyfile(cache / 'host.tar', local / 'host.tar')
    shutil.copyfile(cache / 'receipt.json', local / 'receipt.json')
    if sdk.validated_sha(local, sdk_key) != sdk_sha:
        raise ValueError('cached SDK changed while copying')
    # Wrapper validates the pinned builder labels and runs without networking.
    container_env = dict(env)
    container_env.pop('FES_TARGET_IMAGE_SHARED_CACHE', None)
    container_env['TARGET_IMAGE_CONTAINER_RUNTIME'] = env.get('CONTAINER_RUNTIME', 'docker')
    container_env['TARGET_IMAGE_OUTPUT_VOLUME'] = 'fes-kit-software-' + snapshot.parent.name
    lock = tomllib.loads((image / 'build/target-image.sources.lock.toml').read_text())
    context = hashlib.sha256(''.join(sha(image / name) + '\n' for name in (
        'containers/target-image/Dockerfile', 'containers/target-image/create-builder-user.sh',
        'build/target-image-container-packages.sha256', 'build/target-image.sources.lock.toml')).encode()).hexdigest()
    tag = 'fogcast-target-image-build:' + lock['container']['digest'][7:19] + '-' + context[:12] + '-' + str(os.getuid()) + '-' + str(os.getgid())
    builder_id = run([container_env['TARGET_IMAGE_CONTAINER_RUNTIME'], 'image', 'inspect', tag,
                      '--format', '{{.Id}}'], env=container_env)
    if not re.fullmatch('sha256:[0-9a-f]{64}', builder_id):
        raise ValueError('invalid pinned builder identity')
    existing = subprocess.run([container_env['TARGET_IMAGE_CONTAINER_RUNTIME'], 'volume', 'inspect',
                               container_env['TARGET_IMAGE_OUTPUT_VOLUME']],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    if existing.returncode == 0:
        raise ValueError('private build volume already exists; refusing to claim it')
    try:
        run([image / 'scripts/target-image-container.sh', 'run', 'sh', '-c', runtime_script(revision, driver)],
            env=container_env)
    finally:
        subprocess.run([container_env['TARGET_IMAGE_CONTAINER_RUNTIME'], 'volume', 'rm',
                        container_env['TARGET_IMAGE_OUTPUT_VOLUME']],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    return {'key': sdk_key, 'archive_sha256': sdk_sha, 'container_id': builder_id,
            'container_base': lock['container'], 'container_context_sha256': context}


def package(image, payload, output, version, fogcast, revision, env):
    return json.loads(run(['python3', image / 'scripts/kit-software.py',
                          '--base-root', image / 'build/software/base', '--payload', payload,
                          '--output', output, '--version', version, '--fogcast', fogcast,
                          '--runtime-revision', revision], env=env))


def publish_directory(candidate, output):
    # Linux RENAME_NOREPLACE also refuses an empty directory won by another
    # writer, unlike os.rename. Never overwrite any existing release.
    libc = ctypes.CDLL(None, use_errno=True)
    rename = libc.renameat2
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename.restype = ctypes.c_int
    if rename(-100, os.fsencode(candidate), -100, os.fsencode(output), 1):
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code), str(output))
    fd = os.open(output.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def publish(root, base, base_sha, version, output):
    workflow_sha = sha(Path(__file__))
    if platform.system() != 'Linux' or platform.machine() not in ('x86_64', 'amd64'):
        raise ValueError('kit software builder requires Linux amd64')
    root, base, output = Path(root).resolve(), Path(base).resolve(), Path(output).absolute()
    if not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._+-]{0,127}', version):
        raise ValueError('version must be a simple release identifier, maximum 128 characters')
    if not base.is_file():
        raise ValueError('base image must be a regular file')
    if not re.fullmatch('[0-9a-f]{64}', base_sha) or sha(base) != base_sha:
        raise ValueError('base image does not match reviewed SHA-256')
    if output.exists() or output.is_symlink():
        raise ValueError('output already exists; use a new release directory')
    if output == base or base in output.parents:
        raise ValueError('output overlaps base input')
    env = build_environment()
    for name in tuple(env):
        if (name.startswith('FES_') and name != 'FES_TARGET_IMAGE_SHARED_CACHE') or name == 'FOGCAST_DIR':
            env.pop(name)
    for name in ("CXX", "CC", "AR", "NM", "CXXFILT", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS", "LD_PRELOAD", "LD_LIBRARY_PATH", "GNUMAKEFLAGS", "MAKELEVEL", "LDLIBS",
                 "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
                 "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE"):
        env.pop(name, None)
    revision = run(['git', '-C', root, 'rev-parse', 'HEAD'], env=env)
    if run(['git', '-C', root, 'status', '--porcelain', '--untracked-files=all', '--',
            'sources/FogCast', 'sources/libmister-runtime', 'image'], env=env):
        raise ValueError('selected software/image sources have uncommitted changes; commit them first')
    sdk = load(root / 'image/scripts/toolchain_cache.py', 'kit_sdk_selection')
    cache = sdk.cache_dir(root / 'image')
    if sdk.validated_sha(cache, sdk.key(root / 'image')) is None:
        raise ValueError('compatible cached SDK missing or corrupt; run make kit-sdk')
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='fes-kit-software-') as scratch, \
         tempfile.TemporaryDirectory(prefix='.kit-software-', dir=output.parent) as publication:
        snapshot = Path(scratch) / 'source'
        run(['git', 'clone', '--quiet', '--shared', '--no-checkout', root, snapshot], env=env)
        run(['git', '-C', snapshot, 'checkout', '--quiet', '--detach', revision], env=env)
        image = snapshot / 'image'
        # Private copy prevents a concurrent source image change during extract.
        (image / 'build/software').mkdir(parents=True)
        shutil.copyfile(base, image / 'build/software/base.img')
        if sha(image / 'build/software/base.img') != base_sha:
            raise ValueError('base image changed while copying')
        go_version = build_go(snapshot, version, revision, env)
        driver = prepare_kernel_module(root, snapshot, env)
        toolchain = build_runtime(snapshot, cache, revision, env, driver)
        fogcast = snapshot / 'sources/FogCast'
        payload = Path(scratch) / 'payload'
        (payload / 'bin').mkdir(parents=True)
        for name in ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot'):
            shutil.copyfile(fogcast / ('bin/' + name + '-linux-armv7'), payload / 'bin' / name)
        shutil.copyfile(image / 'build/software/runtime/build/mister-runtime', payload / 'bin/mister-runtime')
        shutil.copytree(image / 'buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/fogcast',
                        payload / 'share/fogcast')
        # Bundled firmware ships as separate objects beside the cores; the
        # kit's /usr/share/fogcast/firmware is the host's bundled_dir.
        firmware = [{'id': m['id'], 'sha256': m['sha256'], 'size': m['size']} for m in
                    bundled_firmware.stage(payload / 'share/fogcast/firmware', shared_cache_root() / 'firmware')]
        # The runtime loads its release's driver; see libmister-runtime window_driver.
        window = payload / 'share/fes-window'
        shutil.copytree(image / 'build/software/fes-window', window)
        driver['version'] = (window / 'version').read_text().strip()
        if not re.fullmatch('[0-9a-f]{16}', driver['version']) or sorted(p.name for p in window.iterdir()) != ['fes_window.ko', 'version']:
            raise ValueError('fes-window build produced an unexpected driver')
        for path in window.iterdir():
            path.chmod(0o444)
        driver['sha256'] = sha(window / 'fes_window.ko')
        for path in (payload / 'bin').iterdir():
            path.chmod(0o555)
        candidate = Path(publication)
        result = package(image, payload, candidate / 'bundle.tar', version, fogcast, revision, env)
        checked = json.loads(run([fogcast / 'bin/fes-kit-bundle-linux-amd64', 'inspect',
                                  '--bundle', candidate / 'bundle.tar'], env=env))
        if checked != result:
            raise ValueError('published bundle inspection differs from pack result')
        factory = json.loads((image / 'build/software/base/usr/share/fes/software/manifest.json').read_text())
        receipt = {'format': 1, 'deployed': False, 'source_commit': revision,
                   'source_trees': {name: run(['git', '-C', snapshot, 'rev-parse', 'HEAD:' + name], env=env)
                                    for name in ('sources/FogCast', 'sources/libmister-runtime', 'image')},
                   'base_image_sha256': base_sha, 'base_contract': factory['base_contract'],
                   'base_factory_manifest_sha256': sha(image / 'build/software/base/usr/share/fes/software/manifest.json'),
                   'toolchain': toolchain, 'go_toolchain': go_version, 'window_driver': driver,
                   'bundled_firmware': firmware,
                   'workflow_sha256': workflow_sha, 'bundle': result,
                   'artifacts': {'bundle.tar': {'sha256': sha(candidate / 'bundle.tar'),
                                               'size': (candidate / 'bundle.tar').stat().st_size}}}
        if sha(Path(__file__)) != workflow_sha:
            raise ValueError('workflow recipe changed during build')
        (candidate / 'receipt.json').write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
        for path in candidate.iterdir():
            path.chmod(0o444)
            with path.open('rb') as stream:
                os.fsync(stream.fileno())
        fd = os.open(candidate, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        publish_directory(candidate, output)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True, type=Path)
    parser.add_argument('--base-sha256', required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    output = args.output or ROOT / 'out/kit-software' / args.version
    receipt = publish(ROOT, args.base, args.base_sha256, args.version, output)
    print(json.dumps({'output': str(output.absolute()), 'deployed': False, 'bundle': receipt['bundle']['id']}))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit('kit-software: ' + str(error))
