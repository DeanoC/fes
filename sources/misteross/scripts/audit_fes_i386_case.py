#!/usr/bin/env python3
"""Observe i386 Yosys parallel_case assertions and their settled selectors.

This diagnostic adds selector outputs to a private generated netlist and a
nonfatal handler only to a private Verilator model. It never changes the
normal netlist simulation waiver. Reports cover the listed exercised modes,
not all reachable states or unobserved sites. Use the i386 lock's authenticated
OFF slot and run with an external time cap. Example:

  timeout 1800s python3 scripts/audit_fes_i386_case.py --cache-root /abs/cache \\
      --output /abs/new-output --jobs 4 --download

The output directory must not exist. Bochs is downloaded only with --download.
"""
from __future__ import annotations

import argparse
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import fes_i386_oracle as oracle
from scripts import i386_verilator
from scripts import test_fes_i386_frontend as frontend
from scripts import test_fes_i386_lockstep as lock
from scripts import test_fes_i386_vectors as vectors

CASE = 'casez (s) // synopsys parallel_case'
FUNCTION = re.compile(r'  function (?:\[[^\]]+\] )?(\w+);\n(.*?)  endfunction\n  assign (.*?) = \1\((.*?)\);', re.S)
MESSAGE = re.compile(r'VL_WRITEF_NX\("(?:\\.|[^"\\])*parallel_case(?:\\.|[^"\\])*"\s*,.*?\);', re.S)
STOP = re.compile(r'VL_STOP_MT\("[^\"]*fes_i386_cpu.netlist.v", (\d+),')


class AuditError(RuntimeError):
    pass


def last_argument(args: str) -> str:
    depth = {'(': 0, '{': 0, '[': 0}
    close = {')': '(', '}': '{', ']': '['}
    last = 0
    for i, char in enumerate(args):
        if char in depth:
            depth[char] += 1
        elif char in close:
            depth[close[char]] -= 1
            if depth[close[char]] < 0:
                raise AuditError('unbalanced function arguments')
        elif char == ',' and all(v == 0 for v in depth.values()):
            last = i + 1
    if any(depth.values()) or last == 0:
        raise AuditError('cannot identify pmux selector argument')
    return args[last:].strip()


def instrument_netlist(text: str, force_site: int | None = None) -> tuple[str, list[dict]]:
    """Expose the onehot-or-zero predicate of every generated pmux input."""
    if re.search(r'\b(?:audit_bad|__audit_sel_\d+)\b', text):
        raise AuditError('netlist already uses a reserved audit signal name')
    sites = []
    selector_ranges = []
    for match in FUNCTION.finditer(text):
        body = match.group(2)
        if CASE not in body:
            continue
        width = re.search(r'input \[(\d+):0\] s;', body)
        if width is None:
            raise AuditError(f'{match.group(1)} has no selector width')
        n = int(width.group(1)) + 1
        patterns = re.findall(r"(\d+)'b([01?]+):", body)
        if (len(patterns) != n or 'default:' not in body
                or any(int(w) != n or len(p) != n or p.count('1') != 1 or '0' in p for w, p in patterns)
                or len({p.index('1') for _, p in patterns}) != n):
            raise AuditError(f'{match.group(1)} is not a defaulted one-hot pmux')
        selector = last_argument(match.group(4))
        arg_start = match.start(4) + match.group(4).rfind(selector)
        selector_ranges.append((arg_start, arg_start + len(selector)))
        sites.append({'function': match.group(1), 'width': n, 'selector': selector,
                      'destination': match.group(3).strip()})
    if len(sites) != text.count(CASE) or len(sites) <= 64:
        raise AuditError(f'parsed {len(sites)} of {text.count(CASE)} generated pmux cases')
    if force_site is not None:
        if not 0 <= force_site < len(sites) or sites[force_site]['width'] < 2:
            raise AuditError('negative control site must name a pmux with at least two selectors')
        forced = f"{sites[force_site]['width']}'d3"
        begin, end = selector_ranges[force_site]
        text = text[:begin] + forced + text[end:]
        sites[force_site]['selector'] = forced
    start = text.find('module fes_i386_cpu(')
    end_header = text.find(');', start)
    if start < 0 or end_header < 0 or text.count('endmodule') != 1:
        raise AuditError('unexpected CPU module form')
    text = (text[:end_header] + ', audit_bad' + text[end_header:end_header + 2]
            + f'\n  output [{len(sites)-1}:0] audit_bad;' + text[end_header + 2:])
    endmodule = text.rfind('endmodule')
    block = '\n'
    for i, site in enumerate(sites):
        block += (f"  wire [{site['width']-1}:0] __audit_sel_{i};\n"
                  f"  assign __audit_sel_{i} = {site['selector']};\n"
                  f"  assign audit_bad[{i}] = |(__audit_sel_{i} & (__audit_sel_{i} - 1'b1));\n")
    text = text[:endmodule] + block + text[endmodule:]
    lines = [n for n, line in enumerate(text.splitlines(), 1) if CASE in line]
    if len(lines) != len(sites):
        raise AuditError('case count changed while exposing selectors')
    for site, line in zip(sites, lines):
        site['line'] = line
    return text, sites


