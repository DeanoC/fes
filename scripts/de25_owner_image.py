#!/usr/bin/env python3
"""Assemble a separate DE25 owner-candidate SD image without accessing a device."""
from __future__ import annotations

import argparse
import copy
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import de25_kernel_test as dep
from scripts import de25_kernel_platform as kernel
from scripts import de25_boot_contract as contract
from scripts import de25_first_boot as firstboot
from scripts.media_container import ensure_media_tools

build = dep.image_builder()
import boot

PROFILE = 'image/kernel/de25-platform'
IMAGE = 'fes-de25-owner.img'
RECIPE_FILES = ('scripts/de25_owner_image.py', 'scripts/media_container.py',
                'scripts/de25_kernel_test.py', 'scripts/de25_kernel_platform.py',
                'scripts/de25_boot_contract.py', 'scripts/de25_first_boot.py',
                'image/de25-nano/build.py', 'image/de25-nano/boot.py',
                'image/de25-nano/kernel_modules.py', PROFILE + '/S15fpga')


def recipe_key():
    value = hashlib.sha256(b'FES-DE25-OWNER-IMAGE-1\n')
    for name in RECIPE_FILES:
        value.update(name.encode() + b'\0' + (ROOT / name).read_bytes())
    return value.hexdigest()


def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def canonical_name(name):
    if name in ('.', './'):
        return ''
    name = name.removeprefix('./').rstrip('/')
    if (not name or not re.fullmatch(r'[A-Za-z0-9_+./@\[\]-]+', name)
            or name.startswith('/') or '..' in PurePosixPath(name).parts):
        raise ValueError('unsupported archive path: ' + name)
    return name


def members(archive):
    result = {}
    for item in archive.getmembers():
        name = canonical_name(item.name)
        if name in result:
            raise ValueError('duplicate rootfs archive path: ' + name)
        result[name] = item
    return result


def selected_kernel(directory):
    record = json.loads(boot.regular_file(directory / 'kernel-build.json').read_text())
    if (record.get('format') != 1 or record.get('release') != kernel.RELEASE
            or record.get('candidate_enabled') is not True
            or record.get('normal_runtime_admission') != 'closed'
            or record.get('kernel') != dep.selected_lock()
            or not re.fullmatch(r'[0-9a-f]{40}', record.get('fes_revision', ''))
            or set(record.get('files', {})) != {'Image', 'de25-owner.dtb', 'kernel.config', 'System.map', 'vmlinux.symvers'}):
        raise ValueError('unsupported candidate kernel receipt')
    for name, row in record['files'].items():
        boot.check_payload(directory / name, row)
    kernel.verify_config((directory / 'kernel.config').read_text())
    return record


def root_changes(base_record, kernel_record, revision):
    manifest = {'format': 1, 'classification': 'de25-owner-candidate',
                'kernel': kernel.RELEASE, 'kernel_image_sha256': kernel_record['files']['Image']['sha256'],
                'modules': [], 'built_in': ['fes_de25_dma', 'stratix10_soc', 'fpga_bridge', 'fpga_region', 'of_fpga_region'],
                'kernel_producer': kernel_record['fes_revision'], 'image_producer': revision,
                'base_producer': base_record['fes_revision'], 'normal_runtime_admission': 'closed'}
    return {'etc/fes-kernel-release': ((kernel.RELEASE + '\n').encode(), 0o644),
            'etc/init.d/S15fpga': ((ROOT / PROFILE / 'S15fpga').read_bytes(), 0o755),
            'usr/share/fes/kernel-modules.json': ((json.dumps(manifest, indent=2, sort_keys=True) + '\n').encode(), 0o644)}


def rewritten_tar(source, destination, additions, epoch):
    removed = []
    with tarfile.open(source) as old, tarfile.open(destination, 'w', format=tarfile.PAX_FORMAT) as new:
        index = members(old)
        if not set(additions) <= set(index):
            raise ValueError('expected base configuration paths missing')
        for name, item in index.items():
            if name == 'lib/modules' or name.startswith('lib/modules/'):
                removed.append((name, item.isdir()))
                continue
            if name in additions:
                continue
            new.addfile(item, old.extractfile(item) if item.isfile() else None)
        for name, (data, mode) in sorted(additions.items()):
            item = tarfile.TarInfo('./' + name)
            item.size, item.mode, item.mtime = len(data), mode, epoch
            item.uid = item.gid = 0
            item.uname = item.gname = 'root'
            new.addfile(item, io.BytesIO(data))
    if not any(name.endswith('.ko') for name, _ in removed):
        raise ValueError('base rootfs lacks its verified vendor modules')
    return removed


def run(argv):
    result = subprocess.run(list(map(str, argv)), check=True, capture_output=True)
    return result.stdout


