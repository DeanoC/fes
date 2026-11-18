#!/usr/bin/env python3
"""#1984: classify every file in the registered pc386 product's build record.

The record's file set is computed exactly as build_pc386_product.create_build_record
computes it (its PINNED_INPUTS, the legacy sealer's import closure, and three
named tool files). For each file this reports how it reached the record and
what the seal does with it, each column measured rather than asserted:

  yosys_read      the seal's Yosys opened it (yosys-reads.json, an strace of the
                  machine program; see trace_yosys_reads.py)
  quartus_read    the generated top.qsf names it (VERILOG_FILE, SYSTEMVERILOG_FILE,
                  SDC_FILE), or a Quartus-read shell source `includes it through
                  the QSF's SEARCH_PATH. Declared by the QSF, not traced: no
                  Quartus run was made for this record.
  inlined_in_qsf  Python reads it and copies its text into top.qsf
  generator_code  Python code in it ran while the producer generated the bytes
                  the compilers read: the Yosys machine program, top.qsf, the
                  guest-rate parameters and the ROM hex (sys.settrace)
  seal_process    it is a Python module loaded in the seal process (sys.modules
                  after the producer is imported), so its code runs there
  hashed_by_seal  the seal reads it only to hash it for the lockstep check
                  (build_fes_pc386_quartus.oracle_identity)

Usage, from the repository root (needs build/firmware/pc386/*.bin, which
trace_yosys_reads.py or scripts/build_pc386_firmware.py writes):

  python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/classify.py sources/misteross
"""
import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
root = Path(sys.argv[1]).absolute()
sys.path[:0] = [str(root), str(root / 'cores/fes-pc386/tools'), str(HERE)]

import build_pc386_product as product  # noqa: E402
from scripts import build_fes_pc386_quartus as legacy, source_closure  # noqa: E402
from scripts.input_identity import input_role  # noqa: E402
import proposal  # noqa: E402

EXPLICIT = {"scripts/source_closure.py", "scripts/bootstrap.sh", "scripts/toolchain_cache.py"}
imports = source_closure.imports("build_fes_pc386_quartus", root)
paths = sorted(set(product.PINNED_INPUTS) | imports | EXPLICIT)

# Calls that produce compiler input, traced for the files whose code runs.
generator = set()


def tracer(frame, event, arg):
    filename = frame.f_code.co_filename
    try:
        relative = Path(filename).resolve().relative_to(root).as_posix()
    except ValueError:
        return None
    generator.add(relative)
    return None


images = {n: root / 'build/firmware/pc386' / product.firmware.IMAGES[n]['file'] for n in product.firmware.IMAGES}
sys.settrace(tracer)
program = legacy.machine_program(Path(legacy.MACHINE_NETLIST))
qsf = product.project_qsf(root, root / product.OUTPUT / 'project', '0' * 32, 1,
                          threads=product.selected_recipe(root)['quartus']['threads'])
rates = legacy.guest_rates()
rom = product.firmware.rom_hex(images)
sys.settrace(None)

seal_process = set()
for module in list(sys.modules.values()):
    filename = getattr(module, '__file__', None)
    if filename:
        try:
            seal_process.add(Path(filename).resolve().relative_to(root).as_posix())
        except ValueError:
            pass

reads = json.loads((HERE / 'yosys-reads.json').read_text())
yosys_read = set(reads['opened_under_root'])

project = root / product.OUTPUT / 'project'
quartus_read = set()
for match in re.finditer(r'-name (?:VERILOG_FILE|SYSTEMVERILOG_FILE|SDC_FILE) "?([^"\n]+)"?', qsf):
    target = (project / match.group(1)).resolve()
    # pc386_system.v is the Yosys netlist the producer writes into the project.
    if target.is_relative_to(root) and not target.relative_to(root).as_posix().startswith('build/'):
        quartus_read.add(target.relative_to(root).as_posix())
search = [(project / m.group(1)).resolve() for m in re.finditer(r'-name SEARCH_PATH "([^"]+)"', qsf)]
for source in list(quartus_read):
    for include in re.findall(r'`include "([^"]+)"', (root / source).read_text(errors='replace')):
        for directory in search:
            if (directory / include).is_file():
                quartus_read.add((directory / include).relative_to(root).as_posix())
inlined = {legacy.QSF}