def instrument_bench(text: str) -> str:
    include = '#include "Vfes_i386_cpu.h"'
    if text.count(include) != 1:
        raise AuditError('CPU testbench include changed')
    text = text.replace(include, include + '\n#include "case_audit.h"')
    text, count = re.subn(r'\b((?:rtl\.)?top)\.eval\(\);',
                          r'case_audit_before(); \1.eval(); case_audit_after(\1);', text)
    if count != 5:
        raise AuditError(f'expected five CPU eval calls, found {count}; audit all calls first')
    return text


def patch_displays(directory: Path, sites: list[dict]) -> int:
    """Suppress repeated text only; compiled assertions still call vl_stop."""
    stopped, replaced = set(), 0
    for path in directory.glob('*.cpp'):
        text = path.read_text()
        stopped.update(map(int, STOP.findall(text)))
        expected = text.count('parallel_case, but')
        if not expected:
            continue
        text, count = MESSAGE.subn('/* audit counts this assertion in vl_stop */', text)
        if count != expected:
            raise AuditError(f'{path}: suppressed {count} of {expected} assertion messages')
        path.write_text(text)
        replaced += count
    expected_lines = {site['line'] for site in sites}
    if stopped != expected_lines or replaced < len(sites):
        raise AuditError(f'generated assertions differ from {len(sites)} mapped pmux sites')
    return replaced


def write_hook(out: Path, sites: list[dict]) -> None:
    (out / 'case_audit.h').write_text('#pragma once\n#include "Vfes_i386_cpu.h"\n'
                                    'void case_audit_before();\nvoid case_audit_after(const Vfes_i386_cpu&);\n')
    template = (lock.SIM / 'case_audit.cpp.in').read_text()
    (out / 'case_audit.cpp').write_text(template.replace('@CASE_COUNT@', str(len(sites))).replace(
        '@CASE_LINES@', ', '.join(str(site['line']) for site in sites)))


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def formal_check(yosys: Path, source: str, out: Path, sites: list[dict]) -> dict:
    """Prove the selectors with each pmux, FF and memory output cut/free.

    No pmux semantics or reset/reachability assumptions may constrain a
    selector. The satisfiability and injected-violation controls make a
    vacuous proof or silently tied-off ignored-cell output a failure.
    """
    forced, _ = instrument_netlist(source, 0)
    (out / 'formal-control.v').write_text(forced)
    (out / 'formal-free.v').write_text('''module sat_state(input clk, input d, output reg q);
always @(posedge clk) q <= d;
endmodule
module sat_memory(input clk, input we, input [1:0] addr, input din, output q);
reg [0:0] ram [0:3];
always @(posedge clk) if (we) ram[addr] <= din;
assign q = ram[addr];
endmodule
''')
    base = 'hierarchy -top fes_i386_cpu; proc -norom -noopt; flatten; cutpoint t:$pmux; opt; '
    tests = [
        ('proof', out / 'fes_i386_cpu.netlist.v', base + 'sat -ignore_unknown_cells -prove audit_bad 0 -show audit_bad',
         'SAT proof finished - no model found: SUCCESS!'),
        ('satisfiable', out / 'fes_i386_cpu.netlist.v', base + 'sat -ignore_unknown_cells -show audit_bad',
         'SAT solving finished - model found:'),
        ('forced', out / 'formal-control.v', base + 'sat -ignore_unknown_cells -prove audit_bad[0] 0 -show audit_bad[0]',
         'SAT proof finished - model found: FAIL!'),
        ('free_ff', out / 'formal-free.v',
         'hierarchy -top sat_state; proc -norom -noopt; flatten; opt; sat -ignore_unknown_cells -prove q 0 -show q',
         'SAT proof finished - model found: FAIL!'),
        ('free_memory', out / 'formal-free.v',
         'hierarchy -top sat_memory; proc -norom -noopt; flatten; opt; sat -ignore_unknown_cells -prove q 0 -show q',
         'SAT proof finished - model found: FAIL!'),
    ]
    ignored = {}
    for name, path, steps, expected in tests:
        print(f'audit: SAT {name}', flush=True)
        log = out / f'formal-{name}.log'
        with log.open('w') as stream:
            subprocess.run([str(yosys), '-T', '-p', f'read_verilog {path}; {steps}'],
                           stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=300)
        text = log.read_text()
        if expected not in text:
            raise AuditError(f'SAT {name} did not produce its required result; see {log}')
        if name in ('proof', 'satisfiable') and text.count('making all cell outputs cutpoints.') != len(sites):
            raise AuditError(f'SAT {name} did not cut every generated pmux; see {log}')
        if name == 'free_ff' and 'No SAT model available for async FF cell' not in text:
            raise AuditError('SAT FF control did not exercise an ignored register')
        if name == 'free_memory' and '($memrd)' not in text:
            raise AuditError('SAT memory control did not exercise an ignored memory read')
        if name == 'proof':
            for line in text.splitlines():
                if 'No SAT model' in line:
                    match = re.search(r'\((\$[^)]+)\)', line)
                    if not match:
                        raise AuditError(f'unrecognized ignored SAT cell: {line}')
                    kind = match.group(1)
                    ignored[kind] = ignored.get(kind, 0) + 1
    allowed = {'$meminit_v2', '$memwr_v2', '$memrd', '$dff', '$dffe', '$sdff', '$sdffe', '$sdffce'}
    if set(ignored) - allowed or not {'$memrd', '$dff'} <= set(ignored):
        raise AuditError(f'unexpected ignored cell types: {ignored}')
    return {'proof': 'all selectors onehot-or-zero under cut-pmux/FF/memory overapproximation',
            'satisfiable': True, 'forced_control': 0, 'ff_and_memory_controls': True,
            'cut_pmux': len(sites), 'ignored_cells': ignored}


