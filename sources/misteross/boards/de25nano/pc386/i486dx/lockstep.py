#!/usr/bin/env python3
"""The lockstep runners on the DE25's i486DX (derive.py), with nothing shared changed.

    python3 boards/de25nano/pc386/i486dx/lockstep.py level3 [test_fes_i386_lockstep.py's arguments]
    python3 boards/de25nano/pc386/i486dx/lockstep.py level4 [test_fes_i486_lockstep.py's arguments]

Each runs the shared runner's own main with its CPU sources replaced by the
DX's in the benches' form (derive.sim_sources: every edit but the top's name,
so lockstep_tb reads the DX's hierarchy as it reads the integer core's), and
its build and report under build/sim/fes-i486dx-lockstep*/, apart from the
integer core's. The suites, cases, oracle, testbench and pass rules are the
shared runner's. The RTL only: the seal-netlist mode (--netlist) is refused.
"""
from pathlib import Path
import importlib.util
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
sys.path.insert(0, str(ROOT))
spec = importlib.util.spec_from_file_location('de25_pc386_i486dx_derive', HERE / 'derive.py')
dx = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dx)
from scripts import test_fes_i386_lockstep as level3  # noqa: E402


def main(argv):
    if len(argv) < 1 or argv[0] not in ('level3', 'level4') or any(a.startswith('--netlist') for a in argv):
        print(__doc__, file=sys.stderr)
        return 2
    level3.OUTPUT = ROOT / 'build/sim/fes-i486dx-lockstep'
    # The runners join RTL with each name; an absolute path stays itself.
    level3.CPU_RTL = dx.sim_sources(level3.OUTPUT / 'derived', ROOT)
    sys.argv = [sys.argv[0], *argv[1:]]
    if argv[0] == 'level3':
        return level3.main()
    from scripts import test_fes_i486_lockstep as level4
    level4.OUTPUT = ROOT / 'build/sim/fes-i486dx-i486-lockstep'
    return level4.main()


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
