#!/bin/bash
# d12-spectrum-rerun.sh BASEREV ARMREV: the rerun of D12's "B's gates pass" rule for the DE25 Spectrum 48
# and 128 packages once #2162's fix (#2163) is in #2036. The full producer (its gates) runs on the base and
# on the arm, as d12-slat.sh's B phase. One job at a time, each holding a Pro slot of the Quartus lease.
set -u
BASE=$1; ARM=$2; P=$HOME/fes-scratch/slat-spec-rerun; L=$P/rerun.log; REPO=/home/deano/fes
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
mkdir -p $P
for side in base:$BASE arm:$ARM; do n=${side%%:*}; r=${side#*:}
  [ -d $P/wt-$n ] || git -C $REPO worktree add -q --detach $P/wt-$n $r || { echo "wt-fail $n" >> $L; echo DONE >> $L; exit 2; }
  (cd $P/wt-$n && test -z "$(git status --porcelain)" && test "$(git rev-parse HEAD)" = "$(git -C $REPO rev-parse $r)") \
    || { echo "tree-fail $n" >> $L; echo DONE >> $L; exit 2; }
done
echo "start $(date -u +%FT%TZ) base $BASE arm $ARM" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $HOME/.altera.quartus/quartus2_lic.dat"
LEASE="python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 600"
for side in base arm; do for model in 48 128; do
  w=$P/wt-$side/sources/misteross; o=$w/build/d12/P-spectrum$model; rm -rf $o
  (cd $w && $LEASE --label slat-rerun-$side-spectrum$model -- python3 -m boards.de25nano.producers.build_spectrum_package \
     $COMMON --model $model --output $o > $P/$side-spectrum$model.log 2>&1)
  echo "$side spectrum$model exit=$? $(date -u +%FT%TZ)" >> $L
done; done
echo DONE >> $L
