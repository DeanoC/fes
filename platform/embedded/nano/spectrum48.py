"""Bind one sealed Spectrum 48K companion to the native mixed-role boot."""

import hashlib
import json
from pathlib import Path
import re
import sys
import subprocess

PROFILE = 'de25-native-spectrum48-v1'
SCOPE = 'nano-compiled-spectrum48-program-v1'
CHUNK = 0xfffc0
MAX_CORE = (8 << 20) - 4096
MAX_ARCHIVE = 8 << 20
ROM_BYTES = 16384
BASES = dict(computer_cpu_base=0x20010300, scanout_cpu_base=0x20010200, probe_cpu_base=0x20010100)


def encode(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def read(path, maximum):
    path = Path(path).absolute()
    if path.resolve(strict=True) != path or not path.is_file():
        raise ValueError('Spectrum companion requires private regular files')
    with path.open('rb') as stream:
        data = stream.read(maximum + 1)
    if not 0 < len(data) <= maximum:
        raise ValueError('invalid Spectrum companion file size')
    return data


def inspect(build, archive):
    """Run in an isolated producer process to keep private imports separate."""
    from boards.de25nano.package.core_package import read_package
    from boards.de25nano.package.export_core_package import (
        build_identity, manifest_toolchain, verify_record_source_at_revision,
    )
    from boards.de25nano.producers.build_menu_package import timing_evidence
    from boards.de25nano.scanout.async_inputs import native_spectrum48
    import hps_io

    raw = read(archive, MAX_ARCHIVE)
    package = read_package(archive)
    d = package.fields
    record = read(build / 'build-inputs.json', 1 << 20)
    evidence = archive.with_suffix('.build-inputs.json')
    fields = verify_record_source_at_revision(build, record)
    params = fields['parameters']
    payload = package.payload_bytes
    identity = build_identity(record)
    if (read(evidence, 1 << 20) != record or archive.stem != package.package_id or
            raw != read(archive, MAX_ARCHIVE) or d['format'] != 3 or
            d['target'] != {'platform': 'de25_nano', 'device': 'A5EB013BB23BE4SCS',
                            'programming_profile': PROFILE} or not native_spectrum48(params) or
            params.get('selection_only') or params.get('fabric_initiator_policy') != 'native-maintenance-absent-v1' or
            d['core']['id'] != 'fes.spectrum' or d['abi'] != {'id': 'fes.computer', 'major': 1, 'minor': 0} or
            d['rom']['role'] != 'firmware' or d['rom']['source_size'] != ROM_BYTES or
            d['build']['id'] != identity or d['build']['toolchain'] != manifest_toolchain(fields) or
            any(d['build'][key] != fields[key] for key in ('revision', 'repository', 'recipe_sha256')) or
            len(payload) > MAX_CORE or len(payload) % 4 or read(build / 'core.rbf', MAX_CORE) != payload):
        raise ValueError('Spectrum archive differs from its sealed build')
    fits = []
    for fit in ('signoff', 'repeat'):
        project = build / fit
        hps = project / 'combined/hps'
        timing = timing_evidence(project, async_review=build / (fit + '-async-input-review'),
                                 audio_review=build / (fit + '-audio-clock-review'))
        if (timing != json.loads(read(build / (fit + '-timing.json'), 4 << 20)) or
                not timing['package_timing_admission'] or read(hps / 'linux-boot.core.rbf', MAX_CORE) != payload):
            raise ValueError('Spectrum build did not independently pass package timing')
        handoff = read(hps / 'native-spectrum-handoff.json', 1 << 20)
        hw = json.loads(handoff)
        generation = json.loads(read(hps / 'generation.json', 1 << 20))
        pinmux = read(hps / 'project/output_files/hps_bootloader_handoff.bin', 4096)
        compiled = json.loads(read(hps / 'build.json', 4 << 20))
        if (generation['handoff_sha256'] != sha(handoff) or hw['spectrum_control']['endpoint']['build_id'] != identity or
                hw['ddr_ingress'] != ['subsys_periph_fes_ddr_probe.avm', 'subsys_periph_fes_mem_cdc.m0'] or
                compiled['generated_handoff_and_sdc_sha256']['output_files/hps_bootloader_handoff.bin'] != sha(pinmux)):
            raise ValueError('Spectrum package differs from its fitted handoff')
        regions = {v['name']: (v['base'], v['span']) for v in hw['spectrum_control']['regions']}
        if regions != {'subsys_periph.fes_scanout.computer': (0x10300, 32),
                       'subsys_periph.fes_scanout_guard.avs': (0x10200, 256),
                       'subsys_periph.fes_ddr_probe.avs': (0x10100, 128)}:
            raise ValueError('Spectrum fitted bridge regions changed')
        fits.append((sha(encode(hw['hps_parameters'])), sha(hps_io.configuration_bytes(pinmux)), sha(handoff)))
    if fits[0][:2] != fits[1][:2]:
        raise ValueError('Spectrum repeat differs in shared HPS resources')
    return dict(package_id=package.package_id, build_id=identity, core_size=len(payload),
                core_sha256=sha(payload), core_sha384=hashlib.sha384(payload).hexdigest(),
                chunk_sha384=[hashlib.sha384(payload[i:i+CHUNK]).hexdigest() for i in range(0, len(payload), CHUNK)],
                handoff_sha256=fits[0][2], hps_parameters_sha256=fits[0][0],
                hps_configuration_sha256=fits[0][1], archive_sha256=sha(raw))


def record_for(proof, context, platform, configuration, rom):
    shared = platform['platform']
    if (any(proof[key] != shared[key] for key in ('hps_parameters_sha256', 'hps_configuration_sha256')) or
            platform['boot_context_sha256'] != sha(encode(context)) or configuration['chunk_bytes'] != CHUNK or
            rom['package_id'] != proof['package_id'] or rom['source_size'] != ROM_BYTES):
        raise ValueError('Spectrum companion does not match the native boot resources')
    record = {key: proof[key] for key in ('package_id', 'build_id', 'core_size', 'core_sha256',
                                         'core_sha384', 'chunk_sha384', 'handoff_sha256')}
    return dict(record, format=1, scope=SCOPE, boot_context_sha256=sha(encode(context)),
                rom_source_sha256=rom['source_sha256'], rom_receipt_sha256=rom['rom_receipt_sha256'], **BASES)


def checked(profile, key='program'):
    record = profile.get('spectrum48_program')
    digest = profile.get('spectrum48_program_sha256')
    if record is None and digest is None:
        return None
    context = profile['hps_context']
    config = profile['hps_configuration']
    if (not isinstance(record, dict) or sha(encode(record)) != digest or
            profile.get(key) != 'fpgaload' or not profile.get('hps_menu_program') or
            any(profile.get(flag) is not True for flag in
                ('selected_programs', 'computer_reload', 'computer_storage_return', 'tenfoot_serial')) or
            record['format'] != 1 or record['scope'] != SCOPE or record['boot_context_sha256'] != sha(encode(context)) or
            any(record[k] != v for k, v in BASES.items()) or config['chunk_bytes'] != CHUNK):
        raise ValueError('invalid compiled Spectrum companion')
    size = record['core_size']
    chunks = record['chunk_sha384']
    digests = {'build_id': 32, 'core_sha384': 96, **dict.fromkeys(
        ('package_id', 'core_sha256', 'handoff_sha256', 'rom_source_sha256', 'rom_receipt_sha256'), 64)}
    if (type(size) is not int or not 0 < size <= MAX_CORE or size % 4 or
            len(chunks) != (size + CHUNK - 1) // CHUNK or len(chunks) > 9 or
            any(not valid_hex(v, 96) for v in chunks) or
            any(not valid_hex(record[k], width) for k, width in digests.items())):
        raise ValueError('invalid Spectrum payload identity or chunk geometry')
    return record


def valid_hex(value, width):
    return isinstance(value, str) and re.fullmatch('[a-f0-9]{%d}' % width, value) and value != '0' * width


def header(record):
    def array(value):
        return '{' + ','.join('0x' + value[i:i+2] for i in range(0, len(value), 2)) + '}'
    fields = [str(record['core_size']) + 'U']
    fields.extend(array(record[k]) for k in ('package_id', 'build_id', 'core_sha256', 'core_sha384'))
    fields.append('{' + ','.join(map(array, record['chunk_sha384'])) + '}')
    return ('#define FES_NANO_SPECTRUM48 1\n'
            '#define FES_NANO_SPECTRUM48_SCANOUT_BASE 0x20010200UL\n'
            '#define FES_NANO_SPECTRUM48_PROGRAM {' + ','.join(fields) + '}\n')


if __name__ == '__main__':
    build, archive = (Path(value).resolve(strict=True) for value in sys.argv[1:])
    root = Path(subprocess.check_output(['git', '-C', str(build), 'rev-parse', '--show-toplevel'],
                                       text=True).strip())
    sys.path.insert(0, str(root / 'sources/misteross'))
    sys.stdout.buffer.write(encode(inspect(build, archive)))
