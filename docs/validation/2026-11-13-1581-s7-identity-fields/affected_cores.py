"""Every core whose source closure holds one of this PR's changed files.

#1581 S7 edits scripts/export_core_package.py, which sits in every audited
closure, so this list is generated, never written by hand (the design doc's
"Identity churn"). Run from the repository root:

    python3 docs/validation/2026-11-13-1581-s7-identity-fields/affected_cores.py \
        [--base origin/main]

The audited closures come from sources/misteross/scripts/source_closures.json.
PC/XT computes its roots at run time, and the ZX81 Quartus family has a
separate legacy producer closure that is not in that audit; both are included
directly so the report covers the affected Quartus producers too.
"""
import argparse
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
MISTEROSS = ROOT / 'sources/misteross'
CLOSURES = MISTEROSS / 'scripts/source_closures.json'
# Producers whose closure is computed at run time rather than audited.
ADDITIONAL_CLOSURES = (
    ('fes.pcxt', 'scripts.build_fes_pcxt_quartus', 'source_roots'),
    ('fes.zx81-quartus (zx81, zx80, ts1000, ts1500)',
     'scripts.build_fes_zx81_quartus', 'SOURCE_ROOTS'),
)


def changed(base: str) -> list[str]:
    """This branch's changed files, as paths below sources/misteross."""
    diff = subprocess.run(['git', 'diff', '--name-only', f'{base}...HEAD'],
                          cwd=ROOT, text=True, capture_output=True, check=True)
    prefix = 'sources/misteross/'
    return sorted(line[len(prefix):] for line in diff.stdout.split()
                  if line.startswith(prefix))


def covered(roots, paths) -> list[str]:
    """The changed paths that fall inside these closure roots."""
    return sorted({path for path in paths for root in roots
                   if path == root or path.startswith(root.rstrip('/') + '/')})


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default='origin/main')
    arguments = parser.parse_args()
    paths = changed(arguments.base)
    if not paths:
        print('no changed file lies under sources/misteross', file=sys.stderr)
        return 1
    audited = json.loads(CLOSURES.read_text())
    sys.path.insert(0, str(MISTEROSS))
    affected = {}
    for producer, roots in sorted(audited.items()):
        hits = covered(roots, paths)
        if hits:
            affected[producer] = hits
    for core_id, module, function in ADDITIONAL_CLOSURES:
        provider = getattr(__import__(module, fromlist=[function]), function)
        roots = provider(MISTEROSS) if callable(provider) else provider
        hits = covered(roots, paths)
        if hits:
            affected[core_id] = hits
    print(f'changed under sources/misteross: {len(paths)}')
    for path in paths:
        print('   ', path)
    print(f'closures holding at least one of them: {len(affected)} of '
          f'{len(audited) + len(ADDITIONAL_CLOSURES)}')
    for name, hits in sorted(affected.items()):
        print(f'    {name}: {", ".join(hits)}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
