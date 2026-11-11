#!/usr/bin/env python3
"""Prepare/verify DE25 boot and rollback files off-board. Never program hardware."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import tomllib

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ('linux-complete.sof', 'linux-boot.hps.jic', 'linux-boot.core.rbf')
VENDOR = ('golden_top_hps.jic', 'vendor-readme.txt')
FILES = (*ARTIFACTS, *VENDOR, 'recovery.hps.rbf', 'linux-programming.json',
         'candidate-info.log', 'rollback-info.log', 'recovery-info.log', 'conversion.log')


def identity(path):
    if path.is_symlink() or not path.is_file() or not path.stat().st_size:
        raise ValueError('expected nonempty regular file: ' + str(path))
    return {'size': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}


def vendor_lock():
    lock = tomllib.loads((ROOT / 'image/de25-nano/vendor.lock.toml').read_text())
    return {r['name']: {k: r[k] for k in ('size', 'sha256')}
            for r in lock['factory_boot'] if r['name'] in VENDOR}


def check_receipt(directory):
    record = json.loads((directory / 'linux-programming.json').read_text())
    if (record.get('format') != 1 or record.get('hps_initialization') != 'HPS FIRST'
            or record.get('production_admission') is not False):
        raise ValueError('expected diagnostic HPS-first producer receipt')
    for name in ARTIFACTS:
        expected = record['artifacts'][name]
        if identity(directory / name) != {k: expected[k] for k in ('size', 'sha256')}:
            raise ValueError('producer artifact differs: ' + name)
    return record


def check_info(directory):
    # Quartus emits a locale-dependent degree sign in the temperature field.
    candidate = (directory / 'candidate-info.log').read_text(errors='replace')
    rollback = (directory / 'rollback-info.log').read_text(errors='replace')
    recovery = (directory / 'recovery-info.log').read_text(errors='replace')
    for text in (candidate, rollback):
        if 'Flash device: MT25QU128\n' not in text or 'JIC Header\n' not in text:
            raise ValueError('expected inspected MT25QU128 JIC')
    for text in (candidate, recovery):
        for marker in ('HPS/FPGA configuration order: HPS_FIRST\n',
                       'HPS IO hash: ', 'HPS Section\n', 'HPIO Section\n'):
            if marker not in text:
                raise ValueError('missing HPS-first section metadata: ' + marker.strip())
        if 'CORE Section\n' in text:
            raise ValueError('bootstrap must not include FPGA core section')
    # The section metadata is diagnostic inspection, not printed-OPN qualification.
    candidate_hash = candidate.split('HPS IO hash: ', 1)[1].splitlines()[0]
    if recovery.split('HPS IO hash: ', 1)[1].splitlines()[0] != candidate_hash:
        raise ValueError('HPS-only recovery and candidate IO hashes differ')


def verify(directory):
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError('expected regular bundle directory')
    record = json.loads((directory / 'bootstrap.json').read_text())
    if (record.get('format') != 1 or record.get('kind') != 'de25-bootstrap-candidate'
            or record.get('hardware_programming') != 'not-performed'
            or record.get('production_admission') is not False):
        raise ValueError('invalid diagnostic bootstrap receipt')
    if set(record['files']) != set(FILES) or {p.name for p in directory.iterdir()} != {*FILES, 'bootstrap.json'}:
        raise ValueError('bootstrap file set differs')
    for name in FILES:
        if identity(directory / name) != record['files'][name]:
            raise ValueError('bootstrap artifact differs: ' + name)
    source = check_receipt(directory)
    if (record['producer_revision'] != source['fes_revision']
            or record['computer_build_id'] != source['computer_build_id']):
        raise ValueError('bootstrap source identity differs')
    for name, expected in vendor_lock().items():
        if identity(directory / name) != expected:
            raise ValueError('locked vendor rollback differs: ' + name)
    check_info(directory)
    return record


def prepare(source, vendor, output, pfg):
    source, vendor, output, pfg = (p.absolute() for p in (source, vendor, output, pfg))
    if output.exists() or output.is_symlink():
        raise ValueError('output already exists')
    record = check_receipt(source)
    for name, expected in vendor_lock().items():
        if identity(vendor / name) != expected:
            raise ValueError('locked vendor rollback differs: ' + name)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.' + output.name + '-', dir=output.parent) as temporary:
        staging = Path(temporary) / 'bundle'
        staging.mkdir()
        for name in (*ARTIFACTS, 'linux-programming.json'):
            shutil.copyfile(source / name, staging / name)
        for name in VENDOR:
            shutil.copyfile(vendor / name, staging / name)
        # Conversion only: no programmer binary, USB, SSH or target device access.
        def run(arguments, name):
            with (staging / name).open('wb') as log:
                subprocess.run([str(pfg), *map(str, arguments)], stdout=log,
                               stderr=subprocess.STDOUT, check=True, timeout=120)
        run(['-c', staging / 'linux-complete.sof', staging / 'recovery.rbf', '-o', 'hps=on'], 'conversion.log')
        if identity(staging / 'recovery.core.rbf') != identity(staging / 'linux-boot.core.rbf'):
            raise ValueError('HPS-only split changed selected core RBF')
        (staging / 'recovery.core.rbf').unlink()
        for name, log in [('linux-boot.hps.jic', 'candidate-info.log'),
                          ('golden_top_hps.jic', 'rollback-info.log'),
                          ('recovery.hps.rbf', 'recovery-info.log')]:
            run(['-i', staging / name], log)
        check_info(staging)
        manifest = {'format': 1, 'kind': 'de25-bootstrap-candidate',
                    'producer_revision': record['fes_revision'],
                    'computer_build_id': record['computer_build_id'],
                    'hardware_programming': 'not-performed', 'production_admission': False,
                    'qspi_boot_and_reboot': 'not-qualified',
                    'rollback_scope': 'locked vendor BSP JIC; not a readback of this board flash',
                    'files': {name: identity(staging / name) for name in FILES}}
        (staging / 'bootstrap.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
        verify(staging)
        staging.rename(output)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    p = commands.add_parser('prepare')
    p.add_argument('--programming-dir', type=Path, required=True)
    p.add_argument('--vendor-boot', type=Path, required=True)
    p.add_argument('--quartus-pfg', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    v = commands.add_parser('verify')
    v.add_argument('--bundle', type=Path, required=True)
    args = parser.parse_args()
    try:
        record = (prepare(args.programming_dir, args.vendor_boot, args.output, args.quartus_pfg)
                  if args.action == 'prepare' else verify(args.bundle))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, str(error) + '\n')
    print(json.dumps(record, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
