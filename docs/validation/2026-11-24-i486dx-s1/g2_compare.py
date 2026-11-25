#!/usr/bin/env python3
"""g2_compare.py BASE.json DX.json: method G2's rule on two lockstep reports. Prints every
failure line in either report, the failures the DX has that the base lacks (the rule: none),
and every leaf value that differs between the two, so what changed is visible."""
import json
import sys


def failures(report):
    found = []
    def walk(value, path):
        if isinstance(value, dict):
            for key, item in value.items():
                walk(item, path + (key,))
        elif isinstance(value, list):
            if path and path[-1] == 'failures':
                found.extend(str(item) for item in value)
            else:
                for n, item in enumerate(value):
                    walk(item, path + (str(n),))
    walk(report, ())
    return found


def leaves(value, path=()):
    if isinstance(value, dict):
        for key, item in value.items():
            yield from leaves(item, path + (key,))
    elif isinstance(value, list):
        for n, item in enumerate(value):
            yield from leaves(item, path + (str(n),))
    else:
        yield '/'.join(path), value


base, dx = (json.load(open(path)) for path in sys.argv[1:3])
fb, fd = failures(base), failures(dx)
print('base failures', len(fb), 'dx failures', len(fd))
new = sorted(set(fd) - set(fb))
print('new in dx', len(new))
for line in new[:50]:
    print('  NEW', line)
lb, ld = dict(leaves(base)), dict(leaves(dx))
diff = sorted(k for k in set(lb) | set(ld) if lb.get(k) != ld.get(k))
print('differing values', len(diff))
for key in diff:
    print('  ', key, '|', str(lb.get(key))[:60], '|', str(ld.get(key))[:60])
print('PASS' if not new else 'FAIL')
