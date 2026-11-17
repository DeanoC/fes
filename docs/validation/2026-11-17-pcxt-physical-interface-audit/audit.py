#!/usr/bin/env python3
"""Read existing PC/XT production reports; never invoke FPGA tools or access a kit."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def ports(report, analysis, direction):
    section = report.split('; ' + analysis + ' Analysis ;', 1)[1]
    section = section.split('; Unconstrained ' + direction + ' Ports', 1)[1]
    section = section.split('\n\n', 1)[0]
    found = re.findall(r'^;\s*(\S+)\s*;\s*No (?:input|output) delay,', section, re.M)
    assert found and len(found) == len(set(found))
    return sorted(found)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', required=True, type=Path)
    parser.add_argument('--package', required=True, type=Path)
    parser.add_argument('--module-root', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    root, attempt = args.module_root.resolve(), args.attempt.resolve()
    sys.path.insert(0, str(root))
    from scripts.core_package import read_package
    from scripts.export_core_package import build_identity

    record_path = attempt / 'build-inputs.json'
    record = json.loads(record_path.read_bytes())
    package = read_package(args.package)
    assert package.fields['core']['id'] == 'fes.pcxt-dos'
    assert package.fields['build']['id'] == build_identity(record_path.read_bytes())
    assert package.fields['build']['revision'] == record['revision']
    assert all(sha(root / path) == digest for path, digest in record['source_inputs'].items())
    evidence = {'build-inputs.json': sha(record_path)}
    runs = []
    for phase, expected_seeds in (('signoff', [1, 2, 3]), ('repeat', [1])):
        summary_path = attempt / phase / 'summary.json'
        summary = json.loads(summary_path.read_text())
        assert summary['seeds'] == expected_seeds
        assert summary['profile']['HALF_RATE_BOARD'] == 0
        assert summary['profile']['HARDWARE_DOS'] == 1
        evidence[str(summary_path.relative_to(attempt))] = sha(summary_path)
        for run in summary['runs']:
            audit = attempt / phase / ('seed-' + str(run['seed'])) / 'audit'
            unconstrained = (audit / 'unconstrained.rpt').read_text()
            coverage = dict((k, int(v)) for k, v in re.findall(
                r'^;\s*([a-z_]+)\s*;\s*(\d+)\s*;',
                (audit / 'check-timing.rpt').read_text(), re.M))
            assert coverage == run['timing_coverage_issues']
            inventories = {}
            for direction, key in (('Input', 'no_input_delay'), ('Output', 'no_output_delay')):
                inventories[direction.lower()] = ports(unconstrained, 'Setup', direction)
                assert inventories[direction.lower()] == ports(unconstrained, 'Hold', direction)
                assert len(inventories[direction.lower()]) == coverage[key]
            no_clock = re.findall(
                r"^;\s*([^;]+?)\s*;\s*No clock feeds this register's clock port\.\s*;",
                (audit / 'check-timing.rpt').read_text(), re.M)
            assert len(no_clock) == len(set(no_clock)) == coverage['no_clock']
            assert set(no_clock) == set(run['no_clock_registers'])
            assert run['reported_endpoint_checks_pass']
            assert all(row['failures'] == 0 for row in run['minimum_pulse_width'].values())
            assert 'No constraints were ignored.' in (audit / 'ignored-constraints.rpt').read_text()
            for name in ('check-timing.rpt', 'unconstrained.rpt', 'constraints.rpt',
                         'ignored-constraints.rpt', 'clocks.tsv', 'counts.tsv', 'endpoints.tsv'):
                path = audit / name
                evidence[str(path.relative_to(attempt))] = sha(path)
            runs.append({
                'phase': phase, 'seed': run['seed'], 'coverage': coverage,
                'unconstrained_ports': inventories,
                'unclocked_groups': dict(sorted(Counter(n.split('~')[0] for n in no_clock).items())),
                'minimum_reported_slack_ns': {
                    kind: min(float(v['worst']['slack_ns']) for k, v in run['by_corner_and_check'].items()
                              if k.endswith('/' + kind))
                    for kind in ('setup', 'hold', 'recovery', 'removal')},
                'clocks': run['clocks'],
                'reported_internal_checks_pass': True,
            })
    output = {
        'classification': 'existing-artifact physical timing coverage inventory',
        'source_audit_base': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
        'artifact_revision': record['revision'], 'build_id': package.fields['build']['id'],
        'package_id': package.package_id, 'archive_sha256': sha(args.package),
        'payload_sha256': package.fields['payload']['sha256'],
        'recorded_inputs_equal_audited_source': len(record['source_inputs']),
        'normal_profile': {'system_clock_hz': 100000000, 'pixel_clock_hz': 74250000,
                           'half_rate_board': False, 'phased_hdmi_clock': True},
        'runs': runs, 'raw_evidence_sha256': evidence,
        'whole_board_qualified': False, 'new_build': False, 'new_hardware_run': False,
    }
    args.output.write_text(json.dumps(output, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
