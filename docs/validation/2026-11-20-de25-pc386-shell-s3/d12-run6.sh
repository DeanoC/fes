#!/bin/bash
# d12-run6.sh MAINREV BRANCHREV: Claude #2036, at the final head after merging main:
# the pc386 producer (package), then D12 A (main compile-only), B (branch full producer) and
# C (branch tooling, main's BUILD_ID) for MENU, ZX81, Spectrum 48/128, ST, C64 and PC/XT. One job at a time.
# Every job runs under flock on chip's one Pro slot (the 3090 lock); quartus_tools.quartus_lease
# counts an ancestor's flock as the lease, so producers that lease inside do not wait on it.
set -u
MAIN=$1; BR=$2; P=/data/projects/fes-de25-pc386/d12; L=$P/d12-run6.log
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
LOCK=/tmp/lander-quartus-3090.lock
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
export FES_TOOLCHAIN_CACHE_ROOT=/data/fes-toolchain-cache FES_GPU_ROUTER=CUDA PATH=/usr/local/cuda/bin:$HOME/bin:$PATH
hb(){ while sleep 300; do echo "heartbeat $(date -u +%FT%TZ) load $(cut -d' ' -f1 /proc/loadavg)" >> $L; done; }
hb & HB=$!; trap 'kill $HB 2>/dev/null' EXIT
fail(){ echo "$1" >> $L; echo DONE >> $L; exit 2; }
disk(){ df -BG --output=avail /data | tail -1 | tr -dc 0-9 | awk -v n=$1 '{ if ($1 < n) exit 1 }'; }
disk 60 || fail disk-fail
git -C /data/projects/fes-claude fetch -q origin || fail fetch-fail
for side in main:$MAIN branch:$BR; do n=${side%%:*}; r=${side#*:}
  [ -d $P/wt6-$n ] || git -C /data/projects/fes-claude worktree add -q --detach $P/wt6-$n $r || fail "wt-fail $n"
  (cd $P/wt6-$n && test -z "$(git status --porcelain)" && test "$(git rev-parse HEAD)" = "$(git -C /data/projects/fes-claude rev-parse $r^{commit})") || fail "tree-fail $n"
done
echo "start $(date -u +%FT%TZ) main $(git -C $P/wt6-main rev-parse HEAD) branch $(git -C $P/wt6-branch rev-parse HEAD)" >> $L
COMMON="--quartus-root $Q --vendor-archive $Z --license-file $LM_LICENSE_FILE"
wb=$P/wt6-branch/sources/misteross; wm=$P/wt6-main/sources/misteross
(cd $wb && timeout 3600 python3 scripts/build_pc386_firmware.py --download > $P/P6-firmware.log 2>&1) || fail firmware-fail
(cd $wb && flock -w 36000 $LOCK timeout 8h python3 boards/de25nano/producers/build_pc386_package.py --vendor-archive $Z --cache-root /data/fes-toolchain-cache --gpu-router CUDA --license-file $LM_LICENSE_FILE > $P/P6-pc386.log 2>&1)
echo "P pc386 exit=$? $(date -u +%FT%TZ)" >> $L
lease(){ echo "flock -w 36000 $LOCK"; }
JOBS="menu boards.de25nano.producers.build_menu_package -
zx81 boards.de25nano.producers.build_zx81_package -
spectrum48 boards.de25nano.producers.build_spectrum_package 48
spectrum128 boards.de25nano.producers.build_spectrum_package 128
st boards.de25nano.producers.build_atari_st_package -
c64 boards.de25nano.producers.build_c64_package pal
pcxt boards.de25nano.producers.build_pcxt_package xt"
for phase in A B C; do
  echo "$JOBS" | while read pkg mod model; do
    disk 30 || { echo "$phase $pkg disk-low" >> $L; continue; }
    m=""; [ "$model" != "-" ] && m="--model $model"; lk=$(lease $phase $pkg)
    case $phase in
      A) (cd $wm && $lk timeout 4h python3 -m $mod $COMMON $m --output $P/A6-$pkg --compile-only > $P/A6-$pkg.log 2>&1); echo "A $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
      B) (cd $wb && $lk timeout 6h python3 -m $mod $COMMON $m --output $wb/build/d12/B6-$pkg > $P/B6-$pkg.log 2>&1); echo "B $pkg exit=$? $(date -u +%FT%TZ)" >> $L ;;
      C) id=$(python3 -c "import json;print(json.load(open('$P/A6-$pkg/build-summary.json'))['build_id'])" 2>/dev/null) || { echo "C $pkg no-main-id" >> $L; continue; }
         (cd $wb && $lk timeout 4h python3 $P/wt6-branch/docs/validation/2026-11-20-de25-pc386-shell-s3/d12_repro.py $wb $mod $id -- $COMMON $m --output $P/C6-$pkg > $P/C6-$pkg.log 2>&1); echo "C $pkg exit=$? id=$id $(date -u +%FT%TZ)" >> $L ;;
    esac
  done
done
echo DONE >> $L
