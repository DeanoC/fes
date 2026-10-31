"""Expand selected core families into independent, complete simulation jobs.

Two tiers: pull requests, the merge queue and pushes to main run the "smoke"
tier, where the slow families run a short boot/frames target instead of their
full suite. Manual runs and the nightly regression run the "full" tier, so
full sims (including the coleco -oss board twins) run nightly only.
"""
from scripts.affected import CORES

COLECO_SCENARIOS = ('graphics', 'stream', 'interactive', 'controllers', 'vdp-io', 'sprites')
TARGETS = {
    'demo': ('sim-fes-demo',),
    'pong': ('sim-fes-pong',),
    'zx81': ('sim-fes-zx81',),
    'coleco': tuple(
        'sim-fes-coleco-' + scenario + suffix
        for suffix in ('', '-oss')
        for scenario in ('unit', *('board-' + name for name in COLECO_SCENARIOS))
    ) + ('sim-fes-coleco-expansion', 'sim-fes-coleco-diagnostic', 'sim-fes-video-parts') + tuple(
        'sim-fes-coleco-sgm-' + name for name in
        ('socket', 'shell-ram', 'ay', 'module', 'audio', 'integrated', 'probe')),
    'sms': ('sim-fes-sms sim-fes-sms-oss',),
    'sg1000': ('sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link sim-fes-sg1000-gp-audio',),
    'apple2': ('sim-fes-apple2',),
    # Four independent groups that together are exactly sim-fes-c64.
    'c64': ('sim-fes-c64-cpu-group', 'sim-fes-c64-video-a-group',
            'sim-fes-c64-video-b-group', 'sim-fes-c64-system-group'),
    'spectrum': ('sim-fes-spectrum',),
    'menu': ('sim-fes-menu',),
    'z80': ('sim-fes-z80',),
    'atari-st': ('sim-fes-atari-st',),
    'ramtest': ('sim-fes-ramtest',),
    'riscv': ('sim-fes-riscv',),
    'pcxt': ('sim-fes-pcxt', 'sim-fes-pcxt-fdc', 'sim-fes-pcxt-dos'),
    'pc386': ('sim-fes-pc386',),
}

# Slow families' PR-tier targets (each about 2.5 minutes or less on a runner).
# Families not listed are already fast and run the same target in both tiers.
SMOKE_TARGETS = {
    'zx81': ('sim-fes-zx81-smoke',),
    'c64': ('sim-fes-c64-smoke',),
    'atari-st': ('sim-fes-atari-st-smoke',),
    'spectrum': ('sim-fes-spectrum-smoke',),
    'pcxt': ('sim-fes-pcxt-smoke',),
    # Default and OSS unit benches, one board scenario, the diagnostic and the
    # integrated SGM bench; the other board scenarios and -oss twins are full.
    'coleco': ('sim-fes-coleco-unit', 'sim-fes-coleco-unit-oss', 'sim-fes-coleco-board-graphics',
               'sim-fes-coleco-diagnostic', 'sim-fes-coleco-sgm-integrated'),
}
TIERS = ('smoke', 'full')
SMOKE_EVENTS = ('pull_request', 'merge_group', 'push')


def tier_for_event(event):
    return 'smoke' if event in SMOKE_EVENTS else 'full'


def simulation_matrix(cores, tier='full'):
    if tier not in TIERS:
        raise ValueError('Expected a simulation tier: ' + ', '.join(TIERS))
    if not isinstance(cores, list) or any(not isinstance(core, str) for core in cores):
        raise ValueError('Expected a list of core names')
    if len(cores) != len(set(cores)) or not set(cores) <= set(CORES):
        raise ValueError('Expected unique known cores')
    return {'include': [{'core': core, 'target': target}
                        for core in cores
                        for target in (SMOKE_TARGETS.get(core) if tier == 'smoke' else None) or TARGETS[core]]}
