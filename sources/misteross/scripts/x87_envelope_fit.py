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
           'fes_x87_fast.sv', 'fes_x87_slow.sv', 'fes_x87_core.sv',
           'fes_x87_trans_pkg.sv', 'fes_x87_trans_rom.sv', 'fes_x87_trans.sv', 'fes_i386_x87.sv')
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


# The i386 RTL asks for Cyclone V block RAM by name, and Agilex 5 has no
# M10K: altera_syncram refuses RAM_BLOCK_TYPE "M10K" outright and synthesis
# stops before elaboration. The x87's two seed tables and the CPU's caches
# both do it. This renames exactly that token in the PRIVATE SNAPSHOT -- no
# RTL changes, and the DE10 seal still infers what it always did.
#
# The approach, the round-trip check and the reasoning are the sibling Agilex 5
# measurement's (scripts/fit_i386_device.py, `retarget_ram_attribute`), adopted
# rather than reinvented so that both measurements differ in as little as
# possible.
M10K_ATTRIBUTE = 'ramstyle = "M10K"'
M20K_ATTRIBUTE = 'ramstyle = "M20K"'


def retarget_ram_attribute(text: str) -> tuple[str, int]:
    """Rename the M10K block request to M20K for an Agilex 5 fit, verified by
    round trip: turning M20K back into M10K must reproduce the original bytes
    exactly, which proves both that nothing else changed and that the source
    held no M20K request already. MLAB requests are legal on both devices and
    are left alone."""
    if M20K_ATTRIBUTE in text:
        raise SystemExit('source already asks for M20K; the round-trip check could not tell '
                         'a rewritten request from an original one')
    rewritten = text.replace(M10K_ATTRIBUTE, M20K_ATTRIBUTE)
    count = text.count(M10K_ATTRIBUTE)
    if rewritten.replace(M20K_ATTRIBUTE, M10K_ATTRIBUTE) != text:
        raise SystemExit('the M10K rename did not round-trip; refusing to fit a snapshot '
                         'that differs from the source in more than that token')
    return rewritten, count


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


def seal_source(output: Path, sources: dict, entity: str, cache_root: Path | None,
                parameters: dict | None = None) -> dict:
    """Elaborate the design through the seal's own Yosys passes and fit THAT,
    instead of handing Quartus the SystemVerilog.

    This exists so the figure is on the same front end as the sibling Agilex 5
    measurement, whose primary flow is this netlist: a direct-SystemVerilog
    delta of mine would differ from its figures by front end as well as by
    scope. The seal's SEAL recipe is device-neutral -- no technology mapping --
    so the same bytes are valid input to either device's Quartus.

    The Yosys log is checked for the implicit-declaration warning of
    DeanoC/yosys#79. A packed array fixed the one occurrence this design had,
    but the failure is silent, so the build refuses rather than trusting that.
    """
    if str(ROOT) not in sys.path:
        sys.path.insert(0, str(ROOT))
    from scripts import yosys_netlist, i386_verilator
    yosys = i386_verilator.locate_yosys(cache_root)
    work = output / 'yosys'
    work.mkdir(parents=True, exist_ok=True)
    for name, data in sources.items():
        (work / name).write_bytes(data)
    # The parameter MUST reach the elaboration: on the unit top the netlist is
    # built from fes_i386_x87 directly, and without -chparam it would be
    # X87_BACKEND = 0 whatever --backend said -- a fit of the wrong design
    # reported under the right label. The CPU top needs none, because its
    # generated wrapper writes the parameter into the instantiation.
    netlist = yosys_netlist.seal_netlist(
        yosys.path if hasattr(yosys, 'path') else yosys, work, work,
        sources=tuple(sources), top=entity, parameters=parameters)
    log = (work / 'yosys.log').read_text(errors='replace')
    bad = [line for line in log.splitlines() if 'implicitly declared' in line]
    if bad:
        raise SystemExit('the seal\'s Yosys reported implicit declarations, which is the '
                         'silent miscompile of DeanoC/yosys#79; refusing to fit this netlist:\n'
                         + '\n'.join(bad[:5]))
    return {netlist.name: netlist.read_bytes()}


