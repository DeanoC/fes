"""Expand selected core families into independent, complete simulation jobs."""
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
    ) + ('sim-fes-coleco-expansion', 'sim-fes-coleco-diagnostic') + tuple(
        'sim-fes-coleco-sgm-' + name for name in
        ('socket', 'shell-ram', 'ay', 'module', 'audio', 'integrated', 'probe')),
    'sms': ('sim-fes-sms sim-fes-sms-oss',),
    'sg1000': ('sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link',),
}


def simulation_matrix(cores):
    if not isinstance(cores, list) or any(not isinstance(core, str) for core in cores):
        raise ValueError('Expected a list of core names')
    if len(cores) != len(set(cores)) or not set(cores) <= set(CORES):
        raise ValueError('Expected unique known cores')
    return {'include': [{'core': core, 'target': target}
                        for core in cores for target in TARGETS[core]]}
