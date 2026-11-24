#!/usr/bin/env python3
"""Run ``go vet ./...`` across every repository Go module on both Linux lanes.

Loads all tracked Go modules together through a temporary ``go.work`` (reusing
``check_deadcode.MODULES`` and ``check_deadcode.LANES`` so there is one
canonical module and target list) and fails when ``go vet`` reports any issue
on either ``linux/amd64`` or ``linux/arm`` (``GOARM=7``).
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

try:
    from scripts import changed_paths, check_deadcode
except ImportError:
    import changed_paths
    import check_deadcode

CHECKER_INPUTS = frozenset({
    'scripts/check_govet.py',
    'tests/test_check_govet.py',
    'scripts/check_deadcode.py',
    'tests/test_ci_workflow.py',
})


def required(paths):
    """No list means full analysis; explicit paths may prove it unnecessary."""
    if paths is None:
        return True
    if not isinstance(paths, list) or any(not isinstance(p, str) or not p for p in paths):
        raise ValueError('changed paths must be an array of nonempty strings')
    if any(path in CHECKER_INPUTS for path in paths):
        return True
    return check_deadcode.required(paths)


def vet_module(root, module, work, lane):
    """Run ``go vet ./...`` for *module* under *lane*, returning error output or ``''``."""
    env = dict(os.environ, GOWORK=str(work), CGO_ENABLED='0', GOFLAGS='-buildvcs=false', **lane)
    result = subprocess.run(
        ['go', 'vet', './...'],
        cwd=root / module,
        env=env,
        capture_output=True,
        text=True,
    )
    if result.returncode == 0:
        return ''
    output = (result.stderr + result.stdout).strip()
    return output or f'go vet exited with code {result.returncode}'


def run_all(root):
    """Vet every tracked module across every Linux lane and return failure tuples."""
    failures = []
    with tempfile.TemporaryDirectory(prefix='fes-govet-') as tmp:
        work = Path(tmp) / 'go.work'
        uses = ''.join(f'\t{root / module}\n' for module in check_deadcode.MODULES)
        work.write_text(f'go {check_deadcode.go_version(root)}\n\nuse (\n{uses})\n')
        for lane in check_deadcode.LANES:
            lane_label = f'{lane["GOOS"]}/{lane["GOARCH"]}'
            for module in check_deadcode.MODULES:
                detail = vet_module(root, module, work, lane)
                if detail:
                    failures.append((lane_label, module, detail))
    return failures


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    changed_paths.add_arguments(parser, help='changed paths; omitted runs the full check')
    parser.add_argument('--plan-only', action='store_true', help='report selection without running go vet')
    args = parser.parse_args(argv)
    try:
        paths = changed_paths.read(args)
        selected = required(paths)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    if args.plan_only:
        print(json.dumps({'selected': selected, 'paths': paths}))
        return 0
    if not selected:
        print('Go vet skipped: no Go module, checker or unknown inputs changed.', file=sys.stderr)
        return 0
    root = Path(__file__).resolve().parent.parent
    failures = run_all(root)
    for lane_label, module, detail in failures:
        print(f'[{lane_label}] {module}:\n{detail}', file=sys.stderr)
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
