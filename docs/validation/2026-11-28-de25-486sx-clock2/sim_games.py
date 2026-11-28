#!/usr/bin/env python3
"""Run #2264's DOOM, 3DBench and Quake workloads on the DE25 486SX at a checkout's SX clock.

method.md G4/G5 (2026-11-28-de25-486sx-clock2): one arm, the shared CPU, at the checkout's 486sx clock,
guest rates and presenter. Run it at the candidate and at main (the C0 = 34 control), then compare the two
analysis.json files with --compare. Quake runs without --x87: the SX has no 387.

Use a clean, dedicated FES checkout for the candidate. Generated testbenches,
binaries and evidence go into a fresh output directory. No source is edited.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
from fractions import Fraction
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys


DRIVES = {
    'pmode': 'b4be6e29297168abb31ca536829cbd8a42e9fb05093d16793932ea51d72814b3',
    'bench': 'e69a4cb6462a79c47f55adef11d41c652481017dd541ae980cf27beed5e4823c',
}
MACHINE_RATES = ('ISA', 'PIT', 'RTC', 'US', 'DMA', 'DOT0', 'DOT1')
ATA_ONLY_ARGUMENTS = ('--floppy-type', 'A:0', '--floppy-type', 'B:0', '--boot-device', '0')
ATA_ONLY_INPUTS = {'floppy_verify': 2, 'floppy_sectors': 26, 'floppy_crc': 64, 'fdc_frozen': 1}


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def save(path, data):
    path.write_text(json.dumps(data, indent=2) + '\n')


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise RuntimeError(f'expected one bench anchor: {before!r}')
    return text.replace(before, after)


def ata_only_media(root, rtl):
    core = root / 'cores/fes-pc386/rtl'
    verifier = core / 'pc386_floppy_verify.sv'
    if not verifier.is_file():
        return None
    if rtl.count(verifier) != 1:
        raise RuntimeError('the F3 machine bench must name exactly one floppy verifier')
    from boards.de25nano.pc386 import derive

    system = core / 'pc386_system.sv'
    geometry = {'floppy_type': 6, 'boot_device': 2, 'floppy_present': 2, 'floppy_write_protect': 2,
                'floppy_two_sided': 2, 'floppy_cylinders': 14, 'floppy_spt': 12}
    ports = {**geometry, **ATA_ONLY_INPUTS}
    derived, record = derive.retarget_system(system.read_bytes())
    if any(derived.count(f".{name}({width}'{'b' if width == 1 else 'd'}0)".encode()) != 1
           for name, width in ports.items()):
        raise RuntimeError('the DE25 system no longer selects the expected ATA-only media contract')
    return {'mode': 'ata-only', 'startup_arguments': list(ATA_ONLY_ARGUMENTS), 'port_values': dict.fromkeys(ports, 0),
            'system': system.relative_to(root).as_posix(), 'system_source_sha256': record['source_sha256'],
            'system_derived_sha256': record['derived_sha256'],
            'verifier': verifier.relative_to(root).as_posix(), 'verifier_sha256': sha(verifier)}


def adapt_bench(sources, output, clock, rates, media=None):
    period = Fraction(10**12, 1) / clock
    cpp = (sources / 'machine_tb.cpp').read_text()
    for before, after in (
        ('CLOCK_HZ = 100e6', f'CLOCK_HZ = {clock.numerator}.0 / {clock.denominator}.0'),
        ('SYS_PERIOD_PS = 10000', f'SYS_PERIOD_PS = {period.numerator}'),
        ('PIXEL_HALF_PS = 6734', f'PIXEL_HALF_PS = 6734 * {period.denominator}'),
        ('STALL_CLOCKS = 10000000', 'STALL_CLOCKS = uint64_t(CLOCK_HZ / 10)'),
        ('    auto oracle = fes_i386::make_oracle(machine);',
         '    auto oracle = fes_i386::make_oracle(machine);\n'
         '        std::printf("MODEL-CLOCK %.9f\\n", CLOCK_HZ);'),
        ('                            if (options.rtl_for >= 0) limit = machine.cycles +',
         '                            std::printf("HANDOVER %s cr0=%08x cr3=%08x flags=%08x",\n'
         '                                        site(state).c_str(), state.cr0, state.cr3, state.eflags);\n'
         '                            for (auto value : state.gpr) std::printf(" %08x", value);\n'
         '                            std::printf("\\n");\n'
         '                            if (options.rtl_for >= 0) limit = machine.cycles +'),
    ):
        cpp = replace_once(cpp, before, after)
    if media is not None:
        boot = '        top.boot_device = uint8_t(boot_device);'
        fixed = '\n'.join(f'        top.{name} = 0;' for name in ATA_ONLY_INPUTS)
        cpp = replace_once(cpp, boot, f'{boot}\n{fixed}')
    # Both clocks use integers in units of 1 / period.denominator picoseconds.
    header = (sources / 'audio_link.h').read_text()
    header = replace_once(header, 'The 48 kHz frame request: 3 every 6250 clocks of 100 MHz.',
                          'Frame requests at the DE25 audio master divided by 256.')
    header = replace_once(header, 'request_phase += 3;', f'request_phase += {rates["AUDIO_N"]};')
    header = replace_once(header, 'request_phase >= 6250', f'request_phase >= {rates["AUDIO_D"]}')
    header = replace_once(header, 'request_phase -= 6250;', f'request_phase -= {rates["AUDIO_D"]};')
    (output / 'audio_link.h').write_text(header)
    bench = output / 'machine_tb.cpp'
    bench.write_text(cpp)
    return bench


def machine_sources(rtl, root, output, loaded_rearm_cycles):
    """Select the board's presenter for both CPU arms when the fit derives it."""
    if loaded_rearm_cycles is None:
        return rtl, None

    from boards.de25nano.pc386 import derive as board_derive

    upstream = root / board_derive.PRESENTER_UPSTREAM
    if rtl.count(upstream) != 1:
        raise RuntimeError('the machine bench must name exactly one upstream presenter')
    source = upstream.read_bytes()
    derived, record = board_derive.retarget_presenter_loaded(source)
    if (record['loaded_rearm_cycles'] != loaded_rearm_cycles
            or record['source_sha256'] != hashlib.sha256(source).hexdigest()
            or record['derived_sha256'] != hashlib.sha256(derived).hexdigest()):
        raise RuntimeError('the presenter derivation differs from the recorded build parameters or bytes')
    target = output / 'derived' / upstream.name
    target.parent.mkdir(exist_ok=True)
    target.write_bytes(derived)
    presenter = {'source': upstream.relative_to(root).as_posix(),
                 'derived': target.relative_to(output).as_posix(), **record}
    return tuple(target if path == upstream else path for path in rtl), presenter


