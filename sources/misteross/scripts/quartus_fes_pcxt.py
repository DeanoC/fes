#!/usr/bin/env python3
"""Fit the complete CPU100/board50 PC/XT diagnostic with Quartus 17.0.2.

Yosys elaborates the unchanged board RTL and preserves its large memories.
Quartus synthesizes, places and routes that elaboration. Each invocation keeps
an isolated source capture and every failed run. This is not a package producer
or physical board qualification: external IO budgets and asynchronous source
requirements are still open. No clocks, paths or timing failures are waived.
"""
from __future__ import annotations

import argparse
from collections import Counter
import csv
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tomllib
import uuid

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.quartus_tools import locate_quartus, quartus_version_line
from scripts.sim_fes_pcxt import SYSTEM_SOURCES

ROOT = Path(__file__).resolve().parents[1]
CORE = Path('cores/fes-pcxt')
RTL = SYSTEM_SOURCES + (str(CORE / 'rtl/fes_pcxt_top.sv'),)
ADAPTERS = CORE / 'quartus/adapters.v'
AUDIT = CORE / 'quartus/audit.tcl'
PINS = CORE / 'constraints-board50.qsf'
DEVICE = '5CSEBA6U23I7'
SDC = ('create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]\n'
       'derive_pll_clocks\nderive_clock_uncertainty\n')


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def input_paths(root: Path) -> list[Path]:
    # The vendor manifests validate files beyond the RTL list, including ROMs
    # and licenses. Capture those too, without copying generated build trees.
    tracked = subprocess.check_output(
        ['git', 'ls-files', '--', str(CORE)], cwd=root, text=True).splitlines()
    paths = set(map(Path, tracked)) | set(map(Path, RTL))
    paths.update((ADAPTERS, AUDIT, PINS, Path('scripts/sim_fes_pcxt.py'),
                  Path('scripts/quartus_fes_pcxt.py'), Path('scripts/quartus_tools.py')))
    paths.update(p.relative_to(root) for p in (root / 'cores/fes-common/generated').glob('*.vh'))
    return sorted(paths)


def capture_sources(root: Path, output: Path) -> tuple[Path, dict[str, str]]:
    frozen = output / 'source'
    frozen.mkdir()
    identities = {}
    for relative in input_paths(root):
        data = (root / relative).read_bytes()
        target = frozen / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        identities[str(relative)] = hashlib.sha256(data).hexdigest()
    return frozen, identities


def elaboration(source: Path, work: Path, library: Path) -> str:
    includes = [source / 'cores/fes-common/generated', work]
    includes += [source / CORE / 'vendor' / name for name in ('kf8259', 'kf8253', 'kf8255')]
    quote = lambda p: '"' + str(p).replace('\\', '\\\\').replace('"', '\\"') + '"'
    return (
        f'read_verilog -lib {quote(library)}; read_verilog -sv '
        + ' '.join('-I ' + quote(p) for p in includes) + ' '
        + ' '.join(quote(source / p) for p in (str(ADAPTERS), *RTL))
        + '; chparam -set MDA 0 -set SCANLINES 0 -set HALF_RATE_BOARD 1 top; '
        # Both libraries spell disabled bus hold differently: OFF is the
        # board's Yosys primitive value, FALSE is Quartus's accepted spelling.
        'setparam -set enable_bus_hold "FALSE" t:altiobuf_bidir; '
        'hierarchy -check -top top; proc -norom; flatten; opt -purge; '
        # Combining byte writes before Verilog export lets Quartus infer the
        # existing dual-read VRAMs, including their OLD_DATA collision rules.
        # Small control arrays with multiple write processes become FFs.
        'memory -nomap; memory_map t:$mem_v2 r:SIZE<=32 %i; opt -purge; '
        'check -assert; write_json before-names.json; '
        # Quartus 17 crashes on flattened array names. This changes names only.
        # Keep this in the SAME Yosys execution: a JSON round trip loses signed
        # parameter metadata, which Quartus needs on PLL integer parameters.
        'rename -unescape; write_json elaborated.json; '
        'write_verilog -noattr elaborated.v\n')


