#!/usr/bin/env python3
"""Prepare or verify the locked off-board DE25 SDM-error boot diagnostic.

This writes only a new directory of regular files. It does not write an SD,
program a device, replace a normal image lock or grant native acceptance.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import zlib

try:
    from . import de25_boot_contract as contract
except ImportError:
    import de25_boot_contract as contract

ROOT = Path(__file__).resolve().parents[1]
PROFILE = ROOT / 'image/de25-nano/sdm-diagnostic'
FILES = ('u-boot.itb', 'stock-u-boot.itb', 'bl31.bin', 'bl31.elf',
         'completion-errors.patch', 'firmware.lock.json', 'diagnostic.json')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def regular(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 4 << 20:
        raise ValueError('expected a regular diagnostic input: ' + str(path))
    return path.read_bytes()


def load_lock():
    lock = json.loads(regular(PROFILE / 'firmware.lock.json'))
    if (lock['format'] != 1 or lock['board'] != 'de25-nano' or
            lock['classification'] != 'unqualified-sdm-error-diagnostic' or
            lock['production_admission'] is not False or
            sha(regular(PROFILE / 'completion-errors.patch')) != lock['patch_sha256']):
        raise ValueError('diagnostic source binding differs')
    return lock


def patch_fit(stock, firmware, lock):
    """Replace only the locked external ATF slot and its size/CRC properties."""
    if sha(stock) != lock['stock_fit_sha256'] or len(stock) != lock['stock_fit_bytes']:
        raise ValueError('FIT differs from locked stock boot artifact')
    if sha(firmware) != lock['bl31_sha256']:
        raise ValueError('BL31 differs from locked compiled diagnostic input')
    nodes, images = contract.fit_images(stock)
    if set(images) != {'atf', 'uboot', 'fdt-0'}:
        raise ValueError('stock FIT image set differs')
    atf = images['atf']
    offset, size = lock['atf_offset'], lock['atf_source_bytes']
    if (atf['file_offset'] != offset or atf['size'] != size or
            atf['sha256'] != lock['stock_bl31_sha256'] or
            atf['type'] != 'firmware' or atf['load'] != [0x80000000] or
            atf['entry'] != [0x80000000] or not 0 < len(firmware) <= size):
        raise ValueError('external stock ATF slot or execution address differs')
    selected = contract.strings(nodes['/configurations']['default'])
    if selected != ['board-0']:
        raise ValueError('stock FIT configuration differs')
    config = nodes['/configurations/board-0']
    if {key: contract.strings(config[key]) for key in ('firmware', 'loadables', 'fdt')} != {
            'firmware': ['atf'], 'loadables': ['uboot'], 'fdt': ['fdt-0']}:
        raise ValueError('stock FIT boot roles differ')
    size_field, crc_field = lock['atf_size_field'], lock['atf_crc32_field']
    old_crc = struct.pack('>I', zlib.crc32(atf['data']) & 0xffffffff)
    if (stock[size_field:size_field + 4] != struct.pack('>I', size) or
            stock[crc_field:crc_field + 4] != old_crc):
        raise ValueError('stock ATF metadata location differs')
    result = bytearray(stock)
    result[offset:offset + size] = firmware + bytes(size - len(firmware))
    result[size_field:size_field + 4] = struct.pack('>I', len(firmware))
    new_crc = struct.pack('>I', zlib.crc32(firmware) & 0xffffffff)
    result[crc_field:crc_field + 4] = new_crc
    result = bytes(result)
    after, changed = contract.fit_images(result)
    expected = {path: dict(props) for path, props in nodes.items()}
    expected['/images/atf']['data-size'] = struct.pack('>I', len(firmware))
    expected['/images/atf/hash']['value'] = new_crc
    if (after != expected or changed['atf']['data'] != firmware or
            any(changed[name] != images[name] for name in ('uboot', 'fdt-0')) or
            len(result) != len(stock)):
        raise ValueError('FIT changed outside the selected ATF payload/metadata')
    return result


def payloads(stock, firmware, elf, lock):
    if sha(elf) != lock['bl31_elf_sha256']:
        raise ValueError('ELF differs from compiled diagnostic input')
    return {'u-boot.itb': patch_fit(stock, firmware, lock),
            'stock-u-boot.itb': stock, 'bl31.bin': firmware, 'bl31.elf': elf,
            'completion-errors.patch': regular(PROFILE / 'completion-errors.patch'),
            'firmware.lock.json': regular(PROFILE / 'firmware.lock.json')}


def manifest(files):
    return {'format': 1, 'kind': 'de25-sdm-error-boot-diagnostic', 'board': 'de25-nano',
            'classification': 'unqualified-sdm-error-diagnostic',
            'production_admission': False, 'hardware_acceptance': False,
            'physical_root_cause_proven': False, 'DMA_fence_proven': False,
            'changed': ['ATF external payload', 'ATF data-size', 'ATF CRC32'],
            'uboot_and_fdt_payloads_unchanged': True,
            'boot_configuration_unchanged': True,
            'recipe_files_sha256': {name: sha(regular(ROOT / name)) for name in (
                'scripts/de25_sdm_diagnostic.py', 'scripts/de25_boot_contract.py')},
            'files': {name: {'size': len(data), 'sha256': sha(data)}
                      for name, data in sorted(files.items())}}


def prepare(stock, firmware, elf, output):
    lock = load_lock()
    files = payloads(regular(stock), regular(firmware), regular(elf), lock)
    record = manifest(files)
    output = Path(output)
    if output.is_symlink() or output.exists():
        raise ValueError('diagnostic output must be new')
    output.mkdir(parents=True)
    for name, data in files.items():
        (output / name).write_bytes(data)
    (output / 'diagnostic.json').write_text(json.dumps(record, indent=2) + '\n')
    verify(output)
    return record


def verify(bundle):
    bundle = Path(bundle)
    if bundle.is_symlink() or not bundle.is_dir() or {p.name for p in bundle.iterdir()} != set(FILES):
        raise ValueError('diagnostic bundle file set differs')
    files = {name: regular(bundle / name) for name in FILES}
    lock = load_lock()
    expected = payloads(files['stock-u-boot.itb'], files['bl31.bin'], files['bl31.elf'], lock)
    if (any(files[name] != data for name, data in expected.items()) or
            json.loads(files['diagnostic.json']) != manifest(expected)):
        raise ValueError('diagnostic bundle differs from locked reconstruction')
    return manifest(expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    build = commands.add_parser('prepare')
    build.add_argument('--vendor-fit', required=True, type=Path)
    build.add_argument('--bl31', required=True, type=Path)
    build.add_argument('--elf', required=True, type=Path)
    build.add_argument('--output', required=True, type=Path)
    check = commands.add_parser('verify')
    check.add_argument('--bundle', required=True, type=Path)
    args = parser.parse_args()
    try:
        if args.command == 'prepare':
            record = prepare(args.vendor_fit, args.bl31, args.elf, args.output)
        else:
            record = verify(args.bundle)
    except (OSError, ValueError, KeyError, struct.error) as error:
        parser.exit(1, 'SDM diagnostic refused: ' + str(error) + '\n')
    print(json.dumps(record, indent=2))


if __name__ == '__main__':
    main()
