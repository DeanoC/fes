"""DE25 factory assembly using the shared native package and software builders.

Produces an uninstalled factory rootfs candidate, not a card or hardware pass.
The ordinary package selector remains responsible for admitting sealed MENU.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import tomllib

import managed_base as base
from scripts import image_artifacts

ROOT, image = base.ROOT, base.image
PROFILE = 'managed-factory'
NATIVE = ROOT / 'image/buildroot/board/fogcast-target'
EXTRA = ROOT / 'image/scripts/native-extra-cores.sh'
INPUTS = 'base-inputs.json'
LOCK = 'native-runtime.inputs.lock.toml'


def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def recipe_key(root=ROOT):
    value = hashlib.sha256(b'FES-DE25-MANAGED-FACTORY-1\n' + base.recipe_key(root).encode())
    for name in ('image/scripts/native-extra-cores.sh', 'image/scripts/native-video-parts.sh',
                 'image/scripts/verify-native-runtime-inputs.sh', 'scripts/image_artifacts.py',
                 'scripts/build.py', 'scripts/recipes.py'):
        value.update(name.encode() + b'\0' + (Path(root) / name).read_bytes())
    return value.hexdigest()


def build_key(inputs):
    return 'factory-' + hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()


def selected(directory):
    return image_artifacts.load(directory, board='de25-nano')


def environment(directory, fogcast, cache):
    manifest, packages = selected(directory)
    package = packages[0]
    env = base.build_environment()
    for key in list(env):
        if key.startswith(('FES_', 'NATIVE_RUNTIME_', 'TARGET_IMAGE_', 'LIBMISTER_')) or key == 'FOGCAST_DIR':
            env.pop(key)
    env.update(FOGCAST_DIR=str(fogcast), FES_TARGET_BOARD='de25-nano',
               FES_PREBUILT_RUNTIME_SHA256=manifest['binaries']['mister-runtime'],
               NATIVE_RUNTIME_MODE='package-only', FES_PACKAGE_IDS='fes.menu',
               FES_MENU_PACKAGE_DIR=str(package['directory']),
               FES_MENU_PACKAGE_SELECTION=str(package['selection_path']),
               TARGET_IMAGE_LOCK_BIN=str(fogcast / 'bin/target-image-lock-linux-amd64'),
               NATIVE_RUNTIME_INPUT_LOCK=str(Path(directory).parent / LOCK),
               NATIVE_RUNTIME_IDLE_FILE=str(cache / 'idle.rbf'),
               NATIVE_RUNTIME_CACHE=str(cache))
    return env


def runtime_lock(directory):
    manifest, packages = selected(directory)
    package = packages[0]['directory']
    fields = tomllib.loads((package / 'manifest.toml').read_text())
    provenance = fields.get('build', {})
    repository, revision = provenance.get('repository'), provenance.get('revision', '')
    if not isinstance(repository, str) or not repository or not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('DE25 MENU requires its sealed build repository and revision')
    idle = {'repository': repository, 'commit': revision,
            'path': 'core-packages/' + package.name + '/core.rbf',
            'sha256': image.digest(package / 'core.rbf'), 'size': (package / 'core.rbf').stat().st_size}
    sections = {'mister_runtime': {'commit': manifest['provenance']['runtime'], 'mount_path': '/runtime-source'},
                'splash_rbf': dict(idle, fat_destination='/idle.rbf'),
                'idle_rbf': dict(idle, install_path='/usr/share/mister-runtime/idle.rbf')}
    # JSON string quoting is valid for TOML basic strings and keeps provenance data inert.
    return 'format = 1\n' + ''.join('\n[' + name + ']\n' + ''.join(
        key + ' = ' + json.dumps(value) + '\n' for key, value in section.items())
        for name, section in sections.items())


def prepare_packages(directory, fogcast, cache):
    env = environment(directory, fogcast, cache)
    cache.mkdir(parents=True, exist_ok=True)
    idle = cache / 'idle.rbf'
    selected_idle = Path(env['FES_MENU_PACKAGE_DIR']) / 'core.rbf'
    if idle.exists() or idle.is_symlink():
        if idle.is_symlink() or not idle.is_file() or image.digest(idle) != image.digest(selected_idle):
            raise ValueError('factory package cache idle differs from selected MENU')
    else:
        shutil.copyfile(selected_idle, idle)
        idle.chmod(0o444)
    image.run([EXTRA, 'fetch', cache], env=env)
    image.run([ROOT / 'image/scripts/verify-native-runtime-inputs.sh',
               env['NATIVE_RUNTIME_INPUT_LOCK'], '/unused-prebuilt-runtime', idle, idle], env=env)
    return env


def post_build(target, inputs):
    if inputs.get('recipe_sha256') != recipe_key():
        raise ValueError('factory post-build inputs differ from selected source')
    image.run([NATIVE / 'native-post-build.sh', target])
    save(target / 'usr/share/fes/managed-factory.json', {
        'format': 1, 'classification': 'managed-factory-candidate',
        'inputs': inputs, 'normal_runtime_admission': 'closed', 'hardware_acceptance': 'not-run'})


def verify_rootfs(directory, env):
    inputs = json.loads((directory / INPUTS).read_text())
    manifest, _ = selected(directory / 'artifacts')
    if inputs.get('profile') != PROFILE or inputs.get('artifacts') != manifest:
        raise ValueError('factory inputs differ from selected artifacts')
    if inputs.get('kernel_image_sha256') != image.digest(directory / 'kernel/Image'):
        raise ValueError('factory inputs differ from selected kernel')
    image.run(['e2fsck', '-fn', directory / 'rootfs.ext4'], stdout=subprocess.DEVNULL)
    contract = base.load(ROOT / 'image/scripts/kit-software.py', 'de25_factory_contract')
    with tempfile.TemporaryDirectory(prefix='fes-de25-factory-') as temporary:
        target = Path(temporary)
        image.run(['tar', '-xf', directory / 'rootfs.tar', '-C', target, '--no-same-owner',
                   '--exclude=./dev/*', '--exclude=dev/*'])
        if (target / 'etc/fes-board').read_bytes() != b'de25-nano\n':
            raise ValueError('factory board identity differs')
        marker = json.loads((target / 'usr/share/fes/managed-factory.json').read_text())
        if marker != {'format': 1, 'classification': 'managed-factory-candidate', 'inputs': inputs,
                      'normal_runtime_admission': 'closed', 'hardware_acceptance': 'not-run'}:
            raise ValueError('factory embedded source inputs differ')
        if (target / '.fes-bootstrap').is_symlink() or not (target / '.fes-bootstrap').is_dir():
            raise ValueError('factory bootstrap mount point is not a directory')
        if not re.search(rb'Type:\s+directory\b', image.debugfs(directory / 'rootfs.ext4', 'stat /.fes-bootstrap')):
            raise ValueError('factory ext4 bootstrap mount point is not a directory')
        base.verify_dbus_directory(target, directory / 'rootfs.ext4')
        startup = target / 'etc/init.d/S40fes-kit-software'
        if (startup.is_symlink() or startup.stat().st_mode & 0o777 != 0o755 or
                startup.read_bytes() != (NATIVE / 'native-rootfs-overlay/etc/init.d' / startup.name).read_bytes()):
            raise ValueError('factory managed startup differs')
        image.run([EXTRA, 'verify-image', env['NATIVE_RUNTIME_CACHE'], target], env=env)
        software = target / 'usr/share/fes/software'
        image.run([Path(env['FOGCAST_DIR']) / 'bin/fes-kit-bundle-linux-amd64', 'verify-directory',
                   '--root', software, '--base', target / 'etc/fes/software-base.json'])
        executables = [software / 'bin' / name for name in contract.BINARIES]
        actual = contract.base_contract(target, executables, board='de25-nano')
        recorded = json.loads((target / 'etc/fes/software-base.json').read_text())
        requires = recorded.pop('requires', None)
        if recorded != actual or requires != contract.menu_requirements(target, 'de25-nano'):
            raise ValueError('factory base or MENU contract differs')
        checked = set(executables)
        checked.update(contract.resolve(target, '/lib/' + name) for name in base.LIBRARIES)
        for name in contract.BINARIES:
            if image.digest(software / 'bin' / name) != manifest['binaries'][name]:
                raise ValueError('factory executable differs: ' + name)
            alias = '/run/fogcast/software/current/bin/' + name
            path = target / 'usr/sbin' / name
            if not path.is_symlink() or os.readlink(path) != alias:
                raise ValueError('factory executable alias differs: ' + name)
            record = image.debugfs(directory / 'rootfs.ext4', 'stat /usr/sbin/' + name)
            if not re.search(rb'Fast link dest:\s*"' + re.escape(alias.encode()) + rb'"', record):
                raise ValueError('factory ext4 executable alias differs: ' + name)
        for name in ('fes-kit-software', 'fes-kit-data', 'fes-bluetooth'):
            path = target / 'usr/sbin' / name
            if path.is_symlink() or image.digest(path) != inputs['executables'][name]:
                raise ValueError('factory base helper differs: ' + name)
            checked.add(path)
        for name in ('etc/fes-board', 'etc/fes/software-base.json', 'etc/fstab', 'etc/inittab',
                     'etc/mdev.conf', 'etc/init.d/S15fpga', 'etc/init.d/S20mister-network',
                     'etc/init.d/S30mister-dropbear', 'etc/init.d/S40fes-kit-software',
                     'usr/share/fes/managed-factory.json', 'usr/share/fes/kernel-modules.json',
                     'usr/share/mister-runtime/idle.rbf'):
            checked.add(target / name)
        checked.update(p for p in software.rglob('*') if p.is_file() and not p.is_symlink())
        checked.update(p for p in (target / 'usr/share/mister-runtime/core-packages').rglob('*') if p.is_file())
        if (target / 'usr/share/fes/managed-base.json').exists() or (target / 'lib/modules').exists():
            raise ValueError('factory contains stale base metadata or external kernel modules')
        configured = (directory / 'buildroot.config').read_text().splitlines()
        for required in ('BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y', 'BR2_ROOTFS_MERGED_USR=y',
                         '# BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW is not set'):
            if required not in configured:
                raise ValueError('factory Buildroot policy differs: ' + required)
        for path in sorted(checked):
            name = '/' + path.relative_to(target).as_posix()
            if image.debugfs(directory / 'rootfs.ext4', 'cat ' + name) != path.read_bytes():
                raise ValueError('factory tar/ext4 mismatch: ' + name)
        return {'classification': 'managed-factory-candidate', 'board': 'de25-nano',
                'base_contract': actual, 'checked_tar_ext4_files': len(checked),
                'hardware_acceptance': 'not-run', 'normal_runtime_admission': 'closed'}


def inside(action, jobs):
    output = Path('/export')
    inputs = json.loads((output / INPUTS).read_text())
    env = prepare_packages(output / 'artifacts', ROOT / 'sources/FogCast',
                           Path('/build') / build_key(inputs) / 'native')
    if action != 'factory-verify':
        base.prepare_sdk(build_key(inputs))
        _, work, command, build_env = image.prepare_buildroot(managed=True, factory_key=build_key(inputs))
        build_env.update(env)
        image.run([*command, 'source' if action == 'factory-fetch' else 'all', f'-j{jobs}'], env=build_env)
        if action == 'factory-fetch':
            return
        image.run([*command, 'sdk', f'-j{jobs}'], env=build_env)
        for name in ('rootfs.ext4', 'rootfs.tar'):
            shutil.copyfile(work / 'images' / name, output / name)
        sdks = list((work / 'images').glob('*_sdk-buildroot.tar.gz'))
        if len(sdks) != 1:
            raise ValueError('factory must export exactly one SDK')
        base.export_sdk(sdks[0], output / 'aarch64-sdk.tar.gz', work / 'host')
        shutil.copyfile(work / '.config', output / 'buildroot.config')
    save(output / 'factory-verify.json', verify_rootfs(output, env))
    import managed_boot
    if action == 'factory-build':
        managed_boot.boot(output, output, PROFILE, inputs['fes_revision'])
    managed_boot.verify_report(output, output, PROFILE, revision=inputs['fes_revision'])


def artifact_names(directory):
    manifest, packages = selected(directory / 'artifacts')
    names = list(base.artifact_names()) + [LOCK, 'artifacts/artifacts.json', 'boot.json', 'qemu-smoke.log']
    names += ['artifacts/bin/' + name for name in image_artifacts.BINARIES]
    names += ['artifacts/' + p.relative_to(directory / 'artifacts').as_posix()
              for package in packages for p in [package['selection_path'], *package['directory'].iterdir()]]
    return names


def verify_receipt(output, *, producer_root=None):
    record = json.loads((output / 'factory.json').read_text())
    inputs = json.loads((output / INPUTS).read_text())
    if (record.get('format') != 1 or record.get('classification') != 'managed-factory-candidate' or
            record.get('board') != 'de25-nano' or record.get('architecture') != 'aarch64' or
            record.get('recipe_sha256') != recipe_key(ROOT if producer_root is None else producer_root) or
            record.get('builder_image') != base.sdk_lock()['builder_image'] or
            record.get('normal_runtime_admission') != 'closed' or
            not re.fullmatch('[0-9a-f]{40}', record.get('fes_revision', '')) or
            record.get('fes_revision') != inputs.get('fes_revision') or
            inputs.get('recipe_sha256') != record.get('recipe_sha256') or
            inputs.get('sdk_sha256') != base.sdk_lock()['sdk_sha256'] or
            set(record.get('files', {})) != set(artifact_names(output))):
        raise ValueError('factory receipt differs from selected source or profile')
    for name, expected in record['files'].items():
        path = output / name
        if path.is_symlink() or not path.is_file() or image.digest(path) != expected:
            raise ValueError('factory artifact differs: ' + name)
    base.owner.selected_kernel(output / 'kernel')
    import managed_boot
    managed_boot.verify_report(output, output, PROFILE,
                              producer_root=ROOT if producer_root is None else producer_root,
                              revision=record['fes_revision'])
    return record


def publish(args):
    verify = args.action == 'verify-factory'
    if verify:
        if any(value is not None for value in (args.kernel, args.sdk, args.artifacts)):
            raise ValueError('verify-factory reads only its recorded inputs')
        verify_receipt(args.output)
    else:
        if any(value is None for value in (args.kernel, args.sdk, args.artifacts)):
            raise ValueError('managed-factory requires --kernel, --sdk and --artifacts')
        selected(args.artifacts)
        if args.output.exists() or args.output.is_symlink():
            raise ValueError('factory output already exists; choose a new directory')
    output, cache = args.output.resolve(), args.cache.resolve()
    for path in (args.kernel, args.sdk, args.artifacts):
        if path is not None and (output == path.resolve() or output in path.resolve().parents or path.resolve() in output.parents):
            raise ValueError('factory output overlaps an input')
    if shutil.disk_usage(output.parent if output.parent.exists() else ROOT).free < 30 * 1024**3:
        raise ValueError('factory build requires at least 30 GiB free')
    revision, source = image.snapshot()
    output.mkdir(parents=True, exist_ok=verify)
    fogcast = source / 'sources/FogCast'
    with image.build_lock(cache), image.output_lock(output):
        image.validate_output(output)
        env = base.build_environment()
        image.run(['make', '-C', fogcast, 'build-fes-kit-bundle-container',
                   'build-target-image-lock-container', 'REVISION=' + revision], env=env)
        if not verify:
            manifest, packages = selected(args.artifacts)
            image_artifacts.pack(args.artifacts / 'bin', packages[0]['directory'], packages[0]['selection_path'],
                                 manifest['provenance']['fogcast'], manifest['provenance']['runtime'],
                                 output / 'artifacts', board='de25-nano')
        image_artifacts.stage(output / 'artifacts', fogcast, board='de25-nano')
        if not verify:
            (output / LOCK).write_text(runtime_lock(output / 'artifacts'))
        # The real selector must admit the package before any OS or compiler build.
        prepare_packages(output / 'artifacts', fogcast, output / 'package-preflight')
        volume = 'fes-de25-factory-' + hashlib.sha256(str(output).encode()).hexdigest()[:16]
        builder = image.ensure_builder(source, args.container_runtime, volume)
        if builder != base.sdk_lock()['builder_image']:
            raise ValueError('factory builder differs from owner-kernel lock')
        if not verify:
            base.stage_kernel_sdk(args.kernel, args.sdk, output, cache)
            base.platform_build.build_bluetooth(source, fogcast, env, 'aarch64')
            manifest, _ = selected(output / 'artifacts')
            inputs = {'profile': PROFILE, 'fes_revision': revision, 'recipe_sha256': recipe_key(),
                      'sdk_sha256': base.sdk_lock()['sdk_sha256'], 'artifacts': manifest,
                      'kernel_image_sha256': image.digest(output / 'kernel/Image'),
                      'executables': {name: image.digest(fogcast / 'bin' / filename)
                                      for name, filename in base.BINARIES.items()}}
            save(output / INPUTS, inputs)
            image.download_buildroot(cache)
        for action in (('factory-verify',) if verify else ('factory-fetch', 'factory-build')):
            image.container(source, args.container_runtime, builder, volume, cache, output,
                            action, args.jobs, inputs=output)
        if not verify:
            save(output / 'factory.json', {'format': 1, 'board': 'de25-nano', 'architecture': 'aarch64',
                 'classification': 'managed-factory-candidate', 'fes_revision': revision,
                 'recipe_sha256': recipe_key(), 'builder_image': builder,
                 'normal_runtime_admission': 'closed', 'hardware_acceptance': 'not-run',
                 'files': {name: image.digest(output / name) for name in artifact_names(output)}})
        verify_receipt(output)
        print(json.dumps({'status': 'passed', 'classification': 'managed-factory-candidate',
                          'output': str(output), 'card_assembly': 'not-run', 'hardware_acceptance': 'not-run'}))
    return 0
