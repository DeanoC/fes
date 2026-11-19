#!/usr/bin/env python3
"""Elaborate one arm of the i486 fit spike once, and fit that netlist per seed.

The machine netlist's bytes depend on every path Yosys is given
(DeanoC/yosys#80), so each arm is elaborated once at the fixed workspace
fit_i386_device.py uses, and the same file is fitted at every seed on both
boards (method.md, "One netlist per arm").

  elaborate  ROOT is a clean checkout at the arm's commit. Writes
             <out>/pc386_system.v and <out>/elaboration.json (arm commit, the
             paths changed from the base, the netlist digest, the Yosys slot).
             --hierarchy drops flatten, as the area audit's per-module fits.
  de10       Fits a netlist in the pc386 sealer's own project (write_project:
             board shell, pins, SDC, settings), BUILD_ID all zeros, the given
             seed, under the Powerboat Quartus Lite builder lock. Writes
             <out>/summary.json and keeps the summary reports.

The DE25 side is scripts/fit_i386_device.py --netlist on chip-30x0-ai-dev.
Run with ROOT's scripts importable:

  python3 fit_arm.py elaborate --root <checkout>/sources/misteross --arm a \\
      --base <sha> --cache-root <abs> --out <abs new dir> [--hierarchy]
  python3 fit_arm.py de10 --root <checkout>/sources/misteross --netlist <abs> \\
      --seed 1 --cache-root <abs> --out <abs new dir>
"""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

ALLOWED = ('sources/misteross/cores/fes-common/rtl/i386/', 'sources/misteross/cores/fes-pc386/rtl/pc386_system.sv')
LITE_LOCK = Path('/tmp/lander-quartus-lite.lock')
ENTITY_DEPTH = 8
# The registers that make arm c a two-identity fit (method.md, "The three arms"): the
# identity latch and the 486-only state. The fitted design's register names under these
# globs are listed, and the exact names counted in Python (TimeQuest globs treat [ ] as a
# class, so a bit select cannot be matched by a glob reliably).
RETAIN_GLOBS = ('*cpu_decode_i486*', '*cpu_cr0_*', '*cpu_tr3*', '*cpu_eflags*', '*cpu_ex_flags*')
RETAINED = {'latch': 'cpu_decode_i486', 'cr0_ne': 'cpu_cr0_ne', 'cr0_wp': 'cpu_cr0_wp', 'cr0_am': 'cpu_cr0_am',
            'cr0_nw': 'cpu_cr0_nw', 'cr0_cd': 'cpu_cr0_cd', 'tr3': 'cpu_tr3[', 'eflags_ac': 'cpu_ex_flags_in[18]',
            'eflags_id': 'cpu_ex_flags_in[21]'}
# EX's EFLAGS copy (Yosys names it after EX's flags_in port) is where AC and ID are counted:
# the committed copy does not keep its own name through Quartus's synthesis.
RETAIN_TCL = '''project_open {top} -revision {top}
create_timing_netlist
set out [open "{out}" w]
foreach pattern {{{patterns}}} {{
    foreach_in_collection r [get_registers -nowarn $pattern] {{ puts $out [get_node_info -name $r] }}
}}
close $out
delete_timing_netlist
project_close
'''


def retention_tcl(top: str, out: str) -> str:
    return RETAIN_TCL.format(top=top, out=out, patterns=' '.join('{' + p + '}' for p in RETAIN_GLOBS))


def read_retention(text: str) -> dict:
    names = sorted(set(text.split()))
    counts = {key: sum(1 for n in names if (sub in n if sub.endswith('[') else
                                             any(n.endswith(sub + tail) or (sub + tail) in n for tail in ('', '~', '|'))))
              for key, sub in RETAINED.items()}
    return {'counts': counts, 'names': names}


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(root: Path, *args: str) -> str:
    return subprocess.run(['git', *args], cwd=root, check=True, capture_output=True, text=True).stdout.strip()


def load(root: Path):
    sys.path.insert(0, str(root))
    from scripts import build_fes_pc386_quartus as sealer
    from scripts import fit_i386_device as device
    from scripts import quartus_x87_spike as spike
    from scripts import yosys_netlist
    return sealer, device, spike, yosys_netlist


