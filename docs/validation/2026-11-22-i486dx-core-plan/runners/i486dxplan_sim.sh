#!/bin/bash
# i486dxplan_sim.sh VARIANT: lockstep subset, latency bench and DOOM profile window on main + VARIANT patch.
set -uo pipefail
V=$1; P=/data/projects/i486dx-plan; REC=/home/deano/fes/.claude/worktrees/happy-payne-d3b9ad/docs/validation/2026-11-22-i486dx-core-plan/patches
C=/home/deano/fes/out/cache/misteross-toolchains; BASE=dafabb6db; WT=$P/wt-sim-$V; O=$P/sims/$V
mkdir -p $O
free=$(df --output=avail -B1G /data | tail -1); [ "$free" -ge 30 ] || { echo "refusing: ${free}G free"; exit 3; }
if [ ! -d "$WT" ]; then
  flock $P/worktree.lock git -C /home/deano/fes worktree add -q --detach "$WT" $BASE || exit 4
  [ "$V" = base ] || git -C "$WT" apply "$REC/$V.patch" || exit 5
fi
git -C "$WT" diff --stat > $O/patch-applied.txt
cd "$WT/sources/misteross"
hb(){ date -u +%FT%TZ > $O/heartbeat; }
hb; echo "lockstep start $(date -u +%FT%TZ)"
python3 scripts/test_fes_i386_lockstep.py --download --cache-root $C --random-programs 500 --random-seeds 8 \
  --limit-per-file 20 --jobs 4 > $O/lockstep.log 2>&1; echo "lockstep rc=$?" | tee -a $O/status
cp build/sim/fes-i386-lockstep/report.json $O/lockstep-report.json 2>/dev/null
hb; python3 scripts/test_fes_i386_latency.py --download --cache-root $C > $O/latency.log 2>&1; echo "latency rc=$?" | tee -a $O/status
cp build/sim/fes-i386-latency/report.json $O/latency-report.json 2>/dev/null
hb; python3 scripts/sim_fes_pc386.py --suite cpu-profile --download --jobs 4 > $O/cpuprof-suite.log 2>&1; echo "cpuprof-suite rc=$?" | tee -a $O/status
hb; cp /home/deano/fes-drives/pc386/by-name/pmode.img $O/pmode.img
M=build/sim/fes-pc386/machine/obj/Vpc386_machine; F=build/firmware/pc386
timeout 4h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/pmode.img --cpu rtl \
  --cpu-profile $O/cpu.prof --limit-seconds 300 --type 'cd \doom|doom -timedemo demo3|' \
  --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5 > $O/doom.log 2>&1; echo "doom rc=$?" | tee -a $O/status
rm -f $O/pmode.img; hb; echo SIM_DONE >> $O/status