def validate_renaming(work: Path) -> dict:
    before = json.loads((work / 'before-names.json').read_text())['modules']
    after = json.loads((work / 'elaborated.json').read_text())['modules']
    if before.keys() != after.keys():
        raise ValueError('portable naming changed the module set')
    for name, old in before.items():
        new = after[name]
        if old['ports'] != new['ports']:
            raise ValueError('portable naming changed ports: ' + name)
        for category in ('cells', 'netnames'):
            records = lambda m: Counter(json.dumps(v, sort_keys=True) for v in m[category].values())
            if records(old) != records(new):
                raise ValueError('portable naming changed ' + category + ': ' + name)
    cells = after['top']['cells']
    counts = dict(Counter(c['type'] for c in cells.values()))
    boundary = {'altera_pll': 2, 'altiobuf_bidir': 16, 'cyclonev_clkena': 1,
                'cyclonev_hps_interface_peripheral_i2c': 1, 'altddio_out': 1}
    if any(counts.get(kind) != count for kind, count in boundary.items()):
        raise ValueError('whole-board primitive boundary is incomplete')
    for name, settings in {
        'memory_clock': {'number_of_clocks': 4, 'output_clock_frequency0': '100.0 MHz',
                         'output_clock_frequency1': '50.0 MHz', 'output_clock_frequency2': '50.0 MHz',
                         'output_clock_frequency3': '50.0 MHz', 'phase_shift0': '0 ps',
                         'phase_shift1': '10000 ps', 'phase_shift2': '0 ps', 'phase_shift3': '18000 ps'},
        'video_clock': {'number_of_clocks': 2, 'output_clock_frequency0': '74.25 MHz',
                        'output_clock_frequency1': '74.25 MHz', 'phase_shift0': '0 ps',
                        'phase_shift1': '8979 ps'},
    }.items():
        cell = cells.get(name, {})
        if cell.get('type') != 'altera_pll':
            raise ValueError('missing declared PLL: ' + name)
        for key, expected in settings.items():
            value = cell['parameters'].get(key)
            if isinstance(expected, int) and isinstance(value, str) and set(value) <= {'0', '1'}:
                value = int(value, 2)
            if value != expected:
                raise ValueError('clock request changed: ' + name + '.' + key)
    return {'complete_cell_and_net_records_equal_under_names': True, 'cells': counts}


def project_qsf(source: Path, seed: int, threads: int) -> str:
    lines = [
        'set_global_assignment -name FAMILY "Cyclone V"',
        f'set_global_assignment -name DEVICE {DEVICE}',
        'set_global_assignment -name TOP_LEVEL_ENTITY top',
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files',
        f'set_global_assignment -name NUM_PARALLEL_PROCESSORS {threads}',
        'set_global_assignment -name GENERATE_RBF_FILE ON',
        f'set_global_assignment -name SEED {seed}',
        'set_global_assignment -name VERILOG_FILE elaborated.v',
        'set_global_assignment -name SDC_FILE clocks.sdc',
        # Match the no-DSP OSS recipe. This board does not need multipliers.
        'set_global_assignment -name DSP_BLOCK_BALANCING "LOGIC ELEMENTS"',
        'set_instance_assignment -name HPS_LOCATION '
        'HPSINTERFACEPERIPHERALI2C_X52_Y60_N111 -entity top -to hdmi_i2c',
        (source / PINS).read_text(),
    ]
    return '\n'.join(lines) + '\n'


