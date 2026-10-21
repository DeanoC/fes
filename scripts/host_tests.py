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
    from scripts import test_policy, host_affected
except ImportError:
    import test_policy
    import host_affected


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--full', action='store_true',
                        help='exhaustive race run for weekly/manual selection')
    parser.add_argument('--plan-only', action='store_true')
    parser.add_argument('--paths-json', help='JSON array of changed repository paths')
    parser.add_argument('--shard', metavar='I/N',
                        help='run shard I of N (1-based) for parallel CI jobs')
    args = parser.parse_args(argv)
    paths = None
    if args.paths_json is not None:
        try:
            paths = json.loads(args.paths_json)
        except ValueError:
            parser.error('--paths-json must be a JSON array of strings')
        if not isinstance(paths, list) or not all(isinstance(p, str) for p in paths):
            parser.error('--paths-json must be a JSON array of strings')
    shard = None
    if args.shard:
        try:
            number, count = (int(part) for part in args.shard.split('/'))
        except ValueError:
            parser.error('--shard must be I/N')
        if not 1 <= number <= count:
            parser.error('--shard must satisfy 1 <= I <= N')
        shard = (number - 1, count)
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
    selection = {'scope': 'all', 'reason': 'full mode or no Go-only change set'}
    if not args.full and paths is not None and host_affected.go_only(paths):
        records = host_affected.listing(root, environment)
        selected = host_affected.select(paths, records, root)
        if selected is not None:
            commands = host_affected.filter_commands(commands, selected, records, root)
            selection = {'scope': 'affected', 'packages': sorted(selected),
                         'reason': 'changed Go packages and reverse import/test consumers'}
        else:
            selection['reason'] = 'changed package absent from current Go graph'
    if shard is not None:
        commands = test_policy.shard_host_commands(commands, *shard)
    report = {'format': 1, 'mode': 'full' if args.full else 'routine',
              'commands': commands, 'selection': selection}
    if shard is not None:
        report['shard'] = args.shard
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