def prepare(output: Path | None, backend: str, period_ns: float, seed: int,
            top: str = 'unit', present: int = 1, flow: str = 'sv',
            cache_root: Path | None = None) -> Path:
    # ONE normalized period for the SDC, the timing gate and the record. The
    # SDC used to be written at three decimals while the gate was passed six
    # and required equality within 1e-6, so a natural ladder value like
    # 13.333333 ns for 75 MHz would have constrained 13.333 and then been
    # rejected by its own gate after a valid fit -- which would have bitten
    # exactly on the clock ladder. Codex found this on PR #1907 before the
    # ladder ran. Six decimals is finer than Quartus's own resolution and is
    # what both sides now see.
    period_ns = round(float(period_ns), 6)
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
    retargeted = {}
    for name, data in sources.items():
        text = data.decode()
        if M10K_ATTRIBUTE in text:
            text, count = retarget_ram_attribute(text)
            retargeted[name] = count
            data = text.encode()
        (project / name).write_bytes(data)
    (project / 'timing.tcl').write_bytes(extra['timing.tcl'])
    (project / 'clocks.sdc').write_text(
        '# Internal virtual-pin fit only; no board I/O timing is established.\n'
        f'create_clock -name x87_clock -period {period_ns} [get_ports clk]\n'
        'derive_clock_uncertainty\n'
        'set data_inputs [remove_from_collection [all_inputs] [get_ports clk]]\n'
        'set_input_delay -clock x87_clock -max 0.000 $data_inputs\n'
        'set_input_delay -clock x87_clock -min 0.000 $data_inputs\n'
        'set_output_delay -clock x87_clock -max 0.000 [all_outputs]\n'
        'set_output_delay -clock x87_clock -min 0.000 [all_outputs]\n')
    (project / f'{PROJECT}.qpf').write_text(
        f'QUARTUS_VERSION = "{metadata["series"]}"\nPROJECT_REVISION = "{PROJECT}"\n')
    entity = 'x87_fit_cpu_top' if top == 'cpu' else TOP
    virtual = ports((project / WRAPPER).read_text() if top == 'cpu'
                    else (RTL / 'fes_i386_x87.sv').read_text(), entity)
    if flow == 'netlist':
        # Replace the SystemVerilog with one elaborated netlist file. The
        # virtual-pin list is unchanged: the seal's recipe keeps the top's
        # ports.
        netlist = seal_source(output, sources, entity, cache_root,
                              None if top == 'cpu' else {'X87_BACKEND': BACKENDS[backend]})
        for name in sources:
            (project / name).unlink()
        sources = netlist
        for name, data in sources.items():
            text, _ = retarget_ram_attribute(data.decode()) if M10K_ATTRIBUTE in data.decode() else (data.decode(), 0)
            (project / name).write_bytes(text.encode())
    assignments = (('FAMILY', '"Agilex 5"'), ('DEVICE', b.DEVICE),
                   ('TOP_LEVEL_ENTITY', entity),
                   ('PROJECT_OUTPUT_DIRECTORY', 'output_files'),
                   ('LAST_QUARTUS_VERSION', f'"{metadata["qsf_version"]}"'),
                   ('NUM_PARALLEL_PROCESSORS', '2'), ('SEED', str(seed)),
                   ('SDC_FILE', 'clocks.sdc'))
    qsf = ('# Internal-only virtual-pin characterisation of the x87 envelope plus one\n'
           '# arithmetic backend; never assemble or program.\n')
    qsf += ''.join(f'set_global_assignment -name {k} {v}\n' for k, v in assignments)
    qsf += ''.join(f'set_global_assignment -name {"VERILOG_FILE" if flow == "netlist" else "SYSTEMVERILOG_FILE"} {n}\n'
                   for n in sources)
    if top != 'cpu':
        qsf += f'set_parameter -name X87_BACKEND {BACKENDS[backend]}\n'
    qsf += ''.join(f'set_instance_assignment -name VIRTUAL_PIN ON -to {{{p}}}\n' for p in virtual)
    qsf += 'set_instance_assignment -name IO_STANDARD "1.1-V" -to clk\n'
    (project / f'{PROJECT}.qsf').write_text(qsf)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    inputs = {'fes_base_revision': revision, 'source_classification': 'working-copy-diagnostic',
              'board': 'de25_nano', 'device': b.DEVICE, 'project': PROJECT,
              'top': entity,
              'fit_kind': top, 'x87_present': present if top == 'cpu' else None,
              'flow': 'yosys-seal-netlist' if flow == 'netlist' else 'quartus-pro-systemverilog-direct',
              'x87_backend': backend, 'x87_backend_value': BACKENDS[backend],
              'period_ns': period_ns, 'clock_hz': int(1e9 / period_ns), 'seed': seed,
              'quartus': metadata, 'diagnostic_only': True, 'virtual_data_ports': virtual,
              'primary_clock': 'clk (fitter-assigned location)',
              'evidence_scope': 'internal virtual-pin fit of the x87 unit; no board I/O, '
                                'no seal and no hardware acceptance',
              'source_sha256': {n: b.sha(d) for n, d in {**sources, **extra}.items()},
              'm10k_retargeted_to_m20k': retargeted,
              'project_sha256': {p.name: b.sha(p.read_bytes()) for p in project.iterdir()}}
    (output / 'inputs.json').write_text(json.dumps(inputs, indent=2, sort_keys=True) + '\n')
    return output


