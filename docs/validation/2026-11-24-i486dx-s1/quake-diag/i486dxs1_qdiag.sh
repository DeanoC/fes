#!/bin/bash
# i486dxs1_qdiag.sh: round 2's Quake bring-up on the base CPU (2d4edf547 + bench x87_present + stall diagnostics).
set -uo pipefail
P=/data/projects/i486dx-s1; WT=$P/wt-qdiag; O=$P/qdiag
cd $WT/sources/misteross
date -u +%FT%TZ > $O/heartbeat
timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --download --jobs 4 > $O/cpuprof-suite.log 2>&1; echo "cpuprof-suite rc=$?" >> $O/status
M=build/sim/fes-pc386/machine/obj/Vpc386_machine; F=build/firmware/pc386
cp /home/deano/fes-drives/pc386/by-name/bench.img $O/bench.img; date -u +%FT%TZ > $O/heartbeat
timeout 8h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/bench.img --cpu rtl --x87 1 \
  --limit-seconds 40 --type 'cd \marks\quakes|quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio|' \
  --rtl-at-prompt 1 --rtl-for 20 --frames $O/frames --frame-every 1.0 > $O/quake.log 2>&1; echo "quake rc=$?" >> $O/status
rm -f $O/bench.img; date -u +%FT%TZ > $O/heartbeat; echo SIM_DONE >> $O/status