hashed = {'scripts/fes_i386_oracle.py', legacy.i386_oracle.BOCHS_PATCH_RELATIVE,
          *(f'cores/fes-common/sim/i386/{n}' for n in legacy.i386_oracle.ORACLE_SOURCES),
          *legacy.i386_oracle.SUITE_SOURCES}


def reason(path):
    why = []
    if path in legacy.PINNED_INPUTS:
        why.append('legacy PINNED_INPUTS')
    elif path in product.PINNED_INPUTS:
        why.append('product PINNED_INPUTS')
    if path in imports:
        why.append('import closure')
    if path in EXPLICIT:
        why.append('named tool file')
    return ', '.join(why)


def kind(row):
    if row['yosys_read'] or row['quartus_read'] or row['inlined_in_qsf']:
        return 'supplies compiler bytes'
    if row['v1_role'] == 'toolchain':
        return 'configures or authenticates tools'
    if row['generator_code']:
        return 'generates compiler bytes'
    if row['path'] in proposal.FIRMWARE_GENERATOR:
        return 'generates compiler bytes (pinned output)'
    if row['seal_process']:
        return 'runs in the seal process (packages, records or checks)'
    if row['v1_role'] == 'packaging':
        return 'reads the finished seal (packaging)'
    if row['hashed_by_seal']:
        return 'validates: lockstep oracle or suite, hashed by the seal'
    if row['path'] in proposal.RUNNERS:
        return 'validates: runs the lockstep, then the seal, as subprocesses'
    if row['path'] == 'scripts/run_logged.sh':
        return 'wraps the Quartus command (logging)'
    if row['path'] in imports:
        return 'static import closure only; not loaded by the seal'
    return 'unclassified'


rows = []
for path in paths:
    row = {'path': path, 'in_record_because': reason(path), 'v1_role': input_role(path),
           'yosys_read': path in yosys_read, 'quartus_read': path in quartus_read,
           'inlined_in_qsf': path in inlined, 'generator_code': path in generator,
           'seal_process': path in seal_process, 'hashed_by_seal': path in hashed,
           'proposed_role': proposal.role_v2(path, input_role)}
    row['kind'] = kind(row)
    rows.append(row)

missing = sorted((yosys_read | quartus_read) - set(paths) - {'build/firmware/pc386/pc386-rom.hex'})
summary = {
    'revision': reads['revision'], 'record_files': len(rows),
    'by_kind': {k: sum(r['kind'] == k for r in rows) for k in sorted({r['kind'] for r in rows})},
    'by_v1_role': {k: sum(r['v1_role'] == k for r in rows) for k in ('design', 'toolchain', 'packaging')},
    'compiler_reads_outside_record': missing,
    'yosys_reads_not_under_source_control': sorted(p for p in yosys_read if p.startswith('build/')),
    'evidence_candidates_in_seal_process': sorted(r['path'] for r in rows
                                                  if r['proposed_role'] in ('evidence', 'firmware-generator')
                                                  and r['seal_process']),
    'evidence_candidates_read_by_a_compiler': sorted(r['path'] for r in rows if r['proposed_role'] == 'evidence'
                                                     and (r['yosys_read'] or r['quartus_read'])),
}
(HERE / 'classification.json').write_text(json.dumps({'summary': summary, 'files': rows}, indent=1) + '\n')

mark = {True: 'yes', False: ''}
lines = ['# pc386 build-record classification (generated by classify.py; do not edit)', '',
         f"Revision `{reads['revision']}`: {len(rows)} files.", '',
         '| File | In the record through | v1 role | Yosys reads | Quartus reads (QSF) | Inlined in QSF '
         '| Generator code | Runs in seal process | Hashed for lockstep | Kind | Proposed v2 role |',
         '| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |']
for r in rows:
    lines.append(f"| `{r['path']}` | {r['in_record_because']} | {r['v1_role']} | {mark[r['yosys_read']]} | "
                 f"{mark[r['quartus_read']]} | {mark[r['inlined_in_qsf']]} | {mark[r['generator_code']]} | "
                 f"{mark[r['seal_process']]} | {mark[r['hashed_by_seal']]} | {r['kind']} | {r['proposed_role']} |")
lines += ['', '```json', json.dumps(summary, indent=1), '```', '']
(HERE / 'classification.md').write_text('\n'.join(lines))
print(json.dumps(summary, indent=1))
