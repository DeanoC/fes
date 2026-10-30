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
from scripts.sim_fes_pcxt import CHIP_VENDORS, SHARED_VENDORS, SYSTEM_SOURCES
from scripts.pcxt_quartus_mailbox import validate_mailbox

ROOT = Path(__file__).resolve().parents[1]
CORE = Path('cores/fes-pcxt')
RTL = SYSTEM_SOURCES + ('cores/fes-pcxt/rtl/pcxt_disk_boot.sv',
                        'cores/fes-common/rtl/fes_computer_mailbox.v',
                        str(CORE / 'rtl/fes_pcxt_top.sv'))
ADAPTERS = CORE / 'quartus/adapters.v'
AUDIT = CORE / 'quartus/audit.tcl'
MAILBOX = CORE / 'quartus/mailbox.sdc'
PINS = CORE / 'constraints-board50.qsf'
PACKAGE_PINS = Path('cores/fes-ramtest/constraints.qsf')
DEVICE = '5CSEBA6U23I7'
SDC = ('create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]\n'
       'derive_pll_clocks\nderive_clock_uncertainty\n')


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def input_paths(root: Path) -> list[Path]:
    # The vendor manifests validate files beyond the RTL list, including ROMs
    # and licenses. Capture those too, without copying generated build trees.
    vendor_roots = [Path('cores/fes-common/vendor') / name for name in SHARED_VENDORS]
    tracked = subprocess.check_output(
        ['git', 'ls-files', '--', str(CORE), *map(str, vendor_roots)], cwd=root, text=True).splitlines()
    paths = set(map(Path, tracked)) | set(map(Path, RTL))
    paths.update((ADAPTERS, AUDIT, MAILBOX, PINS, PACKAGE_PINS, Path('scripts/sim_fes_pcxt.py'),
                  Path('scripts/quartus_fes_pcxt.py'), Path('scripts/quartus_tools.py'),
                  Path('scripts/pcxt_quartus_mailbox.py'), Path('tests/test_pcxt_video_mailbox.py')))
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


