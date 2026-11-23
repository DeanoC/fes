import json, re, pathlib
P = pathlib.Path('/data/projects/i486dx-plan/sims')
R = pathlib.Path('/home/deano/fes/.claude/worktrees/happy-payne-d3b9ad/docs/validation/2026-11-22-i486dx-core-plan')
def fails(v):
    log = (P / v / 'lockstep.log').read_text()
    rc = re.search(r'lockstep rc=(\d+)', (P / v / 'status').read_text())
    bad = sorted(set(l.strip()[:160] for l in log.splitlines() if re.search(r'\b[1-9]\d* fail', l)))
    return (rc and rc.group(1) == '0' and 'i386 lockstep passed' in log), bad
out = {}
base_ok, base_bad = fails('base')
for v in ('base', 'pred', 'ras', 'ind', 'stld', 'ealu', 'lfwd', 'speed'):
    if not (P / v / 'lockstep.log').is_file():
        continue
    ok, bad = fails(v)
    new = [b for b in bad if b not in base_bad]
    out[v] = {'verdict': ('passed: --random-programs 500 --random-seeds 8 --limit-per-file 20' if ok else 'FAILED'),
              'new_failures': len(new), 'failure_lines': new}
(R / 'lockstep.json').write_text(json.dumps(out, indent=1) + '\n')
print(json.dumps({k: (v['verdict'][:6], v['new_failures']) for k, v in out.items()}))
