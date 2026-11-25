#!/bin/bash
# fitrung.sh REV SEED...: method.md's board fits of one rung on Powerboat, Quartus Pro 26.3.
# REV is a pushed rung commit (or the method commit, the 61.44 MHz control). Seed 1 is the
# registered producer (build_pc386_package.py) from a clean worktree of REV; its exit status
# and log are kept whatever it is, because a refusal's reason is a result. Seeds 2-4 are copies
# of seed 1's generated project with only SEED changed. Every Quartus program runs under a Pro
# slot of the Quartus lease (quartus_jobs.py run), one slot per seed, each capped at 300 min.
# After each seed, paths.tcl runs on the kept project under its own slot.
set -u
REV=$1; shift
P=/data/projects/fes-486sx-fmax; REPO=/home/deano/fes
R=$(git -C $REPO rev-parse "$REV") || exit 2
WT=$P/wt-${R:0:9}; L=$P/fit-${R:0:9}.log
HERE=$(cd "$(dirname "$0")" && pwd)
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat YOSYS_MAX_THREADS=1
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
ZIP=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
mkdir -p $P
log() { echo "$* $(date -u +%FT%TZ)" >> $L; }
log "start rev $R seeds $*"
[ -d $WT ] || git -C $REPO worktree add -q --detach $WT $R || { log wt-fail; exit 2; }
cd $WT && test -z "$(git status --porcelain)" || { log tree-fail; exit 2; }
[ "$(git rev-parse HEAD)" = "$R" ] || { log rev-mismatch; exit 2; }
cd sources/misteross
JOBS="python3 $WT/sources/misteross/scripts/quartus_jobs.py run --edition pro --cap-minutes 300"
OUT=$WT/sources/misteross/build/de25-pc386-fmax-s1
SRC=$OUT/signoff/combined/hps/project
sta() {  # sta LABEL PROJECTDIR: the path report, under its own slot
  $JOBS --label fmax-sta-$1 -- bash -c "cd $2 && $Q/bin/quartus_sta -t $HERE/paths.tcl golden_top $2/paths" \
    > $2/paths.log 2>&1
  log "$1 paths exit=$?"
}
for seed in "$@"; do
  if [ "$seed" = 1 ]; then
    nice python3 scripts/build_pc386_firmware.py --download > $P/firmware-${R:0:9}.log 2>&1 || { log firmware-fail; exit 3; }
    rm -rf $OUT
    $JOBS --label fmax-${R:0:9}-s1 -- python3 boards/de25nano/producers/build_pc386_package.py \
      --vendor-archive $ZIP --cache-root $FES_TOOLCHAIN_CACHE_ROOT --gpu-router HIP \
      --output $OUT --package-output $P/packages-${R:0:9} > $P/s1-${R:0:9}.log 2>&1
    log "s1 producer exit=$?"
    [ -f $SRC/output_files/golden_top.sta.rpt ] || { log "s1 no-sta-report"; continue; }
    log "s1 netlist $(sha256sum $SRC/fes_mailbox/pc386_system.v | cut -c1-64)"
    D=$P/s1-${R:0:9}; rm -rf $D; mkdir -p $D; ln -sfn $SRC $D/project
    sta s1 $SRC
  else
    [ -f $SRC/golden_top.qsf ] || { log "s$seed no-seed1-project"; continue; }
    D=$P/s$seed-${R:0:9}; rm -rf $D; mkdir -p $D
    rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox --exclude paths $SRC/ $D/project/
    sed -i "s/^set_global_assignment -name SEED 1\$/set_global_assignment -name SEED $seed/" $D/project/golden_top.qsf
    grep -q "^set_global_assignment -name SEED $seed\$" $D/project/golden_top.qsf || { log "s$seed seed-edit-fail"; continue; }
    log "s$seed netlist $(sha256sum $D/project/fes_mailbox/pc386_system.v | cut -c1-64)"
    $JOBS --label fmax-${R:0:9}-s$seed -- bash -c "cd $D/project && $Q/bin/quartus_sh --flow compile golden_top" > $D/compile.log 2>&1
    log "s$seed exit=$?"
    [ -f $D/project/output_files/golden_top.sta.rpt ] && sta s$seed $D/project
  fi
done
log DONE
