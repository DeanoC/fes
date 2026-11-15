"""Read the fpga2sdram cfg_* constants from a Quartus post-fit .vo and compare
them with the generated HPS DDR layout header. Run from sources/misteross:
  quartus_eda top -c top --simulation --tool=modelsim --format=verilog
  python3 -I ddr_cfg_from_vo.py <project>/simulation/modelsim/top.vo
"""
import re, sys
sys.path.insert(0, '.')
from pathlib import Path
from scripts.fes_de10nano_evidence import hps_ddr_layout, HPS_DDR_CFG_PORTS
text = open(sys.argv[1]).read()
start = text.index('cyclonev_hps_interface_fpga2sdram'); block = text[start:text.index(');', start)]
want = hps_ddr_layout(Path('.'))
failed = False
for port in HPS_DDR_CFG_PORTS:
    bits = re.search(r'\.' + port + r'\(\{([^}]*)\}\)', block).group(1).split(',')
    tokens = [b.strip() for b in bits]
    if len(tokens) != HPS_DDR_CFG_PORTS[port][1] or set(tokens) - {'gnd', 'vcc'}:
        # Only the literal constants prove a tie-off; a net, x or any other spelling fails.
        print(port, 'is not a constant:', tokens)
        failed = True
        continue
    value = int(''.join('1' if b == 'vcc' else '0' for b in tokens), 2)  # MSB first
    print(port, hex(value), 'layout', hex(want[port]), 'OK' if value == want[port] else 'MISMATCH')
    failed |= value != want[port]
sys.exit(1 if failed else 0)
