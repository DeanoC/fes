#!/usr/bin/env python3
"""Replay Doom G4 h2 and Quake G5 on the post-F3 C28 model with online counters."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from fractions import Fraction
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

from opportunity import opportunity
from retire_source import instrument_retire


HERE = Path(__file__).resolve().parent
CAMPAIGN = 'docs/validation/2026-11-25-de25-486dx-clock'
C28_TREE = 'fa699fd163c0539b4e20e1e9a2d17b8f49d28884'
HARNESS = 'd998b9ac136f75b8b44786eaa811d2df364f7ffe2704b064ed30f8874eb35f38'
ADAPTED_BENCH = 'a57c488e6923227d1aafdb928e2f1081679c85be8f569577bb21b3d1b6e5a5b3'
PROFILE = {'doom': '5e3db43fe1fe84b903a5607df0d0b4bf33cf60273b8296179012916cdc99164d',
           'quake': '21a1506588616f51ee5d51182de50783e3cf2cbfe7c9f2e9b7a78fd234f9db0d'}
FIRST_RENDER = 'b835a2fe762373353b45055e390102e1f2f692f2ccaa8b37604781af6400638d'
DRIVE = {'doom': 'pmode', 'quake': 'bench'}


def instrument(bench):
    subprocess.run(['patch', '--batch', '--fuzz=0', '-i', str(HERE / 'machine_tb.patch')],
                   cwd=bench.parent, check=True)
    for name in ('pair_window.hpp', 'pentium_pairing.hpp', 'retire_bytes.hpp', 'x87_stalls.h'):
        shutil.copyfile(HERE / name, bench.parent / name)


def verify(game, output, games, analyzer):
    directory = output / ('dx-' + game)
    original = games.analyze_workload(game, 'dx', output, analyzer)
    if not original['pass'] or games.sha(directory / 'cpu.prof') != PROFILE[game]:
        raise RuntimeError(f'{game}: execution or CPU profile differs from the C28 replay')
    if game == 'doom':
        win = original['window']
        begin, cycles, retired = (win[k] for k in ('from_cycle', 'cycles', 'retired'))
    else:
        if games.sha(directory / 'frames/t0015.00.ppm') != FIRST_RENDER:
            raise RuntimeError('Quake: the frame that starts the rendered suffix differs from C28')
        begin, cycles, retired = 503571519, 2741666815 - 503571519, 640601632
    pair = json.loads((directory / 'pairing.json').read_text())
    if tuple(pair[k] for k in ('from_cycle', 'to_cycle', 'cycles', 'retired')) != (
            begin, begin + cycles, cycles, retired):
        raise RuntimeError(f'{game}: counter window differs from C28 evidence')
    if not 0 <= pair['pairs'] * 2 <= retired - pair['exceptions']:
        raise RuntimeError(f'{game}: invalid non-overlapping pair count')
    if pair['untagged_nonexception_retirements'] + pair['exceptions'] >= retired:
        raise RuntimeError(f'{game}: no ordinary fetched retirements observed')
    if pair['unpaged_memory_disagreements']:
        raise RuntimeError(f'{game}: {pair["unpaged_memory_disagreements"]} unpaged retire tags '
                           'differ from the reference reader; inspect pairing.json before interpreting it')
    # A reset loses the adjacency and machine state inherited at handover.
    lines = (directory / 'run.log').read_text().splitlines()
    handover = next(i for i, line in enumerate(lines) if line.startswith('RTL-BEGIN '))
    if any(line.startswith('CPU-RESET ') for line in lines[handover:]):
        raise RuntimeError(f'{game}: unexpected in-window CPU reset')
    return pair


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True,
                        help='C28 FES checkout with its matching sim_games.py (35b061f58)')
    parser.add_argument('--output', type=Path, required=True, help='fresh, private output directory')
    parser.add_argument('--cache-root', type=Path, required=True, help='locked Verilator cache')
    parser.add_argument('--drives', type=Path, required=True, help='directory of #2194 pmode.img and bench.img')
    parser.add_argument('--workload', choices=('both', 'doom', 'quake'), default='both')
    parser.add_argument('--jobs', type=int, default=4)
    args = parser.parse_args()
    source, output = args.source.resolve(), args.output.resolve()
    selected = ('doom', 'quake') if args.workload == 'both' else (args.workload,)
    def git(*parts):
        return subprocess.check_output(['git', '-C', source, *parts], text=True).strip()
    if git('rev-parse', 'HEAD:sources/misteross') != C28_TREE or git('status', '--porcelain', '--untracked-files=no', '--', 'sources/misteross'):
        parser.error('source must provide the clean C28 post-F3 misteross tree (35b061f58)')
    root = source / 'sources/misteross'
    sys.path[:0] = [str(source / CAMPAIGN), str(root)]
    import sim_games as games
    if games.sha(Path(games.__file__)) != HARNESS:
        parser.error('source sim_games.py differs from the C28 witness')
    drives = {game: (args.drives / (DRIVE[game] + '.img')).resolve() for game in selected}
    for game, path in drives.items():
        if not path.is_file() or games.sha(path) != games.DRIVES[DRIVE[game]]:
            parser.error(f'{game} requires {path} with SHA-256 {games.DRIVES[DRIVE[game]]}')
    from boards.de25nano.pc386 import build
    from boards.de25nano.pc386.i486dx import derive
    from scripts import i386_verilator, pc386_cpu_profile, sim_fes_pc386 as sim
    clock = build.model_clock('486dx')['clock']
    if clock != Fraction(2350000000, 21):
        parser.error(f'expected C28 clock, got {clock}')
    rates = build.guest_rates('486dx')
    parameters = build.record_parameters(root, '486dx')
    output.mkdir(parents=True)
    machine_rtl, presenter = games.machine_sources(sim.MACHINE_RTL, root, output, parameters.get('loaded_rearm_cycles'))
    media = games.ata_only_media(root, machine_rtl)
    if not media:
        raise RuntimeError('expected the post-F3 ATA-only model')
    sim.OUTPUT = output / 'build'
    verilator = i386_verilator.locate(args.cache_root.resolve())
    os.environ.pop('VERILATOR_ROOT', None)
    bench = games.adapt_bench(sim.CORE / 'sim', output, clock, rates, media)
    if games.sha(bench) != ADAPTED_BENCH:
        raise RuntimeError('uninstrumented machine bench differs from C28')
    instrument(bench)
    images = sim.firmware.images(True, args.jobs)
    linked = sim.oracle.link_inputs(args.jobs, True)
    machine_rates = {key: rates[key] for key in ('ATA_RESET_CLOCKS', *(
        f'{device}_{part}' for device in games.MACHINE_RATES for part in ('N', 'D')))}
    audio_rates = {key: rates[key] for key in ('US_N', 'US_D', 'OPL2_N', 'OPL2_D')}
    tool = str(verilator.path)
    audio = sim.verilate(tool, 'audio', 'pc386_audio', sim.AUDIO_RTL, None, [], args.jobs, audio_rates)
    cpu_rtl = instrument_retire(derive.sim_sources(output / 'derived', root), output / 'derived')
    probe = HERE / 'x87_stalls_probe.sv'
    cpu = sim.verilate(tool, 'dx', 'fes_i386_cpu', cpu_rtl + (probe, sim.CORE / 'sim/cpu_profile.vlt'),
                       None, [], args.jobs)
    sim.write_cpu_profile_layout(cpu.parent)
    binary = sim.verilate(tool, 'dx', 'pc386_machine', machine_rtl, bench,
                          [*linked, cpu, audio, sim.NUKED / 'opl3.c'], args.jobs,
                          machine_rates, (audio.parent, cpu.parent))
    evidence_inputs = [Path(__file__), HERE / 'machine_tb.patch', HERE / 'opportunity.py', HERE / 'retire_source.py', probe,
                       *(p for p in cpu_rtl if p.parent.name == 'p5_retire'),
                       bench, output / 'audio_link.h',
                       *(output / h for h in ('pair_window.hpp', 'pentium_pairing.hpp', 'retire_bytes.hpp', 'x87_stalls.h'))]
    games.save(output / 'inputs.json', {
        'revision': git('rev-parse', 'HEAD'), 'misteross_tree': C28_TREE, 'clock_hz': str(clock),
        'historical_cpu_parameters': {'I486': 0, 'x87_present': {g: g == 'quake' for g in selected}},
        'workloads': list(selected), 'workload_drives': {g: games.DRIVES[DRIVE[g]] for g in selected},
        'verilator': verilator.record(), 'processor_archive': games.sha(cpu),
        'machine_binary': games.sha(binary), 'oracle': sim.oracle.build_record(linked),
        'presenter': presenter, 'media': media, 'firmware': {k: games.sha(v) for k, v in images.items()},
        'instrumentation_sha256': {p.name: games.sha(p) for p in evidence_inputs},
    })
    with ThreadPoolExecutor(max_workers=len(selected)) as executor:
        jobs = [executor.submit(games.replay, g, 'dx', binary, images, drives[g], output, media) for g in selected]
        for job in jobs:
            job.result()
    for game in selected:
        result = verify(game, output, games, pc386_cpu_profile)
        result['opportunity'] = opportunity(result, clock)
        if game == 'quake':
            tail = json.loads((output / 'dx-quake/post-first-render.json').read_text())
            first, last = 1678571583, 2741666815
            if tuple(tail[k] for k in ('from_cycle', 'to_cycle', 'cycles', 'retired')) != (
                    first, last, last - first, 249801864):
                raise RuntimeError('post-first-render suffix differs from the C28 profile snapshots')
            tail['opportunity'] = opportunity(tail, clock)
            result['post_first_sampled_render'] = tail
        games.save(output / ('dx-' + game) / 'verified.json', result)
        print(game, json.dumps(result), flush=True)


if __name__ == '__main__':
    main()
