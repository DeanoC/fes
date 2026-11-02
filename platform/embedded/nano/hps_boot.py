#!/usr/bin/env python3
"""Compose an unqualified sealed HPS-first FPGA project with the native SPL.

Regular files only. Verify the existing package and native boot receipts before
conversion, independently inspect the replaced SPL, and require the split core
to remain byte-identical. This supplies no trusted live boot assertion or runtime
permission to program the FPGA.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

from package import checked_native, sha
from sof_hps import inspect
import hps_context

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
PROTOTYPE = ROOT/'sources/misteross/prototypes/de25_board_control'


def producer_module(name, filename):
    spec = importlib.util.spec_from_file_location(name, PROTOTYPE/filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def prepare(project, uboot, entry, pfg, output):
    sys.path.insert(0, str(PROTOTYPE))
    verifier = producer_module('nano_hps_board_verifier', 'verify.py')
    compiler = producer_module('nano_hps_task_runner', 'build.py')
    import seal

    inputs, _, _ = verifier.inputs_for(project)
    if not inputs['embedded_control'].get('build_inputs_sha256') or seal.boot_order(inputs) != 'HPS FIRST':
        raise ValueError('requires a sealed HPS-first diagnostic project')
    hps = project/'board/hps'
    pair = json.loads((hps/'hps-first-pair.json').read_text())
    fit, spl, image, native, _, profile = checked_native(uboot, entry)
    source_context=native.get('native_handoff',{}).get('cold_boot_profile',{}).get('hps_context')
    if source_context is not None and source_context!=hps_context.derive(project):
        raise ValueError('native HPS source context differs from the sealed FPGA project')
    control_map=native.get('native_handoff',{}).get('cold_boot_profile',{}).get('hps_control')
    if control_map is not None and control_map!=hps_context.control(project):
        raise ValueError('native CPU control map differs from the sealed FPGA project')
    reader_memory=native.get('native_handoff',{}).get('cold_boot_profile',{}).get('hps_reader_memory')
    if reader_memory is not None and reader_memory!=hps_context.reader_memory(project):
        raise ValueError('native reader memory differs from the sealed FPGA project')
    binary = pfg.parent.parent/'linux64/quartus_pfg'
    identity = 'wrapper-sha256:'+sha(pfg)+'; binary-sha256:'+sha(binary)
    if identity != pair['pfg_identity']:
        raise ValueError('selected PFG differs from the sealed FPGA producer')
    raw_sof = hps/'project/output_files/golden_top.sof'
    # The assembler SOF has no SPL section. Compare complete compositions with
    # the same topology, using the producer's already sealed vendor-SPL SOF.
    reference = hps/'diagnostic-with-vendor-spl.sof'
    selected = (raw_sof, reference, hps/'seal-result.json', hps/'build-inputs.json', hps/'hps-first-pair.json',
                hps/'hps-first.hps.rbf', hps/'hps-first.core.rbf', fit, spl, image,
                uboot/'result.json', uboot/'build/.config', entry/'result.json', profile, pfg, binary,
                Path(__file__), HERE/'package.py', HERE/'sof_hps.py',
                PROTOTYPE/'verify.py', PROTOTYPE/'build.py', PROTOTYPE/'seal.py')
    hashes = {str(path): sha(path) for path in selected}
    output.mkdir(parents=True)
    commands = []

    def run(label, argv):
        commands.append(compiler.run_phase(argv, output, output/(label+'.log')))

    candidate = output/'native-hps-first.sof'
    for source, name in ((fit, 'u-boot.itb'), (spl, 'u-boot-spl-dtb.hex'), (image, 'ENTRY.IMG')):
        shutil.copyfile(source, output/name)
    selected_spl = output/'u-boot-spl-dtb.hex'
    run('pfg-combine', [pfg, '-c', raw_sof, candidate, '-o', 'hps_path='+str(selected_spl)])
    run('pfg-info', [pfg, '-i', candidate])
    info = (output/'pfg-info.log').read_text(encoding='latin-1')
    if (re.findall(r'(?m)^Device name:\s*(\S+)\s*$', info) != [seal.DEVICE] or
            re.findall(r'(?m)^HPS/FPGA configuration order:\s*(\S+)\s*$', info) != ['HPS_FIRST']):
        raise ValueError('native composition changed the device or HPS-first order')
    embedded = inspect(candidate.read_bytes(), selected_spl.read_bytes(), reference.read_bytes())
    run('pfg-split', [pfg, '-c', candidate, output/'native-hps-first.rbf', '-o', 'hps=on'])
    phase1, core = output/'native-hps-first.hps.rbf', output/'native-hps-first.core.rbf'
    for label, path in (('hps', phase1), ('core', core)):
        if path.is_symlink() or not path.is_file() or not path.stat().st_size:
            raise ValueError('missing regular nonempty native split artifact')
        run('pfg-info-'+label, [pfg, '-i', path])
        if seal.parse_pfg_info((output/('pfg-info-'+label+'.log')).read_bytes()) != pair['hps_io_hash']:
            raise ValueError('native split HPS IO hash differs from the sealed FPGA pair')
    if core.read_bytes() != (hps/'hps-first.core.rbf').read_bytes():
        raise ValueError('native SPL composition changed the canonical core payload')
    verifier.inputs_for(project)
    checked_native(uboot, entry)
    if hashes != {str(path): sha(path) for path in selected}:
        raise ValueError('composition inputs changed')
    for source, name in ((fit, 'u-boot.itb'), (spl, 'u-boot-spl-dtb.hex'), (image, 'ENTRY.IMG')):
        if sha(source) != sha(output/name):
            raise ValueError('copied native boot file differs')
    result = {
        'format': 1, 'status': 'prepared',
        'classification': 'unqualified HPS-first native SPL file composition',
        'package_id': pair['package_id'], 'build_id': pair['build_id'],
        'hps_io_hash': pair['hps_io_hash'], 'hps_initialization': 'HPS FIRST',
        'original_pair_sha256': sha(hps/'hps-first-pair.json'),
        'reference_combined_sof_sha256': sha(reference),
        'original_phase1_sha256': pair['phase1_sha256'],
        'native_boot_id': native['native_handoff']['build_id'],
        'fit_sha256': sha(fit), 'spl_sha256': sha(spl), 'entry_sha256': sha(image),
        'bl31_sha256': native['bl31_sha256'], 'profile_sha256': sha(profile),
        'sof_sha256': sha(candidate), 'phase1_sha256': sha(phase1), 'phase1_size': phase1.stat().st_size,
        'core_sha256': sha(core), 'core_size': core.stat().st_size,
        'core_payload_unchanged': True, 'embedded_spl_identity': embedded,
        'source_context_bound': source_context is not None,
        'hps_context_sha256': hps_context.digest(source_context) if source_context is not None else None,
        'control_probe':control_map is not None,
        'hps_control_sha256':hps_context.digest(control_map) if control_map is not None else None,
        'trusted_boot_handoff': False, 'physical_programming_admission': False,
        'hardware_acceptance': 'not-run', 'inputs': hashes, 'commands': commands,
        'limitations': ['file composition only; no board, SD card or flash touched',
                        'native phase-one image differs from the vendor-SPL package pair',
                        'this receipt must not be used as a live trusted boot assertion',
                        'CPU mapping, DDR preservation, DMA and active bridge return remain unqualified']}
    (output/'result.json').write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('project', 'uboot-build', 'entry-build', 'quartus-pfg', 'output'):
        parser.add_argument('--'+name, required=True, type=Path)
    args = parser.parse_args()
    output = args.output.absolute()
    if (output.exists() or not output.is_relative_to(ROOT/'out') or '..' in output.parts or
            any(path.is_symlink() for path in (output, *output.parents)) or
            any(c in str(output) for c in ',@')):
        parser.error('output must be fresh and private below out/, without commas or @')
    paths = [args.project, args.uboot_build, args.entry_build, args.quartus_pfg]
    if any('..' in path.parts or path.is_symlink() for path in paths):
        parser.error('inputs must be explicit regular paths')
    prepare(*(path.resolve(strict=True) for path in paths), output)
    print('Prepared unqualified HPS-first native files:', output)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit('Nano HPS-first composition: '+str(error))
