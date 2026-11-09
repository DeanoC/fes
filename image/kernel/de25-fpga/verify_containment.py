#!/usr/bin/env python3
"""Check source-bound SDM admission and SMMU containment with injected kernel seams."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT))
from scripts import de25_kernel_platform as platform

LANE = Path(__file__).resolve().parent


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def function(text, name):
    match = re.search(r'(?m)^(?:static void |void |int |struct stratix10_svc_chan \*)'
                      + re.escape(name) + r'\(', text)
    if match is None:
        raise ValueError('function definition missing: ' + name)
    start = text.index('{', match.end())
    end, depth = start + 1, 1
    while depth:
        if text[end] == '{':
            depth += 1
        elif text[end] == '}':
            depth -= 1
        end += 1
    return text[match.start():end] + '\n'


def prepare(source, output):
    record = json.loads((platform.ROOT / platform.PROFILE / 'source.json').read_text())
    tree = output / 'supplier-tree'
    tree.mkdir()
    # Copy only the authenticated supplier files. Never patch the caller's tree.
    for name, row in record['files'].items():
        original = source / name
        if original.is_symlink() or (original.exists() if row['before_sha256'] is None else
                                     not original.is_file() or digest(original) != row['before_sha256']):
            raise ValueError('supplier input differs: ' + name)
        if row['before_sha256'] is not None:
            target = tree / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, target)
    for name, sha in record['unchanged_supplier_sources'].items():
        if (source / name).is_symlink() or digest(source / name) != sha:
            raise ValueError('unchanged supplier differs: ' + name)
        target = tree / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source / name, target)
    platform.apply_profile(tree, platform.ROOT / platform.PROFILE)
    svc = (tree / 'drivers/firmware/stratix10-svc.c').read_text()
    controller = (LANE / 'controller_prefix.c').read_text()
    controller += '\n'.join(function(svc, name) for name in (
        'stratix10_svc_request_channel_byname', 'stratix10_svc_free_channel',
        'stratix10_svc_block_dma'))
    controller += (LANE / 'controller_test.c').read_text()
    (output / 'controller.c').write_text(controller)
    smmu = (tree / 'drivers/iommu/arm/arm-smmu-v3/arm-smmu-v3.c').read_text()
    start = smmu.index('struct arm_smmu_de25_checked_writer {')
    end = smmu.index('\n#endif', start)
    backend = (LANE / 'smmu_prefix.c').read_text()
    backend += function(smmu, 'arm_smmu_make_abort_ste')
    backend += 'struct arm_smmu_ste_writer {struct arm_smmu_entry_writer writer;u32 sid;};\n'
    backend += function(smmu, 'arm_smmu_ste_writer_sync_entry')
    backend += ('static const struct arm_smmu_entry_writer_ops arm_smmu_ste_writer_ops = '
                '{.sync=arm_smmu_ste_writer_sync_entry,.get_used=arm_smmu_get_ste_used};\n')
    backend += function(smmu, 'arm_smmu_write_ste')
    backend += smmu[start:end] + '\n' + (LANE / 'smmu_test.c').read_text()
    (output / 'smmu.c').write_text(backend)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cc', default='cc')
    parser.add_argument('--qemu', type=Path)
    parser.add_argument('--sysroot', type=Path)
    args = parser.parse_args()
    if args.sysroot and not args.qemu:
        parser.error('--sysroot requires --qemu')
    args.output.mkdir(parents=True, exist_ok=False)
    record = prepare(args.source_root, args.output)
    commands = []
    runner = ([str(args.qemu), *(['-L', str(args.sysroot)] if args.sysroot else [])]
              if args.qemu else [])
    common = ['-std=gnu11', '-O2', '-Wall', '-Wextra', '-Werror',
              '-Wno-unused-parameter', '-Wno-misleading-indentation']
    cases = [('controller-enabled', 'controller', ['-D_GNU_SOURCE', '-pthread']),
             ('controller-disabled', 'controller', ['-D_GNU_SOURCE', '-pthread',
                                                    '-DCONFIG_FES_DE25_SDM_CONTAINMENT=0']),
             ('smmu-enabled', 'smmu', ['-Wno-unused-function', '-ffreestanding', '-fno-builtin',
                                      '-fno-tree-loop-distribute-patterns', '-fno-stack-protector',
                                      '-fno-pie', '-no-pie', '-nostdlib', '-static', '-Wl,-e,_start',
                                      '-DCONFIG_FES_DE25_SDM_CONTAINMENT=1'])]
    for name, unit, flags in cases:
        binary = (args.output / name).resolve()
        compile_command = [args.cc, *common, *flags, str(args.output / (unit+'.c')), '-o', str(binary)]
        run_command = [*runner, str(binary)]
        for phase, argv in [('compile', compile_command), ('run', run_command)]:
            result = subprocess.run(argv, capture_output=True, timeout=60)
            (args.output / (name+'.'+phase+'.stdout')).write_bytes(result.stdout)
            (args.output / (name+'.'+phase+'.stderr')).write_bytes(result.stderr)
            commands.append({'name': name, 'phase': phase, 'argv': argv, 'returncode': result.returncode})
            (args.output / 'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
            if result.returncode:
                raise ValueError(name+' '+phase+' failed; logs retained')
        if name == 'smmu-enabled' and (result.stdout.count(b'CASE ') != 23 or b'PASS23' not in result.stdout):
            raise ValueError('SMMU matrix did not complete')
    report = {'passed': True, 'classification': 'actual source functions with injected kernel seams',
              'kernel_revision': record['kernel_revision'], 'supplier_profile': record,
              'controller_concurrent_pairs': 25, 'smmu_boundary_cases': 23,
              'CPU_execution': 'AArch64 QEMU' if args.qemu else 'native',
              'actual_kernel_spinlocks_or_IOMMU_core': False, 'physical_DMA_fence': False,
              'sha256': {p.name: digest(p) for p in args.output.iterdir() if p.is_file()}}
    (args.output / 'checks.json').write_text(json.dumps(report, indent=2)+'\n')
    print('SDM_CONTAINMENT_FUNCTION_MODELS_PASS; no hardware fence')


if __name__ == '__main__':
    main()