def guarded_only_registers(rtl: str, backend: int = 0) -> list[str]:
    """Registers of fes_i386_x87 written ONLY inside sequencer arms guarded by
    `X87_BACKEND == <backend>`, derived from the RTL rather than listed by hand.

    With `backend` 0 these are the DE10's sequenced arithmetic, which a DE25
    build must not hold. With 1 they are the mirror (#1958): what only the
    DE25 backend builds -- the packed decimal arms' registers -- which a DE10
    build must not hold.

    A hand list went stale twice: once when the square root's Q_* arms were
    never guarded, and once when a merge brought in D_CHECK, D_EXACT and
    Q_CHECK unguarded. Deriving the list means a new sequenced arm that is
    left unguarded stops its registers being "guarded-only", and a new guarded
    arm's registers join the list, with nobody having to remember either.

    A write anywhere outside a guarded arm takes a register off the list,
    the reset and F1h branches before `case (seq)` included, so an arm's
    registers stay on it only if the arm (or a guarded start arm) is where
    they are initialised."""
    if backend not in (0, 1):
        raise ValueError(f'X87_BACKEND is 0 or 1, not {backend!r}')
    body = rtl[rtl.index('case (seq)'):]
    arms = list(re.finditer(r'^\s{16}([ADQPBST]_[A-Z0-9_]+):\s*(if \([^\n]*?\))?\s*begin', body, re.M))
    writers: dict[str, list[bool]] = {}
    for k, arm in enumerate(arms):
        end = arms[k + 1].start() if k + 1 < len(arms) else len(body)
        guarded = f'X87_BACKEND == {backend}' in (arm.group(2) or '')
        for reg in set(re.findall(r'^\s+([a-z_0-9]+)\s*<=', body[arm.end():end], re.M)):
            writers.setdefault(reg, []).append(guarded)
    head = rtl[:rtl.index('case (seq)')]
    return sorted(reg for reg, flags in writers.items()
                  if all(flags) and not re.search(rf'^\s+{reg}\s*<=', head, re.M))


# The transcendental engine (#1958) is an entity with memories of its own, not
# sequencer arms, so the register derivation above cannot see it. It is built
# only for the DE25 backend, as fes_x87_core is.
ENGINE_ENTITY = 'fes_x87_trans'
ENGINE_MEMORIES = ('program_rom', 'constant_rom', 'temps')