def timing_summary(work: Path) -> dict:
    if 'PCXT_AUDIT_CAPTURE_COMPLETE' not in (work / 'audit.log').read_text():
        raise ValueError('TimeQuest capture did not complete')
    with (work / 'audit/endpoints.tsv').open() as stream:
        rows = list(csv.DictReader(stream, delimiter='\t'))
    with (work / 'audit/counts.tsv').open() as stream:
        counts = list(csv.DictReader(stream, delimiter='\t'))
    with (work / 'audit/clocks.tsv').open() as stream:
        clock_records = list(csv.DictReader(stream, delimiter='\t'))
        clocks = [r['name'] for r in clock_records]
    if not clocks or len(clocks) != len(set(clocks)):
        raise ValueError('missing or duplicate clock inventory')
    actual = Counter((r['corner'], r['kind'], r['from_clock'], r['to_clock'])
                     if r['kind'] in ('setup', 'hold') else (r['corner'], r['kind'], '*', '*')
                     for r in rows)
    expected = {}
    for r in counts:
        key = tuple(r[k] for k in ('corner', 'kind', 'from_clock', 'to_clock'))
        if key in expected or not 0 <= int(r['count']) < 100000:
            raise ValueError('invalid or truncated endpoint count')
        expected[key] = int(r['count'])
    if actual != Counter({k: v for k, v in expected.items() if v}):
        raise ValueError('endpoint capture does not match declared counts')
    if len({r['corner'] for r in counts}) != 4:
        raise ValueError('expected all four operating conditions')
    corners = {r['corner'] for r in counts}
    matrix = {(corner, kind, a, b) for corner in corners
              for kind in ('setup', 'hold') for a in clocks for b in clocks}
    matrix.update((corner, kind, '*', '*') for corner in corners for kind in ('recovery', 'removal'))
    if set(expected) != matrix:
        raise ValueError('incomplete clock-pair/check matrix')
    by_corner = {}
    for r in rows:
        slack = float(r['slack_ns'])
        if not all(math.isfinite(float(r[k])) for k in ('slack_ns', 'relationship_ns', 'data_delay_ns')):
            raise ValueError('non-finite native slack')
        key = r['corner'] + '/' + r['kind']
        summary = by_corner.setdefault(key, {'endpoints': 0, 'failures': 0, 'worst': r})
        summary['endpoints'] += 1
        summary['failures'] += slack < 0
        if slack < float(summary['worst']['slack_ns']):
            summary['worst'] = r
    for corner in {r['corner'] for r in counts}:
        if any(corner + '/' + kind not in by_corner for kind in ('setup', 'hold')):
            raise ValueError('missing setup or hold endpoint coverage')
    pulse_width = {}
    for corner in corners:
        raw = (work / 'audit' / corner / 'pulse-width.rpt').read_text()
        match = re.search(r'Found (\d+) results \((\d+) violated\)\.\s+Worst case slack is ([+-]?[\d.]+)', raw)
        if not match or not 0 < int(match[1]) < 100000 or not math.isfinite(float(match[3])):
            raise ValueError('missing or truncated minimum pulse-width capture')
        pulse_width[corner] = {'checks': int(match[1]), 'failures': int(match[2]),
                               'minimum_slack_ns': float(match[3])}
    issues = {k: int(v) for k, v in re.findall(
        r'^;\s*([a-z_]+)\s*;\s*(\d+)\s*;', (work / 'audit/check-timing.rpt').read_text(), re.M)}
    if not {'no_clock', 'no_input_delay', 'no_output_delay', 'loops', 'latches'} <= issues.keys():
        raise ValueError('missing timing-coverage checks')
    fmax = []
    with (work / 'audit/fmax.tsv').open() as stream:
        for fields in list(csv.reader(stream, delimiter='\t'))[1:]:
            if (len(fields) < 4 or fields[0] not in corners or fields[1] not in clocks or
                    not all(math.isfinite(float(v)) and float(v) > 0 for v in fields[2:4])):
                raise ValueError('invalid Fmax observation')
            fmax.append({'corner': fields[0], 'clock': fields[1], 'fmax_mhz': float(fields[2]),
                         'restricted_fmax_mhz': float(fields[3]), 'restriction': '\t'.join(fields[4:])})
    if not fmax or {r['corner'] for r in fmax} != corners:
        raise ValueError('missing Fmax observations')
    return {'capture_complete': True, 'by_corner_and_check': by_corner,
            'reported_endpoint_checks_pass': not any(r['failures'] for r in by_corner.values()),
            'minimum_pulse_width': pulse_width, 'timing_coverage_issues': issues,
            'clocks': clock_records, 'fmax': fmax,
            'whole_board_qualified': False,
            'qualification_limits': ['no external IO delay contract',
                                     'no validated asynchronous source/reset contract',
                                     'no repeated-build or hardware acceptance']}


