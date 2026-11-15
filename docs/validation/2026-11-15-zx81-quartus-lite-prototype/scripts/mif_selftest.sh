#!/bin/bash
# mif_selftest.sh <fitted-project-copy> <misteross>: rewrite the lane MIFs with a new
# byte pattern, re-run only MIF update and the assembler (no refit), then check
# that the new bytes decode at the ROM-map positions and that every changed CRAM
# bit is a ROM-map destination.
set -e
Q=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}/bin
cd "$1"; cp output_files/top.rbf fit.rbf
python3 - <<'PY'
import json, random
r = random.Random(99); pats = {}
for lane in range(8):
    words = [r.randrange(256) for _ in range(1024)]; pats[lane] = words
    rows = ''.join(f'{a:03X} : {w:03X};\n' for a, w in enumerate(words))
    open(f'lane{lane}.mif', 'w').write('DEPTH = 1024;\nWIDTH = 10;\nADDRESS_RADIX = HEX;\n'
        f'DATA_RADIX = HEX;\nCONTENT BEGIN\n{rows}END;\n')
json.dump(pats, open('patterns.json', 'w'))
PY
$Q/quartus_cdb top -c top --update_mif > update_mif.log 2>&1
$Q/quartus_asm top -c top > asm.log 2>&1
cd "$2"; project="$1"
python3 -I "$(dirname "$0")/check_bytes.py" . "$project" 8
python3 -I - "$project" <<'PY'
import sys; sys.path.insert(0, '.')
from pathlib import Path
from scripts import rom_map
from scripts.cyclonev_rbf import rbf_load
d = Path(sys.argv[1])
a = rbf_load((d/'fit.rbf').read_bytes()).cram; b = rbf_load((d/'output_files/top.rbf').read_bytes()).cram
db = rom_map.read_database(Path('/data/fes-toolchain-cache/tools/mistral/e7993ca3f425533050f6bf7eb6766fc945a7a8ba5b923d705e3e8d00ee857209/install/share/mistral/source'))
dest = {p for blk in rom_map.rom_blocks(rom_map.parse_ram_offsets(db['data/m10k-mux.txt'].decode()), tuple(range(73, 81)))
        for w in blk['word_bits'] for p in w}
changed = [i*8+k for i in range(len(a)) if a[i] != b[i] for k in range(8) if ((a[i]^b[i]) >> k) & 1]
print('changed CRAM bits', len(changed), 'outside ROM destinations', sum(c not in dest for c in changed))
PY
