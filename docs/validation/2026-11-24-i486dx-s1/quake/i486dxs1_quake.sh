#!/bin/bash
# i486dxs1_quake.sh: method G5. Quake on the DX's RTL (wt-quake: the DX head 494479998 + the x87 fired fix,
# a scratch merge), round 2's command line, --rtl-at-prompt 1 --rtl-for 20, frames every 1.0 s.
set -uo pipefail
P=/data/projects/i486dx-s1; WT=$P/wt-quake; O=$P/quake
cd $WT/sources/misteross
git -C $WT diff --stat > $O/patch-applied.txt; git -C $WT rev-parse HEAD > $O/head.txt
date -u +%FT%TZ > $O/heartbeat
timeout 4h python3 scripts/sim_fes_pc386.py --suite cpu-profile --cpu-core i486dx --download --jobs 6 > $O/build.log 2>&1; echo "build rc=$?" >> $O/status
M=build/sim/fes-pc386/machine-i486dx/obj/Vpc386_machine; F=build/firmware/pc386
sha256sum $M > $O/binary.sha256
cp /home/deano/fes-drives/pc386/by-name/bench.img $O/bench.img; date -u +%FT%TZ > $O/heartbeat
timeout 12h $M --bios $F/pc386-bios.bin --vgabios $F/pc386-vgabios.bin --disk $O/bench.img --cpu rtl --x87 1 \
  --cpu-profile $O/cpu.prof --limit-seconds 300 --type 'cd \marks\quakes|quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio|' \
  --rtl-at-prompt 1 --rtl-for 20 --cpu-profile-every 0.5 --frames $O/frames --frame-every 1.0 > $O/quake.log 2>&1; echo "quake rc=$?" >> $O/status
rm -f $O/bench.img; date -u +%FT%TZ > $O/heartbeat; echo SIM_DONE >> $O/status
