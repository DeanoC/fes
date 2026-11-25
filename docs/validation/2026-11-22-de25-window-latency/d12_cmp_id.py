"""D12 input check: project inputs of A (base compile-only) and B (arm producer) are byte-identical after
normalising each side's output dir (<OUT>), source tree (<SRC>) and BUILD_ID (hex, 128-bit binary, decimal).
  d12_cmp_id.py PKG ID_A ID_B   (run in the slat-d12 scratch dir)"""
import sys
from pathlib import Path
EXT = {'.qsf', '.qpf', '.sdc', '.v', '.sv', '.vh', '.svh', '.tcl', '.ip', '.qsys', '.hex', '.mif', '.json', '.vhd'}
SKIP = ('output_files', 'qdb', 'tmp-clearbox', 'dni', '.qsys_edit', 'software')
PROJ = 'signoff/combined/hps/project'
pkg, ida, idb = sys.argv[1:4]
sides = {'A': (Path(f'A-{pkg}'), Path('wt-base/sources/misteross'), ida),
         'B': (Path(f'wt-arm/sources/misteross/build/d12/B-{pkg}'), Path('wt-arm/sources/misteross'), idb)}
def forms(i):
    v = int(i, 16)
    return [('0x' + i).encode(), format(v, '0128b').encode(), str(v).encode(), i.encode()]
def files(side):
    out, src, i = sides[side]
    root = out / PROJ
    res = {}
    for p in root.rglob('*'):
        rel = p.relative_to(root)
        if p.is_file() and p.suffix in EXT and not any(s in rel.parts for s in SKIP):
            b = p.read_bytes().replace(str(out.resolve()).encode(), b'<OUT>').replace(str(src.resolve()).encode(), b'<SRC>')
            for f in forms(i):
                b = b.replace(f, b'<ID>')
            res[rel.as_posix()] = b
    return res
fa, fb = files('A'), files('B')
only = sorted(set(fa) ^ set(fb))
diff = [n for n in sorted(set(fa) & set(fb)) if fa[n] != fb[n]]
print(f'{pkg}: {len(fa)} files A, {len(fb)} B; only-one-side {len(only)}; differing after <OUT>/<SRC>/<ID> normalisation {len(diff)}')
for n in only + diff: print('  ', n)
sys.exit(1 if only or diff else 0)
