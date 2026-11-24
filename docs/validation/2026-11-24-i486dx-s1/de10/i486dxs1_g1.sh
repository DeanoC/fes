#!/bin/bash
# i486dxs1_g1.sh HEAD: method G1. DE10 pc386 netlist at the base and HEAD from one checkout path, then identities.
set -uo pipefail
HEAD=$1; BASE=2d4edf547; P=/data/projects/i486dx-s1; WT=$P/wt-g1; O=$P/g1
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains YOSYS_MAX_THREADS=1
[ -d $WT ] || git -C /home/deano/fes worktree add -q --detach $WT $BASE || exit 2
for rev in $BASE $HEAD; do
  git -C $WT checkout -q --detach $rev || exit 3
  test -z "$(git -C $WT status --porcelain --untracked-files=no)" || exit 4
  echo "rev $(git -C $WT rev-parse HEAD)" >> $O/g1.log
  (cd $WT/sources/misteross && python3 scripts/build_pc386_firmware.py --download > $O/fw-${rev:0:9}.log 2>&1); echo "firmware rc=$?" >> $O/g1.log
  python3 $WT/docs/validation/2026-11-23-de25-486sx/de10_netlist.py $O/net-${rev:0:9} >> $O/g1.log 2>&1; echo "netlist rc=$?" >> $O/g1.log
  python3 -I $WT/docs/validation/2026-11-23-de25-486sx/de10_design_id.py $WT >> $O/g1.log 2>&1; echo "id rc=$?" >> $O/g1.log
done
cmp $O/net-${BASE:0:9}/pc386_system.v $O/net-${HEAD:0:9}/pc386_system.v && echo "CMP EQUAL" >> $O/g1.log || echo "CMP DIFFER" >> $O/g1.log
echo DONE >> $O/g1.log
