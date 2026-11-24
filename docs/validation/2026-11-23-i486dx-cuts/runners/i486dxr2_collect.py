#!/usr/bin/env python3
"""Copy round 2's evidence from /data/projects/i486dx-plan into this record (run once on powerboat)."""
import json, pathlib, re, shutil, subprocess

P = pathlib.Path('/data/projects/i486dx-plan')
R2 = P / 'r2'
REC = pathlib.Path(__file__).resolve().parent.parent
PLAN = pathlib.Path('/home/deano/fes/.claude/worktrees/happy-payne-d3b9ad/docs/validation/2026-11-22-i486dx-core-plan')
TOOL = P / 'wt-r2-sim-base/sources/misteross'
FIT_FILES = ('fit.json', 'inputs.json', 'netlist.json', 'timing.json', 'timing.tsv')

# A. Fits: round 2's variants, and dx's three seeds from the plan record.
for d in sorted((R2 / 'fits').iterdir()):
    out = REC / 'fits' / d.name
    out.mkdir(parents=True, exist_ok=True)
    for f in FIT_FILES:
        shutil.copy(d / f, out / f)
for s in (1, 2, 3):
    out = REC / 'fits' / f'dx-s{s}'
    out.mkdir(parents=True, exist_ok=True)
    for f in FIT_FILES:
        shutil.copy(PLAN / 'fits' / f'dx-s{s}' / f, out / f)


# B. Lockstep: every failure line of each log against base's (the plan record's run).
def failures(log: pathlib.Path) -> list[str]:
    lines = log.read_text().splitlines()
    return sorted({l.strip() for l in lines if l.startswith('FAIL') or re.search(r'\b[1-9]\d* fail\b', l)})


def suites(log: pathlib.Path) -> dict:
    """Each suite line's pass and fail counts (the built-form comparison's mismatches as its fails)."""
    out = {}
    for l in log.read_text().splitlines():
        m = re.match(r'^([A-Za-z][^:]*?): ', l)
        if not m:
            continue
        p, f, mm = re.search(r'(\d+) pass\b', l), re.search(r'(\d+) fail\b', l), re.search(r'(\d+) mismatches', l)
        if p and f:
            out[m.group(1)] = {'pass': int(p.group(1)), 'fail': int(f.group(1))}
        elif mm:
            out[m.group(1)] = {'pass': None, 'fail': int(mm.group(1))}
        elif l.startswith('BIU DMA/store: PASS'):
            out['BIU DMA/store'] = {'pass': 1, 'fail': 0}
    return out


base_bad = failures(P / 'sims/base/lockstep.log')
lock = {'base': {'source': 'plan record (sims/base)', 'rc': 0, 'failures': base_bad, 'new_failures': [],
                 'suites': suites(P / 'sims/base/lockstep.log')}}
for v in ('l1i2', 'l1d2', 'l12', 'xshift', 'xdisp', 'x87', 'cuts', 'qhalf'):
    log = R2 / 'sims' / v / 'lockstep.log'
    rc = int(re.search(r'lockstep rc=(\d+)', (R2 / 'sims' / v / 'status').read_text()).group(1))
    bad = failures(log)
    lock[v] = {'rc': rc, 'last_line': log.read_text().splitlines()[-1].strip(), 'failures': bad,
               'new_failures': [b for b in bad if b not in base_bad], 'suites': suites(log)}
(REC / 'lockstep.json').write_text(json.dumps(lock, indent=1) + '\n')

# B. The x87 unit bench: its summary line per variant.
unit = {}
for v in ('base', 'xshift', 'xdisp', 'x87', 'cuts'):
    lines = (R2 / 'sims' / v / 'x87unit.log').read_text().splitlines()
    summary = [l.strip() for l in lines if 'against the host x87' in l]
    rc = int(re.search(r'x87unit rc=(\d+)', (R2 / 'sims' / v / 'status').read_text()).group(1))
    unit[v] = {'rc': rc, 'summary': summary[-1] if summary else None}
(REC / 'x87unit.json').write_text(json.dumps(unit, indent=1) + '\n')

# C. Profile windows and latency reports.
(REC / 'profiles').mkdir(exist_ok=True)
(REC / 'latency').mkdir(exist_ok=True)


def window(prof: pathlib.Path, lo: float, hi: float, out: pathlib.Path) -> None:
    subprocess.run(['python3', 'scripts/pc386_cpu_profile.py', str(prof), '--from', repr(lo), '--to', repr(hi),
                    '--json', str(out)], cwd=TOOL, check=True, stdout=subprocess.DEVNULL)


def dumps(prof: pathlib.Path) -> list[float]:
    code = f"import sys;sys.path.insert(0,'.');from scripts import pc386_cpu_profile as p;" \
           f"print(*sorted(p.load(open('{prof}').read().splitlines())))"
    return [float(x) for x in subprocess.run(['python3', '-c', code], cwd=TOOL, check=True, capture_output=True,
                                             text=True).stdout.split()]


env = dict(l.split('=', 1) for l in (R2 / 't3d.env').read_text().splitlines() if '=' in l and not l.startswith('#'))
t3d_from, t3d_to = float(env['T3D_FROM']) + 0.5, float(env['T3D_LIMIT'])
for v in ('base', 'l1i2', 'l1d2', 'l12', 'qhalf', 'cuts'):
    window(R2 / 'sims' / v / 'cpu-3d.prof', t3d_from, t3d_to, REC / 'profiles' / f'3d-{v}.json')
for v in ('l1i2', 'l1d2', 'l12', 'qhalf', 'cuts'):
    h, m, e = dumps(R2 / 'sims' / v / 'cpu.prof')[:3]
    window(R2 / 'sims' / v / 'cpu.prof', h, m, REC / 'profiles' / f'doom-{v}-h1.json')
    window(R2 / 'sims' / v / 'cpu.prof', m, e, REC / 'profiles' / f'doom-{v}-h2.json')
for half in ('h1', 'h2'):
    shutil.copy(PLAN / 'profiles' / f'base-{half}.json', REC / 'profiles' / f'doom-base-{half}.json')
shutil.copy(PLAN / 'latency' / 'base.json', REC / 'latency' / 'base.json')
for v in ('l1i2', 'l1d2', 'l12', 'xshift', 'xdisp', 'x87', 'cuts', 'qhalf'):
    shutil.copy(R2 / 'sims' / v / 'latency-report.json', REC / 'latency' / f'{v}.json')

# Handover and end lines of every window run, and Quake's bring-up.
runs = {}
for v in ('base', 'l1i2', 'l1d2', 'l12', 'qhalf', 'cuts'):
    for name, log in (('3dbench', '3dbench.log'), ('doom', 'doom.log')):
        path = R2 / 'sims' / v / log
        if path.is_file():
            runs[f'{name}-{v}'] = [l for l in path.read_text().splitlines()
                                   if l.startswith(('RTL-BEGIN', 'STOP', 'END'))]
q = (R2 / 'sims/quake-base/quake.log').read_text().splitlines()
runs['quake-base'] = [l for l in q if l.startswith(('PROMPT', 'RTL-BEGIN', 'STOP', 'END'))]
runs['quake-base-status'] = (R2 / 'sims/quake-base/status').read_text().split()
(REC / 'runs.json').write_text(json.dumps(runs, indent=1) + '\n')
print('collected')
