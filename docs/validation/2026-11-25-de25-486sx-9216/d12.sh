#!/bin/bash
# d12.sh MAINREV BRANCHREV PHASES: method.md amendment 1's G7, as #2036's d12-run6.sh ran it, on Powerboat.
# For MENU, ZX81, Spectrum 48/128, ST, C64 pal and PC/XT xt: A (main, --compile-only), B (this branch, full
# producer and gates), C (this branch's tooling with main's BUILD_ID forced, #2036's d12_repro.py). PHASES
# is any of A, B, C (e.g. "A C" or "B"). Each producer runs under a Pro slot of the Quartus lease
# (quartus_jobs.py run, 300-minute cap); producers that lease inside count the outer slot as theirs.
set -u
MAIN=$1; BR=$2; PHASES=$3; P=/data/projects/fes-486sx-9216/d12; REPO=/home/deano/fes; L=$P/d12-${PHASES// /}.log
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains FES_GPU_ROUTER=HIP
mkdir -p $P
fail(){ echo "$1" >> $L; echo DONE >> $L; exit 2; }
for side in main:$MAIN branch:$BR; do n=${side%%:*}; r=${side#*:}
  [ -d $P/wt-$n ] || git -C $REPO worktree add -q --detach $P/wt-$n $r 2>/dev/null || [ -d $P/wt-$n ] || fail "wt-fail $n"
  (cd $P/wt-$n && test -z "$(git status --porcelain)" && test "$(git rev-parse HEAD)" = "$(git -C $REPO rev-parse $r^{commit})") || fail "tree-fail $n"
done
echo "start $PHASES $(date -u +%FT%TZ) main $(git -C $P/wt-main rev-parse HEAD) branch $(git -C $P/wt-branch rev-parse HEAD)" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $LM_LICENSE_FILE"
wb=$P/wt-branch/sources/misteross; wm=$P/wt-main/sources/misteross
REPRO=$P/d12_repro.py; git -C $REPO show $MAIN:docs/validation/2026-11-20-de25-pc386-shell-s3/d12_repro.py > $REPRO
JOBS="menu boards.de25nano.producers.build_menu_package -
zx81 boards.de25nano.producers.build_zx81_package -
spectrum48 boards.de25nano.producers.build_spectrum_package 48
spectrum128 boards.de25nano.producers.build_spectrum_package 128
st boards.de25nano.producers.build_atari_st_package -
c64 boards.de25nano.producers.build_c64_package pal
pcxt boards.de25nano.producers.build_pcxt_package xt"
for phase in $PHASES; do
  echo "$JOBS" | while read pkg mod model; do
    m=""; [ "$model" != "-" ] && m="--model $model"
    case $phase in
      A) w=$wm; out=$P/A-$pkg; args="--output $out --compile-only"; cmd="python3 -m $mod $COMMON $m $args" ;;
      B) w=$wb; out=$wb/build/d12/B-$pkg; cmd="python3 -m $mod $COMMON $m --output $out" ;;
      C) id=$(python3 -c "import json;print(json.load(open('$P/A-$pkg/build-summary.json'))['build_id'])" 2>/dev/null) || { echo "C $pkg no-main-id" >> $L; continue; }
         w=$wb; out=$P/C-$pkg; cmd="python3 $REPRO $wb $mod $id -- $COMMON $m --output $out" ;;
    esac
    rm -rf $out
    (cd $w && python3 $w/scripts/quartus_jobs.py run --edition pro --cap-minutes 300 --label d12-$phase-$pkg -- $cmd > $P/$phase-$pkg.log 2>&1)
    echo "$phase $pkg exit=$? $(date -u +%FT%TZ)" >> $L
  done
done
echo DONE >> $L