def patch_rootfs(base, output, additions, epoch):
    # Only private regular image files are mutated. No mounts or block devices.
    shutil.copyfile(base / 'rootfs.ext4', output / 'rootfs.ext4')
    removed = rewritten_tar(base / 'rootfs.tar', output / 'rootfs.tar', additions, epoch)
    stage = output / 'patch-files'
    stage.mkdir()
    commands = []
    for name, directory in sorted(removed, key=lambda row: (row[0].count('/'), row[0]), reverse=True):
        commands.append(('rmdir /' if directory else 'rm /') + name)
    for i, (name, (data, mode)) in enumerate(sorted(additions.items())):
        source = stage / str(i)
        source.write_bytes(data)
        os.utime(source, (epoch, epoch))
        commands += ['rm /' + name, 'write ' + str(source) + ' /' + name,
                     f'set_inode_field /{name} uid 0', f'set_inode_field /{name} gid 0',
                     f'set_inode_field /{name} mode 0{0o100000 | mode:o}']
        for field in ('atime', 'mtime', 'ctime', 'crtime'):
            commands += [f'set_inode_field /{name} {field} @{epoch}', f'set_inode_field /{name} {field}_extra 0']
    script = output / 'debugfs.commands'
    script.write_text('\n'.join(commands) + '\n')
    result = subprocess.run(['debugfs', '-w', '-f', str(script), str(output / 'rootfs.ext4')],
                            check=True, capture_output=True)
    (output / 'debugfs.log').write_bytes(result.stdout + result.stderr)
    run(['e2fsck', '-fn', output / 'rootfs.ext4'])
    verify_rootfs(output, additions)


def verify_rootfs(directory, additions):
    image = directory / 'rootfs.ext4'
    files = {}
    with tarfile.open(directory / 'rootfs.tar') as archive:
        for name, item in members(archive).items():
            if name == 'lib/modules' or name.startswith('lib/modules/'):
                raise ValueError('vendor module tree survived in candidate archive')
            if item.isfile():
                data = archive.extractfile(item).read()
                if build.debugfs(image, 'cat /' + name) != data:
                    raise ValueError('candidate archive/filesystem mismatch: ' + name)
                if '/' + name in firstboot.FILES:
                    files['/' + name] = hashlib.sha256(data).hexdigest()
        for name, (data, mode) in additions.items():
            item = archive.getmember('./' + name)
            if item.uid or item.gid or item.mode != mode or archive.extractfile(item).read() != data:
                raise ValueError('candidate configuration metadata differs: ' + name)
            info = build.debugfs(image, 'stat /' + name).decode()
            if 'User:     0' not in info or 'Group:     0' not in info:
                raise ValueError('candidate file ownership differs: ' + name)
    result = subprocess.run(['debugfs', '-R', 'stat /lib/modules', str(image)], capture_output=True)
    if b'File not found' not in result.stderr:
        raise ValueError('vendor module tree survived in candidate filesystem')
    if set(files) != set(firstboot.FILES):
        raise ValueError('candidate first-boot file set differs')
    return files


def prepare_boot(base, candidate, output, epoch):
    tree = output / 'boot'
    shutil.copytree(base / 'vendor-boot', tree)
    for source, name in [('Image', 'Image'), ('de25-owner.dtb', 'socfpga_agilex5_de25_nano.dtb')]:
        (tree / name).chmod(0o644)
        shutil.copyfile(candidate / source, tree / name)
        os.utime(tree / name, (epoch, epoch))
    prefix = tree / 'vendor-boot-prefix.bin'
    prefix.chmod(0o644)
    vendor = boot.load_lock()
    offset = vendor['partitions'][0]['start_sector'] * 512
    device = str(prefix) + '@@' + str(offset)
    for name in ['Image', 'socfpga_agilex5_de25_nano.dtb']:
        run(['mcopy', '-o', '-m', '-i', device, tree / name, '::/' + name])
    selected = copy.deepcopy(vendor)
    selected['prefix_sha256'] = dep.digest(prefix)
    for row in selected['boot_files']:
        path = tree / row['name']
        row.update(size=path.stat().st_size, sha256=dep.digest(path))
    # Unmodified U-Boot/script/factory bytes are still bound to the vendor lock.
    for group in ('boot_files', 'factory_boot'):
        for old, new in zip(vendor[group], selected[group]):
            if old['name'] not in ('Image', 'socfpga_agilex5_de25_nano.dtb') and old != new:
                raise ValueError('candidate altered a vendor boot dependency')
    boot._verify_prepared_boot(tree, selected)
    extracted = output / 'fat-readback'
    extracted.mkdir()
    boot.extract_boot_files(prefix, extracted, selected)
    return selected, contract.inspect_boot(tree, selected, boot)