def elaboration(source: Path, work: Path, library: Path, *, package_profile=False, build_id=None,
                dos_profile=False) -> str:
    includes = [source / 'cores/fes-common/generated', work]
    includes += [source / ('cores/fes-common/vendor' if name in SHARED_VENDORS else CORE / 'vendor') / name
                 for name in CHIP_VENDORS]
    # Yosys unquotes filename arguments, but preserves quotes in -I values.
    # Refuse ambiguous include tokens rather than silently losing the headers.
    if any(any(c.isspace() or c in '\\";' for c in str(p)) for p in includes):
        raise ValueError('Yosys include directories must not contain whitespace, quotes or semicolons')
    quote = lambda p: '"' + str(p).replace('\\', '\\\\').replace('"', '\\"') + '"'
    parameters = '-set MDA 0 -set SCANLINES 0 -set HALF_RATE_BOARD ' + ('0' if package_profile else '1')
    parameters += ' -set HARDWARE_DOS ' + ('1' if dos_profile else '0')
    if package_profile:
        if not isinstance(build_id, str) or not re.fullmatch('[0-9a-f]{32}', build_id):
            raise ValueError('package profile requires an explicit BUILD_ID')
        parameters += " -set ENABLE_FES_COMPUTER 1 -set PHASED_HDMI_CLOCK 1 -set BUILD_ID 128'h" + build_id
    return (
        f'read_verilog -lib {quote(library)}; read_verilog -sv -defer '
        + ' '.join('-I ' + str(p) for p in includes) + ' '
        + ' '.join(quote(source / p) for p in (str(ADAPTERS), *RTL))
        + '; chparam ' + parameters + ' top; '
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


def prepare_elaboration_inputs(source: Path, work: Path, *, dos_profile=False) -> dict:
    """Generate the profile's real ROM, microcode and disk inputs in work."""
    spec = importlib.util.spec_from_file_location('pcxt_frozen_inputs', source / 'scripts/sim_fes_pcxt.py')
    generator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(generator)
    for vendor in ('kf8259', 'kf8253', 'kf8255'):
        generator.verify_vendor(vendor)
    if not dos_profile:
        generator.board_inputs(work)
        return {}

    generator.cpu_roms(work)
    core = source / CORE
    metadata = json.loads((core / 'firmware/hardware-dos.json').read_text())
    bios_spec = json.loads((core / 'sim/bios.lock').read_text())
    bios_path = core / metadata['bios']['file']
    disk_path = core / metadata['floppy']['file']
    bios = bios_path.read_bytes()
    disk = disk_path.read_bytes()
    if (len(bios) != metadata['bios']['size']
            or sha(bios_path) != metadata['bios']['sha256']
            or bios_spec['bios']['sha256'] != metadata['bios']['sha256']
            or len(disk) != metadata['floppy']['size']
            or sha(disk_path) != metadata['floppy']['sha256']):
        raise ValueError('DOS BIOS or floppy image differs from its locked provenance')
    firmware = bytes([0xff]) * (65536 - len(bios)) + bios
    generator.boot_roms(work, firmware, prefix='pcxt_glabios')
    font = bytearray(4096)
    offset = bios_spec['ascii_font']['rom_offset']
    for char in range(128):
        font[char * 16:char * 16 + 8] = bios[offset + char * 8:offset + char * 8 + 8]
    (work / 'pcxt_glabios_font.hex').write_text(''.join(f'{byte:02x}\n' for byte in font))
    (work / 'pcxt_freedos_disk.hex').write_text(''.join(f'{byte:02x}\n' for byte in disk))
    return {
        'freedos_image_sha256': metadata['floppy']['sha256'],
        'glabios_rom_sha256': metadata['bios']['sha256'],
        'glabios_font_sha256': hashlib.sha256(font).hexdigest(),
        'loaded_into_sdram_before_cpu_release': True,
        'guest_disk_reads_use_sdram_media_path': True,
    }


def validate_renaming(work: Path, *, package_profile=False) -> dict:
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
    memory_settings = ({'number_of_clocks': 2, 'output_clock_frequency0': '100.0 MHz',
                        'output_clock_frequency1': '100.0 MHz', 'phase_shift0': '0 ps', 'phase_shift1': '5000 ps'}
                       if package_profile else
                       {'number_of_clocks': 4, 'output_clock_frequency0': '100.0 MHz',
                        'output_clock_frequency1': '50.0 MHz', 'output_clock_frequency2': '50.0 MHz',
                        'output_clock_frequency3': '50.0 MHz', 'phase_shift0': '0 ps',
                        'phase_shift1': '10000 ps', 'phase_shift2': '0 ps', 'phase_shift3': '18000 ps'})
    if package_profile and counts.get('cyclonev_hps_interface_mpu_general_purpose') != 1:
        raise ValueError('package profile lacks its runtime mailbox')
    for name, settings in {
        'memory_clock': memory_settings,
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
    hps = {name: cell['type'] for name, cell in cells.items()
           if cell['type'] in ('cyclonev_hps_interface_peripheral_i2c',
                               'cyclonev_hps_interface_mpu_general_purpose')}
    return {'complete_cell_and_net_records_equal_under_names': True, 'cells': counts,
            'hps_primitive_instances': hps}


def project_qsf(source: Path, seed: int, threads: int, *, package_profile=False) -> str:
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
        (source / (PACKAGE_PINS if package_profile else PINS)).read_text(),
    ]
    return '\n'.join(lines) + '\n'


def timing_summary(work: Path, mailbox_contract: bool = False) -> dict:
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
        summary['failures'] += r['slack_ns'].startswith('-')
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
        pulse_width[corner] = {'checks': int(match[1]), 'failures': int(match[2]) + int(match[3].startswith('-') and int(match[2]) == 0),
                               'minimum_slack_ns': float(match[3])}
    coverage = (work / 'audit/check-timing.rpt').read_text()
    issues = {k: int(v) for k, v in re.findall(
        r'^;\s*([a-z_]+)\s*;\s*(\d+)\s*;', coverage, re.M)}
    if not {'no_clock', 'no_input_delay', 'no_output_delay', 'loops', 'latches'} <= issues.keys():
        raise ValueError('missing timing-coverage checks')
    no_clock = re.findall(r"^;\s*([^;]+?)\s*;\s*No clock feeds this register's clock port\.\s*;",
                          coverage, re.M)
    if len(no_clock) != issues['no_clock'] or len(no_clock) != len(set(no_clock)):
        raise ValueError('missing or duplicated unclocked register inventory')
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
    interstage = []
    if mailbox_contract:
        with (work / 'audit/mailbox-interstage.tsv').open() as stream:
            interstage = list(csv.DictReader(stream, delimiter='\t'))
        expected = {(c, k, 'system_video_adapter_' + r + '_meta', 'system_video_adapter_' + r + '_sync')
                    for c in corners for k in ('setup', 'hold') for r in ('ack', 'sent', 'hold')}
        observed = [(r['corner'], r['kind'], r['from'], r['to']) for r in interstage]
        if len(observed) != 24 or set(observed) != expected:
            raise ValueError('missing or duplicated mailbox interstage checks')
        if not all(math.isfinite(float(r['slack_ns'])) for r in interstage):
            raise ValueError('invalid mailbox interstage slack')
    return {'capture_complete': True, 'by_corner_and_check': by_corner,
            'reported_endpoint_checks_pass': not any(r['failures'] for r in by_corner.values()),
            'minimum_pulse_width': pulse_width, 'timing_coverage_issues': issues,
            'no_clock_registers': no_clock,
            'clocks': clock_records, 'fmax': fmax,
            'mailbox_interstage': interstage,
            'whole_board_qualified': False,
            'qualification_limits': ['no external IO delay contract',
                                     'no validated asynchronous source/reset contract',
                                     'no repeated-build or hardware acceptance']}


def run(command: list[str], work: Path, log: str, record: dict, env: dict | None = None) -> None:
    operation = {'command': command, 'cwd': str(work), 'log': log,
                 'started_utc': datetime.now(timezone.utc).isoformat()}
    record['operations'].append(operation)
    print(work.name + ': ' + log, flush=True)
    with (work / log).open('w') as stream:
        result = subprocess.run(command, cwd=work, stdout=stream, stderr=subprocess.STDOUT, env=env)
    operation.update(returncode=result.returncode, completed_utc=datetime.now(timezone.utc).isoformat())
    if result.returncode:
        raise RuntimeError(f'{command[0]} failed ({result.returncode}); see {work / log}')


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--yosys', required=True, type=Path)
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    parser.add_argument('--seed', type=int, action='append', help='repeat; default 1, 2, 3')
    parser.add_argument('--threads', type=int, default=2)
    parser.add_argument('--video-mailbox-contract', action='store_true',
                        help='prove and bind the held video mailbox before applying its scoped diagnostic SDC')
    parser.add_argument('--output', type=Path, help='new output directory; existing evidence is never overwritten')
    parser.add_argument('--package-profile', action='store_true')
    parser.add_argument('--dos-profile', action='store_true',
                        help='elaborate the FreeDOS ROM and SDRAM floppy hardware profile')
    parser.add_argument('--build-id')
    args = parser.parse_args(argv)
    if args.package_profile and not args.video_mailbox_contract:
        parser.error('package profile requires --video-mailbox-contract')
    seeds = args.seed or [1, 2, 3]
    if len(seeds) != len(set(seeds)) or any(not 1 <= s <= 2147483647 for s in seeds):
        parser.error('seeds must be distinct positive signed 32-bit integers')
    if args.threads < 1:
        parser.error('--threads must be positive')
    if args.dos_profile and not args.package_profile:
        parser.error('DOS profile requires --package-profile')
    record = {'classification': ('whole-board Quartus hardware DOS package profile; not physical acceptance'
                                 if args.dos_profile else 'whole-board Quartus diagnostic; not physical acceptance'),
              'toolchain': 'quartus', 'video_mailbox_contract': args.video_mailbox_contract,
              'whole_board_qualified': False, 'device': DEVICE, 'seeds': seeds,
              'profile': {'MDA': 0, 'SCANLINES': 0, 'HALF_RATE_BOARD': int(not args.package_profile),
                          'ENABLE_FES_COMPUTER': int(args.package_profile),
                          'HARDWARE_DOS': int(args.dos_profile), 'BUILD_ID': args.build_id},
              'clock_targets_mhz': {'cpu': 100, 'board': 100 if args.package_profile else 50, 'pixel': 74.25},
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
        if args.video_mailbox_contract:
            abc = yosys.with_name('yosys-abc')
            if not abc.is_file():
                raise ValueError('mailbox contract requires the selected Yosys installation\'s yosys-abc')
            record['mailbox_proof_abc'] = {'path': str(abc), 'sha256': sha(abc)}
            run([sys.executable, str(source / 'tests/test_pcxt_video_mailbox.py'), '-v'],
                source, 'mailbox-proofs.log', record,
                dict(os.environ, YOSYS=str(yosys), YOSYS_ABC=str(abc)))
            proof = (source / 'mailbox-proofs.log').read_text()
            if 'Ran 4 tests' not in proof or 'skipped' in proof or '\nOK\n' not in proof:
                raise ValueError('the complete mailbox proof did not run')
            record['mailbox_proof_log_sha256'] = sha(source / 'mailbox-proofs.log')
        work = output / 'elaboration'
        work.mkdir()
        dos_inputs = prepare_elaboration_inputs(source, work, dos_profile=args.dos_profile)
        if dos_inputs:
            record['hardware_dos_inputs'] = dos_inputs
        (work / 'elaborate.ys').write_text(elaboration(
            source, work, library, package_profile=args.package_profile,
            build_id=args.build_id, dos_profile=args.dos_profile))
        run([str(yosys), '-s', 'elaborate.ys'], work, 'elaborate.log', record)
        record['elaboration'] = validate_renaming(work, package_profile=args.package_profile)
        if args.video_mailbox_contract:
            record['mailbox_binding'] = validate_mailbox(
                json.loads((work / 'before-names.json').read_text())['modules']['top'],
                json.loads((work / 'elaborated.json').read_text())['modules']['top'],
                half_rate_board=not args.package_profile)
        record['generated'] = {p.name: sha(p) for p in work.iterdir() if p.suffix in ('.mem', '.hex', '.vh')}
        record['elaborated_verilog_sha256'] = sha(work / 'elaborated.v')
        for seed in seeds:
            fit = output / f'seed-{seed}'
            fit.mkdir()
            shutil.copy2(work / 'elaborated.v', fit / 'elaborated.v')
            shutil.copy2(source / AUDIT, fit / 'audit.tcl')
            (fit / 'top.qpf').write_text('QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n')
            (fit / 'top.qsf').write_text(project_qsf(source, seed, args.threads, package_profile=args.package_profile))
            (fit / 'clocks.sdc').write_text(SDC)
            if args.video_mailbox_contract:
                shutil.copy2(source / MAILBOX, fit / 'mailbox.sdc')
                (fit / 'clocks.sdc').write_text(SDC + '\nsource mailbox.sdc\n')
            receipt = {'seed': seed, 'qsf_sha256': sha(fit / 'top.qsf'),
                       'sdc_sha256': sha(fit / 'clocks.sdc'), 'status': 'building'}
            record['runs'].append(receipt)
            run([str(quartus_sh), '--flow', 'compile', 'top'], fit, 'compile.log', record)
            run([str(bin_dir / 'quartus_sta'), '-t', 'audit.tcl'], fit, 'audit.log', record)
            if sha(fit / 'clocks.sdc') != receipt['sdc_sha256']:
                raise ValueError('timing constraints changed during the fit')
            receipt.update(timing_summary(fit, mailbox_contract=args.video_mailbox_contract))
            receipt['rbf_sha256'] = sha(fit / 'output_files/top.rbf')
            captures = [fit / name for name in ('top.qsf', 'clocks.sdc', 'audit.tcl', 'compile.log', 'audit.log')]
            if args.video_mailbox_contract:
                captures.append(fit / 'mailbox.sdc')
            captures += list((fit / 'audit').rglob('*'))
            captures += [p for p in (fit / 'output_files').iterdir() if p.suffix in ('.rpt', '.summary')]
            receipt['captures_sha256'] = {str(p.relative_to(fit)): sha(p)
                                          for p in sorted(captures) if p.is_file()}
            receipt['status'] = 'diagnostic-captured'
            (output / 'summary.json').write_text(json.dumps(record, indent=2) + '\n')
            if args.package_profile and (not receipt['reported_endpoint_checks_pass'] or
                    any(v['failures'] for v in receipt['minimum_pulse_width'].values())):
                raise ValueError('package internal timing failed at seed ' + str(seed))
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
