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
import json
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
    # The report's source record (its files and files_sha256) names this
    # runner and the derivation too, so a DX report says which runner and
    # derivation produced it; nothing shared binds them (they are DE25 only,
    # outside the DE10's record closure).
    shared = level3.tested_sources
    level3.tested_sources = lambda: sorted({*shared(), Path(__file__).resolve(), HERE / 'derive.py'})
    sys.argv = [sys.argv[0], *argv[1:]]
    if argv[0] == 'level3':
        return level3.main()
    from scripts import test_fes_i486_lockstep as level4
    level4.OUTPUT = ROOT / 'build/sim/fes-i486dx-i486-lockstep'
    # The level-4 runner records no sources; add the same record to its
    # report, only to one this run wrote: an earlier run's report is removed
    # first, so a run that fails before writing leaves none to relabel.
    # The record is taken before the run, as level 3 takes its own, and a
    # source that changed while the run went on fails the report.
    report = level4.OUTPUT / 'report.json'
    report.unlink(missing_ok=True)
    # Level 4's own runner and suite inputs as well: its cases, its oracle
    # build and the expected-stops lists it checks against.
    with_level3 = level3.tested_sources
    level3.tested_sources = lambda: sorted({
        *with_level3(), Path(level4.__file__).resolve(), ROOT / 'scripts/fes_i486_cases.py',
        ROOT / 'scripts/fes_i486_oracle.py', *level3.SIM.glob('i486-expected-stops*.txt')})
    before = level3.source_record()
    status = level4.main()
    if report.exists():
        data = json.loads(report.read_text())
        data['i486dx_source'] = before
        if level3.source_record() != before:
            data['failures'] = [*data.get('failures', []),
                                'i486dx sources changed during the run: the report names sources it did not test']
            status = status or 1
        report.write_text(json.dumps(data, indent=2, sort_keys=True) + '\n')
    return status


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
