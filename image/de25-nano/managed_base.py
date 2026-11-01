#!/usr/bin/env python3
"""Build the DE25 managed rootfs intermediate through the existing image builder.

This is not a bootable factory image. Factory assembly still requires the real
runtime, a sealed MENU package and its normal compatibility/admission checks.
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import posixpath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import tomllib

LANE = Path(__file__).resolve().parent
ROOT = LANE.parents[1]
sys.path.insert(0, str(ROOT))
from scripts import de25_owner_image as owner
image = owner.build
sys.path.insert(0, str(ROOT / 'scripts'))
from scripts import kit_software
from scripts import platform as platform_build
from scripts.environment import build_environment

BINARIES = {
    name: name + '-linux-aarch64' for name in
    ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot', 'fes-kit-software',
     'fes-bluetooth', 'fes-kit-data')
}
KERNEL_FILES = ('Image', 'de25-owner.dtb', 'kernel.config', 'System.map',
                'vmlinux.symvers', 'kernel-build.json')
LIBRARIES = ('ld-linux-aarch64.so.1', 'libImlib2.so.1', 'libbluetooth.so.3',
             'libbz2.so.1.0', 'libc.so.6', 'libdl.so.2', 'libfreetype.so.6',
             'libgcc_s.so.1', 'libm.so.6', 'libpng16.so.16', 'libpthread.so.0',
             'librt.so.1', 'libstdc++.so.6', 'libz.so.1')


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sdk_lock():
    return tomllib.loads((ROOT / 'image/kernel/de25-dma/linux.lock.toml').read_text())


def recipe_key(root=ROOT):
    root = Path(root)
    paths = set(image.recipe_files(root))
    for directory in ('image/buildroot', 'image/firmware/rtl_bt', 'image/kernel/de25-platform'):
        paths.update(p for p in (root / directory).rglob('*') if p.is_file() and
                     '__pycache__' not in p.parts and p.suffix != '.md')
    paths.update(root / p for p in (
        'image/scripts/kit-software.py', 'image/scripts/ramtest-notices.py',
        'image/scripts/scan-target-image-secrets.sh', 'image/kernel/de25-dma/linux.lock.toml',
        'scripts/de25_owner_image.py', 'scripts/de25_kernel_platform.py',
        'scripts/de25_kernel_test.py', 'scripts/kit_software.py', 'scripts/platform.py'))
    value = hashlib.sha256(b'FES-DE25-MANAGED-BASE-1\n')
    for path in sorted(paths):
        value.update(path.relative_to(root).as_posix().encode() + b'\0' + path.read_bytes())
    return value.hexdigest()


def configuration(*, factory_key=None):
    common = (ROOT / 'image/buildroot/configs/fogcast_target_native_dev_defconfig').read_text()
    lines = [line for line in common.splitlines() if not line.startswith((
        'BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=', 'BR2_ROOTFS_POST_BUILD_SCRIPT=',
        'BR2_TARGET_GENERIC_HOSTNAME=', 'BR2_TARGET_ROOTFS_EXT2_SIZE='))]
    lines += [
        'BR2_aarch64=y', 'BR2_cortex_a53=y', 'BR2_REPRODUCIBLE=y',
        'BR2_TOOLCHAIN_EXTERNAL=y', 'BR2_TOOLCHAIN_EXTERNAL_CUSTOM=y',
        'BR2_TOOLCHAIN_EXTERNAL_PREINSTALLED=y',
        f'BR2_TOOLCHAIN_EXTERNAL_PATH="{toolchain_path(factory_key)}"',
        'BR2_TOOLCHAIN_EXTERNAL_CUSTOM_PREFIX="aarch64-buildroot-linux-gnu"',
        'BR2_TOOLCHAIN_EXTERNAL_GCC_9=y', 'BR2_TOOLCHAIN_EXTERNAL_HEADERS_5_10=y',
        'BR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y', 'BR2_TOOLCHAIN_EXTERNAL_CXX=y',
        '# BR2_TOOLCHAIN_EXTERNAL_INET_RPC is not set',
        '# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set',
        'BR2_TARGET_GENERIC_HOSTNAME="fes-de25-nano"',
        'BR2_TARGET_GENERIC_GETTY_PORT="ttyS0"', 'BR2_TARGET_GENERIC_GETTY_BAUDRATE_115200=y',
        'BR2_TARGET_ROOTFS_EXT2_SIZE="256M"', 'BR2_TARGET_ROOTFS_TAR=y',
        f'BR2_ROOTFS_POST_BUILD_SCRIPT="{LANE}/managed-base-post-build.sh"',
    ]
    if factory_key is not None:
        lines.remove('# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set')
        lines.append('BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y')
    return '\n'.join(lines) + '\n'


def toolchain_path(factory_key=None):
    # Buildroot exports HOST_DIR as its SDK. Keeping the pinned compiler here
    # makes its tool links and compiler-wrapper paths relative to that export.
    return Path('/build') / (factory_key or recipe_key()) / 'host/opt' / ('sdk-' + sdk_lock()['sdk_sha256'])


def prepare_sdk(factory_key=None):
    selected = sdk_lock()
    archive = Path('/cache') / ('aarch64-sdk-' + selected['sdk_sha256'] + '.tar.gz')
    if archive.is_symlink() or image.digest(archive) != selected['sdk_sha256']:
        raise ValueError('managed base SDK differs from owner-kernel source lock')
    destination = toolchain_path(factory_key)
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    image.run(['tar', '-xzf', archive, '--strip-components=1', '-C', destination])
    image.run([destination / 'relocate-sdk.sh'])


def post_build(target):
    target = Path(target).resolve(strict=True)
    inputs = json.loads(Path('/inputs/base-inputs.json').read_text())
    kernel = owner.selected_kernel(Path('/inputs/kernel'))
    (target / 'etc/fes-board').write_text('de25-nano\n')
    additions = owner.root_changes({'fes_revision': inputs['fes_revision']}, kernel,
                                   inputs['fes_revision'], (target / 'etc/mdev.conf').read_bytes())
    # Shared preparation installs this MAC policy before DHCP as S15.
    additions.pop(owner.NETWORK_ID)
    for name, (data, mode) in additions.items():
        destination = target / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        if destination.is_symlink():
            raise ValueError('unsafe managed base configuration destination: ' + name)
        destination.write_bytes(data)
        destination.chmod(mode)
    inittab = target / 'etc/inittab'
    inittab.write_text(inittab.read_text().replace(
        'console::respawn:/sbin/getty -L console 115200 vt100',
        'ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100'))
    image.run([ROOT / 'image/buildroot/board/fogcast-target/prepare-native-rootfs.sh', target])
    for name in ('fes-kit-software', 'fes-bluetooth', 'fes-kit-data'):
        shutil.copyfile(ROOT / 'sources/FogCast/bin' / BINARIES[name], target / 'usr/sbin' / name)
        (target / 'usr/sbin' / name).chmod(0o755)
    if inputs.get('profile') == 'managed-factory':
        import managed_factory
        managed_factory.post_build(target, inputs)
        return
    # PID1 retains the original root at this immutable mount point. Full
    # factory assembly must install its selected bundle before enabling S40.
    (target / '.fes-bootstrap').mkdir(exist_ok=True)
    (target / 'etc/init.d/S40fes-kit-software').unlink(missing_ok=True)
    record = {**inputs, 'classification': 'managed-base-intermediate',
              'normal_runtime_admission': 'closed', 'factory_assembly': 'required',
              'kernel_image_sha256': kernel['files']['Image']['sha256']}
    (target / 'usr/share/fes/managed-base.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')


def verify_bootstrap_mountpoint(target, ext4):
    bootstrap, startup = target / '.fes-bootstrap', target / 'etc/init.d/S40fes-kit-software'
    if (bootstrap.is_symlink() or not bootstrap.is_dir() or
            startup.exists() or startup.is_symlink()):
        raise ValueError('intermediate requires its bootstrap mount directory and disabled managed launch')
    if not re.search(rb'Type:\s+directory\b', image.debugfs(ext4, 'stat /.fes-bootstrap')):
        raise ValueError('ext4 bootstrap mount point must be a directory')


def verify_rootfs(directory):
    image.run(['e2fsck', '-fn', directory / 'rootfs.ext4'], stdout=subprocess.DEVNULL)
    contract = load(ROOT / 'image/scripts/kit-software.py', 'managed_base_contract')
    with tempfile.TemporaryDirectory(prefix='fes-de25-base-') as temporary:
        target = Path(temporary)
        # GNU tar rejects archive members that traverse an extracted symlink;
        # extraction is unprivileged and excludes device nodes.
        image.run(['tar', '-xf', directory / 'rootfs.tar', '-C', target, '--no-same-owner',
                   '--exclude=./dev/*', '--exclude=dev/*'])
        if (target / 'etc/fes-board').read_bytes() != b'de25-nano\n':
            raise ValueError('managed base board identity differs')
        inputs = json.loads((directory / 'base-inputs.json').read_text())
        embedded = json.loads((target / 'usr/share/fes/managed-base.json').read_text())
        kernel = owner.selected_kernel(directory / 'kernel')
        if (embedded != {**inputs, 'classification': 'managed-base-intermediate',
                         'normal_runtime_admission': 'closed', 'factory_assembly': 'required',
                         'kernel_image_sha256': kernel['files']['Image']['sha256']}):
            raise ValueError('managed base embedded source/kernel inputs differ')
        for name, expected in inputs['executables'].items():
            if name not in BINARIES or image.digest(contract.resolve(target, '/usr/sbin/' + name)) != expected:
                raise ValueError('managed base executable differs from its fresh producer: ' + name)
        if set(inputs['executables']) != set(BINARIES):
            raise ValueError('managed base executable set is incomplete')
        configured = (directory / 'buildroot.config').read_text().splitlines()
        for required in ('# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set',
                         'BR2_ROOTFS_MERGED_USR=y', 'BR2_PACKAGE_BLUEZ5_UTILS=y',
                         'BR2_PACKAGE_DBUS=y', '# BR2_PACKAGE_FOGCAST_MISTER_RUNTIME is not set'):
            if required not in configured:
                raise ValueError('managed base configuration lost required policy: ' + required)
        if not re.search(r'(?m)^ttyS0::respawn:.*getty.*ttyS0 115200', (target / 'etc/inittab').read_text()):
            raise ValueError('managed base serial recovery console differs')
        verify_bootstrap_mountpoint(target, directory / 'rootfs.ext4')
        forbidden = [p for p in target.rglob('*') if
                     (p.name.endswith(('.rbf', '.ko')) or
                      p.name.startswith('dropbear_') and p.name.endswith('_host_key'))]
        if forbidden or (target / 'usr/share/fes/software').exists() or (target / 'lib/modules').exists():
            raise ValueError('base contains a core, vendor module, factory bundle or generated SSH key')
        paths = [contract.resolve(target, '/usr/sbin/' + name) for name in BINARIES]
        paths += [contract.resolve(target, '/lib/' + name) for name in LIBRARIES]
        paths += [contract.resolve(target, path) for path in
                  ('/bin/busybox', '/usr/sbin/dropbear', '/usr/bin/dbus-daemon', '/usr/libexec/bluetooth/bluetoothd')]
        closure = contract.base_contract(target, paths, board='de25-nano')
        checked = set(paths)
        for path in ('etc/fes-board', 'etc/inittab', 'etc/fstab', 'etc/mdev.conf',
                     'etc/init.d/S15fpga', 'etc/init.d/S15fes-network-id',
                     'etc/init.d/S20mister-network', 'etc/init.d/S30mister-dropbear',
                     'usr/share/fes/kernel-modules.json', 'usr/share/fes/managed-base.json'):
            checked.add(target / path)
        for path in sorted(checked):
            name = '/' + path.relative_to(target).as_posix()
            if image.debugfs(directory / 'rootfs.ext4', 'cat ' + name) != path.read_bytes():
                raise ValueError('managed rootfs tar/ext4 mismatch: ' + name)
        return {'classification': 'managed-base-intermediate', 'architecture': 'aarch64',
                'base_contract': closure, 'checked_tar_ext4_files': len(checked),
                'bootstrap_mountpoint': 'directory',
                'normal_runtime_admission': 'closed', 'hardware_acceptance': 'not-run'}


def inside(action, jobs):
    output = Path('/export')
    if action == 'base-verify':
        (output / 'base-verify.json').write_text(json.dumps(verify_rootfs(output), indent=2, sort_keys=True) + '\n')
        return
    prepare_sdk()
    source, work, command, environment = image.prepare_buildroot(managed=True)
    if action == 'base-fetch':
        image.run([*command, 'source', f'-j{jobs}'], env=environment)
        return
    image.run([*command, f'-j{jobs}'], env=environment)
    image.run([*command, 'sdk', f'-j{jobs}'], env=environment)
    for name in ('rootfs.ext4', 'rootfs.tar'):
        shutil.copyfile(work / 'images' / name, output / name)
    sdks = list((work / 'images').glob('*_sdk-buildroot.tar.gz'))
    if len(sdks) != 1:
        raise ValueError('managed base did not publish exactly one AArch64 SDK')
    changes = export_sdk(sdks[0], output / 'aarch64-sdk.tar.gz', work / 'host')
    (output / 'sdk-export.json').write_text(json.dumps({'links': changes}, indent=2, sort_keys=True) + '\n')
    shutil.copyfile(work / '.config', output / 'buildroot.config')
    (output / 'base-verify.json').write_text(json.dumps(verify_rootfs(output), indent=2, sort_keys=True) + '\n')


def artifact_names():
    return ('rootfs.ext4', 'rootfs.tar', 'aarch64-sdk.tar.gz', 'buildroot.config', 'base-inputs.json',
            *('kernel/' + name for name in KERNEL_FILES))


def export_sdk(source, destination, host):
    """Make the built SDK's host and sysroot links self-contained."""
    changes = []
    with tarfile.open(source) as old, tarfile.open(destination, 'w:gz', compresslevel=6) as new:
        members = old.getmembers()
        names = {item.name.rstrip('/') for item in members}
        roots = {item.name.split('/', 1)[0] for item in members}
        if len(roots) != 1 or len(names) != len(members):
            raise ValueError('SDK export requires unique paths beneath one archive root')
        root = next(iter(roots))
        for item in members:
            if (item.name.startswith('/') or '..' in Path(item.name).parts or
                    not (item.isfile() or item.isdir() or item.issym() or item.islnk())):
                raise ValueError('unsafe SDK export member')
            if item.issym() and item.linkname.startswith('/'):
                before = item.linkname
                if (item.name == root + '/share/gettext' and
                        before == str(host / 'usr/share/gettext-tiny')):
                    # Buildroot 2021.02.4 creates this absolute host alias.
                    # Export the installed share tree through a relative link.
                    target = root + '/share/gettext-tiny'
                elif before.startswith(str(host) + '/'):
                    target = root + '/' + before[len(str(host)) + 1:]
                elif '/sysroot/' in item.name:
                    target = item.name.split('/sysroot/', 1)[0] + '/sysroot/' + before.lstrip('/')
                else:
                    raise ValueError('SDK absolute link is outside its host/sysroot')
                target = posixpath.normpath(target)
                if target not in names:
                    raise ValueError('SDK absolute link target is absent from export: ' + item.name)
                item.linkname = posixpath.relpath(target, posixpath.dirname(item.name))
                changes.append({'path': item.name, 'before': before, 'after': item.linkname})
            if item.issym() or item.islnk():
                target = posixpath.normpath(posixpath.join(posixpath.dirname(item.name), item.linkname)
                                           if item.issym() else item.linkname)
                if item.linkname.startswith('/') or not (target == root or target.startswith(root + '/')):
                    raise ValueError('SDK export link escapes its archive root')
            new.addfile(item, old.extractfile(item) if item.isfile() else None)
    return changes


