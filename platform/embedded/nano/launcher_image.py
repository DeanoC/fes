#!/usr/bin/env python3
"""Assemble a fresh native launcher SD image; regular files only, no device IO."""
import argparse
import hashlib
import json
import re
from pathlib import Path, PurePosixPath
import shutil
import stat
import struct
import subprocess

from package import checked_native, sha
from cold_profile import EXPERIMENTAL_FILE, MAX_FIT_SIZE
from upload import load_kit, PREFIX_SIZE
from recovery_return_package import load_kit as load_return_trace_kit

ROOT = Path(__file__).resolve().parents[3]
CONTROL = {'FES/CORESEL.JSN', 'FES/UI.JSON', 'FES/INSTALL.JSN',
           'FES/MENU.FCP', 'FES/MENU.JSN', 'FES/MENUSEL.JSN', 'FES/PREFS.JSON'}
PROTECTED = {'u-boot.itb', 'FES/RECOVERY.JSON', EXPERIMENTAL_FILE, 'FES/BOOTCHAIN.JSON'}


def regular(path, limit):
    if any(p.is_symlink() for p in (path, *path.parents)) or not stat.S_ISREG(path.stat().st_mode):
        raise ValueError('regular non-symlink input required: '+str(path))
    if not 0 < path.stat().st_size <= limit:
        raise ValueError('input exceeds its bound: '+str(path))
    return path.read_bytes()


def record(raw):
    def unique(pairs):
        value = {}
        for name, item in pairs:
            if name in value:
                raise ValueError('duplicate launcher manifest field')
            value[name] = item
        return value
    def nonfinite(_):
        raise ValueError('nonfinite launcher manifest value')
    return json.loads(raw, object_pairs_hook=unique, parse_constant=nonfinite)


def staging(directory):
    """Accept the closed fresh staging output, never a pre-existing library."""
    payload = {}
    for path in directory.rglob('*'):
        if path.is_symlink():
            raise ValueError('symlink in launcher staging')
        if path.is_dir():
            continue
        name = path.relative_to(directory).as_posix()
        parts = PurePosixPath(name).parts
        if not re.fullmatch(r'FES/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_.-]+', name) or any(p in ('.', '..') or p.endswith('.') for p in parts):
            raise ValueError('launcher files must stay below FES/')
        payload[name] = regular(path, 64 << 20)
    if not CONTROL <= payload.keys() or sum(map(len, payload.values())) > 80 << 20:
        raise ValueError('complete bounded fresh launcher staging required')
    folded = {name.casefold() for name in payload}
    if len(folded) != len(payload) or folded & {name.casefold() for name in PROTECTED}:
        raise ValueError('ambiguous or protected launcher destination')
    for name in payload:
        if any(str(p).casefold() in folded for p in PurePosixPath(name).parents):
            raise ValueError('launcher file/directory alias')
    for name in ('FES/CORESEL.JSN', 'FES/UI.JSON', 'FES/INSTALL.JSN', 'FES/MENUSEL.JSN'):
        if len(payload[name]) > 8192:
            raise ValueError('launcher manifest exceeds its native bound')
    bootstrap = record(payload['FES/CORESEL.JSN'])
    ui = record(payload['FES/UI.JSON'])
    if not isinstance(bootstrap, dict) or not isinstance(ui, dict) or type(bootstrap.get('format')) is not int or type(ui.get('format')) is not int:
        raise ValueError('integer bootstrap formats required')
    if bootstrap != {'format': 1, 'scope': 'nano-program-selection-v1', 'programs': []} or \
            ui != {'format': 1, 'scope': 'nano-tenfoot-catalog-v1', 'entries': []}:
        raise ValueError('fresh launcher requires the empty bootstrap')
    prefs = record(payload['FES/PREFS.JSON'])
    if not isinstance(prefs, dict) or type(prefs.get('attract_enabled')) is not bool or len(payload['FES/PREFS.JSON']) != 65536 or prefs != {'home': 'library', 'attract_enabled': False}:
        raise ValueError('fresh launcher requires preallocated default preferences')
    catalog = record(payload['FES/INSTALL.JSN'])
    if (not isinstance(catalog, dict) or set(catalog) != {'format', 'scope', 'programs'} or
            type(catalog['format']) is not int or catalog['format'] != 1 or
            catalog['scope'] != 'nano-staged-program-catalog-v1' or
            not isinstance(catalog['programs'], list) or not 1 <= len(catalog['programs']) <= 32):
        raise ValueError('explicit bounded install catalog required')
    required = set(CONTROL)
    for program in catalog['programs']:
        if not isinstance(program, dict) or not isinstance(program.get('files'), dict):
            raise ValueError('complete install source file inventory required')
        for name, expected in program['files'].items():
            if name not in payload or name in CONTROL or hashlib.sha256(payload[name]).hexdigest() != expected:
                raise ValueError('staged install input differs')
            required.add(name)
    menus = record(payload['FES/MENUSEL.JSN'])
    if (not isinstance(menus, dict) or set(menus) != {'format', 'scope', 'programs'} or
            type(menus['format']) is not int or menus['format'] != 1 or menus['scope'] != 'nano-program-selection-v1' or
            not isinstance(menus['programs'], list) or len(menus['programs']) != 1 or
            not isinstance(menus['programs'][0], dict)):
        raise ValueError('single fixed menu selection required')
    menu = menus['programs'][0]
    if (set(menu) != {'path', 'package_id', 'metadata_path', 'metadata_sha256'} or
            menu['path'] != 'FES/MENU.FCP' or menu['metadata_path'] != 'FES/MENU.JSN' or
            menu['metadata_sha256'] != hashlib.sha256(payload['FES/MENU.JSN']).hexdigest()):
        raise ValueError('menu selection differs from fixed staged files')
    if payload.keys() != required:
        raise ValueError('extra files in fresh launcher staging')
    return payload