def engine_presence(entity_rows: list[dict], memories: list[str]) -> dict:
    """What a fit holds of the transcendental engine: the entity rows that name
    it and the memories that are its."""
    return {'entities': sorted({str(value) for row in entity_rows for value in row.values()
                                if ENGINE_ENTITY in str(value)}),
            'memories': sorted(name for name in memories if name in ENGINE_MEMORIES)}


def ram_blocks(text: str) -> list[str]:
    """The memories the fit kept, by name, from the fitter's RAM summary. This
    is how the seed tables' absence is evidenced after commit 4e6e1053b: the
    M10K retarget means a surviving table no longer makes Quartus refuse, so
    "the fit built" stopped being proof. A DE25 fit should list the register
    file's two copies and no seed table; the X87_BACKEND = 0 control should
    list both tables."""
    # Matched on the rows themselves rather than on the section header: the
    # string 'Fitter RAM Summary' also appears in the report's table of
    # contents, and anchoring there picked that one up and matched 270 KB of
    # the wrong report. A row is `; <name>_rtl_N|auto_generated|...`, which
    # only the RAM tables produce.
    names = re.findall(r'^;\s*([A-Za-z_]\w*?)_rtl_\d+\|auto_generated', text, re.M)
    return sorted(set(names))


def lab_resources(text: str) -> dict:
    """The fitter's LAB row. `Total LABs: partially or completely used`, with
    the logic and memory split beneath it."""
    out = {}
    m = re.search(r'^;\s*Total LABs:\s*partially or completely used\s*;\s*([\d,]+)\s*/\s*([\d,]+)',
                  text, re.M)
    if m:
        out['labs'] = {'used': float(m.group(1).replace(',', '')),
                       'available': float(m.group(2).replace(',', ''))}
    for key, name in (('logic_labs', 'Logic LABs'), ('memory_labs', 'Memory LABs')):
        m = re.search(rf'^;\s*--\s*{name}[^;]*;\s*([\d,]+)', text, re.M)
        if m:
            out[key] = {'used': float(m.group(1).replace(',', ''))}
    return out


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

              'clock_hz': inputs['clock_hz'], 'seed': inputs['seed'],
              'evidence_scope': inputs['evidence_scope'], 'hardware_acceptance': 'not-run',
              'programming_artifact': None,
              'tool_sha256': {str(p): b.sha(p.read_bytes()) for p in tools.values()},
              'commands': []}
    deadline = time.monotonic() + b.TIMEOUT_SECONDS
    fitted = project / 'output_files' / f'{PROJECT}.fit.rpt'
    # The same string the SDC holds, from the same stored value.
    period = repr(float(inputs['period_ns']))
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
        # LABs as well as ALMs, because LAB occupancy is what refused the
        # DE10 -- #1889's 4,292 against 4,191 was a LAB count -- and it runs
        # well ahead of ALM occupancy, since ALMs sit sparsely across LABs
        # until the fitter is under pressure. The board builder's resources()
        # has no LAB row; this reads it from the same report.
        record['resources'].update(lab_resources(text))
        record['ram_blocks'] = ram_blocks(text)
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
        # The query matches exact register leaf names, which a flattened and
        # unescaped netlist does not have -- it keeps none of the original
        # names, not even the positive controls. So on the netlist flow the
        # query is NOT APPLICABLE rather than failing, and the exclusion
        # evidence comes from the SystemVerilog-flow fits, which keep names.
        # At x87_present = 0 the 387 is swept and neither check applies.
        skip = (inputs.get('x87_present') == 0
                or inputs.get('flow') == 'yosys-seal-netlist')
        leaves = {line.strip().rsplit('|', 1)[-1]
                  for line in (output / 'registers.txt').read_text().splitlines()
                  if line.strip()}
        # A register may be bit-blasted to name[3] or split by the fitter.
        def held(name: str) -> bool:
            return any(leaf == name or leaf.startswith(name + '[') for leaf in leaves)
        rtl_text = (RTL / 'fes_i386_x87.sv').read_text()
        expected_absent = guarded_only_registers(rtl_text)
        absent = {n: held(n) for n in [*expected_absent, 'rcp_q', 'rsq_q']}
        # The mirror (#1958): what only the DE25 backend builds, the packed
        # decimal arms' registers, which a DE10 fit must not hold.
        de25_only = {n: held(n) for n in guarded_only_registers(rtl_text, 1)}
        present = {n: held(n) for n in ('wm', 'pa', 'cw')}
        record['exclusion'] = {'absent_expected': absent, 'de25_only': de25_only,
                               'positive_controls': present,
                               'registers': len(leaves),
                               'applicable': not skip,
                               'not_applicable_because':
                                   None if not skip else
                                   ('the 387 is swept at x87_present = 0'
                                    if inputs.get('x87_present') == 0 else
                                    'a flattened netlist keeps no original register names')}
        if not skip and not all(present.values()):
            raise SystemExit('the register query found no positive control, so a zero '
                             f'elsewhere means nothing: {present}')
        if inputs['x87_backend'] == 'de25' and not skip:
            leaked = {n: v for n, v in absent.items() if v}
            if leaked:
                raise SystemExit('the DE25 fit still contains the sequenced arithmetic: '
                                 f'{sorted(leaked)}')
            # And gate the RAM list, which the register test cannot cover:
            # `rcp_q` and `rsq_q` are synchronous-read registers that Quartus
            # may ABSORB into the RAM block, so they can read as absent while
            # the seed table itself survives. Parsing the names and only
            # recording them let a build pass while holding arithmetic the
            # exclusion result claimed was gone. Codex found this on PR #1907.
            tables = [n for n in record.get('ram_blocks', []) if n in ('rcp_tab', 'rsq_tab')]
            if tables:
                raise SystemExit('the DE25 fit still contains the sequenced arithmetic\'s '
                                 f'seed tables: {tables}')
        engine = engine_presence(record.get('entities', []), record.get('ram_blocks', []))
        record['exclusion']['transcendental_engine'] = engine
        if inputs['x87_backend'] == 'de10' and not skip and (engine['entities'] or engine['memories']):
            raise SystemExit(f'the DE10 fit contains the transcendental engine: {engine}')
        if inputs['x87_backend'] == 'de25' and not skip and inputs.get('flow') != 'yosys-seal-netlist' \
                and not engine['entities']:
            raise SystemExit('the DE25 fit holds no transcendental engine, so its absence elsewhere means nothing')
        if inputs['x87_backend'] == 'de10' and not skip:
            leaked = {n: v for n, v in de25_only.items() if v}
            if leaked:
                raise SystemExit('the DE10 fit contains registers only the DE25 backend builds: '
                                 f'{sorted(leaked)}')
    except BaseException as error:
        record.update(status='fail', error=str(error))
        if fitted.is_file():
            try:
                text = fitted.read_text(errors='replace')
                record['resources'] = b.resources(text)
                record['resources'].update(lab_resources(text))
                record['ram_blocks'] = ram_blocks(text)
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
    parser.add_argument('--flow', choices=('sv', 'netlist'), default='sv',
                        help="'sv' hands Quartus Pro the SystemVerilog; 'netlist' fits the "
                             "seal's own Yosys netlist, which is the front end the sibling "
                             'Agilex 5 measurement uses, so the figures are comparable')
    parser.add_argument('--present', type=int, choices=(0, 1), default=1,
                        help='with --top cpu: what x87_present is tied to')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--output', type=Path, help='new output directory; existing ones are refused')
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    parser.add_argument('--cache-root', type=Path,
                        default=os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or None,
                        help="the shared toolchain cache, for --flow netlist's locked Yosys")
    args = parser.parse_args(argv)
    b = board()
    output = prepare(args.output, args.backend, args.period_ns, args.seed,
                     args.top, args.present, args.flow, args.cache_root)
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
