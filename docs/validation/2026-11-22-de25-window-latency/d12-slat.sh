#!/bin/bash
# d12-slat.sh BASEREV ARMREV PACKAGE...: the D12 comparison for the DE25 packages whose closures hold
# machine/board.py (#2117 S-lat), by #2036's d12.md method. JOBS has a row for each package that
# closure_ids.py lists with board_py_in_closure, under its record name; the arguments after the two
# revisions name the rows to run. A: base compile-only; B: arm full producer (its gates); C: arm
# tooling with A's BUILD_ID forced (#2036's d12_repro.py). One job at a time, each holding a Pro slot
# of the Quartus lease (quartus_jobs.py run). D12_DIR sets the scratch directory.
set -u
BASE=$1; ARM=$2; shift 2; P=${D12_DIR:-$HOME/fes-scratch/slat-d12}; mkdir -p $P; L=$P/d12.log; REPO=/home/deano/fes
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
JOBS="spectrum48 boards.de25nano.producers.build_spectrum_package 48
spectrum128 boards.de25nano.producers.build_spectrum_package 128
spectrumplus3 boards.de25nano.producers.build_spectrum_package plus3
spectrumpentagon boards.de25nano.producers.build_spectrum_package pentagon
spectrumnative48 boards.de25nano.producers.build_spectrum_package native48
st boards.de25nano.producers.build_atari_st_package 520st
c64pal boards.de25nano.producers.build_c64_package pal
c64nativepal boards.de25nano.producers.build_c64_package nativepal
pcxt boards.de25nano.producers.build_pcxt_package xt"
[ $# -gt 0 ] || { echo "no-package: name the JOBS rows to run" >> $L; echo DONE >> $L; exit 2; }
for pkg in "$@"; do
  grep -q "^$pkg " <<< "$JOBS" || { echo "no-package $pkg" >> $L; echo DONE >> $L; exit 2; }
done
PKGS=" $* "
for side in base:$BASE arm:$ARM; do n=${side%%:*}; r=${side#*:}
  [ -d $P/wt-$n ] || git -C $REPO worktree add -q --detach $P/wt-$n $r || { echo "wt-fail $n" >> $L; echo DONE >> $L; exit 2; }
  (cd $P/wt-$n && test -z "$(git status --porcelain)" && test "$(git rev-parse HEAD)" = "$(git -C $REPO rev-parse $r)") \
    || { echo "tree-fail $n" >> $L; echo DONE >> $L; exit 2; }
done
REPRO=$P/wt-base/docs/validation/2026-11-20-de25-pc386-shell-s3/d12_repro.py
echo "start $(date -u +%FT%TZ) base $BASE arm $ARM packages$PKGS" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $HOME/.altera.quartus/quartus2_lic.dat"
LEASE="python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 360"
job(){ phase=$1; pkg=$2; mod=$3; m="--model $4"
  case $phase in
    A) w=$P/wt-base/sources/misteross; o=$P/A-$pkg; rm -rf $o
       (cd $w && $LEASE --label slat-d12-A-$pkg -- python3 -m $mod $COMMON $m --output $o --compile-only > $P/A-$pkg.log 2>&1)
       echo "A $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
    B) w=$P/wt-arm/sources/misteross; o=$w/build/d12/B-$pkg; rm -rf $o
       (cd $w && $LEASE --label slat-d12-B-$pkg -- python3 -m $mod $COMMON $m --output $o > $P/B-$pkg.log 2>&1)
       echo "B $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
    C) w=$P/wt-arm/sources/misteross; o=$P/C-$pkg; rm -rf $o
       id=$(python3 -c "import json;print(json.load(open('$P/A-$pkg/build-summary.json'))['build_id'])" 2>/dev/null) \
         || { echo "C $pkg no-base-id" >> $L; return; }
       (cd $w && $LEASE --label slat-d12-C-$pkg -- python3 $REPRO $w $mod $id -- $COMMON $m --output $o > $P/C-$pkg.log 2>&1)
       echo "C $pkg exit=$? id=$id $(date -u +%FT%TZ)" >> $L ;;
  esac
}
for phase in A B C; do
  while read -r pkg mod model; do
    case "$PKGS" in *" $pkg "*) job $phase $pkg $mod $model ;; esac
  done <<< "$JOBS"
done
echo DONE >> $L
