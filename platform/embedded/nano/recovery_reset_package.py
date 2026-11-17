#!/usr/bin/env python3
"""Compose a separately admitted, volatile reset-entry SOF; no card image."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import recovery
import recovery_package as base
import recovery_reset_probe as probe
import recovery_return_trace as console
from sof_hps import decode_hex, inspect

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
EXTRA = {'producer_sha256', 'spl_elf_sha256', 'console_route',
         'compiled_fdt_sha256', 'fit_fdt_sha256', 'loaded_image'}


def checked_build(directory):
    directory = Path(directory)
    receipt = json.loads((directory/'result.json').read_text())
    mode = receipt.get('reset_entry_probe')
    if (type(mode) is not dict or receipt.get('network_return_trace') is not None or
            not EXTRA.issubset(mode) or mode['producer_sha256'] != base.sha(HERE/'recovery_reset_probe.py') or
            mode['spl_elf_sha256'] != base.sha(directory/'build/spl/u-boot-spl') or
            mode['compiled_fdt_sha256'] != base.sha(directory/'build/u-boot.dtb') or
            mode['fit_fdt_sha256'] != base.sha(directory/'extracted-fdt-0.bin')):
        raise ValueError('current explicit reset-entry build required')
    source_record = {k: v for k, v in mode.items() if k not in EXTRA}
    probe.check_sources(directory/'source', source_record)
    for name in ('readelf', 'host_fdtget'):
        tool = receipt['cross_tools'][name]
        if base.sha(tool['path']) != tool['sha256']:
            raise ValueError('reset inspection tool changed')
    env = dict(os.environ, **receipt['environment'])
    symbols = subprocess.check_output([receipt['cross_tools']['readelf']['path'], '-Ws',
        directory/'build/spl/u-boot-spl'], env=env, text=True, timeout=30)
    probe.check_linked(symbols); probe.check_state(symbols)
    probe.check_binary(decode_hex((directory/'build/spl/u-boot-spl-dtb.hex').read_bytes()), mode['nonce'])
    if probe.check_elf_image((directory/'build/spl/u-boot-spl').read_bytes(),
            decode_hex((directory/'build/spl/u-boot-spl-dtb.hex').read_bytes()), symbols) != mode['loaded_image']:
        raise ValueError('SPL loaded-image proof differs')
    tool = receipt['cross_tools']['host_fdtget']['path']
    for name in ('build/u-boot.dtb', 'extracted-fdt-0.bin'):
        if console.check_console(tool, directory/name, env=env) != mode['console_route']:
            raise ValueError('reset-entry UART binding differs')
    # Only the validated diagnostic record is removed in this private view.
    # Every default recovery gate still checks the actual ELF/FIT/SPL/config.
    with tempfile.TemporaryDirectory(prefix='fes-reset-build-check-') as name:
        view = Path(name)
        plain = dict(receipt); del plain['reset_entry_probe']
        (view/'result.json').write_text(json.dumps(plain))
        for name in ('build/u-boot.itb', 'build/spl/u-boot-spl-dtb.hex', 'build/.config', 'build/u-boot'):
            dest = view/name; dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(directory/name, dest)
        for name in set(recovery.PINNED_PHY_SOURCES) | {'cmd/fes_nano_phy.c'}:
            dest = view/'source'/name; dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(directory/'source'/name, dest)
        base.checked_build(view)
    return receipt


def prepare(directory, vendor, pfg, output, *, lock=Path('/tmp/lander-quartus-powerboat-pro.lock')):
    directory, vendor, pfg = (Path(p).resolve(strict=True) for p in (directory, vendor, pfg))
    output = Path(output).absolute()
    if (output.exists() or '..' in output.parts or not output.is_relative_to(ROOT/'out') or
            any(p.is_symlink() for p in (output, *output.parents)) or any(c in str(output) for c in ',@')):
        raise ValueError('reset output must be a fresh private directory below out/')
    receipt = checked_build(directory)
    if base.sha(vendor) != base.VENDOR_SOF:
        raise ValueError('reset-entry SOF requires the retained Nano reference')
    sources = [HERE/'recovery_reset_package.py', HERE/'recovery_reset_probe.py',
               HERE/'recovery_reset_probe.inc', HERE/'spl_console.inc', HERE/'recovery.py',
               HERE/'recovery_package.py', HERE/'recovery_return_trace.py', HERE/'sof_hps.py',
               HERE.parent/'firmware/build_terasic_uboot.py', HERE.parent/'firmware/nano_cold.c',
               HERE.parent/'firmware/nano_cold_mmio.inc',
               *(HERE.parent/'firmware'/n for n in probe.HEADERS),
               directory/'result.json', directory/'build/spl/u-boot-spl',
               directory/'build/spl/u-boot-spl-dtb.hex', directory/'build/u-boot.itb', vendor, pfg]
    sources += [directory/'source'/n for n in receipt['reset_entry_probe']['sources']]
    inputs = {str(p): base.sha(p) for p in sources}
    if shutil.disk_usage(ROOT).free < 30 << 30:
        raise ValueError('at least 30 GiB free required')
    output.mkdir(parents=True)
    commands = []
    def run(label, args):
        args = [str(a) for a in args]; commands.append(args)
        with (output/(label+'.log')).open('xb') as stream:
            subprocess.run(args, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=120)
    sof = output/'reset-entry.sof'
    spl = directory/'build/spl/u-boot-spl-dtb.hex'
    # Shared with managed DE25 Pro jobs; never start a second Pro process.
    with Path(lock).open('a') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        run('pfg-combine', [pfg, '-c', vendor, sof, '-o', 'hps_path='+str(spl)])
        run('pfg-info', [pfg, '-i', sof])
    info = (output/'pfg-info.log').read_text(encoding='latin-1')
    if ('Device name: A5EB013BB23BE4SCS' not in info or
            'HPS/FPGA configuration order: HPS_AFTER_INIT_DONE' not in info):
        raise ValueError('reset-entry device or boot order differs')
    embedded = inspect(sof.read_bytes(), spl.read_bytes(), vendor.read_bytes())
    if any(base.sha(p) != h for p, h in inputs.items()):
        raise ValueError('reset-entry composition inputs changed')
    result = dict(format=1, status='prepared-reset-entry-sof', nonce=receipt['reset_entry_probe']['nonce'],
                  classification='volatile HPS rebootstrap diagnostic; not SDM reset or runtime admission',
                  sof_sha256=base.sha(sof), spl_hex_sha256=base.sha(spl),
                  embedded_spl_identity=embedded, inputs=inputs, commands=commands,
                  sd_image_prepared=False, sd_write_authorized=False, flash_write_authorized=False,
                  hardware_actions=False, hardware_acceptance=False)
    (output/'result.json').write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    return result


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('uboot-build', 'vendor-sof', 'quartus-pfg', 'output'):
        p.add_argument('--'+name, required=True, type=Path)
    a = p.parse_args(); prepare(a.uboot_build, a.vendor_sof, a.quartus_pfg, a.output)
    print('Prepared volatile reset-entry SOF:', a.output)


if __name__ == '__main__':
    try: main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as e:
        raise SystemExit('Reset-entry packaging: '+str(e))
