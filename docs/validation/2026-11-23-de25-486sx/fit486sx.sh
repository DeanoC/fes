#!/bin/bash
# fit486sx.sh REV: method.md's four seeds of the DE25 486SX pc386 on Powerboat.
# Seed 1 is the registered producer (build_pc386_package.py) from a clean worktree of REV;
# seeds 2-4 are copies of its generated project with only SEED changed. Each Quartus run
# holds a Pro slot of the Quartus lease (quartus_jobs.py run), one per seed.
set -u
REV=$1
P=/data/projects/fes-486sx; WT=$P/wt-${REV:0:9}; L=$P/fit-${REV:0:9}.log
REPO=/home/deano/fes
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat YOSYS_MAX_THREADS=1
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
ZIP=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
mkdir -p $P
echo "start $(date -u +%FT%TZ) rev $REV" >> $L
[ -d $WT ] || git -C $REPO worktree add -q --detach $WT $REV || { echo wt-fail >> $L; echo DONE >> $L; exit 2; }
cd $WT && test -z "$(git status --porcelain)" || { echo tree-fail >> $L; echo DONE >> $L; exit 2; }
[ "$(git rev-parse HEAD)" = "$(git -C $REPO rev-parse $REV)" ] || { echo rev-mismatch >> $L; echo DONE >> $L; exit 2; }
cd sources/misteross
JOBS="python3 $WT/sources/misteross/scripts/quartus_jobs.py run --edition pro --cap-minutes 300"
nice python3 scripts/build_pc386_firmware.py --download > $P/firmware-${REV:0:9}.log 2>&1 || { echo firmware-fail >> $L; echo DONE >> $L; exit 3; }
OUT=$WT/sources/misteross/build/de25-pc386-486sx-s1
rm -rf $OUT
$JOBS --label 486sx-s1 -- python3 boards/de25nano/producers/build_pc386_package.py \
  --vendor-archive $ZIP --cache-root $FES_TOOLCHAIN_CACHE_ROOT --gpu-router HIP \
  --output $OUT --package-output $P/packages-${REV:0:9} > $P/s1-${REV:0:9}.log 2>&1
RC=$?
echo "s1 producer exit=$RC $(date -u +%FT%TZ)" >> $L
# The producer's gates (timing, signoff review, package admission) are method.md's pass rule:
# a refused seed 1 stops the run; no further seed is fitted.
[ $RC -eq 0 ] || { echo "s1 producer-refused" >> $L; echo DONE >> $L; exit 5; }
SRC=$OUT/signoff/combined/hps/project
[ -f $SRC/output_files/golden_top.fit.rpt ] || { echo no-seed1-fit >> $L; echo DONE >> $L; exit 4; }
echo "s1 netlist $(sha256sum $SRC/fes_mailbox/pc386_system.v | cut -c1-64)" >> $L
for seed in 2 3 4; do
  D=$P/s$seed-${REV:0:9}; rm -rf $D; mkdir -p $D
  rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox $SRC/ $D/project/
  sed -i "s/^set_global_assignment -name SEED 1\$/set_global_assignment -name SEED $seed/" $D/project/golden_top.qsf
  grep -q "^set_global_assignment -name SEED $seed\$" $D/project/golden_top.qsf || { echo "s$seed seed-edit-fail" >> $L; continue; }
  echo "s$seed netlist $(sha256sum $D/project/fes_mailbox/pc386_system.v | cut -c1-64)" >> $L
  $JOBS --label 486sx-s$seed -- bash -c "cd $D/project && $Q/bin/quartus_sh --flow compile golden_top" > $D/compile.log 2>&1
  echo "s$seed exit=$? $(date -u +%FT%TZ)" >> $L
done
echo DONE >> $L
