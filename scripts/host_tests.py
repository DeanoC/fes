#!/usr/bin/env python3
"""Run FogCast host Go tests: routine fast/race split or exhaustive --full.

Routine mode go-lists the FogCast module to race every package except the two
race-expensive ones, which get concurrency-focused instrumentation instead.
Full mode races every package in all three modules without -short or -run.
Never installs dependencies; uses the inherited Go environment as-is.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

try:
    from scripts import test_policy
except ImportError:
    import test_policy


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--full', action='store_true',
                        help='exhaustive race run for weekly/manual selection')
    parser.add_argument('--plan-only', action='store_true')
    args = parser.parse_args(argv)
    root = Path(__file__).resolve().parents[1]
    environment = dict(os.environ)
    # Native software tests; a cross-build shell must not redirect either the
    # package listing or the test runs.
    for name in ('GOOS', 'GOARCH', 'GOARM'):
        environment.pop(name, None)
    packages = None
    if not args.full:
        packages = subprocess.check_output(
            ['go', 'list', './...'], cwd=root / test_policy.FOGCAST_MODULE,
            text=True, env=environment).split()
    commands = test_policy.host_test_commands(full=args.full, packages=packages)
    report = {'format': 1, 'mode': 'full' if args.full else 'routine',
              'commands': commands}
    if args.plan_only:
        report['status'] = 'planned'
        print(json.dumps(report, indent=2))
        return 0
    report['results'] = []
    for command in commands:
        # Test output goes to stderr so stdout stays a standalone JSON report.
        completed = subprocess.run(command['argv'],
                                   cwd=root / command['cwd'], env=environment,
                                   stdout=sys.stderr, stderr=sys.stderr)
        report['results'].append({'label': command['label'],
                                  'returncode': completed.returncode})
        if completed.returncode:
            report['status'] = 'failed'
            report['not_started'] = [step['label']
                                     for step in commands[len(report['results']):]]
            print(json.dumps(report, indent=2))
            return 1
    report['status'] = 'passed'
    print(json.dumps(report, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