def arguments_for(workload, output, media=None):
    common = ['--cpu', 'rtl', '--cpu-profile', str(output / 'cpu.prof'), '--cpu-profile-every', '0.5']
    if media is not None:
        common.extend(ATA_ONLY_ARGUMENTS)
    if workload == 'doom':
        return common + ['--limit-seconds', '300', '--type', r'cd \doom|doom -timedemo demo3|',
                         '--rtl-at-flip', '40', '--rtl-for', '1.0']
    if workload == '3dbench':
        return common + ['--limit-seconds', '11.5', '--type', r'cd \marks\3dbench|3dbench|',
                         '--keys', '7.3:|', '--rtl-from', '7.5',
                         '--frames', str(output / 'frames'), '--frame-every', '0.5']
    return common + ['--limit-seconds', '300', '--type',
                     r'cd \marks\quakes|quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio|',
                     '--rtl-at-prompt', '1', '--rtl-for', '20',
                     '--frames', str(output / 'frames'), '--frame-every', '1.0']


RUNS = (('sx', 'doom'), ('sx', '3dbench'), ('sx', 'quake'))
MODEL = '486sx'


def replay(workload, arm, binary, images, drive, output, media=None):
    directory = output / f'{arm}-{workload}'
    directory.mkdir()
    if workload != 'doom':
        (directory / 'frames').mkdir()
    disk = directory / 'disk.img'
    shutil.copyfile(drive, disk)
    command = [str(binary), '--bios', str(images['bios']), '--vgabios', str(images['vgabios']),
               '--disk', str(disk), *arguments_for(workload, directory, media)]
    save(directory / 'command.json', command)
    timeout_hours = 12 if workload == 'quake' else 8
    with (directory / 'run.log').open('w') as log:
        try:
            result = subprocess.run(command, cwd=directory, stdout=log, stderr=subprocess.STDOUT,
                                    timeout=timeout_hours * 3600)
        except subprocess.TimeoutExpired:
            outcome = {'return_code': 124, 'timeout_hours': timeout_hours}
        else:
            outcome = {'return_code': result.returncode}
    save(directory / 'result.json', outcome)


