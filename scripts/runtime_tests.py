#!/usr/bin/env python3
"""Select routine or exhaustive native runtime checks from changed inputs."""
import argparse
import json
from pathlib import Path
import subprocess

RUNTIME = 'sources/libmister-runtime'


def command(paths, jobs=2, full=False):
    # Only ordinary source/test edits have an established build graph. Build
    # recipes, dependencies, runner/policy changes and unknown inputs stay full.
    routine = not full and bool(paths)
    for path in paths or ():
        parts = Path(path).parts
        valid = (path and not path.startswith('/') and '..' not in parts and
                 Path(path).as_posix() == path)
        source = (path.startswith(tuple(RUNTIME + '/' + p + '/' for p in
                                       ('src', 'include', 'tests'))) and
                  path.endswith(('.cpp', '.hpp', '.h')))
        guide = path.endswith('.md') and (path.startswith('docs/') or
                                         path.startswith(RUNTIME + '/docs/'))
        routine = routine and valid and (source or guide)
    target = 'test-routine' if routine else 'test'
    return {'cwd': RUNTIME, 'argv': ['make', '-j' + str(jobs), target]}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--paths-json')
    parser.add_argument('--jobs', type=int, default=2, choices=range(1, 33))
    parser.add_argument('--full', action='store_true')
    parser.add_argument('--plan-only', action='store_true')
    args = parser.parse_args(argv)
    paths = None
    if args.paths_json is not None:
        try:
            paths = json.loads(args.paths_json)
        except ValueError:
            parser.error('--paths-json must be a JSON array of strings')
        if not isinstance(paths, list) or not all(isinstance(p, str) for p in paths):
            parser.error('--paths-json must be a JSON array of strings')
    selected = command(paths, args.jobs, args.full)
    report = {'format': 1, 'command': selected, 'status': 'planned'}
    code = 0
    if not args.plan_only:
        import sys
        code = subprocess.run(selected['argv'],
                              cwd=Path(__file__).resolve().parents[1] / selected['cwd'],
                              stdout=sys.stderr, stderr=sys.stderr).returncode
        report.update(status='passed' if code == 0 else 'failed', returncode=code)
    print(json.dumps(report, indent=2))
    return code


if __name__ == '__main__':
    raise SystemExit(main())
