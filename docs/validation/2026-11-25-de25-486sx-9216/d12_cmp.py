"""Compare two DE25 build trees' project inputs: every source-like file under signoff/combined/hps/project
(QSF, SDC, HDL, Tcl, IP, qsys, hex/mif), excluding Quartus output/database dirs. Paths are normalised."""
import hashlib, sys
from pathlib import Path
EXT = {'.qsf', '.qpf', '.sdc', '.v', '.sv', '.vh', '.svh', '.tcl', '.ip', '.qsys', '.hex', '.mif', '.json', '.vhd'}
SKIP = ('output_files', 'qdb', 'tmp-clearbox', 'dni', '.qsys_edit', 'software')
def files(root):
    out = {}
    for p in root.rglob('*'):
        rel = p.relative_to(root)
        if p.is_file() and p.suffix in EXT and not any(s in rel.parts for s in SKIP):
            out[rel.as_posix()] = p.read_bytes()
    return out
a, c = Path(sys.argv[1]), Path(sys.argv[2])
fa, fc = files(a), files(c)
norm = lambda b, r: b.replace(str(r).encode(), b'<ROOT>')
only = sorted(set(fa) ^ set(fc))
diff = [n for n in sorted(set(fa) & set(fc)) if norm(fa[n], a.parents[4]) != norm(fc[n], c.parents[4])]
print(f'{len(fa)} files A, {len(fc)} C; only-one-side {len(only)}; differing after path normalisation {len(diff)}')
for n in only[:10]: print('  only', n, 'A' if n in fa else 'C')
for n in diff[:15]: print('  diff', n)
