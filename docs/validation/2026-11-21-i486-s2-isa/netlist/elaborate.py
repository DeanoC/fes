#!/usr/bin/env python3
"""Elaborate fes_i386_cpu through the seal's Yosys program for one tree and parameter set, then stat -width it."""
import json, subprocess, sys
from pathlib import Path
tree, out, params = Path(sys.argv[1]), Path(sys.argv[2]), json.loads(sys.argv[3])
sys.path.insert(0, str(tree / 'sources/misteross'))
from scripts import yosys_netlist as y
root = tree / 'sources/misteross'
tool = y.authenticate(root, cache_root=Path('/home/deano/fes/out/cache/misteross-toolchains'), gpu_router='HIP')
netlist = y.seal_netlist(tool.path, out, root, parameters=params or None)
stat = subprocess.run([str(tool.path), '-q', '-p', f'read_verilog {netlist}; hierarchy -top fes_i386_cpu; stat -width; tee -o {out}/stat.txt stat -width'],
                      capture_output=True, text=True, cwd=out)
print(json.dumps({'netlist': str(netlist), 'sha256': y.sha256(netlist), 'rc': stat.returncode, 'err': stat.stderr[-500:]}))