def inside(revision):
    base, candidate, output = Path('/base'), Path('/kernel'), Path('/export')
    base_record = build.verify_receipt(base)
    build.verify_rootfs(base)
    kernel_record = selected_kernel(candidate)
    epoch = build.lock()['source_date_epoch']
    os.environ.update(TZ='UTC', LC_ALL='C', SOURCE_DATE_EPOCH=str(epoch),
                      E2FSPROGS_FAKE_TIME=str(epoch), MTOOLSRC='/dev/null')
    additions = root_changes(base_record, kernel_record, revision)
    hashes = []
    for number in (1, 2):
        stage = output / ('pass-' + str(number))
        stage.mkdir()
        patch_rootfs(base, stage, additions, epoch)
        selected, report = prepare_boot(base, candidate, stage, epoch)
        result = boot.assemble(stage / 'boot/vendor-boot-prefix.bin', stage / 'rootfs.ext4', stage / IMAGE, selected)
        hashes.append({name: dep.digest(stage / name) for name in (IMAGE, 'rootfs.ext4', 'rootfs.tar', 'boot/vendor-boot-prefix.bin')})
    if hashes[0] != hashes[1]:
        raise ValueError('independent candidate assemblies differ')
    chosen = output / 'pass-1'
    for name in (IMAGE, 'rootfs.ext4', 'rootfs.tar', 'boot'):
        (chosen / name).rename(output / name)
    boot_report = {'format': 1, 'classification': 'host-only-boot-contract', 'board': 'de25-nano',
                   'image_producer': revision, 'image_sha256': hashes[0][IMAGE], 'boot': report}
    save(output / 'boot-contract.json', boot_report)
    record = {'format': 1, 'classification': 'de25-owner-candidate-image', 'board': 'de25-nano',
              'architecture': 'aarch64', 'fes_revision': revision, 'kernel': kernel_record, 'recipe_sha256': recipe_key(),
              'base_image_producer': base_record['fes_revision'], 'base_image_receipt_sha256': dep.digest(base / 'image.json'),
              'kernel_receipt_sha256': dep.digest(candidate / 'kernel-build.json'), 'derived_boot_layout': selected,
              'assembly_hashes': hashes, 'source_date_epoch': epoch, 'normal_runtime_admission': 'closed',
              'hardware_acceptance': 'not-run', 'files': {}}
    for name in [IMAGE, 'rootfs.ext4', 'rootfs.tar', 'boot-contract.json',
                 *('boot/' + p.relative_to(output / 'boot').as_posix() for p in sorted((output / 'boot').rglob('*')) if p.is_file())]:
        record['files'][name] = {'sha256': dep.digest(output / name), 'size': (output / name).stat().st_size}
    save(output / 'owner-image.json', record)
    expectation = {'format': 1, 'kind': 'de25-first-boot-expectation', 'boot_contract': boot_report,
                   'kernel_release': kernel.RELEASE, 'file_sha256': verify_rootfs(output, additions),
                   'image_receipt_sha256': dep.digest(output / 'owner-image.json'),
                   'rootfs_tar_sha256': dep.digest(output / 'rootfs.tar'),
                   'whole_image_observation': 'unavailable: no installed image digest marker'}
    firstboot.validate_expectation(expectation)
    save(output / 'first-boot-expectation.json', expectation)
    build.verify_receipt(base)
    selected_kernel(candidate)
    save(output / 'verify.json', verify_candidate(output))
    print('DE25_OWNER_CANDIDATE_IMAGE_PASS', flush=True)


