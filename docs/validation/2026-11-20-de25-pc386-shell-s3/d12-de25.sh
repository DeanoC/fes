#!/bin/bash
# d12-de25.sh MAINREV BRANCHREV: Claude #2035, the D12 comparison for the DE25 MENU, ZX81, Spectrum 48/128 and ST
# packages. A: main compile-only; B: branch full producer (its gates); C: branch tooling with main's BUILD_ID.
set -u
MAIN=$1; BR=$2; P=/data/projects/fes-de25-pc386/d12; L=$P/d12.log; mkdir -p $P
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
hb(){ while sleep 300; do echo "heartbeat $(date -u +%FT%TZ) load $(cut -d' ' -f1 /proc/loadavg)" >> $L; done; }
hb & HB=$!; trap 'kill $HB 2>/dev/null' EXIT
df -BG --output=avail /data | tail -1 | tr -dc 0-9 | awk '{ if ($1 < 60) exit 1 }' || { echo disk-fail >> $L; echo DONE >> $L; exit 2; }
for side in main:$MAIN branch:$BR; do n=${side%%:*}; r=${side#*:}
  [ -d $P/wt-$n ] || git -C /data/projects/fes-claude worktree add -q --detach $P/wt-$n $r || { echo "wt-fail $n" >> $L; exit 2; }
  (cd $P/wt-$n && test -z "$(git status --porcelain)") || { echo "tree-fail $n" >> $L; exit 2; }
done
echo "start $(date -u +%FT%TZ) main $MAIN branch $BR" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $HOME/.altera.quartus/quartus2_lic.dat"
job(){ phase=$1; pkg=$2; mod=$3; model=$4
  m=""; [ "$model" != "-" ] && m="--model $model"
  case $phase in
    A) w=$P/wt-main/sources/misteross; o=$P/A-$pkg
       (cd $w && timeout 4h python3 -m $mod $COMMON $m --output $o --compile-only > $P/A-$pkg.log 2>&1); echo "A $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
    B) w=$P/wt-branch/sources/misteross; o=$w/build/d12/B-$pkg
       (cd $w && timeout 6h python3 -m $mod $COMMON $m --output $o > $P/B-$pkg.log 2>&1); echo "B $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
    C) w=$P/wt-branch/sources/misteross; o=$P/C-$pkg
       id=$(python3 -c "import json;print(json.load(open('$P/A-$pkg/build-summary.json'))['build_id'])" 2>/dev/null) || { echo "C $pkg no-main-id" >> $L; return; }
       (cd $w && timeout 4h python3 $P/d12_repro.py $w $mod $id -- $COMMON $m --output $o > $P/C-$pkg.log 2>&1); echo "C $pkg exit=$? id=$id $(date -u +%FT%TZ)" >> $L ;;
  esac
}
export -f job; export P L Q Z COMMON
JOBS="menu boards.de25nano.producers.build_menu_package -
zx81 boards.de25nano.producers.build_zx81_package -
spectrum48 boards.de25nano.producers.build_spectrum_package 48
spectrum128 boards.de25nano.producers.build_spectrum_package 128
st boards.de25nano.producers.build_atari_st_package -"
{ echo "$JOBS" | sed 's/^/A /'; echo "$JOBS" | sed 's/^/B /'; } | xargs -P 1 -L 1 bash -c 'job $0 $1 $2 $3'
echo "$JOBS" | sed 's/^/C /' | xargs -P 1 -L 1 bash -c 'job $0 $1 $2 $3'
echo DONE >> $L