def analyze_workload(workload, arm, output, analyzer):
    directory = output / f'{arm}-{workload}'
    report = json.loads((directory / 'result.json').read_text())
    lines = (directory / 'run.log').read_text().splitlines()
    # machine_tb exits 1 at its hard time limit, the intended 3DBench stop.
    expected_end, expected_code = ('END limit ', 1) if workload == '3dbench' else ('END rtl-window ', 0)
    markers = ('MODEL-CLOCK ', 'PROMPT ', 'RTL-BEGIN ', 'HANDOVER ', 'STOP ', 'RTL ', 'END ')
    report['markers'] = [line for line in lines if line.startswith(markers)]
    report['pass'] = (any(line.startswith(expected_end) for line in lines)
                      and any(re.match(r'RTL retired=[1-9]\d* oracle_steps=0\b', line) for line in lines)
                      and not any(line.startswith(('STOP ', 'SHUTDOWN ')) for line in lines)
                      and report['return_code'] == expected_code)
    if workload != 'doom':
        frames = directory / 'frames'
        report['frames'] = sorted(path.name for path in frames.glob('t*.ppm') if path.stat().st_size)
        report['pass'] &= bool(report['frames']) and (frames / 'final.ppm').is_file()
    if workload != 'quake' and report['pass']:
        snapshots = analyzer.load((directory / 'cpu.prof').read_text().splitlines())
        bounds = (8.0, 11.5) if workload == '3dbench' else sorted(snapshots)[1:3]
        window = analyzer.window(snapshots, *bounds)
        save(directory / 'window.json', window)
        report['window'] = {key: window[key] for key in ('from', 'to', 'from_cycle', 'cycles', 'retired', 'cpi')}
        report['pass'] &= window['retired'] > 0
    return report


def outcome(report):
    """The execution outcome method.md's G5 compares: end marker, CPU stop and exit code."""
    ends = [line.split()[1] if len(line.split()) > 1 else '' for line in report['markers'] if line.startswith('END ')]
    stops = [line for line in report['markers'] if line.startswith(('STOP ', 'SHUTDOWN '))]
    return {'end': ends[-1] if ends else None, 'stopped': bool(stops), 'return_code': report['return_code']}


def analyze(output, analyzer):
    results = {work: analyze_workload(work, arm, output, analyzer) for arm, work in RUNS
               if (output / f'{arm}-{work}/result.json').is_file()}
    g4 = None
    if all(work in results for work in ('doom', '3dbench')):
        g4 = results['doom']['pass'] and results['3dbench']['pass']
    quake = results.get('quake')
    if quake is not None:
        quake['outcome'] = outcome(quake)
    report = {'g4_pass': g4, 'workloads': results,
              'cpi': {work: results[work].get('window', {}).get('cpi') for work in ('doom', '3dbench')
                      if work in results}}
    save(output / 'analysis.json', report)
    print(json.dumps(report, indent=2))
    if g4 is None or quake is None:
        return 2
    return 0 if g4 else 1


