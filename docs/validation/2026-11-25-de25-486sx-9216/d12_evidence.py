#!/usr/bin/env python3
"""d12_evidence.py D12DIR BRANCHBUILD: G7's evidence for each package, printed in full.

D12DIR holds A-<pkg> (main, --compile-only) and C-<pkg> (branch tooling, A's BUILD_ID forced);
BRANCHBUILD holds B-<pkg> (the branch's full producer). For each package:
- RBF: the full SHA-256 of A's and C's core.rbf, and whether they are equal. MENU's C stops at its
  review after compiling and copies no core.rbf; its compiled RBF, signoff/combined/hps/linux-boot.core.rbf
  (the file the producer copies to core.rbf), is compared instead and labelled so. Any other
  package without a C core.rbf fails;
- B: its BUILD_ID, RBF SHA-256 and package_timing_admission;
- inputs: every source-like project file (d12_cmp.py's set) of A and B, compared after replacing each
  side's root path and its own BUILD_ID (hex, 128-bit binary, decimal) with one token. Prints every file
  that still differs, or "none", and every file that differed only by root path or BUILD_ID.
Exit 1 if any package has a remaining input difference or an unequal A/C RBF pair.
"""
import hashlib
import json
import sys
from pathlib import Path

EXT = {'.qsf', '.qpf', '.sdc', '.v', '.sv', '.vh', '.svh', '.tcl', '.ip', '.qsys', '.hex', '.mif', '.json', '.vhd'}
SKIP = ('output_files', 'qdb', 'tmp-clearbox', 'dni', '.qsys_edit', 'software')
PKGS = ('menu', 'zx81', 'spectrum48', 'spectrum128', 'st', 'c64', 'pcxt')


def files(root):
    return {p.relative_to(root).as_posix(): p.read_bytes() for p in root.rglob('*')
            if p.is_file() and p.suffix in EXT and not any(s in p.relative_to(root).parts for s in SKIP)}


def normal(data, top, build_id):
    value = int(build_id, 16)
    for token in (str(top).encode(), build_id.encode(), format(value, '0128b').encode(), str(value).encode()):
        data = data.replace(token, b'<X>')
    return data


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None


def main(d12, branch):
    d12, branch, bad = Path(d12), Path(branch), False
    for pkg in PKGS:
        a, b, c = d12 / f'A-{pkg}', branch / f'B-{pkg}', d12 / f'C-{pkg}'
        ida = json.loads((a / 'build-summary.json').read_text())['build_id']
        bsum = json.loads((b / 'build-summary.json').read_text())
        ra, rc, where = sha(a / 'core.rbf'), sha(c / 'core.rbf'), 'core.rbf'
        if rc is None and pkg == 'menu':  # the one documented review-stage exception
            rc, where = sha(c / 'signoff/combined/hps/linux-boot.core.rbf'), 'signoff/combined/hps/linux-boot.core.rbf'
        print(f'== {pkg}')
        print(f'  A root {a}  BUILD_ID {ida}  core.rbf {ra}')
        print(f'  C root {c}  {where} {rc}  ' + ('EQUAL to A' if rc == ra else 'no RBF' if rc is None else 'DIFFERS from A'))
        print(f"  B root {b}  BUILD_ID {bsum['build_id']}  core.rbf {bsum['rbf_sha256']}  "
              f"admitted {bsum['package_timing_admission']}")
        bad |= rc != ra  # a missing C RBF fails, except MENU's compiled one above
        pa, pb = a / 'signoff/combined/hps/project', b / 'signoff/combined/hps/project'
        fa, fb = files(pa), files(pb)
        only = sorted(set(fa) ^ set(fb))
        normalised, remaining = [], []
        for name in sorted(set(fa) & set(fb)):
            if fa[name] == fb[name]:
                continue
            if normal(fa[name], a, ida) == normal(fb[name], b, bsum['build_id']):
                normalised.append(name)
            else:
                remaining.append(name)
        print(f'  inputs: {len(fa)} A, {len(fb)} B, only one side {only or "none"}')
        print(f'  differ by root path or BUILD_ID only ({len(normalised)}): ' + ', '.join(normalised))
        print(f'  differ otherwise: {remaining or "none"}')
        bad |= bool(only or remaining)
    print('ALL PASS' if not bad else 'SOME FAIL')
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
