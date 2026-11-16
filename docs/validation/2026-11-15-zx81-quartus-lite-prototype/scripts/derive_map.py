"""Prototype: correlate each stored M10K bit with every candidate logical (address, bit)."""
import json, sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]).resolve()))
from scripts import rom_map
from scripts.cyclonev_rbf import rbf_load
proj = Path(sys.argv[2])
db = rom_map.read_database(Path('/data/fes-toolchain-cache/tools/mistral/e7993ca3f425533050f6bf7eb6766fc945a7a8ba5b923d705e3e8d00ee857209/install/share/mistral/source'))
blocks = rom_map.rom_blocks(rom_map.parse_ram_offsets(db['data/m10k-mux.txt'].decode()), tuple(range(73, 81)))
cram = rbf_load((proj/'output_files/top.rbf').read_bytes()).cram
pat = json.loads((proj/'patterns.json').read_text())
layouts = {'4w+k': lambda w,k: 4*w+k, 'w+256k': lambda w,k: w+256*k}
result = {}
for name, addr in layouts.items():
    found = {}
    for i in range(40):
        best = None
        for k in range(4):
            for b in range(10):
                for inv in (0, 1):
                    agree = 0
                    for lane in range(8):
                        for w, bits in enumerate(blocks[lane]['word_bits']):
                            p = bits[i]; s = (cram[p>>3] >> (p&7)) & 1
                            agree += (s ^ inv) == ((pat[str(lane)][addr(w,k)] >> b) & 1)
                    if best is None or agree > best[0]: best = (agree, k, b, inv)
        found[i] = best
    exact = sum(v[0] == 8*256 for v in found.values())
    print(name, 'exact stored bits:', exact, '/40')
    result[name] = found
best = max(result, key=lambda n: sum(v[0] == 2048 for v in result[n].values()))
print(best, {i: v[1:] for i, v in result[best].items()})
