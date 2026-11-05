#!/usr/bin/env python3
"""Diagnostic standard-core/persona simulation; never program or admit a core."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

import sim
from build import capture_standard_board

HERE, ROOT = sim.HERE, sim.ROOT


def run(output=None, *, basic=False):
    output, sources = sim.prepare('standard-expansion', output)
    scanout = HERE.parent / 'scanout'
    link = HERE.parent / 'link-proof'
    display = sim.load(HERE.parent / 'display/build.py', 'standard_expansion_reader')
    reader = output / 'fes_menu_reader_de25.v'
    reader.write_bytes(display.retarget_menu_ram(
        (ROOT / 'cores/fes-menu/rtl/fes_menu_reader.v').read_bytes())[0])
    # Real CPU writes distinct bytes 1KiB apart repeatedly. A vacant socket
    # mirrors those addresses in internal1K; the real16K persona separates them.
    program = bytes.fromhex('f3 31 ff 7f 3e 5a 32 00 40 3e a5 32 00 44 c3 04 00')
    firmware = output / 'standard-expansion-rom.hex'
    if basic:
        firmware.write_bytes((sim.ZX81 / 'zx8x.hex').read_bytes())
    else:
        firmware.write_text(''.join(f'{byte:02x}\n' for byte in program + bytes(8192 - len(program))))
    sources += [HERE / 'fes_de25_zx81_standard.v', HERE / 'standard_expansion_harness.v',
        sim.ZX81 / 'fes_zx81_core.v', sim.ZX81 / 'zx81_video_720p.v',
        sim.ZX81 / 'zx81_ram_pack.v', scanout / 'fes_de25_session_display.v',
        scanout / 'fes_de25_scanout.v', sim.COMMON / 'rtl/fes_display_plane.v',
        sim.COMMON / 'rtl/fes_display_cdc.v', ROOT / 'cores/fes-menu/rtl/fes_menu_control.v',
        ROOT / 'cores/fes-menu/rtl/fes_menu_video.v', reader,
        link / 'fpga/shell.v', link / 'zx81-ram/personas.v', link / 'zx81-standard/bus.v']
    derived = {p.name: p.read_bytes() for p in sources if p.is_relative_to(output)}
    derived[firmware.name] = firmware.read_bytes()
    repository = ROOT.parents[1]
    captured = capture_standard_board()
    extras = [Path(__file__), HERE / 'sim.py', HERE / 'standard_expansion_harness.v',
              HERE / 'standard_expansion_tb.cpp', sim.ZX81 / 'zx81_ram_pack.v',
              sim.ZX81 / 'zx81_bus_pack.vh', link / 'fpga/shell.v', link / 'zx81-ram/personas.v',
              link / 'generated/fes_de25_pr_control.vh', link / 'generated/fes_de25_pr_control.hpp',
              link / 'zx81-standard/bus.v']
    extras += [HERE / 'cosim' / name for name in ('basic.hpp', 'basic_observer.hpp', 'wire.hpp')]
    runtime = repository / 'sources/libmister-runtime'
    client_sources = [runtime / 'src/native' / name for name in
                      ('fes_gp.cpp', 'de25_pr_control.cpp', 'artifacts.cpp', 'diagnostic.cpp')]
    extras += [*client_sources, *runtime.joinpath('src/native').rglob('*.hpp'),
               runtime / 'include/libmister-runtime/runtime.h',
               runtime / 'tests/support/de25_standard_descriptor.hpp']
    captured.update({p.relative_to(repository).as_posix(): p.read_bytes() for p in extras})
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repository, text=True).strip()
    committed = all(subprocess.run(['git', 'show', revision + ':' + name], cwd=repository,
                    capture_output=True).stdout == data for name, data in captured.items())
    snapshot = output / 'functional-source'
    for name, data in captured.items():
        p = snapshot / name
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(data)
    # Compile the retained bytes, including the actual runtime clients. The
    # test adapter only clocks the Avalon pins; it implements no GP/PR protocol.
    def frozen(path):
        return path if path.is_relative_to(output) else snapshot / path.relative_to(repository)
    runtime = snapshot / 'sources/libmister-runtime'
    includes = (HERE, sim.COMMON / 'generated', sim.ZX81, sim.Z80, scanout, link)
    client_includes = (runtime / 'include', runtime / 'src', runtime / 'tests/support')
    flags = ('UNUSEDSIGNAL', 'UNUSEDPARAM', 'UNOPTFLAT', 'CASEINCOMPLETE', 'WIDTHTRUNC',
             'WIDTHEXPAND', 'SYNCASYNCNET', 'PINCONNECTEMPTY', 'DECLFILENAME',
             'IMPLICITSTATIC', 'VARHIDDEN', 'CASEX', 'BLKSEQ')
    command = ['verilator', '--cc', '--exe', '--build', '-j', '2', '--x-initial', 'unique', '-O3', '-Wall',
        *['-Wno-' + flag for flag in flags], '--top-module', 'fes_de25_standard_expansion_harness',
        '--Mdir', str(output / 'obj'),
        *['-I' + str(frozen(p)) for p in includes],
        '-CFLAGS', '-O2 -std=c++14 -pthread ' + ' '.join('-I' + str(p) for p in
            (frozen(link / 'generated'), frozen(HERE / 'generated'), *client_includes)),
        '-LDFLAGS', '-pthread', *[str(frozen(p)) for p in sources],
        str(frozen(HERE / 'standard_expansion_tb.cpp')), *[str(frozen(p)) for p in client_sources]]
    with (output / 'build.log').open('wb') as log:
        subprocess.run(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=300)
    with (output / 'simulation.log').open('wb') as log:
        for seed in (17, 53):
            log.write(f'RANDOM_INITIAL_MEMORY seed={seed}\n'.encode())
            log.flush()
            directory = output
            if basic:
                directory = output / f'seed-{seed}'
                directory.mkdir(mode=0o700)
                (directory / firmware.name).write_bytes(firmware.read_bytes())
            subprocess.run([str(output / 'obj/Vfes_de25_standard_expansion_harness'),
                *(['--basic'] if basic else []),
                '+verilator+rand+reset+2', '+verilator+seed+' + str(seed)],
                cwd=directory, stdout=log, stderr=subprocess.STDOUT, check=True,
                timeout=1800 if basic else 120)
            if basic and (directory / firmware.name).read_bytes() != firmware.read_bytes():
                raise ValueError('real-ROM simulation firmware changed during execution')
    require_unchanged = all((repository / name).read_bytes() == data for name, data in captured.items())
    if not require_unchanged:
        raise ValueError('standard expansion proof sources changed during simulation')
    if any((output / name).read_bytes() != data for name, data in derived.items()):
        raise ValueError('derived simulation source changed during execution')
    sha = lambda data: hashlib.sha256(data).hexdigest()
    receipt = {'status': 'passed', 'classification': 'actual standard core, FesGpCoreDriver, De25PrControl and RAM personas; ' + ('real-ROM BASIC' if basic else 'diagnostic ROM'),
        'source_revision': revision, 'source_committed': committed,
        'source_sha256': {name: sha(data) for name, data in sorted(captured.items())},
        'derived_source_sha256': {name: sha(data) for name, data in sorted(derived.items())},
        'firmware_sha256': sha(firmware.read_bytes()), 'build_command': command,
        'random_memory_seeds': [17, 53],
        'verilator_version': subprocess.check_output(['verilator', '--version'], text=True).strip(),
        'binary_sha256': sha((output / 'obj/Vfes_de25_standard_expansion_harness').read_bytes()),
        'simulation_log_sha256': sha((output / 'simulation.log').read_bytes()),
        'physical_PR': 'selection-mux-model-only', 'primitive_memory': 'behavioral',
        'production_Qsys_reset_boundary': 'explicit bus_reset port; RTL equivalence checked; Qsys generation and physical PR are separate gates',
        'BASIC_firmware': 'actual-ROM-SAVE-load-run-reload' if basic else 'not-tested', 'hardware_acceptance': False}
    if basic:
        import re
        results = [json.loads(value) for value in re.findall(
            r'^PASS STANDARD_BASIC (\{[^\n]+\}) uploads=2 callbacks=3 picture_frames=[0-9]+$',
            (output / 'simulation.log').read_text(), re.M)]
        if len(results) != 2 or any(result.get('loads') != 2 or result.get('runs') != 2 or
                result.get('printed_value') != 7 or not result.get('reload_removed_edit') for result in results):
            raise ValueError('incomplete actual standard BASIC roundtrip evidence')
        saves = [(output / f'seed-{seed}/standard-rom-save.p').read_bytes() for seed in (17, 53)]
        if saves[0] != saves[1] or any(result['saved_bytes'] != len(saves[0]) for result in results):
            raise ValueError('actual standard ROM SAVE differs across seeds')
        receipt['BASIC_roundtrips'] = results
        receipt['actual_SAVE_sha256'] = sha(saves[0])
        receipt['upload_transport'] = 'actual ComputerMediaSnapshot/FesGpCoreDriver.InsertMedia; advertised 512-byte chunks'
    (output / 'proof.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    print((output / 'simulation.log').read_text(), end='')
    return output


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--basic', action='store_true', help='run actual ROM SAVE/load/run/reload with empty/16K/empty personas')
    args = parser.parse_args()
    print(run(args.output, basic=args.basic))
