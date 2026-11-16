#!/usr/bin/env python3
"""Fit `fes_i386_x87` on Agilex 5, with either arithmetic backend.

An internal virtual-pin characterisation of the x87 **envelope** plus whichever
arithmetic `X87_BACKEND` selects, so that the DE25 387's area and closure can
be read on its own and added to the sibling CPU/machine measurement
(`docs/validation/2026-11-16-i386-agilex5-fit/`). It establishes no board
pinout, no I/O timing, no seal and no hardware acceptance.

The flow, the device, the corner gate and the Quartus Pro lock are the DE25
x87 board project's (`boards/de25nano/x87/build.py`), which this imports rather
than copies; what it adds is a selectable top-level parameter, a constraint
that is an argument rather than a fixed 200 MHz, and the fitter's
resource-by-entity table, which is the evidence for what the selection left out.

  python3 scripts/x87_envelope_fit.py --backend de25 --period-ns 16.276 \
      --seed 1 --quartus-root /data/altera_pro/26.3 --output /abs/new/dir
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
PROJECT = 'fes_i386_x87_fit'
TOP = 'fes_i386_x87'
BACKENDS = {'de10': 0, 'de25': 1}
# The envelope, then the arithmetic the selection may instantiate. Both source
# lists are compiled in both configurations, exactly as the CPU compiles them:
# what changes is the parameter, and the fitter's entity table is what shows
# which arithmetic was actually built.
SOURCES = ('fes_i386_pkg.sv', 'fes_x87_pkg.sv', 'fes_i386_x87_mul.sv',
           'fes_x87_fast.sv', 'fes_x87_slow.sv', 'fes_x87_core.sv', 'fes_i386_x87.sv')


def board():
    """The DE25 x87 project's own builder, for its lock, its device, its
    Quartus metadata, its heartbeat/time-cap runner and its report parsing."""
    path = ROOT / 'boards/de25nano/x87/build.py'
    spec = importlib.util.spec_from_file_location('x87_de25_build', path)
    module = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(path.parent))
    spec.loader.exec_module(module)
    return module


def ports(text: str) -> list[str]:
    """Every port of the module but `clk`, as a VIRTUAL_PIN pattern. A real
    primary clock input is kept so the clock tree is fitted."""
    head = text[text.index(f'module {TOP}'):]
    head = head[:head.index('\n);')]
    found = []
    for line in head.splitlines():
        m = re.match(r"\s*(?:input|output)\s+(?:logic|wire|reg)?\s*"
                     r"(?:\[[^\]]*\]\s*)*([A-Za-z_]\w*)", line)
        if m and m.group(1) != 'clk':
            wide = '[' in line[:line.index(m.group(1))]
            found.append(m.group(1) + ('[*]' if wide else ''))
    if not found:
        raise SystemExit('no ports parsed from the module header')
    return found


def prepare(output: Path | None, backend: str, period_ns: float, seed: int) -> Path:
    b = board()
    metadata = b.board_tools().quartus_metadata()
    sources = {name: (RTL / name).read_bytes() for name in SOURCES}
    extra = {'timing.tcl': (ROOT / 'scripts/x87_envelope_timing.tcl').read_bytes(),
             'x87_envelope_fit.py': Path(__file__).read_bytes()}
    if output is None:
        parent = ROOT / 'build/diagnostic/x87-envelope'
        parent.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix='run-', dir=parent))
    else:
        output = output.resolve()
        output.mkdir(parents=True, exist_ok=False)
    project = output / 'project'
    project.mkdir()
    snapshot = output / 'source'
    snapshot.mkdir()
    for name, data in {**sources, **extra}.items():
        (snapshot / name).write_bytes(data)
    for name, data in sources.items():
        (project / name).write_bytes(data)
    (project / 'timing.tcl').write_bytes(extra['timing.tcl'])
    (project / 'clocks.sdc').write_text(
        '# Internal virtual-pin fit only; no board I/O timing is established.\n'
        f'create_clock -name x87_clock -period {period_ns:.3f} [get_ports clk]\n'
        'derive_clock_uncertainty\n'
        'set data_inputs [remove_from_collection [all_inputs] [get_ports clk]]\n'
        'set_input_delay -clock x87_clock -max 0.000 $data_inputs\n'
        'set_input_delay -clock x87_clock -min 0.000 $data_inputs\n'
        'set_output_delay -clock x87_clock -max 0.000 [all_outputs]\n'
        'set_output_delay -clock x87_clock -min 0.000 [all_outputs]\n')
    (project / f'{PROJECT}.qpf').write_text(
        f'QUARTUS_VERSION = "{metadata["series"]}"\nPROJECT_REVISION = "{PROJECT}"\n')
    virtual = ports((RTL / 'fes_i386_x87.sv').read_text())
    assignments = (('FAMILY', '"Agilex 5"'), ('DEVICE', b.DEVICE),
                   ('TOP_LEVEL_ENTITY', TOP),
                   ('PROJECT_OUTPUT_DIRECTORY', 'output_files'),
                   ('LAST_QUARTUS_VERSION', f'"{metadata["qsf_version"]}"'),
                   ('NUM_PARALLEL_PROCESSORS', '2'), ('SEED', str(seed)),
                   ('SDC_FILE', 'clocks.sdc'))
    qsf = ('# Internal-only virtual-pin characterisation of the x87 envelope plus one\n'
           '# arithmetic backend; never assemble or program.\n')
    qsf += ''.join(f'set_global_assignment -name {k} {v}\n' for k, v in assignments)
    qsf += ''.join(f'set_global_assignment -name SYSTEMVERILOG_FILE {n}\n' for n in SOURCES)
    qsf += f'set_parameter -name X87_BACKEND {BACKENDS[backend]}\n'
    qsf += ''.join(f'set_instance_assignment -name VIRTUAL_PIN ON -to {{{p}}}\n' for p in virtual)
    qsf += 'set_instance_assignment -name IO_STANDARD "1.1-V" -to clk\n'
    (project / f'{PROJECT}.qsf').write_text(qsf)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    inputs = {'fes_base_revision': revision, 'source_classification': 'working-copy-diagnostic',
              'board': 'de25_nano', 'device': b.DEVICE, 'project': PROJECT, 'top': TOP,
              'x87_backend': backend, 'x87_backend_value': BACKENDS[backend],
              'period_ns': period_ns, 'clock_hz': int(1e9 / period_ns), 'seed': seed,
              'quartus': metadata, 'diagnostic_only': True, 'virtual_data_ports': virtual,
              'primary_clock': 'clk (fitter-assigned location)',
              'evidence_scope': 'internal virtual-pin fit of the x87 unit; no board I/O, '
                                'no seal and no hardware acceptance',
              'source_sha256': {n: b.sha(d) for n, d in {**sources, **extra}.items()},
              'project_sha256': {p.name: b.sha(p.read_bytes()) for p in project.iterdir()}}
    (output / 'inputs.json').write_text(json.dumps(inputs, indent=2, sort_keys=True) + '\n')
    return output


def entities(text: str) -> list[dict]:
    """The fitter's resource table by entity. This is the evidence that the
    arithmetic the selection did not choose is not merely unused but absent:
    a DE25 fit must show no reciprocal seed memory and no sequenced adder."""
    rows = []
    block = re.search(r'Fitter Resource Utilization by Entity(.*?)\n\n\n', text, re.S)
    if not block:
        block = re.search(r'Resource Utilization by Entity(.*?)\n\n\n', text, re.S)
    if not block:
        return rows
    for line in block.group(1).splitlines():
        cells = [c.strip() for c in line.strip().strip(';').split(';')]
        if len(cells) > 3 and cells[0] and not set(cells[0]) <= set('-+ '):
            rows.append({'entity': cells[0], 'cells': cells[1:]})
    return rows


def compile_project(output: Path, tool: Path, version: str) -> dict:
    b = board()
    inputs = json.loads((output / 'inputs.json').read_text())
    project = output / 'project'
    for name, expected in inputs['project_sha256'].items():
        if b.sha((project / name).read_bytes()) != expected:
            raise SystemExit(f'prepared project source changed: {name}')
    tools = {n: tool.with_name(n) for n in ('quartus_syn', 'quartus_fit', 'quartus_sta')}
    for path in tools.values():
        if not path.is_file() or not os.access(path, os.X_OK):
            raise SystemExit(f'missing executable {path}')
    record = {'quartus_version': version, 'device': inputs['device'],
              'x87_backend': inputs['x87_backend'], 'period_ns': inputs['period_ns'],
              'clock_hz': inputs['clock_hz'], 'seed': inputs['seed'],
              'evidence_scope': inputs['evidence_scope'], 'hardware_acceptance': 'not-run',
              'programming_artifact': None,
              'tool_sha256': {str(p): b.sha(p.read_bytes()) for p in tools.values()},
              'commands': []}
    deadline = time.monotonic() + b.TIMEOUT_SECONDS
    fitted = project / 'output_files' / f'{PROJECT}.fit.rpt'
    period = f'{inputs["period_ns"]:.6f}'
    try:
        for label, command in (('synthesis', [str(tools['quartus_syn']), PROJECT]),
                               ('fit', [str(tools['quartus_fit']), PROJECT]),
                               ('sta', [str(tools['quartus_sta']), '--multicorner', PROJECT]),
                               ('timing-gate', [str(tools['quartus_sta']), '-t', 'timing.tcl',
                                                str(output), period])):
            b.require_disk(output)
            record['commands'].append(command)
            b.run(command, output, label, deadline)
        text = fitted.read_text(errors='replace')
        record['resources'] = b.resources(text)
        record['entities'] = entities(text)
        record['timing'] = json.loads((output / 'timing.json').read_text())
        if record['timing'].get('status') != 'pass':
            raise SystemExit(f'all-corner timing gate did not pass at {period} ns')
        sta = (project / 'output_files' / f'{PROJECT}.sta.rpt').read_text(errors='replace')
        rows = re.findall(r'^;\s*(Illegal Clocks|Unconstrained Clocks)\s*;\s*(\d+)\s*;\s*(\d+)\s*;', sta, re.M)
        if {r[0] for r in rows} != {'Illegal Clocks', 'Unconstrained Clocks'}:
            raise SystemExit('TimeQuest report lacks unconstrained/illegal clock coverage')
        if any(int(s) or int(h) for _, s, h in rows):
            raise SystemExit('TimeQuest reports unconstrained or illegal clocks')
    except BaseException as error:
        record.update(status='fail', error=str(error))
        if fitted.is_file():
            try:
                text = fitted.read_text(errors='replace')
                record['resources'] = b.resources(text)
                record['entities'] = entities(text)
            except BaseException as parse_error:
                record['resource_parse_error'] = str(parse_error)
        if (output / 'timing.json').is_file():
            try:
                record['timing'] = json.loads((output / 'timing.json').read_text())
            except (ValueError, OSError) as parse_error:
                record['timing_parse_error'] = str(parse_error)
        (output / 'build.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
        raise
    record['status'] = 'pass'
    (output / 'build.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
    return record


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('action', choices=('project', 'build'))
    parser.add_argument('--backend', choices=sorted(BACKENDS), default='de25')
    parser.add_argument('--period-ns', type=float, default=16.276,
                        help='clock period; 16.276 ns is the 61.44 MHz headline constraint')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--output', type=Path, help='new output directory; existing ones are refused')
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    args = parser.parse_args(argv)
    b = board()
    output = prepare(args.output, args.backend, args.period_ns, args.seed)
    print(f'x87 envelope fit: project in {output}', flush=True)
    if args.action == 'project':
        return 0
    if not args.quartus_root:
        raise SystemExit('--quartus-root or QUARTUS_ROOTDIR is required to build')
    tool = Path(args.quartus_root) / 'quartus/bin/quartus_syn'
    if not tool.is_file():
        raise SystemExit(f'no Quartus at {tool}')
    version = b.board_tools().quartus_metadata()['qsf_version']
    # The shared builder lock, so two Quartus runs never overlap on this host.
    with b.LOCK.open('a+') as lock:
        import fcntl
        fcntl.flock(lock, fcntl.LOCK_EX)
        record = compile_project(output, tool, version)
    print(json.dumps({k: record[k] for k in ('status', 'x87_backend', 'period_ns', 'seed', 'resources')},
                     indent=2, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
