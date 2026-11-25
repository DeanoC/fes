#!/bin/bash
# g1.sh BASE HEAD: method.md's G1. One checkout path ($P/wt-de10), the DE10 seal's machine netlist at BASE
# then HEAD (#2171's de10_netlist.py, from BASE), and the DE10 design identities of both trees.
set -u
P=/data/projects/fes-486sx-9216; REPO=/home/deano/fes; W=$P/wt-de10; BASE=$1; HEAD=$2
export YOSYS_MAX_THREADS=1 FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
S=$P/g1-scripts; mkdir -p $S
git -C $REPO show $BASE:docs/validation/2026-11-23-de25-486sx/de10_netlist.py > $S/de10_netlist.py
git -C $REPO show $BASE:docs/validation/2026-11-23-de25-486sx/de10_design_id.py > $S/de10_design_id.py
[ -d $W ] || git -C $REPO worktree add -q --detach $W $BASE
for side in base:$BASE head:$HEAD; do
  name=${side%%:*}; rev=${side#*:}
  git -C $W checkout -q --detach $rev && [ -z "$(git -C $W status --porcelain)" ] || { echo "$name tree-fail"; exit 2; }
  (cd $W/sources/misteross && nice python3 scripts/build_pc386_firmware.py --download > $P/g1-firmware-$name.log 2>&1) || { echo "$name firmware-fail"; exit 3; }
  cp $S/de10_netlist.py $W/docs/validation/2026-11-23-de25-486sx/de10_netlist.py
  echo "$name $(git -C $W rev-parse HEAD) netlist $(cd $W && python3 -I docs/validation/2026-11-23-de25-486sx/de10_netlist.py $P/g1-$name)"
  git -C $W checkout -q -- docs
  echo "$name design-id $(python3 -I $S/de10_design_id.py $W)"
done
cmp $P/g1-base/pc386_system.v $P/g1-head/pc386_system.v && echo "CMP EQUAL" || echo "CMP DIFFER"
