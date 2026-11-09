#!/usr/bin/env python3
"""Area audit #1548: the pc386 Quartus build with the Yosys hierarchy kept (no flatten),
so the fit report lists ALMs per entity. Analysis only; the package is scratch."""
import os, sys
from pathlib import Path
root = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(root))
from scripts import build_fes_pc386_quartus as b
orig = b.machine_program
def hier(netlist, sources=None):
    p = orig(netlist, sources)
    assert "flatten; " in p
    return p.replace("flatten; ", "").replace("write_verilog", "tee -o " + str(root / "build/area-yosys-stat.txt") + " stat; write_verilog")
b.machine_program = hier
_elab = b.elaborate_machine
def elab(root_, output, project, yosys):
    # the check looks for "module pc386_system(" which a hierarchical netlist still has
    return _elab(root_, output, project, yosys)
b.elaborate_machine = elab
print(b.build(root=root, cache_root=Path(os.environ["CACHE_ROOT"]), seed=1))
