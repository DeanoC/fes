"""Package a matched DE25-Nano R1 flash pair and launcher SD image receipt."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from experiment import require_experiment
from cold_profile import EXPERIMENTAL_FILE, MAX_FIT_SIZE
from flash_trial import ARRAY_BYTES, FLASH, PART, validate_plan
from launcher_image import record, regular
from package import sha
from sof_hps import inspect
from upload import PREFIX_SIZE

JTAG_ID = '0x4362C0DD'
ROOT = Path(__file__).resolve().parents[3]


def loader_identity(parts):
    rows = [line.split() for line in parts.splitlines() if line.startswith(PART+' ')]
    if len(rows) != 1 or [item for item in rows[0] if item.startswith('0x')][:2] != [JTAG_ID, '0xFFFFFFFF']:
        raise ValueError('Quartus flash-loader part must match the exact R1 JTAG ID')
    return rows[0]


def factory_settings(raw, output):
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
    flash = ET.SubElement(devices, 'flash_device', id='Flash_Device_1', type=FLASH)
    ET.SubElement(flash, 'partition', id='ALL_MAIN_ARRAY', reserved='0', fixed_s_addr='1',
                  s_addr='0x00000000', e_addr='0x00FFFFFF', fixed_e_addr='1', size='0')
    ET.SubElement(devices, 'flash_loader').text = PART
    assigns = ET.SubElement(root, 'assignments')
    assignment = ET.SubElement(assigns, 'assignment', page='0', partition_id='ALL_MAIN_ARRAY')
    ET.SubElement(assignment, 'raw_file_id').text = 'factory'
    return ET.tostring(root, encoding='unicode')


def prepare(composition, launcher, factory, expected_factory_sha, pfg, lock, output):
    require_experiment()
    native_result = composition/'result.json'
    sd_result = launcher/'result.json'
    native = record(regular(native_result, 1 << 20))
    sd = record(regular(sd_result, 1 << 20))
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
    reference, = [Path(path) for path in native['inputs'] if Path(path).name == 'golden_top.sof']
    source_head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    binary = pfg.parent.parent/'linux64/quartus_pfg'
    parts = binary.with_name('pgm_parts.txt')
    inputs = {path: sha(path) for path in (reference, pfg, binary, parts, spl, fit, disk, native_result, sd_result)}
    if (any(inputs[path] != native['inputs'].get(str(path)) for path in (reference, pfg, binary)) or
            inputs[spl] != native.get('spl_sha256')):
        raise ValueError('native composition input or matched PFG differs')
    loader_identity(regular(parts, 8 << 20).decode('ascii'))
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
    settings.write_text(factory_settings(local_factory, output))
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
    (output/'plan.json').write_text(json.dumps(plan, indent=2)+'\n')
    result = dict(status='prepared-file-only-r1-boot-flash', plan_sha256=plan_sha,
                  part=PART, jtag_id=JTAG_ID, fit_sha256=fit_sha, native_boot_id=native['native_boot_id'],
                  sd_image=dict(path=str(disk), sha256=inputs[disk]), commands=commands,
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
