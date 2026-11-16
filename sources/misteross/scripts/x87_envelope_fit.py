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
# --top cpu fits fes_i386_cpu with `x87_present` TIED, so that the pair
# present=0 and present=1 gives what the 387 costs inside the CPU. The unit
# fit above cannot answer that: it keeps regs_o (640 register bits) and the
# trace outputs that the machine trims away, so its ALM count is inflated
# relative to the same logic inside the CPU. The two figures answer different
# questions and must not be averaged -- the unit fit says whether the envelope
# plus backend closes and roughly how big it is; this pair is the number that
# adds to a CPU or machine measurement.
#
# fes_i386_cpu is also the envelope the sibling Agilex 5 measurement calls C
# (docs/validation/2026-11-16-i386-agilex5-fit/method.md), so the delta is
# comparable in kind to its figures.
CPU_TOP = 'fes_i386_cpu'
WRAPPER = 'x87_fit_cpu_top.sv'


def board():
    """The DE25 x87 project's own builder, for its lock, its device, its
    Quartus metadata, its heartbeat/time-cap runner and its report parsing."""
    path = ROOT / 'boards/de25nano/x87/build.py'
    spec = importlib.util.spec_from_file_location('x87_de25_build', path)
    module = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(path.parent))
    spec.loader.exec_module(module)
    return module


def ports(text: str, module: str = TOP) -> list[str]:
    """Every port of the module but `clk`, as a VIRTUAL_PIN pattern. A real
    primary clock input is kept so the clock tree is fitted."""
    head = text[text.index(f'module {module}'):]
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


def unconnected_in_machine() -> set[str]:
    """The ports pc386_system leaves unconnected on its fes_i386_cpu, written
    `.name()`. Reading them from the machine is what keeps this fit's envelope
    the machine's, so the delta is a figure that can be added to a CPU or
    machine measurement."""
    path = ROOT / 'cores/fes-pc386/rtl/pc386_system.sv'
    text = path.read_text()
    body = text[text.index('fes_i386_cpu '):]
    # Balance the parentheses rather than stopping at the first ');': the port
    # list contains nested ones, and truncating early silently shortens the
    # list of ports treated as unused -- which would quietly change this fit's
    # envelope.
    def balance(text: str, opened: int) -> int:
        depth = 0
        for i in range(opened, len(text)):
            if text[i] == '(':
                depth += 1
            elif text[i] == ')':
                depth -= 1
                if depth == 0:
                    return i
        raise SystemExit("could not balance pc386_system's fes_i386_cpu instantiation")
    # Skip a `#(...)` parameter list if there is one: its parentheses are not
    # the port list's, and balancing from the first '(' would close on it and
    # silently report that nothing is unconnected.
    cursor = body.index('(')
    if body[body.index('fes_i386_cpu ') + len('fes_i386_cpu '):cursor].strip().endswith('#') \
       or '#' in body[:cursor]:
        cursor = body.index('(', balance(body, cursor) + 1)
    depth, start, end = 0, cursor, None
    for i in range(start, len(body)):
        if body[i] == '(':
            depth += 1
        elif body[i] == ')':
            depth -= 1
            if depth == 0:
                end = i
                break
    if end is None:
        raise SystemExit('could not find the end of pc386_system\'s fes_i386_cpu instantiation')
    return set(re.findall(r'\.([a-z_][a-z0-9_]*)\s*\(\s*\)', body[start:end]))


def cpu_wrapper(present: int) -> str:
    """A thin top that ties `x87_present`, so the fitter sweeps the 387 at 0
    and builds it at 1. Everything else is passed straight through, and the
    ports are read from fes_i386_cpu rather than listed here, so the wrapper
    cannot drift from the module it wraps."""
    text = (RTL / 'fes_i386_cpu.sv').read_text()
    head = text[text.index(f'module {CPU_TOP}'):]
    head = head[head.index(') ('):]
    head = head[:head.index('\n);')]
    ports = []
    for line in head.splitlines():
        m = re.match(r"\s*(input|output)\s+(?:logic|wire|reg)?\s*((?:\[[^\]]*\]\s*)*)([A-Za-z_]\w*)", line)
        if m:
            ports.append((m.group(1), m.group(2).strip(), m.group(3)))
    if not ports:
        raise SystemExit('no ports parsed from fes_i386_cpu')
    # The machine leaves many of the CPU's outputs unconnected, and the
    # fitter sweeps what feeds them. A virtual-pin fit that exposed them all
    # would keep arch_x87_regs (640 register bits) and the whole x87 trace,
    # and the figure would not be the CPU's. So this wrapper leaves
    # unconnected exactly what pc386_system leaves unconnected, read from its
    # own instantiation rather than listed here.
    unused = unconnected_in_machine()
    decls, conns = [], []
    for direction, width, name in ports:
        if name == 'x87_present':
            continue
        if name in unused:
            conns.append(f'        .{name}()')
            continue
        decls.append(f'    {direction} {("logic " + width) if width else "logic"} {name}')
        conns.append(f'        .{name}({name})')
    return ('// SPDX-License-Identifier: MIT\n'
            '// Generated by scripts/x87_envelope_fit.py; not merged RTL. It exists so\n'
            '// that a fit can tie x87_present, which fes_i386_cpu takes as an input.\n'
            f'module x87_fit_cpu_top (\n' + ',\n'.join(decls) + '\n);\n'
            f'    {CPU_TOP} #(.X87_BACKEND($X87_BACKEND$)) cpu (\n'
            f'        .x87_present(1\'b{present}),\n' + ',\n'.join(conns) + '\n    );\n'
            'endmodule\n')