def verify_candidate(directory):
    record = json.loads(boot.regular_file(directory / 'owner-image.json').read_text())
    assemblies = record.get('assembly_hashes')
    if (record.get('format') != 1 or record.get('classification') != 'de25-owner-candidate-image'
            or record.get('board') != 'de25-nano' or record.get('architecture') != 'aarch64'
            or record.get('recipe_sha256') != recipe_key()
            or record.get('normal_runtime_admission') != 'closed'
            or not isinstance(assemblies, list) or len(assemblies) != 2
            or assemblies[0] != assemblies[1]):
        raise ValueError('unsupported candidate image receipt or recipe')
    vendor = boot.load_lock()
    expected = {IMAGE, 'rootfs.ext4', 'rootfs.tar', 'boot-contract.json', 'boot/vendor-boot-prefix.bin'}
    expected.update('boot/' + row['name'] for row in vendor['boot_files'])
    expected.update('boot/factory-boot/' + row['name'] for row in vendor['factory_boot'])
    if set(record['files']) != expected:
        raise ValueError('candidate artifact file set differs')
    for name, row in record['files'].items():
        boot.check_payload(directory / name, row)
    assembled = {name: record['files'][name]['sha256'] for name in
                 (IMAGE, 'rootfs.ext4', 'rootfs.tar', 'boot/vendor-boot-prefix.bin')}
    if assemblies[0] != assembled:
        raise ValueError('assembly hashes differ from candidate artifacts')
    for source, name in [('Image', 'Image'), ('de25-owner.dtb', 'socfpga_agilex5_de25_nano.dtb')]:
        if record['kernel']['files'][source] != record['files']['boot/' + name]:
            raise ValueError('candidate boot payload differs from selected kernel')
    selected = record['derived_boot_layout']
    fixed = {key: value for key, value in vendor.items() if key not in ('prefix_sha256', 'boot_files')}
    if {key: value for key, value in selected.items() if key not in ('prefix_sha256', 'boot_files')} != fixed:
        raise ValueError('candidate changed vendor boot geometry/dependencies')
    for old, new in zip(vendor['boot_files'], selected['boot_files'], strict=True):
        expected_row = dict(old, **record['files']['boot/' + old['name']])
        if new != expected_row:
            raise ValueError('candidate boot layout differs from payload receipt')
        if old['name'] not in ('Image', 'socfpga_agilex5_de25_nano.dtb') and old != new:
            raise ValueError('candidate changed vendor bootloader/script')
    boot.verify_image(directory / IMAGE, directory / 'rootfs.ext4', selected)
    actual = contract.inspect_boot(directory / 'boot', selected, boot)
    report = json.loads((directory / 'boot-contract.json').read_text())
    if report['boot'] != actual or report['image_sha256'] != record['files'][IMAGE]['sha256']:
        raise ValueError('candidate boot contract differs')
    additions = root_changes({'fes_revision': record['base_image_producer']}, record['kernel'], record['fes_revision'])
    files = verify_rootfs(directory, additions)
    expected = json.loads((directory / 'first-boot-expectation.json').read_text())
    firstboot.validate_expectation(expected)
    if (expected['boot_contract'] != report or expected['file_sha256'] != files
            or expected['image_receipt_sha256'] != dep.digest(directory / 'owner-image.json')
            or expected['rootfs_tar_sha256'] != record['files']['rootfs.tar']['sha256']
            or expected['kernel_release'] != kernel.RELEASE):
        raise ValueError('candidate first-boot expectation differs')
    return {'classification': 'off-board candidate structural verification',
            'image_sha256': record['files'][IMAGE]['sha256'], 'reproducibility': 'two identical assemblies',
            'hardware_boot': 'not-run', 'normal_runtime_admission': 'closed'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verify', type=Path)
    parser.add_argument('--base', type=Path)
    parser.add_argument('--kernel', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--container-runtime', default='docker')
    parser.add_argument('--inside', help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    try:
        if args.verify:
            print(json.dumps(verify_candidate(args.verify.resolve()), sort_keys=True))
            return 0
        if args.inside:
            if ROOT != Path('/work') or not re.fullmatch('[0-9a-f]{40}', args.inside):
                raise ValueError('inside requires a committed isolated source mount')
            inside(args.inside)
            return 0
        if not all((args.base, args.kernel, args.output)):
            parser.error('--base, --kernel and a new --output are required')
        revision, source = build.snapshot()
        base, candidate, output = args.base.resolve(), args.kernel.resolve(), args.output.resolve()
        for path in (base, candidate):
            if output == path or output in path.parents or path in output.parents:
                raise ValueError('candidate output must be separate from inputs')
        build.verify_receipt(base)
        selected_kernel(candidate)
        existing = output.parent
        while not existing.exists():
            existing = existing.parent
        if shutil.disk_usage(existing).free < 30 << 30:
            raise ValueError('candidate assembly requires at least 30 GiB free')
        output.mkdir(parents=True, exist_ok=False)
        tools = ensure_media_tools(source, args.container_runtime)
        save(output / 'tools.json', {'image': tools, 'role': 'pinned unprivileged media tools'})
        name = 'fes-de25-owner-image-' + hashlib.sha256(str(output).encode()).hexdigest()[:16]
        command = [args.container_runtime, 'run', '--rm', '--name', name, '--network', 'none',
                   '--cap-drop=ALL', '--security-opt=no-new-privileges', '--entrypoint', 'python3',
                   '--volume', f'{source}:/work:ro', '--volume', f'{base}:/base:ro',
                   '--volume', f'{candidate}:/kernel:ro', '--volume', f'{output}:/export',
                   tools, '/work/scripts/de25_owner_image.py', '--inside', revision]
        kernel.run_container(command, args.container_runtime, name, output, 1800)
        return 0
    except (OSError, ValueError, KeyError, subprocess.SubprocessError, tarfile.TarError) as error:
        parser.exit(1, 'de25-owner-image: ' + str(error) + '\n')


if __name__ == '__main__':
    raise SystemExit(main())
