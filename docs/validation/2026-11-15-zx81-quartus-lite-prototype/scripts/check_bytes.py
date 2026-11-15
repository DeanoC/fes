import json, sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]).resolve()))
from scripts import rom_map
from scripts.cyclonev_rbf import rbf_load
proj = Path(sys.argv[2]); lanes = int(sys.argv[3])
db = rom_map.read_database(Path('/data/fes-toolchain-cache/tools/mistral/e7993ca3f425533050f6bf7eb6766fc945a7a8ba5b923d705e3e8d00ee857209/install/share/mistral/source'))
perm = [0,20,10,30,1,21,11,31,2,22,12,32,3,23,13,33,4,24,14,34,5,25,15,35,6,26,16,36,7,27,17,37,8,28,18,38,9,29,19,39]
blocks = rom_map.rom_blocks(rom_map.parse_ram_offsets(db['data/m10k-mux.txt'].decode()), tuple(range(73, 73+lanes)))
raw = (proj/'output_files/top.rbf').read_bytes(); cram = rbf_load(raw).cram
pat = json.loads((proj/'patterns.json').read_text())
print('rbf bytes', len(raw))
failed = False
for lane, block in enumerate(blocks):
    ok = pad_blank = 0
    for w, bits in enumerate(block['word_bits']):
        logical = 0
        for i, p in enumerate(bits):
            s = (cram[p>>3] >> (p&7)) & 1
            if perm[i] % 10 >= 8: pad_blank += s
            elif not s: logical |= 1 << perm[i]
        ok += all(((logical >> (10*k)) & 0xff) == (pat[str(lane)][4*w+k] & 0xff) for k in range(4))
    print(f'lane{lane}: bytes match in {ok}/256 rows; padding bits blank {pad_blank}/{256*8}')
    failed |= ok != 256 or pad_blank != 256*8
sys.exit(1 if failed else 0)
