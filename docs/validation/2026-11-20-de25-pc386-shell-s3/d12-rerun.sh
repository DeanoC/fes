#!/bin/bash
# d12-rerun.sh BRANCHREV: Claude #2035, D12 B and C again for Spectrum 48/128 and ST at the fixed branch head
# (the first batch's B/C failed on spec.board.MACHINE_HZ). Waits for the first batch's DONE; one job at a time.
set -u
BR=$1; P=/data/projects/fes-de25-pc386/d12; L=$P/d12-rerun.log
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
until tail -1 $P/d12.log | grep -q DONE; do sleep 60; done
hb(){ while sleep 300; do echo "heartbeat $(date -u +%FT%TZ) load $(cut -d' ' -f1 /proc/loadavg)" >> $L; done; }
hb & HB=$!; trap 'kill $HB 2>/dev/null' EXIT
df -BG --output=avail /data | tail -1 | tr -dc 0-9 | awk '{ if ($1 < 60) exit 1 }' || { echo disk-fail >> $L; echo DONE >> $L; exit 2; }
git -C /data/projects/fes-claude fetch -q origin "$BR" 2>/dev/null || git -C /data/projects/fes-claude fetch -q origin
[ -d $P/wt-branch2 ] || git -C /data/projects/fes-claude worktree add -q --detach $P/wt-branch2 $BR || { echo "wt-fail" >> $L; echo DONE >> $L; exit 2; }
(cd $P/wt-branch2 && test -z "$(git status --porcelain)" && test "$(git rev-parse HEAD)" = "$(git rev-parse $BR)") || { echo "tree-fail" >> $L; echo DONE >> $L; exit 2; }
echo "start $(date -u +%FT%TZ) branch $BR" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $HOME/.altera.quartus/quartus2_lic.dat"
w=$P/wt-branch2/sources/misteross
for job in "spectrum48 boards.de25nano.producers.build_spectrum_package 48" "spectrum128 boards.de25nano.producers.build_spectrum_package 128" "st boards.de25nano.producers.build_atari_st_package -"; do
  set -- $job; pkg=$1; mod=$2; m=""; [ "$3" != "-" ] && m="--model $3"
  (cd $w && timeout 6h python3 -m $mod $COMMON $m --output $w/build/d12/B2-$pkg > $P/B2-$pkg.log 2>&1); echo "B2 $pkg exit=$? $(date -u +%FT%TZ)" >> $L
done
for job in "spectrum48 boards.de25nano.producers.build_spectrum_package 48" "spectrum128 boards.de25nano.producers.build_spectrum_package 128" "st boards.de25nano.producers.build_atari_st_package -"; do
  set -- $job; pkg=$1; mod=$2; m=""; [ "$3" != "-" ] && m="--model $3"
  id=$(python3 -c "import json;print(json.load(open('$P/A-$pkg/build-summary.json'))['build_id'])" 2>/dev/null) || { echo "C2 $pkg no-main-id" >> $L; continue; }
  (cd $w && timeout 4h python3 $P/d12_repro.py $w $mod $id -- $COMMON $m --output $P/C2-$pkg > $P/C2-$pkg.log 2>&1); echo "C2 $pkg exit=$? id=$id $(date -u +%FT%TZ)" >> $L
done
echo DONE >> $L
