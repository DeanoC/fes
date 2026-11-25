#!/bin/bash
# i486dxs1_g2.sh HEAD: method G2. Lockstep level 3 (round 2's subset) and level 4 (486sx defaults),
# base (shared runner) then DX (i486dx/lockstep.py), then the x87 unit and engine benches.
set -uo pipefail
HEAD=$1; P=/data/projects/i486dx-s1; WT=$P/wt-g2b; O=$P/g2b
C=/home/deano/fes/out/cache/misteross-toolchains
[ -d $WT ] || git -C /home/deano/fes worktree add -q --detach $WT $HEAD || exit 2
git -C $WT checkout -q --detach $HEAD || exit 3
test -z "$(git -C $WT status --porcelain --untracked-files=no)" || exit 4
cd $WT/sources/misteross
hb(){ date -u +%FT%TZ > $O/heartbeat; }
L3="--download --cache-root $C --random-programs 500 --random-seeds 8 --limit-per-file 20 --jobs 8"
L4="--download --cache-root $C --identity 486sx --jobs 8"
hb; timeout 6h python3 scripts/test_fes_i386_lockstep.py $L3 > $O/l3-base.log 2>&1; echo "l3-base rc=$?" >> $O/status
cp build/sim/fes-i386-lockstep/report.json $O/l3-base.json
hb; timeout 6h python3 boards/de25nano/pc386/i486dx/lockstep.py level3 $L3 > $O/l3-dx.log 2>&1; echo "l3-dx rc=$?" >> $O/status
cp build/sim/fes-i486dx-lockstep/report.json $O/l3-dx.json
hb; timeout 8h python3 scripts/test_fes_i486_lockstep.py $L4 > $O/l4-base.log 2>&1; echo "l4-base rc=$?" >> $O/status
cp build/sim/fes-i486-lockstep/report.json $O/l4-base.json
hb; timeout 8h python3 boards/de25nano/pc386/i486dx/lockstep.py level4 $L4 > $O/l4-dx.log 2>&1; echo "l4-dx rc=$?" >> $O/status
cp build/sim/fes-i486dx-i486-lockstep/report.json $O/l4-dx.json
hb; timeout 4h python3 scripts/test_fes_i386_x87_unit.py --download --cache-root $C > $O/x87unit.log 2>&1; echo "x87unit rc=$?" >> $O/status
cp build/sim/fes-i386-x87-unit/report.json $O/x87unit.json
hb; timeout 4h python3 scripts/test_fes_x87_trans_engine.py --cache-root $C > $O/trans.log 2>&1; echo "trans rc=$?" >> $O/status
cp build/sim/fes-x87-trans-engine/report.json $O/trans.json
hb; timeout 4h python3 scripts/test_fes_i386_x87_testfloat.py --testfloat-gen /tmp/fes-x87-testfloat/build/Linux-x86_64-GCC/testfloat_gen --cache-root $C > $O/testfloat.log 2>&1; echo "testfloat rc=$?" >> $O/status
cp build/sim/fes-i386-x87-testfloat/report.json $O/testfloat.json
git -C /tmp/fes-x87-testfloat rev-parse HEAD > $O/testfloat-revision.txt
sha256sum cores/fes-common/rtl/i386/fes_i386_x87*.sv cores/fes-common/rtl/i386/fes_x87_*.sv > $O/x87-files.sha256
hb; echo SIM_DONE >> $O/status