def prepare(output: Path | None, backend: str, period_ns: float, seed: int,
            top: str = 'unit', present: int = 1) -> Path:
    b = board()
    metadata = b.board_tools().quartus_metadata()
    sources = {name: (RTL / name).read_bytes() for name in SOURCES}
    if top == 'cpu':
        # Everything fes_i386_cpu needs, in the order the netlist program
        # lists it, plus the generated tie wrapper.
        if str(ROOT) not in sys.path:
            sys.path.insert(0, str(ROOT))
        from scripts import yosys_netlist
        sources = {Path(name).name: (ROOT / name).read_bytes()
                   for name in yosys_netlist.CPU_SOURCES}
        sources[WRAPPER] = cpu_wrapper(present).replace(
            '$X87_BACKEND$', str(BACKENDS[backend])).encode()
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
    entity = WRAPPER if top == 'cpu' else 'fes_i386_x87.sv'
    virtual = ports((project / WRAPPER).read_text() if top == 'cpu'
                    else (RTL / 'fes_i386_x87.sv').read_text(),
                    'x87_fit_cpu_top' if top == 'cpu' else TOP)
    assignments = (('FAMILY', '"Agilex 5"'), ('DEVICE', b.DEVICE),
                   ('TOP_LEVEL_ENTITY', 'x87_fit_cpu_top' if top == 'cpu' else TOP),
                   ('PROJECT_OUTPUT_DIRECTORY', 'output_files'),
                   ('LAST_QUARTUS_VERSION', f'"{metadata["qsf_version"]}"'),
                   ('NUM_PARALLEL_PROCESSORS', '2'), ('SEED', str(seed)),
                   ('SDC_FILE', 'clocks.sdc'))
    qsf = ('# Internal-only virtual-pin characterisation of the x87 envelope plus one\n'
           '# arithmetic backend; never assemble or program.\n')
    qsf += ''.join(f'set_global_assignment -name {k} {v}\n' for k, v in assignments)
    qsf += ''.join(f'set_global_assignment -name SYSTEMVERILOG_FILE {n}\n' for n in sources)
    if top != 'cpu':
        qsf += f'set_parameter -name X87_BACKEND {BACKENDS[backend]}\n'
    qsf += ''.join(f'set_instance_assignment -name VIRTUAL_PIN ON -to {{{p}}}\n' for p in virtual)
    qsf += 'set_instance_assignment -name IO_STANDARD "1.1-V" -to clk\n'
    (project / f'{PROJECT}.qsf').write_text(qsf)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    inputs = {'fes_base_revision': revision, 'source_classification': 'working-copy-diagnostic',
              'board': 'de25_nano', 'device': b.DEVICE, 'project': PROJECT,
              'top': 'x87_fit_cpu_top' if top == 'cpu' else TOP,
              'fit_kind': top, 'x87_present': present if top == 'cpu' else None,
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
              'top': inputs['top'], 'fit_kind': inputs['fit_kind'],
              'x87_present': inputs.get('x87_present'),
              'virtual_ports': len(inputs['virtual_data_ports']),
              'flow': 'quartus-pro-systemverilog-direct',
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
        # Amended rule 7, matched on exact leaf names from the dump above.
        # ABSENT are registers written ONLY inside arms guarded by
        # `X87_BACKEND == 0`: the adder's operand registers (A_SUM), the
        # square root's working register (Q_SEED and Q_RES) and the two seed
        # tables' read registers. `dv_rem` is deliberately NOT here -- FPREM
        # writes it from A_OP and FPREM is kept, so it is present by design.
        # PRESENT are positive controls: if the query cannot find these, it
        # cannot find anything, and a zero above would mean nothing.
        skip = inputs.get('x87_present') == 0   # the 387 is swept; neither check applies
        leaves = {line.strip().rsplit('|', 1)[-1]
                  for line in (output / 'registers.txt').read_text().splitlines()
                  if line.strip()}
        # A register may be bit-blasted to name[3] or split by the fitter.
        def held(name: str) -> bool:
            return any(leaf == name or leaf.startswith(name + '[') for leaf in leaves)
        absent = {n: held(n) for n in ('ua_add', 'ub_add', 'qy_s', 'rcp_q', 'rsq_q')}
        present = {n: held(n) for n in ('wm', 'pa', 'cw')}
        record['exclusion'] = {'absent_expected': absent, 'positive_controls': present,
                               'registers': len(leaves)}
        if not skip and not all(present.values()):
            raise SystemExit('the register query found no positive control, so a zero '
                             f'elsewhere means nothing: {present}')
        if inputs['x87_backend'] == 'de25' and not skip:
            leaked = {n: v for n, v in absent.items() if v}
            if leaked:
                raise SystemExit('the DE25 fit still contains the sequenced arithmetic: '
                                 f'{sorted(leaked)}')
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
    parser.add_argument('--top', choices=('unit', 'cpu'), default='unit',
                        help="'unit' fits fes_i386_x87 alone; 'cpu' fits fes_i386_cpu with "
                             "x87_present tied, so present=0 against present=1 gives what the "
                             '387 costs inside the CPU')
    parser.add_argument('--present', type=int, choices=(0, 1), default=1,
                        help='with --top cpu: what x87_present is tied to')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--output', type=Path, help='new output directory; existing ones are refused')
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    args = parser.parse_args(argv)
    b = board()
    output = prepare(args.output, args.backend, args.period_ns, args.seed,
                     args.top, args.present)
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
