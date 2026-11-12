"""Compare two lockstep report.json files, leaving out only the tool fields:
verilator, the old yosys path, and netlist (the --netlist run's record of the
Yosys program, sources, netlist digest and identity, which an RTL run lacks)."""
import json
import sys

SKIP = {'verilator', 'yosys', 'netlist'}


def walk(a, b, path, out):
    if isinstance(a, dict) and isinstance(b, dict):
        for k in sorted(set(a) | set(b)):
            if not path and k in SKIP:
                continue
            if k not in a or k not in b:
                out.append(f'{path}/{k}: only in {"second" if k not in a else "first"}')
            else:
                walk(a[k], b[k], f'{path}/{k}', out)
    elif isinstance(a, list) and isinstance(b, list) and len(a) == len(b):
        for i, (x, y) in enumerate(zip(a, b)):
            walk(x, y, f'{path}[{i}]', out)
    elif a != b:
        out.append(f'{path}: {str(a)[:120]} != {str(b)[:120]}')


def hashes(value, path='', found=None):
    found = {} if found is None else found
    if isinstance(value, dict):
        for k, v in value.items():
            if 'hash' in k:
                found[f'{path}/{k}'] = v
            hashes(v, f'{path}/{k}', found)
    elif isinstance(value, list):
        for i, v in enumerate(value):
            hashes(v, f'{path}[{i}]', found)
    return found


a, b = (json.load(open(p)) for p in sys.argv[1:3])
out = []
walk(a, b, '', out)
ha, hb = hashes(a), hashes(b)
print(f'hash fields compared: {len(ha)} vs {len(hb)}; all equal: {ha == hb}')
print(f'top-level keys: {sorted(a)}')
print(f'differences outside {sorted(SKIP)}: {len(out)}')
for line in out[:50]:
    print(' ', line)
sys.exit(1 if out or ha != hb else 0)
