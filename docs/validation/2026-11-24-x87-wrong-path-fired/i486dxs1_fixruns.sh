#!/bin/bash
# The x87 fired fix on main (wt-fix): the new case without the fix (reverse-applied), then the full suites with it.
set -uo pipefail
WT=/data/projects/i486dx-s1/wt-fix; O=/data/projects/i486dx-s1/fixruns; C=/home/deano/fes/out/cache/misteross-toolchains
cd $WT/sources/misteross
Q="--download --cache-root $C --random-programs 0 --pm-programs 0 --task-programs 0 --debug-programs 0 --undefined-programs 0 --limit-per-file 1 --jobs 8"
git -C $WT apply -R /data/projects/i486dx-s1/x87-fired-fix.patch || exit 3
timeout 3h python3 scripts/test_fes_i386_lockstep.py $Q > $O/before.log 2>&1; echo "before rc=$?" >> $O/status
cp build/sim/fes-i386-lockstep/report.json $O/before.json
git -C $WT apply /data/projects/i486dx-s1/x87-fired-fix.patch || exit 3
timeout 8h python3 scripts/test_fes_i386_lockstep.py --download --cache-root $C --jobs 8 > $O/l3-full.log 2>&1; echo "l3-full rc=$?" >> $O/status
cp build/sim/fes-i386-lockstep/report.json $O/l3-full.json
timeout 8h python3 scripts/test_fes_i486_lockstep.py --download --cache-root $C --jobs 8 > $O/l4-full.log 2>&1; echo "l4-full rc=$?" >> $O/status
cp build/sim/fes-i486-lockstep/report.json $O/l4-full.json
timeout 4h python3 scripts/test_fes_i386_x87_unit.py --download --cache-root $C > $O/x87unit.log 2>&1; echo "x87unit rc=$?" >> $O/status
cp build/sim/fes-i386-x87-unit/report.json $O/x87unit.json
echo SIM_DONE >> $O/status
