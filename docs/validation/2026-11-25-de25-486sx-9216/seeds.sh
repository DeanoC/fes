#!/bin/bash
# seeds.sh REV: method.md's G3-G5 on Powerboat. Seed 1 is the registered producer (486sx) from a clean
# worktree of REV; seeds 2-4 are copies of its generated project with only SEED changed, laid out as the
# producer lays out its build, compiled, then admitted by admit_seed.py (the producer's own admission).
# After every seed, #2224's loaded_route.tcl measures LOADED's route. Every Quartus program holds a Pro
# slot of the Quartus lease, capped at 300 minutes.
set -u
REV=$1; P=/data/projects/fes-486sx-9216; REPO=/home/deano/fes; L=$P/seeds.log
R=$(git -C $REPO rev-parse "$REV"); W=$P/wt-g3-${R:0:9}; HERE=$W/docs/validation/2026-11-25-de25-486sx-9216
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat YOSYS_MAX_THREADS=1
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
ZIP=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
log() { echo "$* $(date -u +%FT%TZ)" >> $L; }
log "start rev $R"
[ -d $W ] || git -C $REPO worktree add -q --detach $W $R || { log wt-fail; exit 2; }
[ "$(git -C $W rev-parse HEAD)" = "$R" ] && [ -z "$(git -C $W status --porcelain)" ] || { log tree-fail; exit 2; }
M=$W/sources/misteross; cd $M
JOBS="python3 $M/scripts/quartus_jobs.py run --edition pro --cap-minutes 300"
LOADED=$(git -C $REPO rev-parse evidence/2224-record)
git -C $REPO show $LOADED:docs/validation/2026-11-24-de25-486sx-fmax/loaded_route.tcl > $P/loaded_route.tcl
nice python3 scripts/build_pc386_firmware.py --download > $P/g3-firmware.log 2>&1 || { log firmware-fail; exit 3; }
OUT=$M/build/g3-s1
$JOBS --label g3-s1 -- python3 boards/de25nano/producers/build_pc386_package.py --model 486sx \
  --vendor-archive $ZIP --cache-root $FES_TOOLCHAIN_CACHE_ROOT --gpu-router HIP \
  --output $OUT --package-output $P/packages-g3 > $P/g3-s1.log 2>&1
log "s1 producer exit=$?"
SRC=$OUT/signoff/combined/hps/project
[ -f $SRC/output_files/golden_top.sta.rpt ] || { log "s1 no-sta-report"; exit 4; }
log "s1 netlist $(sha256sum $SRC/fes_mailbox/pc386_system.v | cut -c1-64)"
$JOBS --label g3-loaded-s1 -- bash -c "cd $SRC && $Q/bin/quartus_sta -t $P/loaded_route.tcl golden_top $P/loaded-s1.tsv" > $P/loaded-s1.log 2>&1
log "s1 loaded exit=$?"
for seed in 2 3 4; do
  D=$P/g3-s$seed; rm -rf $D; mkdir -p $D/signoff/combined/hps
  cp $OUT/build-inputs.json $D/build-inputs.json
  rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox $SRC/ $D/signoff/combined/hps/project/
  sed -i "s/^set_global_assignment -name SEED 1\$/set_global_assignment -name SEED $seed/" $D/signoff/combined/hps/project/golden_top.qsf
  grep -q "^set_global_assignment -name SEED $seed\$" $D/signoff/combined/hps/project/golden_top.qsf || { log "s$seed seed-edit-fail"; continue; }
  log "s$seed netlist $(sha256sum $D/signoff/combined/hps/project/fes_mailbox/pc386_system.v | cut -c1-64)"
  $JOBS --label g3-s$seed -- bash -c "cd $D/signoff/combined/hps/project && $Q/bin/quartus_sh --flow compile golden_top" > $D/compile.log 2>&1
  log "s$seed compile exit=$?"
  $JOBS --label g3-admit-s$seed -- python3 $HERE/admit_seed.py $M $D $ZIP > $D/admit.log 2>&1
  log "s$seed admit exit=$?"
  $JOBS --label g3-loaded-s$seed -- bash -c "cd $D/signoff/combined/hps/project && $Q/bin/quartus_sta -t $P/loaded_route.tcl golden_top $P/loaded-s$seed.tsv" > $P/loaded-s$seed.log 2>&1
  log "s$seed loaded exit=$?"
done
log DONE
