#!/bin/bash
# i486dxr2_quake.sh VARIANT: method.md C3, Quake on the RTL with the 387 (bench-x87.patch), handover at the prompt.
set -uo pipefail
V=$1; P=/data/projects/i486dx-plan; R2=$P/r2; PATCHES=$P/rec-r2/docs/validation/2026-11-23-i486dx-cuts/patches
BASE=dafabb6db; WT=$P/wt-r2-quake-$V; O=$R2/sims/quake-$V
mkdir -p $O/frames
free=$(df --output=avail -B1G /data | tail -1); [ "$free" -ge 30 ] || { echo "refusing: ${free}G free"; exit 3; }
if [ ! -d "$WT" ]; then
  flock $P/worktree.lock git -C /home/deano/fes worktree add -q --detach "$WT" $BASE || exit 4
  git -C "$WT" apply "$PATCHES/bench-x87.patch" || exit 5
  [ "$V" = base ] || git -C "$WT" apply "$PATCHES/$V.patch" || exit 5
fi
git -C "$WT" diff --stat > $O/patch-applied.txt
cd "$WT/sources/misteross"
date -u +%FT%TZ > $O/heartbeat
timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --download --jobs 4 > $O/cpuprof-suite.log 2>&1; echo "cpuprof-suite rc=$?" >> $O/status
M=build/sim/fes-pc386/machine/obj/Vpc386_machine; F=build/firmware/pc386
cp /home/deano/fes-drives/pc386/by-name/bench.img $O/bench.img; date -u +%FT%TZ > $O/heartbeat
timeout 10h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/bench.img --cpu rtl --x87 1 \
  --cpu-profile $O/cpu.prof --limit-seconds 300 --type 'cd \marks\quakes|quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio|' \
  --rtl-at-prompt 1 --rtl-for 20 --cpu-profile-every 0.5 --frames $O/frames --frame-every 1.0 > $O/quake.log 2>&1; echo "quake rc=$?" >> $O/status
rm -f $O/bench.img; date -u +%FT%TZ > $O/heartbeat; echo SIM_DONE >> $O/status