def elaborate(args) -> int:
    sealer, device, spike, yosys_netlist = load(args.root)
    if git(args.root, 'status', '--porcelain', '--untracked-files=no'):
        raise SystemExit('the checkout has uncommitted changes to tracked files')
    commit = git(args.root, 'rev-parse', 'HEAD')
    changed = git(args.root, 'diff', '--name-only', args.base, commit).splitlines()
    stray = [path for path in changed if not path.startswith(ALLOWED)]
    if stray:
        raise SystemExit(f'the arm changes files outside the RTL: {stray}')
    rom = args.root / sealer.ROM_HEX
    if not rom.is_file():
        raise SystemExit(f'build the pinned firmware first: {rom}')
    args.out.mkdir(parents=True)
    yosys = yosys_netlist.authenticate(args.root, cache_root=args.cache_root, gpu_router='HIP', lock=sealer.LOCK)
    with device.elaboration_lease() as stable:
        program = spike.flatten(sealer.machine_program(stable), args.hierarchy)
        yosys_netlist.run(yosys.path, program, cwd=args.root, program_file=stable.with_name('machine.ys'),
                          log=stable.with_name('yosys.log'))
        for name in (sealer.MACHINE_NETLIST, 'machine.ys', 'yosys.log'):
            source = stable.with_name(name)
            shutil.copyfile(source, args.out / name)
            source.unlink()
    netlist = args.out / sealer.MACHINE_NETLIST
    elaborated = sha(netlist)
    if args.hierarchy:
        spike.plain_module_names(netlist)
    record = {
        'arm': args.arm, 'commit': commit, 'base': args.base, 'changed_from_base': changed,
        'hierarchy': args.hierarchy, 'netlist_sha256_as_elaborated': elaborated,
        'netlist_sha256': sha(netlist), 'netlist_bytes': netlist.stat().st_size,
        'program_sha256': hashlib.sha256(program.encode()).hexdigest(),
        'rom_hex_sha256': sha(rom), 'guest_rates': sealer.guest_rates(),
        'yosys': yosys_netlist.lock_record(args.root, yosys, 'HIP', sealer.LOCK),
        'host': os.uname().nodename, 'created_utc': datetime.now(timezone.utc).isoformat(),
    }
    (args.out / 'elaboration.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
    print(f'arm {args.arm} {"hier" if args.hierarchy else "flat"}: {record["netlist_sha256"]}', flush=True)
    return 0


STA_ENTRY = re.compile(r"^Type\s*: (.+?) Model (Setup|Hold|Recovery|Removal|Minimum Pulse Width) '([^']+)'\n"
                       r"Slack\s*: (-?[0-9.]+)\nTNS\s*: (-?[0-9.]+)", re.M)


def corners(summary_text: str) -> list[dict]:
    """Every corner's every clock and analysis from top.sta.summary."""
    return [{'corner': corner, 'analysis': analysis.lower().replace(' ', '_'), 'clock': clock,
             'slack_ns': float(slack), 'tns_ns': float(tns)}
            for corner, analysis, clock, slack, tns in STA_ENTRY.findall(summary_text)]


MAP_ROWS = {
    'alm_estimate': 'Estimate of Logic utilization (ALMs needed)',
    'aluts': 'Combinational ALUT usage for logic',
    'registers': 'Dedicated logic registers',
    'block_memory_bits': 'Total block memory bits',
    'dsp_blocks': 'Total DSP Blocks',
}


def map_resources(map_text: str) -> dict:
    """Analysis & Synthesis' resource estimates: seed-independent."""
    found = {}
    for key, label in MAP_ROWS.items():
        match = re.search(rf'^; {re.escape(label)}\s*; ([0-9,]+)', map_text, re.M)
        found[key] = int(match.group(1).replace(',', '')) if match else None
    return found


def lock_wait(path: Path):
    lease = path.open('a+')
    notice = 0.0
    while True:
        try:
            fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return lease
        except BlockingIOError:
            if time.monotonic() - notice >= 300:
                print(f'heartbeat: waiting for {path}', file=sys.stderr, flush=True)
                notice = time.monotonic()
            time.sleep(5)


def de10(args) -> int:
    sealer, device, spike, yosys_netlist = load(args.root)
    elaboration = json.loads((args.netlist.parent / 'elaboration.json').read_text())
    if sha(args.netlist) != elaboration['netlist_sha256']:
        raise SystemExit('the netlist is not the one its elaboration.json records')
    sealer._check_seed(args.seed)
    spike.check_free_space(args.out)
    args.out.mkdir(parents=True)
    tools = sealer._authenticate_tools(args.root, cache_root=args.cache_root, gpu_router='HIP')
    project = sealer.write_project(args.root, args.out, '0' * 32, args.seed)
    shutil.copyfile(args.netlist, project / sealer.MACHINE_NETLIST)
    if sha(project / sealer.MACHINE_NETLIST) != elaboration['netlist_sha256']:
        raise SystemExit('the fitted netlist copy does not match')
    env = dict(os.environ, QUARTUS_ROOTDIR=str(tools['quartus_root']))
    lease = lock_wait(LITE_LOCK)
    started = time.monotonic()
    try:
        spike.run([str(tools['quartus_sh']), '--flow', 'compile', sealer.board.TOP], project,
                  args.out / 'quartus.log', env)
        built = project / 'output_files'
        top = sealer.board.TOP
        (project / 'paths.tcl').write_text(spike.PATHS_TCL.format(
            top=top, out='paths.tsv', clock=spike.SYSTEM_CLOCK, x87_paths=''))
        spike.run([str(Path(tools['quartus_root']) / 'bin' / 'quartus_sta'), '-t', 'paths.tcl'], project,
                  args.out / 'paths.log', env)
        (project / 'retain.tcl').write_text(retention_tcl(top, 'retained.tsv'))
        spike.run([str(Path(tools['quartus_root']) / 'bin' / 'quartus_sta'), '-t', 'retain.tcl'], project,
                  args.out / 'retain.log', env)
    finally:
        fcntl.flock(lease, fcntl.LOCK_UN)
        lease.close()
    minutes = (time.monotonic() - started) / 60
    fit_text = (built / f'{top}.fit.rpt').read_text(errors='replace')
    sta_text = (built / f'{top}.sta.rpt').read_text(errors='replace')
    map_text = (built / f'{top}.map.rpt').read_text(errors='replace')
    try:
        sealer.validate_timing_report(sta_text)
        closes, reason = True, None
    except sealer.board.BuildError as error:
        closes, reason = False, str(error)
    for name in (f'{top}.fit.summary', f'{top}.sta.summary', f'{top}.map.summary'):
        shutil.copyfile(built / name, args.out / name)
    shutil.copyfile(project / 'paths.tsv', args.out / 'paths.tsv')
    entities = spike.entities(fit_text, depth=ENTITY_DEPTH) if elaboration['hierarchy'] else []
    # Arm c holds both identities only if the strap's latch survives
    # synthesis: every map-report line that names it, removed or kept.
    strap_lines = [line.strip() for line in map_text.splitlines() if 'decode_i486' in line][:20]
    summary = {
        'board': 'de10', 'arm': elaboration['arm'], 'commit': elaboration['commit'], 'seed': args.seed,
        'hierarchy': elaboration['hierarchy'], 'netlist_sha256': elaboration['netlist_sha256'],
        'build_id': '0' * 32, 'flow': 'build_fes_pc386_quartus.write_project, quartus_sh --flow compile',
        'quartus': tools['quartus_version'], 'device': sealer.board.TARGET, 'host': os.uname().nodename,
        'compile_minutes': round(minutes, 1), 'closes': closes, 'timing_failure': reason,
        'clocks': spike.multicorner(sta_text), 'corners': corners((built / f'{top}.sta.summary').read_text()),
        'resources': spike.resources(fit_text), 'synthesis': map_resources(map_text),
        'entities': entities, 'strap_map_lines': strap_lines,
        'retained_registers': read_retention((project / 'retained.tsv').read_text()), 'paths': spike.paths((project / 'paths.tsv').read_text()),
        'created_utc': datetime.now(timezone.utc).isoformat(),
    }
    (args.out / 'summary.json').write_text(json.dumps(summary, indent=2, sort_keys=True) + '\n')
    if elaboration['hierarchy']:
        start = fit_text.find(spike.ENTITY_HEADER)
        end = fit_text.find('\n+', fit_text.find('\n;', start) + 1)
        (args.out / 'entities.rpt').write_text(fit_text[start:end] + '\n')
    if not args.keep:
        shutil.rmtree(project)
    print(f'de10 arm {summary["arm"]} seed {args.seed}: {summary["resources"]["alms"]["used"]} ALMs, '
          f'closes={closes}, {minutes:.0f} min', flush=True)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest='command', required=True)
    one = sub.add_parser('elaborate')
    one.add_argument('--arm', required=True)
    one.add_argument('--base', required=True)
    one.add_argument('--hierarchy', action='store_true')
    two = sub.add_parser('de10')
    two.add_argument('--netlist', type=Path, required=True)
    two.add_argument('--seed', type=int, required=True)
    two.add_argument('--keep', action='store_true', help="keep the Quartus project and its databases")
    for each in (one, two):
        each.add_argument('--root', type=Path, required=True)
        each.add_argument('--cache-root', type=Path, required=True)
        each.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    for path in (args.root, args.cache_root, args.out):
        if not path.is_absolute():
            raise SystemExit(f'{path} must be absolute')
    if args.out.exists():
        raise SystemExit(f'{args.out} exists')
    return elaborate(args) if args.command == 'elaborate' else de10(args)


if __name__ == '__main__':
    raise SystemExit(main())
