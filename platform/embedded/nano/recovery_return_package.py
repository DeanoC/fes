#!/usr/bin/env python3
"""Prepare the explicit return-trace recovery files; regular files only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import recovery
import recovery_package as base
import recovery_return_trace as trace
from upload import load_kit as load_base_kit

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
BASE_FIT = 'c27efed4828f73ad000fe78649ae01d665e12452f457c3e569d25b20b5b031dd'
BASE_SOF = 'ce143f50e05c9895381047f921be4a0c38d9a7eacfa75b3c0709be4ff7eafe75'
BASE_SPL = '83e1f29015c8f07a95d439f9e6e8f93be6f76a918121e2fd658462e7deca3e23'
BASE_BL31 = '96889cbd3412b0458cdcf6b43d24462f6f40a7850b508e08f00d61569c8a26d5'
MODE = trace.SCOPE


def sha(p):
    with Path(p).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def regular(p, maximum):
    p = Path(p)
    if (any(q.is_symlink() for q in (p, *p.parents)) or not p.is_file() or
            not 0 < p.stat().st_size <= maximum):
        raise ValueError('trace recovery input must be a bounded regular file: ' + str(p))
    return p


def checked_build(directory):
    """Validate the added layer, then delegate the unchanged base build gates."""
    directory = Path(directory)
    receipt = json.loads(regular(directory/'result.json', 1 << 20).read_text())
    mode = receipt.get('network_return_trace')
    if not isinstance(mode, dict) or mode != dict(
            profile_sha256=sha(HERE/'recovery_return_trace.py'), sources=mode.get('sources'),
            source_inverse_verified=True, phases=len(trace.PHASES), UART_poll_limit=100000,
            string_byte_limit=96, hardware_qualified=False, default_recovery_profile_compatible=False,
            console_route=mode.get('console_route'),
            compiled_fdt_sha256=sha(regular(directory/'build/u-boot.dtb',1<<20)),
            fit_fdt_sha256=sha(regular(directory/'extracted-fdt-0.bin',1<<20))):
        raise ValueError('explicit current return-observation build required')
    tool=receipt['cross_tools']['host_fdtget']
    if sha(regular(tool['path'],32<<20))!=tool['sha256']:
        raise ValueError('trace console inspection tool changed')
    env=dict(os.environ,**receipt['environment'])
    route=trace.check_console(tool['path'],directory/'build/u-boot.dtb',env=env)
    if route!=mode['console_route'] or trace.check_console(tool['path'],directory/'extracted-fdt-0.bin',env=env)!=route:
        raise ValueError('trace package console binding differs')
    original = trace.reconstruct(directory/'source', mode['sources'])
    trace.check_phy_sources(directory/'source', mode['sources'],
                            receipt['network_recovery']['phy_sources'], recovery)
    names = {'build/u-boot.itb': 2 << 20, 'build/spl/u-boot-spl-dtb.hex': 2 << 20,
             'build/.config': 1 << 20, 'build/u-boot': 32 << 20,
             'build/u-boot.dtb':1<<20,'extracted-fdt-0.bin':1<<20}
    for name, maximum in names.items():
        regular(directory/name, maximum)
    # This private source view removes only the independently inverted trace
    # statements. The original validator checks all base gates against the
    # actual unchanged receipt, ELF, FIT, SPL and configuration bytes.
    with tempfile.TemporaryDirectory(prefix='fes-return-build-check-') as d:
        view = Path(d)
        (view/'result.json').write_bytes((directory/'result.json').read_bytes())
        for name in names:
            p = view/name; p.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(directory/name, p)
        for name in set(recovery.PINNED_PHY_SOURCES) | {'cmd/fes_nano_phy.c'}:
            p = view/'source'/name; p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(original[name] if name in original else (directory/'source'/name).read_text())
        base.checked_build(view)
    trace.check_linked(subprocess.check_output(
        ['readelf', '-Ws', directory/'build/u-boot'], text=True, timeout=30))
    return receipt, directory/'build/u-boot.itb', directory/'build/spl/u-boot-spl-dtb.hex', directory/'build/.config'


def prepare(directory, baseline, output):
    output = Path(output).absolute()
    if (output.exists() or '..' in output.parts or not output.is_relative_to(ROOT/'out') or
            any(p.is_symlink() for p in (output, *output.parents)) or any(c in str(output) for c in ',@')):
        raise ValueError('trace output must be a fresh private directory under out/')
    directory, baseline = Path(directory).resolve(strict=True), Path(baseline).resolve(strict=True)
    if not directory.is_relative_to(ROOT/'out'):
        raise ValueError('trace build must be private under this worktree out/')
    receipt, fit, spl, config = checked_build(directory)
    old = load_base_kit(baseline)
    prior = json.loads((baseline/'result.json').read_text())
    if (old['fit_sha256'] != BASE_FIT or sha(baseline/'network-recovery.sof') != BASE_SOF or
            prior['spl_sha256'] != receipt['spl_sha256'] or receipt['spl_sha256'] != BASE_SPL or
            prior['bl31_sha256'] != receipt['bl31_sha256'] or receipt['bl31_sha256'] != BASE_BL31):
        raise ValueError('traced build requires the exact qualified baseline SOF/SPL/BL31')
    tools = {n: Path(shutil.which(n) or '').absolute() for n in ('mformat', 'mmd', 'mcopy')}
    if any(not p.is_file() for p in tools.values()):
        raise ValueError('mtools is required')
    inputs = [directory/'result.json', fit, spl, config, directory/'build/u-boot',
              baseline/'result.json', baseline/'RECOVERY.JSON', baseline/'network-recovery.sof',
              baseline/'nano-recovery-sd.img', HERE/'recovery_return_trace.py', HERE/'recovery.py',
              HERE/'recovery_package.py', HERE/'upload.py', Path(__file__),
              HERE.parent/'firmware/build_terasic_uboot.py', *(p.resolve() for p in tools.values()),
              directory/'build/u-boot.dtb',directory/'extracted-fdt-0.bin',
              Path(receipt['cross_tools']['host_fdtget']['path'])]
    inputs += [directory/'source'/n for n in receipt['network_return_trace']['sources']['prepared']]
    inputs += [directory/'source'/n for n in receipt['network_recovery']['phy_sources']['prepared']]
    hashes = {str(p): sha(p) for p in inputs}
    output.mkdir(parents=True)
    shutil.copy2(fit, output/'u-boot.itb'); shutil.copy2(spl, output/'u-boot-spl-dtb.hex')
    shutil.copy2(baseline/'network-recovery.sof', output/'network-recovery.sof')
    manifest = json.loads(old['manifest'])
    manifest.update(classification='explicit recovery return-observation diagnostic; no board acceptance',
                    fit_sha256=sha(fit), return_observations=dict(scope=MODE, phases=34,
                    profile_sha256=sha(HERE/'recovery_return_trace.py')))
    manifest['limitations'] = ['explicit trace-aware loader/operator required',
        'regular-file image only; no device access or physical qualification',
        'returned boundaries prove neither command success nor DMA drain']
    (output/'RECOVERY.JSON').write_text(json.dumps(manifest, indent=2, sort_keys=True)+'\n')
    commands = []
    def run(label, argv):
        argv = [str(x) for x in argv]; commands.append(argv)
        with (output/(label+'.log')).open('xb') as stream:
            subprocess.run(argv, check=True, stdout=stream, stderr=subprocess.STDOUT, timeout=120)
    disk = base.make_card(output, fit, tools, run)
    with (baseline/'nano-recovery-sd.img').open('rb') as stream:
        before = stream.read(base.FAT_SECTOR*512)
    with disk.open('rb') as stream:
        after = stream.read(base.FAT_SECTOR*512)
    padded = (max(len(old['fit']), fit.stat().st_size)+511) & ~511
    expected = bytearray(before); start = base.FIT_SECTOR*512
    expected[start:start+padded] = fit.read_bytes()+bytes(padded-fit.stat().st_size)
    if after != expected:
        raise ValueError('trace prefix changes bytes outside the fixed FIT mirror')
    if any(sha(Path(n)) != h for n, h in hashes.items()):
        raise ValueError('trace packaging input changed')
    result = dict(manifest, status='prepared-return-trace', inputs=hashes, commands=commands,
                  tools={n:dict(path=str(p),resolved_path=str(p.resolve())) for n,p in tools.items()},
                  uboot_build=str(directory.relative_to(ROOT)), build_receipt_sha256=sha(directory/'result.json'),
                  sd_image_sha256=sha(disk), sd_image_size=disk.stat().st_size,
                  prefix_sha256=hashlib.sha256(after).hexdigest(), old_prefix_sha256=old['prefix_sha256'],
                  reused_qualified_SOF=True, prefix_change_only_fit_mirror=True, hardware_actions=False)
    (output/'result.json').write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    return result


def load_kit(directory):
    """Accept only this trace-aware package; no generic-status normalization."""
    directory = Path(directory)
    record = json.loads(regular(directory/'result.json', 1 << 20).read_text())
    manifest = regular(directory/'RECOVERY.JSON', 16384).read_bytes()
    description = json.loads(manifest)
    mode = dict(scope=MODE, phases=34, profile_sha256=sha(HERE/'recovery_return_trace.py'))
    required_manifest=('format','fit_sha256','spl_sha256','bl31_sha256','sof_sha256','fat32_start_sector',
        'fat32_sectors','native_handoff','automatic_writes','spl_boot_mode','spl_payload_path',
        'spl_fat_partition','recovery_phy','phy_driver_linked','return_observations')
    if (not isinstance(description,dict) or any(k not in description for k in required_manifest) or
            record.get('status') != 'prepared-return-trace' or record.get('return_observations') != mode or
            record.get('native_handoff') is not False or record.get('automatic_writes') is not False or
            record.get('hardware_actions') is not False or record.get('prefix_change_only_fit_mirror') is not True or
            record.get('reused_qualified_SOF') is not True or record.get('recovery_phy') != recovery.PHY or
            record.get('spl_sha256') != BASE_SPL or record.get('bl31_sha256') != BASE_BL31 or
            record.get('sof_sha256') != BASE_SOF or record.get('spl_boot_mode') != 'fat' or
            record.get('spl_payload_path') != 'u-boot.itb' or record.get('spl_fat_partition') != 1 or
            record.get('fat32_start_sector') != 32768 or record.get('fat32_sectors') != 262144 or
            not description or any(record.get(k) != v for k, v in description.items())):
        raise ValueError('not the explicit return-trace recovery package')
    for name, want in record['inputs'].items():
        if sha(regular(name, 256 << 20)) != want:
            raise ValueError('trace package source changed: ' + name)
    if set(record['tools'])!={'mformat','mmd','mcopy'}:
        raise ValueError('trace package tool dispatch differs')
    for name,tool in record['tools'].items():
        path=Path(tool['path']);resolved=regular(tool['resolved_path'],256<<20)
        if (not path.is_absolute() or path.name!=name or str(path.resolve(strict=True))!=str(resolved) or
                str(resolved) not in record['inputs']):
            raise ValueError('trace package tool target differs')
    name=record['uboot_build']; relative=Path(name)
    if (relative.is_absolute() or '..' in relative.parts or relative.as_posix()!=name or
            not relative.parts or relative.parts[0]!='out'):
        raise ValueError('trace build reference is outside the worktree')
    build = ROOT/relative
    required={str(HERE/'recovery_return_trace.py'),str(HERE/'recovery.py'),
              str(HERE/'recovery_package.py'),str(HERE/'upload.py'),str(Path(__file__)),
              str(HERE.parent/'firmware/build_terasic_uboot.py'),str(build/'result.json')}
    if not required.issubset(record['inputs']):
        raise ValueError('trace package omitted producer bindings')
    if sha(build/'result.json') != record['build_receipt_sha256']:
        raise ValueError('trace package build receipt changed')
    checked_build(build)
    fit = regular(directory/'u-boot.itb', 2 << 20).read_bytes()
    disk = regular(directory/'nano-recovery-sd.img', 150994944)
    if (disk.stat().st_size != record['sd_image_size'] or disk.stat().st_size != 150994944 or
            sha(disk) != record['sd_image_sha256'] or hashlib.sha256(fit).hexdigest() != record['fit_sha256'] or
            sha(regular(directory/'network-recovery.sof', 32 << 20)) != BASE_SOF or
            sha(regular(directory/'u-boot-spl-dtb.hex', 2 << 20)) != BASE_SPL):
        raise ValueError('trace recovery artifact changed')
    with disk.open('rb') as stream:
        prefix = stream.read(base.FAT_SECTOR*512)
    if (hashlib.sha256(prefix).hexdigest() != record['prefix_sha256'] or
            prefix[base.FIT_SECTOR*512:base.FIT_SECTOR*512+len(fit)] != fit):
        raise ValueError('trace recovery prefix changed')
    return dict(manifest=manifest, fit=fit, prefix_sha256=record['prefix_sha256'],
                image_sha256=record['sd_image_sha256'], fit_sha256=record['fit_sha256'],
                scope=MODE, SPL_sha256=BASE_SPL, BL31_sha256=BASE_BL31)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--uboot-build', required=True, type=Path)
    parser.add_argument('--baseline-kit', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args=parser.parse_args()
    prepare(args.uboot_build, args.baseline_kit, args.output)
    load_kit(args.output)
    print('Prepared return-trace recovery regular files:', args.output)


if __name__ == '__main__':
    try: main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as e:
        raise SystemExit('Return-trace recovery package: '+str(e))