def collect(directory: Path, sites: list[dict]) -> tuple[int, list[dict]]:
    totals = defaultdict(lambda: [0, 0, 0])
    files = sorted(directory.glob('case-*.json'))
    if not files:
        raise AuditError('no diagnostic process reports')
    evals = 0
    for path in files:
        report = json.loads(path.read_text())
        evals += report['evals']
        for row in report['sites']:
            i = row['index']
            if i < 0 or i >= len(sites) or row['line'] != sites[i]['line']:
                raise AuditError(f'{path}: unknown assertion site')
            totals[i][0] += row['events']
            totals[i][1] += row['transient_evals']
            totals[i][2] += row['settled_evals']
    rows = []
    for i, counts in sorted(totals.items()):
        rows.append({'index': i, **sites[i], 'events': counts[0],
                     'transient_evals': counts[1], 'settled_evals': counts[2]})
    return evals, rows


def corpus_paths(corpus: Path) -> list[Path]:
    receipt = corpus / 'fes-i386-corpus.json'
    if not corpus.is_absolute() or not receipt.is_file():
        raise AuditError('--corpus must name an absolute pinned-receipt corpus directory')
    paths = sorted(path for path in corpus.glob('*.MOO.gz') if lock.in_scope(path.name[:-7]))
    if not paths:
        raise AuditError('corpus has no in-scope opcode files')
    receipt_value = json.loads(receipt.read_text())
    if receipt_value.get('revision') != vectors.REVISION or not isinstance(receipt_value.get('files'), dict):
        raise AuditError('corpus receipt does not name the pinned revision and files')
    expected = {name for name in receipt_value['files']
                if name.endswith('.MOO.gz') and lock.in_scope(name[:-7])}
    actual = {path.name for path in paths}
    if expected != actual:
        raise AuditError(f'corpus differs from receipt: missing={sorted(expected-actual)}, '
                         f'extra={sorted(actual-expected)}')
    vectors.verify(corpus, paths + [corpus / 'revocation_list.txt'])
    return paths


def test_environment(base: dict[str, str], counts: Path, table: Path) -> dict[str, str]:
    env = {key: value for key, value in base.items() if not key.startswith('FES_I386_')}
    env.update(FES_CASE_AUDIT_DIR=str(counts), FES_I386_DIVERGENCES=str(table),
               FES_I386_LATENCY='1', FES_I386_PRESET='0')
    return env