def compare(candidate, control):
    """G4 at the candidate, CPI against the control (reported), and G5's Quake outcome equality."""
    a, b = (json.loads((path / 'analysis.json').read_text()) for path in (candidate, control))
    ratios = {work: (a['cpi'][work] / b['cpi'][work] if a['cpi'].get(work) and b['cpi'].get(work) else None)
              for work in ('doom', '3dbench')}
    quake = {'candidate': a['workloads']['quake']['outcome'], 'control': b['workloads']['quake']['outcome']}
    report = {'g4_pass': a['g4_pass'] is True, 'cpi_ratio_candidate_over_control': ratios,
              'g5_quake': quake, 'g5_pass': quake['candidate'] == quake['control']}
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if report['g4_pass'] and report['g5_pass'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path, help='clean FES checkout of the candidate')
    parser.add_argument('--output', required=True, type=Path, help='fresh output directory')
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--drives', type=Path, help='directory of the census bench.img and pmode.img')
    parser.add_argument('--analyze-existing', action='store_true', help='analyze retained logs and profiles only')
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--compare', nargs=2, type=Path, metavar=('CANDIDATE', 'CONTROL'),
                        help='compare two retained outputs and exit')
    args = parser.parse_args()
    if args.compare:
        return compare(*(path.resolve() for path in args.compare))
    source, output = args.source.resolve(), args.output.resolve()

    def git(*arguments):
        return subprocess.check_output(['git', '-C', source, *arguments], text=True).strip()

    if git('status', '--porcelain', '--untracked-files=no'):
        parser.error('--source must have clean tracked files')
    revision = git('rev-parse', 'HEAD')
    root = source / 'sources/misteross'
    sys.path.insert(0, str(root))
    from scripts import pc386_cpu_profile

    if args.analyze_existing:
        if json.loads((output / 'inputs.json').read_text())['revision'] != revision:
            parser.error('--source must match the revision of the retained run')
        return analyze(output, pc386_cpu_profile)
    if not args.cache_root or not args.drives:
        parser.error('--cache-root and --drives are required to run the simulations')
    output.mkdir(parents=True)
    shutil.copyfile(__file__, output / 'harness.py')
    from boards.de25nano.pc386 import build
    from scripts import i386_verilator, sim_fes_pc386 as sim

    drives = {name: (args.drives / f'{name}.img').resolve() for name in DRIVES}
    if any(sha(path) != DRIVES[name] for name, path in drives.items()):
        raise RuntimeError('a census image differs from the #2194 fixture')
    pll = build.model_clock(MODEL)
    clock, rates = pll['clock'], build.guest_rates(MODEL)
    parameters = build.record_parameters(root, MODEL)
    if parameters['x87_present'] is not False:
        raise RuntimeError('the 486SX model records a 387')
    loaded_rearm_cycles = parameters.get('loaded_rearm_cycles')
    machine_rtl, presenter = machine_sources(sim.MACHINE_RTL, root, output, loaded_rearm_cycles)
    media = ata_only_media(root, machine_rtl)
    sim.OUTPUT = output / 'build'
    verilator = i386_verilator.locate(args.cache_root.resolve())
    os.environ.pop('VERILATOR_ROOT', None)
    bench = adapt_bench(sim.CORE / 'sim', output, clock, rates, media)
    images = sim.firmware.images(True, args.jobs)
    linked = sim.oracle.link_inputs(args.jobs, True)
    args.verilator = str(verilator.path)
    machine_rates = {key: rates[key] for key in ('ATA_RESET_CLOCKS', *(
        f'{device}_{part}' for device in MACHINE_RATES for part in ('N', 'D')))}
    audio_rates = {key: rates[key] for key in ('US_N', 'US_D', 'OPL2_N', 'OPL2_D')}
    audio = sim.verilate(args.verilator, 'audio', 'pc386_audio', sim.AUDIO_RTL, None, [], args.jobs, audio_rates)
    binaries = {}
    witness = {'revision': revision, 'harness_sha256': sha(output / 'harness.py'), 'model': MODEL,
               'c0': pll['c0'], 'clock_hz': str(clock), 'guest_rate_parameters': rates,
               'verilator': verilator.record(), 'oracle': sim.oracle.build_record(linked),
               'fixtures': DRIVES, 'firmware': {name: sha(path) for name, path in images.items()},
               'adapted_bench': {path.name: sha(path) for path in (bench, output / 'audio_link.h')},
               'binaries': {}}
    if presenter is not None:
        witness['presenter'] = presenter
    if media is not None:
        witness['media'] = media
    save(output / 'inputs.json', witness)
    for arm, rtl in (('sx', sim.CPU_RTL),):
        print(f'Building {arm} at {float(clock):.6f} Hz', flush=True)
        cpu = sim.verilate(args.verilator, arm, 'fes_i386_cpu', rtl + (sim.CORE / 'sim/cpu_profile.vlt',),
                           None, [], args.jobs)
        sim.write_cpu_profile_layout(cpu.parent)
        binary = sim.verilate(args.verilator, arm, 'pc386_machine', machine_rtl, bench,
                              [*linked, cpu, audio, sim.NUKED / 'opl3.c'], args.jobs,
                              machine_rates, (audio.parent, cpu.parent))
        binaries[arm] = binary
        witness['binaries'][arm] = sha(binary)
    save(output / 'inputs.json', witness)
    with ThreadPoolExecutor(max_workers=len(RUNS)) as executor:
        runs = [executor.submit(replay, work, arm, binaries[arm], images,
                                drives['pmode' if work == 'doom' else 'bench'], output, media)
                for arm, work in RUNS]
        for run in runs:
            run.result()
    return analyze(output, pc386_cpu_profile)


if __name__ == '__main__':
    raise SystemExit(main())