def verify_protected_layout(disk, image, prefix_sha256):
    with disk.open('rb') as current, image.open('rb') as original:
        prefix = original.read(PREFIX_SIZE)
        if hashlib.sha256(prefix).hexdigest() != prefix_sha256 or current.read(PREFIX_SIZE) != prefix:
            raise ValueError('protected SD prefix changed')
        boot_sector = original.read(512)
        if len(boot_sector) != 512 or current.read(512) != boot_sector:
            raise ValueError('FAT boot sector changed')
        backup = struct.unpack_from('<H', boot_sector, 50)[0]
        if not 0 < backup < struct.unpack_from('<H', boot_sector, 14)[0]:
            raise ValueError('invalid FAT backup sector')
        current.seek(PREFIX_SIZE+backup*512); original.seek(PREFIX_SIZE+backup*512)
        if original.read(512) != boot_sector or current.read(512) != boot_sector:
            raise ValueError('FAT backup boot sector changed')


def verify_prepared_image(disk, receipt, inputs, fit_data, output, run):
    """Check the installed files against the pinned staging and recovery bytes."""
    staging_dir, = [path.parents[1] for path in inputs if path.parts[-2:] == ('FES', 'CORESEL.JSN')]
    template, = [path for path in inputs if path.name == 'nano-recovery-sd.img']
    staged = staging(staging_dir)
    if any(inputs.get(staging_dir/name) != hashlib.sha256(data).hexdigest() for name, data in staged.items()):
        raise ValueError('launcher staging differs from its pinned input inventory')
    payload = dict(staged, **{EXPERIMENTAL_FILE: fit_data})
    if receipt.get('files') != {name: dict(bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
                               for name, data in payload.items()}:
        raise ValueError('launcher file receipt differs from pinned payloads')
    for name, source in (('u-boot.itb', 'u-boot.itb'), ('FES/RECOVERY.JSON', 'RECOVERY.JSON')):
        path = template.with_name(source)
        data = regular(path, 8 << 20)
        if inputs.get(path) != hashlib.sha256(data).hexdigest():
            raise ValueError('unbound protected recovery file: '+source)
        payload[name] = data
    if disk.stat().st_size != template.stat().st_size:
        raise ValueError('launcher disk geometry differs from the recovery template')
    verify_protected_layout(disk, template, receipt['protected_prefix_sha256'])

    fat = str(disk)+'@@'+str(PREFIX_SIZE)
    listing = run('sd-directory', ['mdir', '-a', '-b', '-s', '-i', fat, '::/']).read_text().splitlines()
    directories = {parent.as_posix()+'/' for name in payload for parent in PurePosixPath(name).parents
                   if parent != PurePosixPath('.')}
    expected = {'::/'+name.casefold() for name in (*payload, *directories)}
    if len(listing) != len(expected) or {name.casefold() for name in listing} != expected:
        raise ValueError('launcher disk has missing, extra or aliased FAT entries')
    for index, (name, data) in enumerate(sorted(payload.items())):
        target = output/('sd-readback-'+str(index))
        run('sd-readback-'+str(index), ['mcopy', '-i', fat, '::'+name, target])
        if regular(target, len(data)) != data:
            raise ValueError('launcher disk payload differs: '+name)


def prepare(uboot, entry, sdk, recovery, files, output, *, recovery_profile='standard', recovery_source_root=None):
    if recovery_profile not in ('standard', 'return-trace') or (recovery_profile == 'standard' and recovery_source_root is not None):
        raise ValueError('explicit supported recovery profile and producer root required')
    def checked_recovery():
        return (load_kit(recovery) if recovery_profile == 'standard' else
                load_return_trace_kit(recovery, source_root=recovery_source_root))
    if ('..' in output.parts or output.exists() or not output.is_relative_to(ROOT/'out') or
            any(p.is_symlink() for p in (output, *output.parents))):
        raise ValueError('output must be a fresh private directory below out/')
    payload = staging(files)
    for name in ('result.json', 'RECOVERY.JSON', 'u-boot.itb', 'nano-recovery-sd.img'):
        path = recovery/name
        if any(p.is_symlink() for p in (path, *path.parents)) or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('regular recovery input required')
    receipt_inputs = {str(p): hashlib.sha256(regular(p, limit)).hexdigest() for p, limit in (
        (uboot/'result.json', 1 << 20), (entry/'result.json', 1 << 20),
        (sdk/'result.json', 1 << 20), (recovery/'result.json', 131072),
        (recovery/'RECOVERY.JSON', 16384), (recovery/'u-boot.itb', 8 << 20))}
    receipt_inputs.update({str(p): sha(p) for p in (Path(__file__), Path(__file__).with_name('package.py'),
                          Path(__file__).with_name('upload.py'), Path(__file__).with_name('cold_profile.py'))})
    base = checked_recovery()
    if recovery_profile == 'return-trace':
        for name in ('recovery_return_package.py', 'recovery_return_trace.py', 'recovery_package.py', 'recovery.py'):
            path = Path(__file__).with_name(name)
            receipt_inputs[str(path)] = sha(path)
    image = recovery/'nano-recovery-sd.img'
    receipt_inputs[str(image)] = base['image_sha256']
    fit, _, _, boot, loaded = checked_native(uboot, entry)[:5]
    native = boot['native_handoff']
    selected_raw = regular(sdk/'result.json', 1 << 20)
    selected = record(selected_raw)
    if (hashlib.sha256(selected_raw).hexdigest() != loaded.get('native_build_sha256') or
            selected.get('status') != 'cross-built' or selected.get('program') != 'fpgaload' or
            selected.get('elf_sha256') != loaded.get('payload_sha256') or
            selected.get('usb_shared') is not True or selected.get('usb_input') != 'boot-keyboard' or
            selected.get('hps_menu_program') != loaded.get('hps_menu_program') or
            not isinstance(selected.get('hps_menu_program'), dict) or
            native.get('startup', {}).get('mode') != 'autoboot'):
        raise ValueError('matched autoboot, paired menu and shared keyboard SDK required')
    plan = selected.get('writable_storage_plan')
    with image.open('rb') as source:
        mbr = source.read(512)
    if (not isinstance(plan, dict) or plan.get('scope') != 'nano-owned-fat-storage-v1' or
            plan.get('mbr_sha256') != hashlib.sha256(mbr).hexdigest() or
            plan.get('fit_sha256') != base['fit_sha256'] or plan.get('fit_bytes') != len(base['fit']) or
            plan.get('recovery_sha256') != hashlib.sha256(base['manifest']).hexdigest() or
            plan.get('recovery_bytes') != len(base['manifest'])):
        raise ValueError('compiled writable plan differs from the protected recovery media')
    # The SDK itself carries the exact checked companion files. Do not admit a
    # separately substituted menu even when its generic archive is well formed.
    for name in ('MENU.FCP', 'MENU.JSN', 'MENUSEL.JSN'):
        expected = selected.get('menu_assets', {}).get(name, {})
        data = payload['FES/'+name]
        if expected.get('size') != len(data) or expected.get('sha256') != hashlib.sha256(data).hexdigest():
            raise ValueError('staged menu differs from the selected SDK')
    fit_data = regular(fit, MAX_FIT_SIZE)
    if hashlib.sha256(fit_data).hexdigest() != boot.get('fit_sha256'):
        raise ValueError('native FIT changed after admission')
    inputs = dict(receipt_inputs)
    inputs[str(fit)] = boot['fit_sha256']
    inputs.update({str(files/name): hashlib.sha256(data).hexdigest() for name, data in payload.items()})
    if any(sha(Path(name)) != expected for name, expected in inputs.items()):
        raise ValueError('launcher inputs changed before assembly')
    output.mkdir(mode=0o700, parents=True)
    disk = output/'native-launcher-sd.img'
    shutil.copyfile(image, disk)
    fat = str(disk)+'@@'+str(PREFIX_SIZE)
    commands = []

    def run(label, argv):
        argv = [str(a) for a in argv]
        commands.append(argv)
        with (output/(label+'.log')).open('xb') as log:
            subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=120)

    sources = output/'payload'
    readback = output/'readback'
    sources.mkdir(mode=0o700); readback.mkdir(mode=0o700)
    payload[EXPERIMENTAL_FILE] = fit_data
    directories = {str(p) for name in payload for p in PurePosixPath(name).parents if str(p) not in ('.', 'FES')}
    for i, directory in enumerate(sorted(directories, key=lambda p: (p.count('/'), p))):
        run('directory-'+str(i), ['mmd', '-i', fat, '::'+directory])
    for i, (name, data) in enumerate(sorted(payload.items())):
        source = sources/str(i)
        source.write_bytes(data)
        run('write-'+str(i), ['mcopy', '-i', fat, source, '::'+name])
        target = readback/str(i)
        run('read-'+str(i), ['mcopy', '-i', fat, '::'+name, target])
        if target.read_bytes() != data:
            raise ValueError('launcher file readback differs')
    for i, (name, expected) in enumerate((('u-boot.itb', base['fit']), ('FES/RECOVERY.JSON', base['manifest']))):
        target = readback/('protected-'+str(i))
        run('protected-'+str(i), ['mcopy', '-i', fat, '::'+name, target])
        if target.read_bytes() != expected:
            raise ValueError('protected recovery file changed')
    verify_protected_layout(disk, image, base['prefix_sha256'])
    if any(sha(Path(name)) != expected for name, expected in inputs.items()):
        raise ValueError('launcher inputs changed during assembly')
    checked_native(uboot, entry)
    if checked_recovery() != base:
        raise ValueError('recovery package changed during image assembly')
    result = dict(format=1, status='prepared', classification='regular-file native launcher SD image; hardware unqualified',
                  recovery_profile=recovery_profile,
                  recovery_source_root=None if recovery_source_root is None else str(recovery_source_root),
                  native_boot_id=native['build_id'], startup=native['startup'], preferences='sd-checkpoint',
                  image_sha256=sha(disk), image_bytes=disk.stat().st_size, inputs=inputs, commands=commands,
                  files={name: dict(bytes=len(data), sha256=hashlib.sha256(data).hexdigest()) for name, data in payload.items()},
                  protected_prefix_sha256=base['prefix_sha256'], hardware_actions=False,
                  power_on_qualified=False, reboot_persistence_qualified=False,
                  bootstrap_delivery='requires the separately matched HPS-first SPL; no QSPI programming')
    (output/'result.json').write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('uboot-build', 'entry-build', 'native-build', 'recovery-kit', 'launcher-staging', 'output'):
        parser.add_argument('--'+name, required=True, type=Path)
    parser.add_argument('--recovery-profile', choices=('standard', 'return-trace'), default='standard')
    parser.add_argument('--recovery-source-root', type=Path)
    args = parser.parse_args()
    result = prepare(*(getattr(args, name).absolute() for name in
                       ('uboot_build', 'entry_build', 'native_build', 'recovery_kit', 'launcher_staging', 'output')),
                     recovery_profile=args.recovery_profile, recovery_source_root=args.recovery_source_root)
    print('Prepared regular SD image:', args.output, result['image_sha256'])


if __name__ == '__main__':
    main()
