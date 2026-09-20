"""Plan software CI from a merge-base diff; never claim FPGA builds or kit acceptance."""
import argparse
import json
from fnmatch import fnmatchcase
from pathlib import Path
import subprocess

# Logical module names survive the monorepo move; update only these roots.
MODULE_ROOTS = {'host': 'sources/FogCast', 'runtime': 'sources/libmister-runtime',
                'contracts': 'sources/mister-packages', 'fpga': 'sources/misteross'}
LANES = ('parent', 'host', 'runtime', 'contracts', 'fpga')
CORES = ('demo', 'pong', 'zx81', 'coleco', 'sg1000', 'sms')


# Keep this software suite shared with test_changed. A producer edit validates
# package/provenance behavior without recompiling unchanged RTL.
FPGA_SOFTWARE_TESTS = (
    'test_build_fes_*.py', 'test_functional_identity.py',
    'test_export_core_package.py', 'test_compiler_read_audit.py',
    'test_legacy_source.py', 'test_source_repository.py',
    'test_core_package.py', 'test_search_placer_qor.py',
    'test_coleco_sim_shards.py',
)
FPGA_PRODUCER_HELPERS = {
    'build_fes_catch.py',
    'fes_build_common.py', 'fes_de10nano_evidence.py', 'compiler_read_audit.py',
    'source_repository.py', 'legacy_source.py', 'functional_execution.py',
    'core_package.py', 'export_core_package.py', 'search_placer_qor.py',
}
# Directory ownership includes cross-core consumers: demo imports Pong board
# models/constraints/PLL; SG-1000 and SMS still include Coleco generated headers.
# Shared implementation has moved into fes-common, but those edges remain.
COLECO_CONSUMERS = ('coleco', 'sg1000', 'sms')
CORE_DIRECTORIES = {
    'fes-demo': ('demo',), 'fes-pong': ('demo', 'pong'), 'pong': ('pong',),
    'fes-zx81': ('zx81',), 'fes-coleco': COLECO_CONSUMERS,
    'fes-sg1000': ('sg1000',), 'fes-sms': ('sms',),
}
SHARED_RTL = {
    'coleco_vdp.sv': COLECO_CONSUMERS,
    'coleco_dpram.v': COLECO_CONSUMERS,
    'coleco_video_dpram.v': COLECO_CONSUMERS,
    'coleco_video_720p.v': COLECO_CONSUMERS,
    't80pa.v': COLECO_CONSUMERS,
    'fes_computer_gp.v': COLECO_CONSUMERS,
    'fes_application_gp.v': ('demo', 'coleco'),
    'fes_video_720p.v': ('demo', 'pong'),
    'fes_audio_i2s.v': ('demo', 'coleco'), 'fes_audio_pll.v': ('demo',),
    'fes_audio_output.v': ('coleco',), 'fes_sn76489.sv': ('coleco',),
}


def fpga_cores(path):
    """Bounded source-family closure; unclassified graph inputs fail broad.

    These are consumer rules, not an inventory of source files. New files in a
    known core family inherit its consumers; new shared units, families, build
    graph files and toolchain configuration select every existing family.
    tests/test_affected.py checks current literal source/include references in
    simulation/producer recipes. Dynamically constructed or relative cross-family
    references require explicit review and corresponding rule/test updates.
    """
    relative = Path(path).relative_to(MODULE_ROOTS['fpga'])
    parts = relative.parts
    if len(parts) >= 3 and parts[0] == 'cores':
        if parts[1] in CORE_DIRECTORIES:
            return CORE_DIRECTORIES[parts[1]], 'core family and dependent consumers'
        if parts[1:3] == ('fes-common', 'rtl'):
            if len(parts) >= 5 and parts[3] == 'tv80':
                return COLECO_CONSUMERS, 'shared TV80 consumers'
            if len(parts) == 4 and parts[3] in SHARED_RTL:
                return SHARED_RTL[parts[3]], 'shared RTL consumers'
    if len(parts) == 2 and parts[0] == 'scripts':
        producer = any(parts[1] == f'build_fes_{core}{suffix}.py'
                       for core in CORES for suffix in ('', '_oss'))
        if producer or parts[1] in FPGA_PRODUCER_HELPERS:
            return (), 'FPGA producer/package software tests; RTL unchanged'
        if parts[1] == 'sim_fes_demo.py':
            return ('demo',), 'demo simulation recipe'
    if len(parts) == 2 and parts[0] == 'tests' and any(
            fnmatchcase(parts[1], pattern) for pattern in FPGA_SOFTWARE_TESTS):
        return (), 'FPGA producer/package software tests; RTL unchanged'
    return CORES, 'unclassified FPGA/shared graph input; all core families'


def changed_paths(root, base, head='HEAD'):
    def git(*args):
        return subprocess.check_output(['git', '-C', str(root), *args])
    ancestor = git('merge-base', base, head).decode().strip()
    # No rename detection: both old and new ownership must be considered.
    paths = git('diff', '--name-only', '--no-renames', '-z', ancestor, head)
    return sorted(set(p.decode('utf-8', 'surrogateescape') for p in paths.split(b'\0') if p))


def documentation(path):
    parts = Path(path).parts
    return ('docs' in parts or path.endswith('.md')) and Path(path).name != 'AGENTS.md'


def plan(paths):
    selected = set()
    cores = set()
    reasons = []
    for path in sorted(set(paths)):
        if documentation(path):
            continue
        owner = next((module for module, prefix in MODULE_ROOTS.items()
                      if path == prefix or path.startswith(prefix + '/')), None)
        if owner is None or owner == 'contracts':
            selected.update(LANES)
            cores.update(CORES)
            reasons.append(f'{path}: shared contract or unknown/root input')
        elif owner == 'fpga':
            selected.update(('parent', 'fpga'))
            consumers, reason = fpga_cores(path)
            cores.update(consumers)
            reasons.append(f'{path}: {reason}')
        else:
            selected.update(('parent', owner))
            if owner == 'runtime':
                selected.add('host')
                reasons.append(f'{path}: runtime, host protocol consumers and parent integration')
            else:
                reasons.append(f'{path}: {owner} and parent integration')
    return {'lanes': {lane: lane in selected for lane in LANES},
            'skipped': [lane for lane in LANES if lane not in selected],
            'cores': sorted(cores), 'paths': sorted(set(paths)), 'reasons': reasons,
            'always': ['planner tests', 'diff whitespace checks'],
            'not_run': ['FPGA synthesis/place-and-route', 'cold image builds', 'hardware acceptance']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--head', default='HEAD')
    parser.add_argument('--root', type=Path, default=Path.cwd())
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    # A new branch's all-zero before SHA has no ancestor: run every lane.
    result = plan(['<new-branch>'] if set(args.base) == {'0'} else
                  changed_paths(args.root, args.base, args.head))
    encoded = json.dumps(result, indent=2)
    if args.output:
        args.output.write_text(encoded + '\n')
    print(encoded)


if __name__ == '__main__':
    main()
