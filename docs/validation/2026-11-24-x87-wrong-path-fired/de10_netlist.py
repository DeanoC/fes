#!/usr/bin/env python3
"""de10_netlist.py OUT: the DE10 seal's machine netlist (pc386_quartus.machine_program, the
sealed guest rates, no extra parameters) written by the pc386 lock's Yosys to OUT, run from
the repository root as the sealer runs it. Run it twice from one checkout path, before and
after an RTL edit, to compare the DE10 netlist byte for byte (Yosys's auto-generated names
follow paths, DeanoC/yosys#80)."""
import hashlib
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3] / 'sources/misteross'
sys.path.insert(0, str(ROOT))
from scripts import pc386_quartus as sealer  # noqa: E402
from scripts import yosys_netlist  # noqa: E402

out = Path(sys.argv[1]).resolve()
out.mkdir(parents=True, exist_ok=True)
sealer.check_rom(ROOT)
tool = yosys_netlist.authenticate(ROOT, cache_root=Path(os.environ['FES_TOOLCHAIN_CACHE_ROOT']),
                                  gpu_router=os.environ.get('FES_GPU_ROUTER', 'HIP'), lock=sealer.LOCK)
work = ROOT / 'build/de10-pc386-netlist'
work.mkdir(parents=True, exist_ok=True)
netlist = work / sealer.MACHINE_NETLIST
yosys_netlist.run(tool.path, sealer.machine_program(netlist.relative_to(ROOT)), cwd=ROOT,
                  program_file=work / 'machine.ys', log=work / 'yosys.log')
data = netlist.read_bytes()
(out / 'pc386_system.v').write_bytes(data)
print(hashlib.sha256(data).hexdigest(), tool.identity, 'YOSYS_MAX_THREADS=' + os.environ.get('YOSYS_MAX_THREADS', 'unset'))
