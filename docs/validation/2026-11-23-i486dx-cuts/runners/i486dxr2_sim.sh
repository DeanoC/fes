#!/bin/bash
# i486dxr2_sim.sh VARIANT: lockstep subset, x87 unit bench (x87 variants and base), latency bench,
# DOOM and 3DBench profile windows (non-x87 variants) on main dafabb6db + VARIANT patch.
set -uo pipefail
V=$1; P=/data/projects/i486dx-plan; R2=$P/r2; PATCHES=$P/rec-r2/docs/validation/2026-11-23-i486dx-cuts/patches
C=/home/deano/fes/out/cache/misteross-toolchains; BASE=dafabb6db; WT=$P/wt-r2-sim-$V; O=$R2/sims/$V
# The 3DBench window (method.md C2) is read from $R2/t3d.env (T3D_KEYS, T3D_FROM, T3D_LIMIT), written from
# the dated amendment that fixes it; the run waits for that file, so no window runs before the amendment.
mkdir -p $O
free=$(df --output=avail -B1G /data | tail -1); [ "$free" -ge 30 ] || { echo "refusing: ${free}G free"; exit 3; }
if [ ! -d "$WT" ]; then
  flock $P/worktree.lock git -C /home/deano/fes worktree add -q --detach "$WT" $BASE || exit 4
  [ "$V" = base ] || git -C "$WT" apply "$PATCHES/$V.patch" || exit 5
fi
git -C "$WT" diff --stat > $O/patch-applied.txt
cd "$WT/sources/misteross"
hb(){ date -u +%FT%TZ > $O/heartbeat; }
case $V in xshift|xdisp|x87) X87=1; CPI=0;; cuts) X87=1; CPI=1;; base) X87=1; CPI=1;; *) X87=0; CPI=1;; esac
if [ "$V" != base ]; then
  hb; timeout 4h python3 scripts/test_fes_i386_lockstep.py --download --cache-root $C --random-programs 500 --random-seeds 8 \
    --limit-per-file 20 --jobs 4 > $O/lockstep.log 2>&1; echo "lockstep rc=$?" >> $O/status
  cp build/sim/fes-i386-lockstep/report.json $O/lockstep-report.json 2>/dev/null
fi
if [ $X87 = 1 ]; then
  hb; timeout 4h python3 scripts/test_fes_i386_x87_unit.py --download --cache-root $C > $O/x87unit.log 2>&1; echo "x87unit rc=$?" >> $O/status
fi
# base's lockstep, latency and DOOM window are the plan record's; base runs the x87 unit bench and 3DBench here.
if [ "$V" != base ]; then
hb; timeout 4h python3 scripts/test_fes_i386_latency.py --download --cache-root $C > $O/latency.log 2>&1; echo "latency rc=$?" >> $O/status
cp build/sim/fes-i386-latency/report.json $O/latency-report.json 2>/dev/null
fi
if [ $CPI = 1 ]; then
  hb; timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --download --jobs 4 > $O/cpuprof-suite.log 2>&1; echo "cpuprof-suite rc=$?" >> $O/status
  M=build/sim/fes-pc386/machine/obj/Vpc386_machine; F=build/firmware/pc386
  until [ -f $R2/t3d.env ]; do hb; sleep 60; done; . $R2/t3d.env; cp $R2/t3d.env $O/
  hb; cp /home/deano/fes-drives/pc386/by-name/bench.img $O/bench.img
  timeout 8h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/bench.img --cpu rtl \
    --cpu-profile $O/cpu-3d.prof --limit-seconds $T3D_LIMIT --type 'cd \marks\3dbench|3dbench|' --keys "$T3D_KEYS" \
    --rtl-from $T3D_FROM --cpu-profile-every 0.5 > $O/3dbench.log 2>&1; echo "3dbench rc=$?" >> $O/status
  rm -f $O/bench.img
  if [ "$V" != base ]; then
  hb; cp /home/deano/fes-drives/pc386/by-name/pmode.img $O/pmode.img
  timeout 8h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/pmode.img --cpu rtl \
    --cpu-profile $O/cpu.prof --limit-seconds 300 --type 'cd \doom|doom -timedemo demo3|' \
    --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5 > $O/doom.log 2>&1; echo "doom rc=$?" >> $O/status
  rm -f $O/pmode.img
  fi
fi
hb; echo SIM_DONE >> $O/status
