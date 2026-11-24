#!/bin/bash
# DE10 pc386 netlist on the fix branch without the fix (reverse-applied) and with it, from one checkout path.
set -uo pipefail
WT=/data/projects/i486dx-s1/wt-fixd12; O=/data/projects/i486dx-s1/fixd12; mkdir -p $O; : > $O/log
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains YOSYS_MAX_THREADS=1
D=$WT/docs/validation/2026-11-24-x87-wrong-path-fired
(cd $WT/sources/misteross && python3 scripts/build_pc386_firmware.py --download > $O/fw.log 2>&1); echo "fw rc=$?" >> $O/log
true
python3 $D/de10_netlist.py $O/before >> $O/log 2>&1; echo "before rc=$?" >> $O/log
git -C $WT apply /data/projects/i486dx-s1/x87-fired-fix.patch || { echo apply-fail >> $O/log; exit 3; }
python3 $D/de10_netlist.py $O/after >> $O/log 2>&1; echo "after rc=$?" >> $O/log
cmp $O/before/pc386_system.v $O/after/pc386_system.v && echo "CMP EQUAL" >> $O/log || echo "CMP DIFFER" >> $O/log
echo DONE >> $O/log
