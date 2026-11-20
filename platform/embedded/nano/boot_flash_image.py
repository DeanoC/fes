"""Package a matched DE25-Nano R1 flash pair and launcher SD image receipt."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

JTAG_ID = '0x4362C0DD'
ROOT = Path(__file__).resolve().parents[3]
SOURCE_SCOPE = Path('platform/embedded')


def clean_head(root):
    if subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=root):
        raise ValueError('a clean committed checkout is required to publish flash images')
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()


def committed_sources(root, head):
    # The cold helpers also load modules by path. Pin the whole embedded Python
    # tree so a new transitive import cannot escape a hand-maintained list.
    tracked = subprocess.check_output(['git', 'ls-tree', '-r', '-z', '--name-only', head, '--', str(SOURCE_SCOPE)], cwd=root)
    result = {}
    for name in tracked.decode('utf-8').split('\0'):
        if not name.endswith('.py'):
            continue
        path = root/name
        committed = subprocess.check_output(['git', 'show', head+':'+name], cwd=root)
        digest = hashlib.sha256(committed).hexdigest()
        if path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError('flash producer source differs from its commit: '+name)
        result[path] = digest
    if not result:
        raise ValueError('committed embedded Python source tree required')
    return result


def add_input(paths, path, digest, sha):
    if (not path.is_absolute() or '..' in path.parts or path in paths and paths[path] != digest or
            not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest) or
            not path.is_file() or path.resolve() != path or sha(path) != digest):
        raise ValueError('input differs from its recorded digest: '+str(path))
    paths[path] = digest


def recorded_inputs(inventory, sha):
    if not isinstance(inventory, dict) or not inventory:
        raise ValueError('complete recorded input inventory required')
    paths = {}
    for name, digest in inventory.items():
        add_input(paths, Path(name), digest, sha)
    return paths


def check_commit_input(root, head, relative, digest):
    blob = subprocess.run(['git', 'show', head+':'+relative.as_posix()], cwd=root, capture_output=True)
    if blob.returncode or hashlib.sha256(blob.stdout).hexdigest() != digest:
        raise ValueError('source differs from selected commit: '+relative.as_posix())


def launcher_inputs(root, head, inventory, sha):
    paths = recorded_inputs(inventory, sha)
    producer = Path('platform/embedded/nano/launcher_image.py')
    if sum(path.parts[-len(producer.parts):] == producer.parts for path in paths) != 1:
        raise ValueError('one recorded launcher image producer required')
    for path, digest in paths.items():
        prefix = subprocess.run(['git', '-C', str(path.parent), 'rev-parse', '--show-prefix'],
                                capture_output=True, text=True)
        if prefix.returncode == 0:
            relative = Path(prefix.stdout.rstrip('\n'))/path.name
            if not relative.is_relative_to('out'):
                check_commit_input(root, head, relative, digest)
        elif path.parts[-len(producer.parts):] == producer.parts:
            raise ValueError('launcher producer must belong to a Git worktree')
    return paths


def composition_inputs(root, head, inventory, tools, *, sha, read_record):
    """Check the recorded files and the source inventories in their pinned receipts."""
    paths = recorded_inputs(inventory, sha)
    producer = Path('platform/embedded/nano/hps_boot.py')
    origins = [path.parents[len(producer.parts)-1] for path in paths
               if path.parts[-len(producer.parts):] == producer.parts]
    if len(origins) != 1:
        raise ValueError('one recorded HPS composition producer required')
    origin = origins[0]
    if not set(tools) <= paths.keys() or any(p not in tools and not p.is_relative_to(origin) for p in paths):
        raise ValueError('unbound composition input or Quartus converter')

    def recorded_receipt(file_name, receipt_path):
        candidates = [receipt_path(path) for path in paths if path.name == file_name]
        if len(candidates) != 1 or candidates[0] not in paths:
            raise ValueError('missing pinned composition receipt for '+file_name)
        data = read_record(candidates[0])
        if sha(candidates[0]) != paths[candidates[0]]:
            raise ValueError('composition receipt changed: '+str(candidates[0]))
        return data

    entry = recorded_receipt('entry.img', lambda p: p.with_name('result.json'))
    uboot = recorded_receipt('u-boot-spl-dtb.hex', lambda p: p.parents[2]/'result.json')
    fpga = recorded_receipt('golden_top.sof', lambda p: p.parents[2]/'build-inputs.json')
    module = Path(fpga['source_path'])
    if module.is_absolute() or '..' in module.parts or module.is_relative_to('out'):
        raise ValueError('FPGA source_path must name a repository module')
    for prefix, sources in ((Path('.'), entry.get('sources')), (Path('.'), entry.get('native_build_sources')),
                            (Path('.'), uboot.get('native_handoff', {}).get('producer_sources')),
                            (module, fpga.get('source_inputs'))):
        if not isinstance(sources, dict) or not sources:
            raise ValueError('missing composition producer source inventory')
        for name, digest in sources.items():
            relative = Path(name)
            if relative.is_absolute() or '..' in relative.parts:
                raise ValueError('composition source must be repository-relative: '+name)
            add_input(paths, origin/prefix/relative, digest, sha)
    add_input(paths, origin/'platform/embedded/firmware/build_terasic_uboot.py', uboot.get('producer_sha256'), sha)

    for path, digest in paths.items():
        if path in tools:
            continue
        relative = path.relative_to(origin)
        if relative.is_relative_to('out'):
            continue
        check_commit_input(root, head, relative, digest)
    return paths


def loader_identity(parts, part):
    rows = [line.split() for line in parts.splitlines() if line.startswith(part+' ')]
    if len(rows) != 1 or [item for item in rows[0] if item.startswith('0x')][:2] != [JTAG_ID, '0xFFFFFFFF']:
        raise ValueError('Quartus flash-loader part must match the exact R1 JTAG ID')
    return rows[0]


def factory_settings(raw, output, part, flash_type):
    root = ET.Element('pfg', version='1')
    ET.SubElement(root, 'settings', mode='ASX4')
    files = ET.SubElement(root, 'raw_files')
    ET.SubElement(files, 'raw_file', id='factory', bitswap='1', type='RBF').text = str(raw)
    files = ET.SubElement(root, 'output_files')
    jic = ET.SubElement(files, 'output_file', name='factory-main-array', directory=str(output)+'/', type='JIC')
    ET.SubElement(jic, 'file_options')
    ET.SubElement(jic, 'flash_device_id').text = 'Flash_Device_1'
    rpd = ET.SubElement(jic, 'secondary_file', type='SEC_RPD', name='factory-main-array')
    ET.SubElement(rpd, 'file_options', bitswap='1')
    devices = ET.SubElement(root, 'flash_devices')
    flash = ET.SubElement(devices, 'flash_device', id='Flash_Device_1', type=flash_type)
    ET.SubElement(flash, 'partition', id='ALL_MAIN_ARRAY', reserved='0', fixed_s_addr='1',
                  s_addr='0x00000000', e_addr='0x00FFFFFF', fixed_e_addr='1', size='0')
    ET.SubElement(devices, 'flash_loader').text = part
    assigns = ET.SubElement(root, 'assignments')
    assignment = ET.SubElement(assigns, 'assignment', page='0', partition_id='ALL_MAIN_ARRAY')
    ET.SubElement(assignment, 'raw_file_id').text = 'factory'
    return ET.tostring(root, encoding='unicode')


def prepare(composition, launcher, factory, expected_factory_sha, pfg, lock, output):
    source_head = clean_head(ROOT)
    sources = committed_sources(ROOT, source_head)
    sys.path.insert(0, str(ROOT/SOURCE_SCOPE))
    from experiment import require_experiment
    from cold_profile import EXPERIMENTAL_FILE, MAX_FIT_SIZE
    from flash_trial import ARRAY_BYTES, FLASH, PART, validate_plan
    from launcher_image import record, regular
    from package import sha
    from sof_hps import inspect
    from upload import PREFIX_SIZE

    require_experiment()
    native_result = composition/'result.json'
    sd_result = launcher/'result.json'
    native_bytes = regular(native_result, 1 << 20)
    sd_bytes = regular(sd_result, 1 << 20)
    native = record(native_bytes)
    sd = record(sd_bytes)
    spl = composition/'u-boot-spl-dtb.hex'
    fit = composition/'u-boot.itb'
    disk = launcher/'native-launcher-sd.img'
    fit_sha = hashlib.sha256(regular(fit, MAX_FIT_SIZE)).hexdigest()
    if (native.get('fit_sha256') != fit_sha or sd.get('native_boot_id') != native.get('native_boot_id') or
            sd.get('startup', {}).get('mode') != 'autoboot' or sd.get('status') != 'prepared' or
            sd.get('files', {}).get(EXPERIMENTAL_FILE, {}).get('sha256') != fit_sha or
            sd.get('image_bytes') != disk.stat().st_size or
            not disk.is_file() or disk.stat().st_size > 256 << 20 or
            any(p.is_symlink() for p in (disk, *disk.parents)) or sd.get('image_sha256') != sha(disk)):
        raise ValueError('matched autoboot launcher SD image required')
    binary = pfg.parent.parent/'linux64/quartus_pfg'
    parts = binary.with_name('pgm_parts.txt')
    inputs = composition_inputs(ROOT, source_head, native.get('inputs'), (pfg, binary), sha=sha,
                                read_record=lambda path: record(regular(path, 1 << 20)))
    reference, = [path for path in inputs if path.name == 'golden_top.sof']
    selected = launcher_inputs(ROOT, source_head, sd.get('inputs'), sha)
    for path, digest in (*selected.items(), (parts, sha(parts)), (spl, native['spl_sha256']),
                         (fit, fit_sha), (disk, sd['image_sha256']),
                         (native_result, hashlib.sha256(native_bytes).hexdigest()),
                         (sd_result, hashlib.sha256(sd_bytes).hexdigest())):
        add_input(inputs, path, digest, sha)
    if sha(spl) != inputs[spl]:
        raise ValueError('matched SPL differs from the native composition')
    loader_identity(regular(parts, 8 << 20).decode('ascii'), PART)
    factory_bytes = regular(factory, ARRAY_BYTES)
    if len(factory_bytes) != ARRAY_BYTES or hashlib.sha256(factory_bytes).hexdigest() != expected_factory_sha:
        raise ValueError('exact 16 MiB factory backup and SHA-256 required')
    inputs[factory] = expected_factory_sha
    if not lock.is_file() or any(p.is_symlink() for p in (output, *output.parents)):
        raise ValueError('existing Quartus lock and private output path required')
    output.mkdir(mode=0o700)
    commands = []

    def run(label, argv):
        commands.append([str(arg) for arg in argv])
        with (output/(label+'.log')).open('xb') as log:
            subprocess.run(argv, cwd=output, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=240)

    def convert(label, *args):
        run(label, ['flock', '-w', '60', lock, pfg, *args])

    run('read-sd-fit', ['mcopy', '-i', str(disk)+'@@'+str(PREFIX_SIZE), '::'+EXPERIMENTAL_FILE, 'sd-fit.itb'])
    if sha(output/'sd-fit.itb') != fit_sha:
        raise ValueError('SD FIT readback differs from the native composition')
    (output/'input.sof').write_bytes(regular(reference, 8 << 20))
    (output/'input-spl.hex').write_bytes(regular(spl, 1 << 20))
    convert('native', '-c', 'input.sof', 'native.jic', 'native.rpd', 'native.map',
            '-o', 'hps_path=input-spl.hex', '-o', 'device='+FLASH, '-o', 'flash_loader='+PART,
            '-o', 'mode=ASX4', '-o', 'hps=on', '-o', 'bitswap=on')
    inspect(regular(output/'input_hps_auto.sof', 8 << 20), regular(spl, 1 << 20),
            regular(composition/'native-hps-first.sof', 8 << 20))
    if (sha(output/'native.core.rbf') != native['core_sha256'] or
            sha(composition/'native-hps-first.sof') != native.get('sof_sha256')):
        raise ValueError('phase-two core differs from the native composition')
    native_prefix = regular(output/'native.rpd', ARRAY_BYTES)
    native_array = native_prefix.ljust(ARRAY_BYTES, b'\xff')
    local_factory = output/'factory-main-array.bin'
    local_factory.write_bytes(factory_bytes)
    settings = output/'factory.pfg'
    settings.write_text(factory_settings(local_factory, output, PART, FLASH))
    convert('factory', '-c', settings)
    if regular(output/'factory-main-array.rpd', ARRAY_BYTES) != factory_bytes:
        raise ValueError('factory RPD differs from the full backup')
    images = {}
    for role, name, array in (('native', 'native.hps.jic', native_array),
                              ('factory-recovery', 'factory-main-array.jic', factory_bytes)):
        path = output/name
        convert(role+'-inspect', '-i', path)
        info = (output/(role+'-inspect.log')).read_text(encoding='latin1')
        names = re.findall(r'(?m)^Device name: (\S+)$', info)
        crc = re.findall(r'(?m)^CRC: 0x([0-9A-Fa-f]+)$', info)
        jic = regular(path, ARRAY_BYTES + (64 << 10))
        if (not names or names[0] != PART or re.findall(r'(?m)^JTAG ID: (\S+)$', info) != [JTAG_ID] or
                re.findall(r'(?m)^Flash device: (\S+)$', info) != [FLASH] or
                len(crc) != 1 or int(crc[0], 16) != int.from_bytes(jic[-2:], 'little') or
                (role == 'native' and 'HPS/FPGA configuration order: HPS_FIRST' not in info)):
            raise ValueError('PFG reported an unexpected loader, device or flash')
        images[role] = dict(path=str(path), sha256=hashlib.sha256(jic).hexdigest(),
                            array_sha256=hashlib.sha256(array).hexdigest())
    if any(sha(path) != digest for path, digest in inputs.items()):
        raise ValueError('packaging inputs changed')
    plan = dict(format=1, serial='TRWU8CBL', source_commit=source_head, backup_sha256=expected_factory_sha, images=images)
    plan_sha = validate_plan(plan)
    if clean_head(ROOT) != source_head or any(sha(path) != digest for path, digest in sources.items()):
        raise ValueError('flash producer source changed before publication')
    (output/'plan.json').write_text(json.dumps(plan, indent=2)+'\n')
    result = dict(status='prepared-file-only-r1-boot-flash', plan_sha256=plan_sha,
                  part=PART, jtag_id=JTAG_ID, fit_sha256=fit_sha, native_boot_id=native['native_boot_id'],
                  sd_image=dict(path=str(disk), sha256=inputs[disk]), commands=commands,
                  producer_sources={str(path.relative_to(ROOT)): value for path, value in sources.items()},
                  inputs={str(path): value for path, value in inputs.items()}, hardware_actions=False)
    (output/'result.json').write_text(json.dumps(result, indent=2)+'\n')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('composition', 'launcher-image', 'factory-backup', 'quartus-pfg', 'quartus-lock', 'output'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--factory-sha256', required=True)
    args = parser.parse_args()
    result = prepare(args.composition.absolute(), args.launcher_image.absolute(), args.factory_backup.absolute(),
                     args.factory_sha256, args.quartus_pfg.absolute(), args.quartus_lock.absolute(), args.output.absolute())
    print('Prepared opt-in R1 flash plan', result['plan_sha256'])


if __name__ == '__main__':
    main()
