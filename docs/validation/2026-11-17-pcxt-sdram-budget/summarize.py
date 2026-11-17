#!/usr/bin/env python3
"""Validate existing production fits and summarize conditional SDRAM STA rows."""
import argparse
import csv
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import sys
from database_identity import verify as verify_database


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    return json.loads(path.read_text())


def verify_native_endpoint(native, native_run):
    path = native / 'audit/endpoints.tsv'
    if sha(path) != native_run['captures_sha256']['audit/endpoints.tsv']:
        raise ValueError('native endpoint report differs from producer receipt')
    return path


def summarize(args):
    root = args.module_root.resolve()
    sys.path.insert(0, str(root))
    from scripts.core_package import read_package
    from scripts.export_core_package import build_identity
    record_path = args.attempt / 'build-inputs.json'
    record = load(record_path)
    pkg = read_package(args.package)
    assert pkg.fields['build']['id'] == build_identity(record_path.read_bytes())
    assert pkg.fields['build']['revision'] == record['revision']
    assert all(sha(root / name) == digest for name, digest in record['source_inputs'].items())
    payload = pkg.fields['payload']['sha256']
    assert sha(args.attempt / 'signoff/seed-1/output_files/top.rbf') == payload
    assert sha(args.attempt / 'repeat/seed-1/output_files/top.rbf') == payload
    original = load(args.attempt / 'signoff/summary.json')
    assert sha(args.quartus_sta) == original['quartus_binaries']['quartus_sta']
    assert original['profile']['HALF_RATE_BOARD'] == 0
    assert original['profile']['HARDWARE_DOS'] == 1
    reference_path = Path(__file__).with_name('reference.json')
    reference = load(reference_path)
    runs = []
    for seed in (1, 2, 3):
        work = args.work / ('seed-' + str(seed))
        native = args.attempt / 'signoff' / ('seed-' + str(seed))
        native_run = next(r for r in original['runs'] if r['seed'] == seed)
        assert sha(native / 'output_files/top.rbf') == native_run['rbf_sha256']
        assert sha(work / 'output_files/top.rbf') == sha(native / 'output_files/top.rbf')
        for name in ('top.qsf', 'clocks.sdc', 'mailbox.sdc', 'audio.sdc'):
            assert (work / name).read_bytes() == (native / name).read_bytes(), name
        receipt = verify_database(native, work, Path(__file__).with_name('collect.tcl'))
        assert receipt['quartus_sta_sha256'] == sha(args.quartus_sta)
        assert receipt['quartus_asm_sha256'] == original['quartus_binaries']['quartus_asm']
        assert load(work / 'conditional-result.json')['exit_code'] == 0
        assert 'No constraints were ignored.' in (work / 'conditional/ignored.rpt').read_text()
        table = work / 'conditional/channels.tsv'
        with table.open() as stream:
            rows = list(csv.DictReader(stream, delimiter='\t'))
        assert len(rows) == 568
        assert len({(r['corner'], r['role'], r['kind'], r['port']) for r in rows}) == len(rows)
        assert all(r['count'] == '1' and math.isfinite(float(r['slack_ns'])) for r in rows)
        corners = sorted({r['corner'] for r in rows})
        assert len(corners) == 4
        dq_ports = {f'SDRAM_DQ[{i}]' for i in range(16)}
        command_ports = {f'SDRAM_A[{i}]' for i in range(13)} | {f'SDRAM_BA[{i}]' for i in range(2)}
        command_ports |= {'SDRAM_nCS', 'SDRAM_nRAS', 'SDRAM_nCAS', 'SDRAM_nWE',
                          'SDRAM_CKE', 'SDRAM_DQML', 'SDRAM_DQMH'}
        expected = {'read': dq_ports, 'write': dq_ports | command_ports,
                    'output_enable': dq_ports, 'second_chip_select': {'SDRAM_nCS'}}
        for corner in corners:
            for role, port_set in expected.items():
                for kind in ('setup', 'hold'):
                    assert {r['port'] for r in rows if r['corner'] == corner and r['role'] == role and r['kind'] == kind} == port_set
        groups = {
            'read': lambda r: r['role'] == 'read',
            'write_dq': lambda r: r['role'] == 'write' and r['port'].startswith('SDRAM_DQ['),
            'write_command_address_connector': lambda r: r['role'] == 'write' and not r['port'].startswith('SDRAM_DQ[')
                and r['port'] not in ('SDRAM_CKE', 'SDRAM_DQML', 'SDRAM_DQMH'),
            'shell_diagnostic_mirrors': lambda r: r['role'] == 'write'
                and r['port'] in ('SDRAM_CKE', 'SDRAM_DQML', 'SDRAM_DQMH'),
            'output_enable_pin_window_only': lambda r: r['role'] == 'output_enable',
            'second_chip_select_reference': lambda r: r['role'] == 'second_chip_select',
        }
        minima = {}
        for group, select in groups.items():
            minima[group] = {}
            for kind in ('setup', 'hold'):
                selected = [r for r in rows if select(r) and r['kind'] == kind]
                worst = min(selected, key=lambda r: float(r['slack_ns']))
                minima[group][kind] = {
                    'slack_ns': float(worst['slack_ns']), 'corner': worst['corner'],
                    'port': worst['port'], 'from': worst['from'], 'to': worst['to'],
                    'negative_rows': sum(float(r['slack_ns']) < 0 for r in selected),
                    'row_count': len(selected),
                }
        # This separately timed handoff comes from the unchanged native reports.
        endpoint_path = verify_native_endpoint(native, native_run)
        with endpoint_path.open() as stream:
            handoff = [r for r in csv.DictReader(stream, delimiter='\t')
                       if re.fullmatch(r'sdram_pads_\d+__captured', r['from'])]
        assert len(handoff) == 128
        for corner in corners:
            for kind in ('setup', 'hold'):
                selected = [r for r in handoff if r['corner'] == corner and r['kind'] == kind]
                assert len(selected) == 16 and len({r['from'] for r in selected}) == 16
                assert all(re.fullmatch(r'system_storage_controller_dq_fall\[\d+\]', r['to']) for r in selected)
                assert all(float(r['slack_ns']) >= 0 for r in selected)
        hashes = {str(p.relative_to(work)): sha(p) for p in (work / 'conditional').rglob('*') if p.is_file()}
        runs.append({
            'database_receipt_sha256': sha(work / 'database-receipt.json'),
            'database_input_sha256': receipt['pre_sta']['sha256'],
            'database_output_sha256': receipt['post_sta']['sha256'],
            'native_endpoint_receipt_verified': True,
            'seed': seed, 'fitted_rbf_sha256': sha(native / 'output_files/top.rbf'),
            'sealed_payload_seed': seed == 1, 'rows': len(rows), 'corners': corners,
            'minima': minima,
            'native_capture_to_controller_ns': {kind: min(float(r['slack_ns']) for r in handoff if r['kind'] == kind)
                                                 for kind in ('setup', 'hold')},
            'native_endpoint_sha256': sha(endpoint_path), 'report_sha256': hashes,
        })
    return {
        'classification': 'conditional single-edge postfit SDRAM reference',
        'source_base': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
        'artifact_revision': record['revision'], 'build_id': pkg.fields['build']['id'],
        'package_id': pkg.package_id, 'payload_sha256': payload,
        'record_sha256': sha(record_path), 'recorded_inputs_equal_source': len(record['source_inputs']),
        'sta_binary_sha256': sha(args.quartus_sta), 'reference_sha256': sha(reference_path),
        'collector_sha256': sha(Path(__file__).with_name('collect.tcl')),
        'runs': runs, 'nominal_tCK_margin_ns': 10 - reference['memory_reference']['tCK_min_ns'],
        'conditional_setup_misses': any(r['minima'][g]['setup']['slack_ns'] < 0 for r in runs for g in groups),
        'physical_budget_complete': False, 'whole_board_qualified': False,
        'new_fit_or_package': False, 'hardware_run': False,
        'unresolved': reference['physical_budget_unknowns'],
    }


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('attempt', 'package', 'module-root', 'work', 'quartus-sta', 'output'):
        p.add_argument('--' + name, type=Path, required=True)
    args = p.parse_args()
    args.output.write_text(json.dumps(summarize(args), indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