def verify_receipt(output, *, producer_root=None):
    """Check artifacts and current kernel policy against their producer recipe.

    SDK consumers may supply the receipt's authenticated Git source tree. A
    software update need not rebuild its compatible, immutable base image.
    """
    record = json.loads((output / 'base.json').read_text())
    if (record.get('format') != 1 or record.get('board') != 'de25-nano' or
            record.get('architecture') != 'aarch64' or
            record.get('classification') != 'managed-base-intermediate' or
            not re.fullmatch(r'[0-9a-f]{40}', record.get('fes_revision', '')) or
            record.get('recipe_sha256') != recipe_key(ROOT if producer_root is None else producer_root) or
            record.get('sdk_sha256') != sdk_lock()['sdk_sha256'] or
            record.get('builder_image') != sdk_lock()['builder_image'] or
            record.get('normal_runtime_admission') != 'closed' or
            set(record.get('files', {})) != set(artifact_names())):
        raise ValueError('managed base receipt differs from the selected source profile')
    for name, expected in record['files'].items():
        path = output / name
        if path.is_symlink() or not path.is_file() or image.digest(path) != expected:
            raise ValueError('managed base artifact differs: ' + name)
    owner.selected_kernel(output / 'kernel')
    inputs = json.loads((output / 'base-inputs.json').read_text())
    if any(inputs.get(key) != record[key] for key in ('fes_revision', 'recipe_sha256', 'sdk_sha256')):
        raise ValueError('managed base producer inputs differ from receipt')
    return record


