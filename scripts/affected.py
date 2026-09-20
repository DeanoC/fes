"""Plan software CI from a merge-base diff; never claim FPGA builds or kit acceptance."""
import argparse
import json
from pathlib import Path
import subprocess

# Logical module names survive the monorepo move; update only these roots.
MODULE_ROOTS = {'host': 'sources/FogCast', 'runtime': 'sources/libmister-runtime',
                'contracts': 'sources/mister-packages', 'fpga': 'sources/misteross'}
LANES = ('parent', 'host', 'runtime', 'contracts', 'fpga')
CORES = ('demo', 'pong', 'zx81', 'coleco', 'sg1000', 'sms')


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
            # Coleco sources are also compiled by SMS/SG-1000. Conservatively
            # include every core until a proven source dependency graph exists.
            cores.update(CORES)
            reasons.append(f'{path}: FPGA sources and shared core dependencies')
        else:
            selected.update(('parent', owner))
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
