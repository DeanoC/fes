#!/usr/bin/env python3
"""Compare a Quartus ROM initialisation file (.mif) with upstream's two case-statement tables
at main f3140cb33: words 0-255 must be the log-sine table and 256-511 the exp table.
  check_mif.py ROM.mif   (from anywhere in the repository)
"""
import re, subprocess, sys
from pathlib import Path

V = 'sources/misteross/cores/fes-pc386/vendor/opl2_fpga'
top = subprocess.run(['git', '-C', str(Path(__file__).parent), 'rev-parse', '--show-toplevel'],
                     capture_output=True, text=True, check=True).stdout.strip()


def table(name):
    text = subprocess.run(['git', '-C', top, 'show', f'f3140cb33:{V}/{name}'], capture_output=True, text=True,
                          check=True).stdout
    rows = {int(a, 16): int(v) for a, v in re.findall(r"8'h([0-9a-f]+): out <= (\d+);", text)}
    assert sorted(rows) == list(range(256)), name
    return [rows[i] for i in range(256)]


expected = table('opl2_log_sine_lut.sv') + table('opl2_exp_lut.sv')
text = Path(sys.argv[1]).read_text()
RADIX = {'HEX': 16, 'BIN': 2, 'DEC': 10, 'UNS': 10, 'OCT': 8}
base = RADIX[re.search(r'DATA_RADIX\s*=\s*(\w+);', text).group(1)[:3]]
address_base = RADIX[re.search(r'ADDRESS_RADIX\s*=\s*(\w+);', text).group(1)[:3]]
assert re.search(r'DEPTH\s*=\s*512;', text) and re.search(r'WIDTH\s*=\s*12;', text)
words = {}
body = text.split('CONTENT BEGIN', 1)[1].split('END;', 1)[0]
for entry in body.split(';'):
    if ':' not in entry:
        continue
    address, value = (part.strip() for part in entry.split(':', 1))
    values = [int(v, base) for v in value.split()]
    if address.startswith('['):
        lo, hi = (int(x, address_base) for x in address.strip('[]').split('..'))
        for a in range(lo, hi + 1):
            words[a] = values[(a - lo) % len(values)]
    else:
        start = int(address, address_base)
        for k, v in enumerate(values):
            words[start + k] = v
got = [words.get(a) for a in range(512)]
bad = [a for a in range(512) if got[a] != expected[a]]
print(f"{'PASS' if not bad else 'FAIL'}: 512 words, {len(bad)} differ" + (f', first at {bad[0]:03x}' if bad else ''))
sys.exit(1 if bad else 0)