def stage_kernel_sdk(kernel, sdk, output, cache):
    """Copy the matching kernel and locked compiler without modifying inputs."""
    owner.selected_kernel(kernel.resolve())
    if sdk.is_symlink() or not sdk.is_file() or image.digest(sdk) != sdk_lock()['sdk_sha256']:
        raise ValueError('SDK archive differs from owner-kernel source lock')
    cached_sdk = cache / ('aarch64-sdk-' + sdk_lock()['sdk_sha256'] + '.tar.gz')
    if cached_sdk.is_symlink() or cached_sdk.exists() and not cached_sdk.is_file():
        raise ValueError('SDK cache destination must be a regular file')
    with tempfile.NamedTemporaryFile(prefix='managed-sdk-', dir=cache, delete=False) as stream:
        temporary = Path(stream.name)
    try:
        shutil.copyfile(sdk, temporary)
        if image.digest(temporary) != sdk_lock()['sdk_sha256']:
            raise ValueError('SDK archive changed during copying')
        temporary.replace(cached_sdk)
    finally:
        temporary.unlink(missing_ok=True)
    kernel_dir = output / 'kernel'
    kernel_dir.mkdir(exist_ok=True)
    for name in KERNEL_FILES:
        shutil.copyfile(kernel / name, kernel_dir / name)


