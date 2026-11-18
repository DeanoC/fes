"""Does a range of commits move any of the twelve cores' BUILD_IDs?

A `BUILD_ID` hashes its producer's transitive source closure, so it moves only
when a commit touches a file inside that closure. That makes "has my measured
identity gone stale?" a mechanical question, not a judgement -- and it is worth
being mechanical about, because the obvious fear ("every merge invalidates the
condition-3 runs") is wrong and would send you round a measurement treadmill
for nothing.

    python3 closure_touched.py <base> <head>

Exit 0 and "no closure hits" means every measured identity in README.md still
ships. Any hit names the core and the file, and that core's condition 3 has to
be retaken at the merged head.
"""
import json
import os
import subprocess
import sys

PRODUCERS = (
    'build_fes_sg1000_oss', 'build_fes_zx81_oss', 'build_fes_sms_oss',
    'build_fes_coleco_socket_v2', 'build_fes_apple2_oss', 'build_fes_spectrum_oss',
    'build_fes_atari_st_oss', 'build_fes_c64_oss', 'build_fes_pong',
    'build_fes_menu', 'build_fes_ramtest', 'build_fes_riscv',
)
PREFIX = 'sources/misteross/'
ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                    '..', '..', '..'))


def closures():
    with open(os.path.join(ROOT, PREFIX, 'scripts/source_closures.json')) as handle:
        return json.load(handle)


def changed(base, head):
    out = subprocess.run(['git', '-C', ROOT, 'diff', '--name-only', base, head],
                         capture_output=True, text=True, check=True).stdout.split()
    return [path[len(PREFIX):] for path in out if path.startswith(PREFIX)], out


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    base, head = sys.argv[1], sys.argv[2]
    table = closures()
    inside, every = changed(base, head)
    print(f'{len(every)} files changed, {len(inside)} under {PREFIX}')
    problems = 0
    for name in PRODUCERS:
        keys = [k for k in table if k == name] or [k for k in table if name in k]
        if not keys:
            print(f'  {name:30} NO CLOSURE -- cannot answer')
            problems += 1
            continue
        roots = table[keys[0]]
        hits = [f for f in inside
                if any(f == r or f.startswith(r.rstrip('/') + '/') for r in roots)]
        problems += len(hits)
        print(f'  {keys[0]:30} {len(roots):3} roots  '
              f'{"hits: " + ", ".join(hits) if hits else "none"}')
    print()
    if problems:
        print(f'{problems} closure hit(s): those cores\' condition 3 must be retaken')
        return 1
    print('no closure hits: every measured BUILD_ID still ships')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