def run(args: argparse.Namespace) -> int:
    if not args.output.is_absolute() or args.output.exists():
        raise AuditError('--output must be an absolute path that does not exist')
    out = args.output
    out.mkdir(parents=True)
    counts = out / 'counts'
    counts.mkdir()
    verilator = i386_verilator.locate(args.cache_root)
    yosys = args.cache_root / 'slots' / verilator.slot / 'install/bin/yosys'
    if not yosys.is_file():
        raise AuditError('authenticated i386 slot lacks Yosys')
    inputs = oracle.link_inputs(args.jobs, args.download)
    netlist = frontend.synthesize_netlist(str(yosys), [lock.RTL / p for p in lock.CPU_RTL], lock.TOP, out)
    source_hash = sha(netlist)
    raw_netlist = netlist.read_text()
    updated, sites = instrument_netlist(raw_netlist, args.negative_control)
    netlist.write_text(updated)
    (out / 'sites.json').write_text(json.dumps(sites, indent=2) + '\n')
    formal = None if args.negative_control is not None else formal_check(yosys, raw_netlist, out, sites)
    (out / 'lockstep_tb.cpp').write_text(instrument_bench((lock.SIM / 'lockstep_tb.cpp').read_text()))
    write_hook(out, sites)
    obj = out / 'obj'
    flags = [flag for flag in frontend.NETLIST_WARNINGS if flag != '--no-assert-case']
    command = [str(verilator.path), '--cc', '--exe', '--assert', '-j', str(args.jobs), '-O3', '--top-module', lock.TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', *flags, '--Mdir', str(obj),
               '-CFLAGS', f'-std=c++17 -O2 -DVL_USER_STOP -I{lock.SIM} -I{lock.DDR_MODEL} -I{out}',
               '-LDFLAGS', '-pthread', str(netlist), str(out / 'lockstep_tb.cpp'),
               str(out / 'case_audit.cpp'), *map(str, inputs)]
    with (out / 'build.log').open('w') as stream:
        subprocess.run(command, cwd=lock.ROOT, env=verilator.environment(), stdout=stream,
                       stderr=subprocess.STDOUT, check=True, timeout=600)
        suppressed = patch_displays(obj, sites)
        subprocess.run(['make', '-C', str(obj), '-f', 'Vfes_i386_cpu.mk', '-j', str(args.jobs)],
                       cwd=lock.ROOT, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=1200)
    table = out / 'divergences.txt'
    lock.divergence_table(vectors.load_divergences(), table)
    env = test_environment(os.environ, counts, table)
    bench = str(obj / 'Vfes_i386_cpu')
    modes = [('reset_store', lock.run_reset_store), ('directed', lock.run_directed),
             ('unreal', lock.run_unreal), ('pmode', lock.run_pmode), ('paging', lock.run_paging),
             ('no387', lock.run_no387), ('memory', lock.run_memory)]
    if args.negative_control is not None:
        modes = modes[:1]
    results = {}
    for name, fn in modes:
        print(f'audit: {name}', flush=True)
        result = fn(bench, env)
        results[name] = result
        (out / 'partial-results.json').write_text(json.dumps(results, indent=2) + '\n')
        if result['failures'] or result['summary']['failed']:
            raise AuditError(f'{name} lockstep failed: {result["failures"][:3]}')
    runs = [('random', 1, True, {}), ('random_no_latency', 1, False, {}),
            ('protected', 0x1000, True, {'pm_weight': 25}),
            ('task', 0x2000, True, {'pm_weight': 25, 'tasks': True}),
            ('debug', 0x3000, True, {'debug': True}),
            ('undefined', 0x4000, True, {'pm_weight': 25, 'undefined': True})]
    for name, seed, latency, extra in ([] if args.negative_control is not None else runs):
        print(f'audit: {name}', flush=True)
        result = lock.run_random(bench, env, seed, args.random_programs, latency, **extra)
        results[name] = result
        (out / 'partial-results.json').write_text(json.dumps(results, indent=2) + '\n')
        if result['failures'] or result['summary']['failed']:
            raise AuditError(f'{name} lockstep failed: {result["failures"][:3]}')
    if args.negative_control is None and args.unbuilt:
        print('audit: exhaustive unbuilt encodings', flush=True)
        encodings = lock.unbuilt_encodings()
        chunks = [encodings[n::args.jobs] for n in range(args.jobs)]
        with ThreadPoolExecutor(max_workers=args.jobs) as executor:
            unbuilt = list(executor.map(lambda chunk: lock.run_unbuilt(bench, env, chunk), chunks))
        if any(result['failures'] or result['summary']['failed'] for result in unbuilt):
            raise AuditError('unbuilt-encoding lockstep failed')
        results['unbuilt'] = {'encodings': len(encodings), 'chunks': unbuilt}
        (out / 'partial-results.json').write_text(json.dumps(results, indent=2) + '\n')
    if args.negative_control is None and args.corpus is not None:
        corpus = args.corpus
        paths = corpus_paths(corpus)
        revoked = frozenset(vectors.revoked_hashes((corpus / 'revocation_list.txt').read_text()))
        entries = vectors.load_divergences()
        print(f'audit: corpus {len(paths)} files', flush=True)
        def run_file(path: Path) -> dict:
            return lock.run_file(str(path), bench, dict(env, FES_I386_MODE='corpus'),
                                 revoked, entries, args.limit_per_file)
        files = []
        with ThreadPoolExecutor(max_workers=args.jobs) as executor:
            for result in executor.map(run_file, paths):
                files.append(result)
                if len(files) % 25 == 0 or len(files) == len(paths):
                    print(f'audit: corpus {len(files)}/{len(paths)} files', flush=True)
                    results['corpus'] = {'revision': vectors.REVISION, 'files': len(files),
                                         'limit_per_file': args.limit_per_file, 'results': files}
                    (out / 'partial-results.json').write_text(json.dumps(results, indent=2) + '\n')
        if any(result['failures'] or result['summary']['failed'] for result in files):
            raise AuditError('corpus lockstep failed')
        results['corpus'] = {'revision': vectors.REVISION, 'files': len(files),
                             'limit_per_file': args.limit_per_file, 'results': files}
    evals, observed = collect(counts, sites)
    if not observed:
        raise AuditError('no assertions observed; positive control of the assertion path required')
    source_head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=lock.ROOT, text=True).strip()
    source_status = subprocess.check_output(['git', 'status', '--porcelain', '--',
                                             'sources/misteross'], cwd=lock.ROOT.parent.parent,
                                            text=True).splitlines()
    input_paths = [*(lock.RTL / name for name in lock.CPU_RTL), lock.SIM / 'lockstep_tb.cpp',
                   lock.SIM / 'case_audit.cpp.in', Path(__file__).resolve()]
    report = {'source_head': source_head, 'source_status': source_status,
              'source_sha256': {str(path.relative_to(lock.ROOT)): sha(path) for path in input_paths},
              'verilator': verilator.record(), 'yosys_sha256': sha(yosys),
              'netlist_sha256': source_hash, 'instrumented_netlist_sha256': sha(netlist),
              'case_sites': len(sites), 'evals': evals, 'observed_sites': observed,
              'unobserved_sites': [i for i in range(len(sites)) if i not in {x['index'] for x in observed}],
              'suppressed_display_calls': suppressed, 'random_programs_per_mode': args.random_programs,
              'negative_control': args.negative_control, 'formal': formal, 'results': results}
    (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    stable = sum(site['settled_evals'] for site in observed)
    print(f'audit: {len(observed)}/{len(sites)} sites fired; {stable} sampled evals remained multi-hot')
    if args.negative_control is not None:
        return 0 if any(site['index'] == args.negative_control and site['settled_evals'] for site in observed) else 1
    return 1 if stable else 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    i386_verilator.add_argument(parser)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--random-programs', type=int, default=100)
    parser.add_argument('--unbuilt', action='store_true', help='run all unbuilt-encoding lockstep cases')
    parser.add_argument('--corpus', type=Path, help='absolute pinned-receipt MOO corpus directory')
    parser.add_argument('--limit-per-file', type=int, help='optional per-opcode corpus bound')
    parser.add_argument('--negative-control', type=int,
                        help='diagnostic only: force this selector multi-hot and run reset-store')
    parser.add_argument('--download', action='store_true')
    args = parser.parse_args()
    if args.cache_root is None or not Path(args.cache_root).is_absolute():
        parser.error('--cache-root must be an absolute path')
    args.cache_root = Path(args.cache_root)
    if not 1 <= args.jobs <= 64 or args.random_programs < 1 or (args.limit_per_file is not None and args.limit_per_file < 1):
        parser.error('jobs must be 1..64; random-programs and limit-per-file must be positive')
    try:
        return run(args)
    except (AuditError, OSError, ValueError, subprocess.SubprocessError, frontend.FrontendError, vectors.CorpusError,
            i386_verilator.VerilatorError, oracle.OracleError, lock.LockstepError) as error:
        print(f'i386 case audit: {error}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