def publish(args):
    output, cache = args.output.resolve(), args.cache.resolve()
    output.mkdir(parents=True, exist_ok=True)
    cache.mkdir(parents=True, exist_ok=True)
    if shutil.disk_usage(output).free < 30 * 1024**3:
        raise ValueError('managed base build requires at least 30 GiB free')
    if args.action == 'managed-base' and (args.kernel is None or args.sdk is None):
        raise ValueError('managed-base requires --kernel and --sdk')
    if args.action == 'verify-base' and (args.kernel is not None or args.sdk is not None):
        raise ValueError('verify-base reads only its recorded inputs')
    revision, source = image.snapshot()
    with image.build_lock(cache), image.output_lock(output):
        image.validate_output(output)
        volume = 'fes-de25-managed-' + hashlib.sha256((str(ROOT) + '\0' + str(cache)).encode()).hexdigest()[:16]
        builder = image.ensure_builder(source, args.container_runtime, volume)
        if builder != sdk_lock()['builder_image']:
            raise ValueError('managed base builder differs from the owner-kernel lock')
        if args.action == 'verify-base':
            verify_receipt(output)
            image.container(source, args.container_runtime, builder, volume, cache, output, 'base-verify', args.jobs)
            return 0
        (output / 'base.json').unlink(missing_ok=True)
        (output / 'base-verify.json').unlink(missing_ok=True)
        stage_kernel_sdk(args.kernel, args.sdk, output, cache)
        environment = build_environment()
        go = kit_software.build_go(source, 'de25-base-' + revision[:12], revision, environment, 'de25-nano')
        platform_build.build_kit_software(source, source / 'sources/FogCast', environment, 'aarch64')
        image.run(['make', '-C', source / 'sources/FogCast', 'build-fes-kit-data', 'FES_KIT_DATA_ARCH=aarch64'], env=environment)
        binaries = {name: image.digest(source / 'sources/FogCast/bin' / filename) for name, filename in BINARIES.items()}
        inputs = {'fes_revision': revision, 'recipe_sha256': recipe_key(),
                  'sdk_sha256': sdk_lock()['sdk_sha256'], 'go_toolchain': go, 'executables': binaries}
        (output / 'base-inputs.json').write_text(json.dumps(inputs, indent=2, sort_keys=True) + '\n')
        image.download_buildroot(cache)
        for action in ('base-fetch', 'base-build'):
            image.container(source, args.container_runtime, builder, volume, cache, output, action, args.jobs, inputs=output)
        record = {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64',
                  'classification': 'managed-base-intermediate', 'fes_revision': revision,
                  'recipe_sha256': recipe_key(), 'sdk_sha256': sdk_lock()['sdk_sha256'],
                  'builder_image': builder, 'normal_runtime_admission': 'closed',
                  'files': {name: image.digest(output / name) for name in artifact_names()},
                  'hardware_acceptance': 'not-run'}
        (output / 'base.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
        verify_receipt(output)
        print(json.dumps({'status': 'passed', 'output': str(output),
                          'classification': record['classification'], 'board_boot': 'not-run'}, indent=2))
    return 0


if __name__ == '__main__':
    if len(sys.argv) != 3 or sys.argv[1] != '_post-build' or str(ROOT) != '/src':
        raise SystemExit('internal managed base post-build action only')
    post_build(sys.argv[2])
