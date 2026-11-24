#!/bin/bash
# census486_l4.sh: level-N oracle census of the 486 candidates (scratch, 23 Nov 2026)
LEVEL=${1:-4}
cd /home/deano/fes/.claude/worktrees/de25-486sx-cfe307/sources/misteross
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
S=~/fes-scratch/duke486; LOG=$S/logs-l$LEVEL; mkdir -p $LOG
run() { # id drive limit
  FES_PC386_DRIVES=$S/store nice python3 scripts/sim_fes_pc386.py --suite game --game $1 --drive $2 --cpu oracle \
    --cpu-level $LEVEL --x87-fitted --instruction-census --frame-every 1 --limit-seconds $3 --jobs 2 > $LOG/$1-$(basename $2 .img).log 2>&1
  echo "$1 $(basename $2) rc=$?" >> $LOG/done.txt
}
OUT=build/sim/fes-pc386$([ $LEVEL = 4 ] && echo -level4)
( run duke3d $S/duke-vga0.img 50; mv $OUT/game-duke3d-x87 $S/runs-l$LEVEL-duke-vga0
  run duke3d $S/duke-vga2.img 50; mv $OUT/game-duke3d-x87 $S/runs-l$LEVEL-duke-vga2 ) &
sleep 90
for t in war2 swshar z; do run $t $S/store/by-name/c486a.img 30 & sleep 60; done
for t in rally pslave; do run $t $S/store/by-name/c486b.img 30 & sleep 60; done
wait; echo ALLDONE >> $LOG/done.txt