def run(command: list[str], work: Path, log: str, record: dict) -> None:
    operation = {'command': command, 'cwd': str(work), 'log': log,
                 'started_utc': datetime.now(timezone.utc).isoformat()}
    record['operations'].append(operation)
    print(work.name + ': ' + log, flush=True)
    with (work / log).open('w') as stream:
        result = subprocess.run(command, cwd=work, stdout=stream, stderr=subprocess.STDOUT)
    operation.update(returncode=result.returncode, completed_utc=datetime.now(timezone.utc).isoformat())
    if result.returncode:
        raise RuntimeError(f'{command[0]} failed ({result.returncode}); see {work / log}')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--yosys', required=True, type=Path)
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    parser.add_argument('--seed', type=int, action='append', help='repeat; default 1, 2, 3')
    parser.add_argument('--threads', type=int, default=2)
    parser.add_argument('--output', type=Path, help='new output directory; existing evidence is never overwritten')
    args = parser.parse_args()
    seeds = args.seed or [1, 2, 3]
    if len(seeds) != len(set(seeds)) or any(not 1 <= s <= 2147483647 for s in seeds):
        parser.error('seeds must be distinct positive signed 32-bit integers')
    if args.threads < 1:
        parser.error('--threads must be positive')
    record = {'classification': 'whole-board Quartus diagnostic; not physical acceptance',
              'whole_board_qualified': False, 'device': DEVICE, 'seeds': seeds,
              'profile': {'MDA': 0, 'SCANLINES': 0, 'HALF_RATE_BOARD': 1},
              'clock_targets_mhz': {'cpu': 100, 'board': 50, 'pixel': 74.25},
              'operations': [], 'runs': [], 'hardware': False, 'status': 'preparing'}
    output = None
    created = False
    try:
        _, quartus_sh = locate_quartus(args.quartus_root, ROOT)
        record['quartus_version'] = quartus_version_line(quartus_sh)
        yosys = args.yosys.resolve(strict=True)
        record['yosys'] = {'path': str(yosys), 'sha256': sha(yosys),
                           'version': subprocess.check_output([str(yosys), '-V'], text=True).strip()}
        lock = tomllib.loads((ROOT / CORE / 'toolchain.lock').read_text())
        expected = lock['tool']['yosys']['commit']
        if 'git sha1 ' + expected[:9] not in record['yosys']['version']:
            raise ValueError('select the PC/XT lock\'s Yosys for matched elaboration')
        record['yosys']['source_commit'] = expected
        library = yosys.parent.parent / 'share/yosys/intel_alm/common/megafunction_bb.v'
        record['yosys_library'] = {'path': str(library), 'sha256': sha(library)}
        bin_dir = quartus_sh.parent
        record['quartus_binaries'] = {n: sha(bin_dir / n) for n in
                                     ('quartus_sh', 'quartus_map', 'quartus_fit', 'quartus_asm', 'quartus_sta')}
        record['quartus_native_binaries'] = {n.name: sha(n) for n in
                                            sorted((bin_dir.parent / 'linux64').glob('quartus_*'))
                                            if n.name in ('quartus_sh', 'quartus_map', 'quartus_fit',
                                                          'quartus_asm', 'quartus_sta')}
        stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
        output = (args.output or ROOT / 'build/diagnostic/fes-pcxt-quartus' / stamp).resolve()
        output.mkdir(parents=True, exist_ok=False)
        created = True
        print(output, flush=True)
        record['revision'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        selected = [str(p) for p in input_paths(ROOT)]
        record['selected_working_tree_changes'] = subprocess.check_output(
            ['git', 'status', '--porcelain', '--', *selected], cwd=ROOT, text=True).splitlines()
        source, record['sources'] = capture_sources(ROOT, output)
        work = output / 'elaboration'
        work.mkdir()
        spec = importlib.util.spec_from_file_location('pcxt_frozen_inputs', source / 'scripts/sim_fes_pcxt.py')
        generator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(generator)
        for vendor in ('kf8259', 'kf8253', 'kf8255'):
            generator.verify_vendor(vendor)
        generator.board_inputs(work)
        (work / 'elaborate.ys').write_text(elaboration(source, work, library))
        run([str(yosys), '-s', 'elaborate.ys'], work, 'elaborate.log', record)
        record['elaboration'] = validate_renaming(work)
        record['generated'] = {p.name: sha(p) for p in work.iterdir() if p.suffix in ('.mem', '.hex', '.vh')}
        record['elaborated_verilog_sha256'] = sha(work / 'elaborated.v')
        for seed in seeds:
            fit = output / f'seed-{seed}'
            fit.mkdir()
            shutil.copy2(work / 'elaborated.v', fit / 'elaborated.v')
            shutil.copy2(source / AUDIT, fit / 'audit.tcl')
            (fit / 'top.qpf').write_text('QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n')
            (fit / 'top.qsf').write_text(project_qsf(source, seed, args.threads))
            (fit / 'clocks.sdc').write_text(SDC)
            receipt = {'seed': seed, 'qsf_sha256': sha(fit / 'top.qsf'),
                       'sdc_sha256': sha(fit / 'clocks.sdc'), 'status': 'building'}
            record['runs'].append(receipt)
            run([str(quartus_sh), '--flow', 'compile', 'top'], fit, 'compile.log', record)
            run([str(bin_dir / 'quartus_sta'), '-t', 'audit.tcl'], fit, 'audit.log', record)
            receipt.update(timing_summary(fit))
            receipt['rbf_sha256'] = sha(fit / 'output_files/top.rbf')
            captures = [fit / name for name in ('top.qsf', 'clocks.sdc', 'audit.tcl', 'compile.log', 'audit.log')]
            captures += list((fit / 'audit').rglob('*'))
            captures += [p for p in (fit / 'output_files').iterdir() if p.suffix in ('.rpt', '.summary')]
            receipt['captures_sha256'] = {str(p.relative_to(fit)): sha(p)
                                          for p in sorted(captures) if p.is_file()}
            receipt['status'] = 'diagnostic-captured'
            (output / 'summary.json').write_text(json.dumps(record, indent=2) + '\n')
        if any(sha(ROOT / p) != value for p, value in record['sources'].items()):
            raise ValueError('selected sources changed during the diagnostic')
        record['status'] = 'diagnostic-captured'
        return 0
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as error:
        record.update(status='failed', error=str(error))
        print(str(error), file=sys.stderr)
        return 1
    finally:
        if created:
            record['completed_utc'] = datetime.now(timezone.utc).isoformat()
            (output / 'summary.json').write_text(json.dumps(record, indent=2) + '\n')


if __name__ == '__main__':
    raise SystemExit(main())
