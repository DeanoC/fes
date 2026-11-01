#!/usr/bin/env python3
"""Run FogCast host Go tests: routine fast/race split or exhaustive --full.

Routine mode go-lists the FogCast module to race every package except the two
race-expensive ones, which get concurrency-focused instrumentation instead.
Full mode races every package in all three modules without -short or -run.
CI can include the standalone host build when production/build inputs require it.
Uses the inherited Go environment; requested host builds use the parent Make target.
"""
import argparse
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys

try:
    from scripts import test_policy, host_affected
except ImportError:
    import test_policy
    import host_affected


def ui_requirement(selection_scope, go_changes):
    """Return whether verified host inputs require the browser UI suite."""
    required = (selection_scope != 'affected' or
                any(not path.endswith('_test.go') or
                    path.startswith(('sources/FogCast/internal/hostapi/',
                                     'sources/FogCast/cmd/fogcast-api/',
                                     'sources/FogCast/ui/'))
                    for path in go_changes))
    reason = ('UI/API, production/full or uncertain inputs' if required else
              'only verified unrelated Go tests and guides changed; boundary retained')
    return required, reason


def ui_schedule(required, shard=None):
    """Describe the UI or boundary target placed on this host-test shard."""
    command_scheduled = shard is None or shard[0] == shard[1] - 1
    return {
        'required': required,
        'scheduled': required and command_scheduled,
        'boundary_scheduled': not required and command_scheduled,
        'command_scheduled': command_scheduled,
        'target': 'test-ui' if required else 'test-ui-boundary',
    }


def plan_ui(root, paths, full=False, shard=None, environment=None):
    """Resolve the host UI target for a parent-level plan.

    Go package ownership must be verified before browser tests can be skipped.
    Any failure to resolve that graph keeps the browser suite selected.
    """
    root = Path(root).resolve()
    environment = dict(os.environ) if environment is None else dict(environment)
    for name in ('GOOS', 'GOARCH', 'GOARM'):
        environment.pop(name, None)
    go_changes = host_affected.go_inputs(paths)
    selection_scope = 'all'
    if not full and go_changes is not None:
        try:
            records = host_affected.listing(root, environment)
            if host_affected.select(paths, records, root) is not None:
                selection_scope = 'affected'
        except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
            # The package graph could not prove the changes are unrelated.
            pass
    required, reason = ui_requirement(selection_scope, go_changes)
    return {**ui_schedule(required, shard), 'reason': reason}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--full', action='store_true',
                        help='exhaustive race run for nightly/manual or shared/unknown inputs')
    parser.add_argument('--plan-only', action='store_true')
    parser.add_argument('--build-host-if-needed', action='store_true',
                        help='include make host for non-test/full inputs, on shard 1 only')
    parser.add_argument('--ui-if-needed', action='store_true',
                        help='run UI tests unless only verified unrelated Go tests changed; last shard only')
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
    selection = {'scope': 'all', 'reason': 'full mode or inputs outside Go and reviewed guides'}
    go_changes = host_affected.go_inputs(paths)
    if not args.full and go_changes is not None:
        records = host_affected.listing(root, environment)
        selected = host_affected.select(paths, records, root)
        if selected is not None:
            commands = host_affected.filter_commands(commands, selected, records, root)
            selection = {'scope': 'affected', 'packages': sorted(selected),
                         'reason': 'changed Go packages and reverse import/test consumers'}
        else:
            selection['reason'] = 'uncertain package ownership or embedded guide inputs'
    if shard is not None:
        commands = test_policy.shard_host_commands(commands, *shard)
    if args.build_host_if_needed:
        build_required = (selection['scope'] != 'affected' or
                          any(not path.endswith('_test.go') for path in go_changes))
        build_scheduled = build_required and (shard is None or shard[0] == 0)
        if build_scheduled:
            commands.insert(0, {'cwd': '.', 'label': 'host binary build', 'argv': ['make', 'host']})
    if args.ui_if_needed:
        # Reuse the graph/embedded-guide validation above. API and UI test
        # changes retain the browser suite even though they are test-only.
        ui_required, ui_reason = ui_requirement(selection['scope'], go_changes)
        ui = ui_schedule(ui_required, shard)
        if ui['command_scheduled']:
            # The boundary guard scans ALL Go files, including test files.
            commands.append({'cwd': test_policy.FOGCAST_MODULE,
                             'label': 'host UI tests' if ui_required else 'host UI boundary',
                             'argv': ['make', ui['target']]})
    report = {'format': 1, 'mode': 'full' if args.full else 'routine',
              'commands': commands, 'selection': selection}
    if args.build_host_if_needed:
        report['host_build'] = {'required': build_required, 'scheduled': build_scheduled,
            'reason': ('production/build/full or uncertain inputs' if build_required else
                       'only Go tests and verified guides changed')}
    if args.ui_if_needed:
        report['host_ui'] = {**ui, 'reason': ui_reason}
    if shard is not None:
        report['shard'] = args.shard
    if args.plan_only:
        report['status'] = 'planned'
        print(json.dumps(report, indent=2))
        return 0
    if args.ui_if_needed and ui['command_scheduled']:
        tools = ['make', 'sh', 'rg', *(['node'] if ui_required else [])]
        missing = [tool for tool in tools if shutil.which(tool) is None]
        if missing:
            report.update(status='error', error='missing UI prerequisites: ' + ', '.join(missing))
            print(json.dumps(report, indent=2))
            return 2
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
