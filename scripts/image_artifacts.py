#!/usr/bin/env python3
"""Export and select bootstrap artifacts without running component producers."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tempfile
import tomllib

BINARIES = ('mister-agent', 'mister-runtime', 'fogcast-kit', 'fogcast-tenfoot',
            'fes-kit-data', 'fes-kit-software')
BOARD = 'de10-nano'


def target_architecture(board):
    from appliance import bootstrap_architecture
    return bootstrap_architecture(board)


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def packages_at(root, records):
    from recipes import recipe_for
    result = []
    for inputs in records:
        selection = inputs['selection']
        identity = selection['package_id']
        if not re.fullmatch('[0-9a-f]{64}', identity):
            raise ValueError('invalid artifact package identity')
        recipe = recipe_for(selection['core_id'])
        if recipe.video_profiles:
            raise ValueError('bootstrap artifacts accept display firmware only; install playable cores separately')
        result.append({'directory': root / 'core-packages' / identity,
                       'selection_path': root / recipe.selection_filename, 'inputs': inputs})
    if [p['inputs']['selection']['core_id'] for p in result] != ['fes.menu']:
        raise ValueError('bootstrap artifacts require exactly fes.menu; install playable cores separately')
    return tuple(result)


def check_menu_admission(runtime, menu, board=BOARD):
    """Refuse a runtime that image assembly would refuse for this MENU package."""
    spec = importlib.util.spec_from_file_location(
        'kit_software_assembly', Path(__file__).resolve().parents[1] / 'image/scripts/kit-software.py')
    software = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(software)
    requires = software.menu_package_requirements(menu / 'manifest.toml', board)
    try:
        provides = software.runtime_menu_interfaces(runtime)
    except ValueError as error:
        raise ValueError('bootstrap mister-runtime lacks valid menu interface records; '
                         'export or pack a runtime built from current sources') from error
    if missing := software.unmet(provides, requires):
        raise ValueError('bootstrap mister-runtime does not accept its MENU interfaces: ' + ', '.join(missing))


def load(root, *, board=BOARD):
    from build import verify_package_outputs
    architecture = target_architecture(board)
    root = Path(root).resolve()
    manifest_path = root / 'artifacts.json'
    if not manifest_path.is_file():
        raise ValueError('bootstrap artifacts missing; run scripts/image_artifacts.py export --help, '
                         'then make image IMAGE_ARTIFACTS=/absolute/artifact-directory')
    if manifest_path.is_symlink() or manifest_path.stat().st_size > 1024 * 1024:
        raise ValueError('invalid bootstrap artifact manifest')
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('format') != 1:
        raise ValueError('unsupported bootstrap artifact format')
    if (manifest.get('board', BOARD) != board or
            manifest.get('architecture', 'armv7') != architecture):
        raise ValueError('bootstrap artifact board or architecture differs from the selected image')
    for component in ('fogcast', 'runtime'):
        if not re.fullmatch('[0-9a-f]{40}', manifest['provenance'][component]):
            raise ValueError('invalid bootstrap source provenance')
    if set(manifest['binaries']) != set(BINARIES):
        raise ValueError('bootstrap artifact binary set differs')
    if (root / 'bin').is_symlink():
        raise ValueError('bootstrap bin directory must not be a symlink')
    for name in BINARIES:
        path = root / 'bin' / name
        if path.is_symlink() or not path.is_file() or sha(path) != manifest['binaries'][name]:
            raise ValueError('bootstrap artifact changed: ' + name)
        with path.open('rb') as stream:
            header = stream.read(20)
        elf_class, machine = (1, 40) if architecture == 'armv7' else (2, 183)
        if header[:6] != b'\x7fELF' + bytes((elf_class, 1)) or header[18:20] != machine.to_bytes(2, 'little'):
            raise ValueError('bootstrap artifact must match the selected little-endian ARM ELF target: ' + name)
    from appliance import validate_static_arm
    for name in ('fes-kit-data', 'fes-kit-software'):
        try:
            validate_static_arm(root / 'bin' / name, architecture)
        except ValueError as error:
            raise ValueError('bootstrap helper must be a static ARM ELF: ' + name) from error
    packages = packages_at(root, manifest['packages'])
    for package in packages:
        path = package['selection_path']
        try:
            metadata = path.lstat()
        except OSError as error:
            raise ValueError('bootstrap package selection is unavailable: ' + path.name) from error
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise ValueError('bootstrap package selection must be a regular file: ' + path.name)
        if metadata.st_mode & 0o222:
            raise ValueError('bootstrap package selection is writable: ' + path.name)
    verify_package_outputs(root, packages)
    # Assembly checks this again in the target; fail before any build starts.
    check_menu_admission(root / 'bin' / 'mister-runtime', packages[0]['directory'], board)
    return manifest, packages


def stage(root, fogcast, *, board=BOARD):
    """Copy validated inputs into the private assembly snapshot, then recheck."""
    from build import publish_file
    manifest, packages = load(root, board=board)
    architecture = target_architecture(board)
    destination = fogcast / 'bin'
    destination.mkdir(exist_ok=True)
    for name in BINARIES:
        path = destination / (name + '-linux-' + architecture)
        publish_file(Path(root) / 'bin' / name, path)
        path.chmod(0o555)
        if sha(path) != manifest['binaries'][name]:
            raise ValueError('bootstrap artifact changed while staging: ' + name)
    (destination / 'kit-software.revision').write_text(manifest['provenance']['fogcast'] + '\n')
    return manifest, packages


def pack(bin_dir, menu_package, menu_selection, fogcast_revision, runtime_revision, output, *, board=BOARD):
    """Select independently produced binaries and MENU; never compile or deploy."""
    architecture = target_architecture(board)
    bin_dir, menu_package, menu_selection = map(Path, (bin_dir, menu_package, menu_selection))
    output = Path(output).absolute()
    if output.exists() or output.is_symlink():
        raise ValueError('artifact output already exists; choose a new directory')
    selection = tomllib.loads(menu_selection.read_text())
    identity = selection.get('package_id', '')
    if selection.get('core_id') != 'fes.menu' or not re.fullmatch('[0-9a-f]{64}', identity):
        raise ValueError('MENU selection is required')
    inputs = {'selection': selection, 'selection_sha256': sha(menu_selection),
              'manifest_sha256': sha(menu_package / 'manifest.toml'),
              'core_rbf_sha256': sha(menu_package / 'core.rbf')}
    if (menu_package / 'rom-map.json').exists():
        inputs['rom_map_sha256'] = sha(menu_package / 'rom-map.json')
    manifest = {'format': 1, 'board': board, 'architecture': architecture,
                'provenance': {'fogcast': fogcast_revision, 'runtime': runtime_revision},
                'binaries': {name: sha(bin_dir / name) for name in BINARIES}, 'packages': [inputs]}
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.image-artifacts-', dir=output.parent) as temporary:
        root = Path(temporary) / 'artifacts'
        (root / 'bin').mkdir(parents=True)
        for name in BINARIES:
            source = bin_dir / name
            if source.is_symlink():
                raise ValueError('binary input must not be a symlink: ' + name)
            shutil.copyfile(source, root / 'bin' / name)
            (root / 'bin' / name).chmod(0o555)
        shutil.copytree(menu_package, root / 'core-packages' / identity, symlinks=True)
        selection_copy = root / 'fes-menu.package-selection.toml'
        shutil.copyfile(menu_selection, selection_copy)
        selection_copy.chmod(0o444)
        (root / 'artifacts.json').write_text(json.dumps(manifest, sort_keys=True, indent=2) + '\n')
        load(root, board=board)
        root.rename(output)
    return manifest


def export(image_output, output):
    """Seed a portable input set from a retained, receipt-checked bootstrap image."""
    from build import recorded_image_fingerprint, reusable, verify_package_outputs
    image_output, output = Path(image_output).resolve(), Path(output).absolute()
    if output.exists() or output.is_symlink():
        raise ValueError('artifact output already exists; choose a new directory')
    info = json.loads((image_output / 'inputs.json').read_text())
    fingerprint = recorded_image_fingerprint(info)
    receipt = json.loads((image_output / 'image.json').read_text())
    if not {'linux.img', 'inputs.json'} <= receipt.get('files', {}).keys():
        raise ValueError('source image receipt must bind linux.img and inputs.json')
    if not reusable(image_output, 'image', fingerprint):
        raise ValueError('source image receipt does not match retained files')
    records = [p for p in info['fpga_packages'] if p['selection']['core_id'] == 'fes.menu']
    packages = packages_at(image_output, records)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.image-artifacts-', dir=output.parent) as temporary:
        root = Path(temporary) / 'artifacts'
        (root / 'bin').mkdir(parents=True)
        # debugfs has its own command parser. Use fixed, quoted destination paths
        # in a private working directory, never interpolate user paths into -R.
        def dump(source, destination):
            subprocess.run(['debugfs', '-R', 'dump ' + source + ' ' + destination,
                            str(image_output / 'linux.img')], cwd=root, check=True,
                           stdout=subprocess.DEVNULL)
            if not (root / destination).is_file():
                raise ValueError('source image lacks ' + source)
        dump('/usr/share/fes/software/manifest.json', 'factory.json')
        factory = json.loads((root / 'factory.json').read_text())
        for name in BINARIES:
            source = ('/usr/sbin/' if name.startswith('fes-') else '/usr/share/fes/software/bin/') + name
            dump(source, 'bin/' + name)
            (root / 'bin' / name).chmod(0o555)
        for package in packages:
            shutil.copytree(package['directory'], root / 'core-packages' / package['directory'].name)
            selection_copy = root / package['selection_path'].name
            shutil.copyfile(package['selection_path'], selection_copy)
            selection_copy.chmod(0o444)
        manifest = {'format': 1, 'provenance': factory['provenance'],
                    'source_image_sha256': sha(image_output / 'linux.img'),
                    'binaries': {name: sha(root / 'bin' / name) for name in BINARIES},
                    'packages': records}
        (root / 'factory.json').unlink()
        (root / 'artifacts.json').write_text(json.dumps(manifest, sort_keys=True, indent=2) + '\n')
        load(root)
        # Check the retained source again after extracting its bytes.
        if not reusable(image_output, 'image', fingerprint):
            raise ValueError('source image changed during artifact export')
        root.rename(output)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    command = sub.add_parser('export', help='export from a retained bootstrap image; requires debugfs')
    command.add_argument('--image-output', required=True, type=Path)
    command.add_argument('--output', required=True, type=Path)
    command = sub.add_parser('pack', help='select separately produced ARM binaries and MENU')
    command.add_argument('--board', choices=(BOARD, 'de25-nano'), default=BOARD)
    command.add_argument('--bin-dir', required=True, type=Path)
    command.add_argument('--menu-package', required=True, type=Path)
    command.add_argument('--menu-selection', required=True, type=Path)
    command.add_argument('--fogcast-revision', required=True)
    command.add_argument('--runtime-revision', required=True)
    command.add_argument('--output', required=True, type=Path)
    command = sub.add_parser('check')
    command.add_argument('--board', choices=(BOARD, 'de25-nano'), default=BOARD)
    command.add_argument('directory', type=Path)
    args = parser.parse_args()
    if args.action == 'export':
        export(args.image_output, args.output)
    elif args.action == 'pack':
        pack(args.bin_dir, args.menu_package, args.menu_selection,
             args.fogcast_revision, args.runtime_revision, args.output, board=args.board)
    else:
        load(args.directory, board=args.board)


if __name__ == '__main__':
    main()
