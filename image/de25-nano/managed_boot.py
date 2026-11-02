"""Full-system AArch64 image packaging smoke, separate from native acceptance.

Boot the selected managed rootfs and its real owner kernel on QEMU's virt
machine. No FPGA, Terasic bootloader, physical peripheral or watchdog is tested.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

import managed_base as base

ROOT, image = base.ROOT, base.image
PROFILES = {'managed-base': ('base.json', 'managed-base-intermediate'),
            'managed-factory': ('factory.json', 'managed-factory-candidate')}
FILES = ('qemu-smoke.log', 'boot.json')
OBSERVATION_SECONDS = 45
COMMAND_LINE = 'root=/dev/vda ro rootwait console=ttyAMA0,115200 init=/sbin/init fogcast_target_smoke=1'


def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def regular(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise ValueError('boot smoke requires a regular file: ' + str(path))
    return path


def recipe_key(root=ROOT):
    value = hashlib.sha256(b'FES-DE25-MANAGED-BOOT-1\n')
    for name in ('image/de25-nano/managed_boot.py', 'image/de25-nano/build.py',
                 'image/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S49fogcast-target-smoke',
                 'image/kernel/de25-dma/linux.lock.toml'):
        value.update(name.encode() + b'\0' + (Path(root) / name).read_bytes())
    return value.hexdigest()


def identity(directory, profile):
    if profile not in PROFILES:
        raise ValueError('select a managed image profile explicitly')
    inputs = json.loads(regular(directory / 'base-inputs.json').read_text())
    revision, recipe = inputs.get('fes_revision', ''), inputs.get('recipe_sha256', '')
    if (not re.fullmatch('[0-9a-f]{40}', revision) or not re.fullmatch('[0-9a-f]{64}', recipe) or
            inputs.get('profile') != ('managed-factory' if profile == 'managed-factory' else None)):
        raise ValueError('boot smoke image inputs differ from selected profile')
    return {'board': 'de25-nano', 'architecture': 'aarch64', 'profile': profile,
            'classification': PROFILES[profile][1], 'fes_revision': revision, 'recipe_sha256': recipe,
            'inputs_sha256': image.digest(regular(directory / 'base-inputs.json')),
            'rootfs_sha256': image.digest(regular(directory / 'rootfs.ext4')),
            'kernel_sha256': image.digest(regular(directory / 'kernel/Image'))}


def authenticated_image(directory, profile):
    """Verify with current policy and archived producer source read only as data."""
    if profile not in PROFILES:
        raise ValueError('select a managed image profile explicitly')
    receipt = PROFILES[profile][0]
    present = {name for name in ('base.json', 'factory.json', 'image.json')
               if (directory / name).exists() or (directory / name).is_symlink()}
    if present != {receipt}:
        raise ValueError('boot smoke requires only the selected managed image receipt')
    record = json.loads(regular(directory / receipt).read_text())
    revision = record.get('fes_revision', '')
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('boot smoke image producer is invalid')
    actual = subprocess.check_output(['git', '-C', ROOT, 'rev-parse', '--verify', '--end-of-options',
                                      revision + '^{commit}'], text=True).strip()
    if actual != revision:
        raise ValueError('boot smoke image producer commit differs')
    with tempfile.TemporaryDirectory(prefix='fes-de25-boot-source-') as temporary:
        source, archive = Path(temporary) / 'source', Path(temporary) / 'source.tar'
        source.mkdir()
        image.run(['git', '-C', ROOT, 'archive', '--format=tar', '--output', archive,
                   revision, 'image', 'scripts'])
        image.run(['tar', '-xf', archive, '-C', source, '--no-same-owner'])
        if profile == 'managed-base':
            base.verify_receipt(directory, producer_root=source)
        else:
            import managed_factory
            managed_factory.verify_receipt(directory, producer_root=source)
    return identity(directory, profile)


def verify_log(path):
    lines = regular(path).read_text(errors='replace').splitlines()
    roots = [line.removeprefix('TARGET_IMAGE_SMOKE_ROOT options=') for line in lines
             if line.startswith('TARGET_IMAGE_SMOKE_ROOT options=')]
    if (len(roots) != 1 or 'ro' not in roots[0].split(',') or 'rw' in roots[0].split(',') or
            lines.count('TARGET_IMAGE_SMOKE_VOLATILE /run /tmp /var/log writable tmpfs') != 1 or
            lines.count('TARGET_IMAGE_SMOKE_DBUS machine-id and system bus ready') != 1 or
            lines.count('TARGET_IMAGE_SMOKE_READY') != 1):
        raise ValueError('QEMU did not prove read-only root, volatile mounts and D-Bus readiness')
    for line in lines:
        if any(marker in line for marker in ('TARGET_IMAGE_SMOKE_FAILED', 'Kernel panic',
                                             'Internal error:', 'mister-main: waiting for /media/fat payloads')):
            raise ValueError('QEMU boot log contains a failed smoke or kernel diagnostic')
    return {'readonly_root': True, 'volatile_tmpfs': True, 'dbus_ready': True, 'init_smoke_ready': True}


def command(kernel, disk):
    return ['qemu-system-aarch64', '-machine', 'virt,gic-version=2', '-accel', 'tcg,thread=single',
            '-cpu', 'cortex-a53', '-m', '512M', '-nographic', '-no-reboot', '-nic', 'none',
            '-monitor', 'none', '-kernel', str(kernel),
            '-drive', 'file=' + str(disk) + ',if=none,id=root,format=raw',
            '-device', 'virtio-blk-device,drive=root', '-append', COMMAND_LINE]


def observe(argv, log, *, observation_seconds=OBSERVATION_SECONDS, terminate_seconds=10, kill_seconds=5):
    """Always reap our own QEMU child, retaining its log even when it fails."""
    with Path(log).open('xb') as stream:
        child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT)
        try:
            reason = 'guest-exited'
            try:
                child.wait(timeout=observation_seconds)
            except subprocess.TimeoutExpired:
                reason = 'observation-complete'
                child.terminate()
                try:
                    child.wait(timeout=terminate_seconds)
                except subprocess.TimeoutExpired:
                    raise ValueError('QEMU did not stop after the bounded observation')
            if child.returncode not in ((0, -15) if reason == 'observation-complete' else (0,)):
                raise ValueError('QEMU exited unsuccessfully: ' + str(child.returncode))
            return {'reason': reason, 'returncode': child.returncode}
        finally:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=kill_seconds)


def qemu_provenance():
    executable = Path(shutil.which('qemu-system-aarch64') or '/missing-qemu-system-aarch64')
    version = subprocess.check_output([executable, '--version'], text=True).splitlines()[0]
    return {'version': version, 'executable_sha256': image.digest(executable)}


def verify_report(directory, output, profile, *, producer_root=ROOT, revision=None):
    report = json.loads(regular(output / 'boot.json').read_text())
    producer = report.get('producer_revision', '')
    termination = report.get('termination', {})
    if (report.get('format') != 1 or report.get('classification') != 'aarch64-image-packaging-boot' or
            report.get('image') != identity(directory, profile) or
            report.get('recipe_sha256') != recipe_key(producer_root) or
            report.get('builder_image') != base.sdk_lock()['builder_image'] or
            not re.fullmatch('[0-9a-f]{40}', producer) or revision is not None and producer != revision or
            report.get('observation_seconds') != OBSERVATION_SECONDS or
            termination.get('reason') not in ('guest-exited', 'observation-complete') or
            termination.get('returncode') not in ((0, -15) if termination.get('reason') == 'observation-complete' else (0,)) or
            report.get('qemu_log_sha256') != image.digest(regular(output / 'qemu-smoke.log')) or
            report.get('checks') != verify_log(output / 'qemu-smoke.log') or
            report.get('normal_runtime_admission') != 'closed' or
            report.get('hardware_acceptance') != 'not-run'):
        raise ValueError('boot smoke report differs from the selected image or recipe')
    qemu = report.get('qemu', {})
    argv = report.get('argv', [])
    # Both paths are recorded, but never reopened from untrusted report data.
    if (not isinstance(qemu.get('version'), str) or not qemu['version'].startswith('QEMU emulator version ') or
            not re.fullmatch('[0-9a-f]{64}', qemu.get('executable_sha256', '')) or
            not isinstance(argv, list) or not all(isinstance(item, str) for item in argv) or
            len(argv) != len(command('', '')) or '-kernel' not in argv or '-drive' not in argv):
        raise ValueError('boot smoke emulator provenance differs')
    try:
        kernel, drive = argv[argv.index('-kernel') + 1], argv[argv.index('-drive') + 1]
    except IndexError as error:
        raise ValueError('boot smoke emulator arguments differ') from error
    if argv != command(kernel, drive.removeprefix('file=').removesuffix(',if=none,id=root,format=raw')):
        raise ValueError('boot smoke emulator arguments differ')
    return report


def boot(directory, output, profile, revision):
    for name in (*FILES, 'boot-run.json'):
        if (output / name).exists() or (output / name).is_symlink():
            raise ValueError('boot smoke preserves existing evidence; select a new output')
    selected = identity(directory, profile)
    run = {'state': 'running', 'image': selected, 'producer_revision': revision,
           'hardware_access': 'not-performed'}
    save(output / 'boot-run.json', run)
    try:
        qemu = qemu_provenance()
        with tempfile.TemporaryDirectory(prefix='.boot-smoke-', dir=output) as temporary:
            disk = Path(temporary) / 'rootfs.img'
            shutil.copyfile(directory / 'rootfs.ext4', disk)
            if image.digest(disk) != selected['rootfs_sha256']:
                raise ValueError('boot smoke disposable disk differs from selected rootfs')
            argv = command(directory / 'kernel/Image', disk)
            termination = observe(argv, output / 'qemu-smoke.log')
        checks = verify_log(output / 'qemu-smoke.log')
        if identity(directory, profile) != selected:
            raise ValueError('boot smoke input changed during execution')
        report = {'format': 1, 'classification': 'aarch64-image-packaging-boot', 'image': selected,
                  'producer_revision': revision, 'recipe_sha256': recipe_key(),
                  'builder_image': base.sdk_lock()['builder_image'], 'qemu': qemu, 'argv': argv,
                  'observation_seconds': OBSERVATION_SECONDS, 'termination': termination, 'checks': checks,
                  'qemu_log_sha256': image.digest(output / 'qemu-smoke.log'),
                  'normal_runtime_admission': 'closed', 'hardware_acceptance': 'not-run'}
        save(output / 'boot.json', report)
        verify_report(directory, output, profile, revision=revision)
        run['state'] = 'passed'
        return report
    except BaseException as error:
        (output / 'boot.json').unlink(missing_ok=True)
        run.update(state='failed', error=str(error))
        raise
    finally:
        save(output / 'boot-run.json', run)


def publish(args):
    if any(value is not None for value in (args.kernel, args.sdk, args.artifacts)):
        raise ValueError('boot-smoke reads only its recorded image inputs')
    if args.image_profile is None or args.boot_output is None:
        raise ValueError('boot-smoke requires --image-profile and --boot-output')
    directory, output = args.output.resolve(), args.boot_output.resolve()
    if output == directory or output in directory.parents or directory in output.parents:
        raise ValueError('boot smoke output overlaps the selected image')
    if args.boot_output.exists() or args.boot_output.is_symlink():
        raise ValueError('boot smoke preserves existing evidence; select a new output')
    if shutil.disk_usage(output.parent if output.parent.exists() else ROOT).free < 30 * 1024**3:
        raise ValueError('boot smoke requires at least 30 GiB free')
    revision, source = image.snapshot()
    with image.output_lock(directory):
        selected = authenticated_image(directory, args.image_profile)
        output.mkdir(parents=True)
        with image.output_lock(output):
            volume = 'fes-de25-boot-' + hashlib.sha256(str(output).encode()).hexdigest()[:16]
            builder = image.ensure_builder(source, args.container_runtime, volume)
            if builder != base.sdk_lock()['builder_image']:
                raise ValueError('boot smoke builder differs from owner-kernel lock')
            image.run([args.container_runtime, 'run', '--rm', '--name', volume,
                       '--platform', 'linux/amd64', '--network', 'none', '--cap-drop=ALL',
                       '--security-opt=no-new-privileges', '--read-only', '--tmpfs', '/tmp:rw,nosuid,nodev',
                       '--user', f'{os.getuid()}:{os.getgid()}', '--ulimit', 'core=0:0',
                       '--volume', f'{source}:/src:ro', '--volume', f'{directory}:/inputs:ro',
                       '--volume', f'{output}:/export', '--env', 'LC_ALL=C', '--env', 'TZ=UTC',
                       '--entrypoint', '/usr/bin/python3', builder,
                       '/src/image/de25-nano/managed_boot.py', '_inside', args.image_profile, revision])
            report = verify_report(directory, output, args.image_profile, revision=revision)
            if report['image'] != selected:
                raise ValueError('boot smoke image changed after authentication')
            authenticated_image(directory, args.image_profile)
    print(json.dumps({'status': 'passed', 'output': str(output),
                      'classification': report['classification'], 'hardware_acceptance': 'not-run'}))
    return 0


if __name__ == '__main__':
    if len(sys.argv) != 4 or sys.argv[1] != '_inside' or str(ROOT) != '/src':
        raise SystemExit('internal managed image boot smoke action only')
    boot(Path('/inputs'), Path('/export'), sys.argv[2], sys.argv[3])
