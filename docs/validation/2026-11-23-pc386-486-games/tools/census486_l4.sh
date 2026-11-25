#!/bin/bash
# census486_l4.sh: level-N oracle census of the 486 candidates (scratch, 23 Nov 2026).
# One synchronous warm-up run (which must reach its limit) builds machine/ before the parallel runs share it. Each run's
# status is its own exit (1 for a FAIL verdict, which a census expects for some titles; other
# codes are infrastructure failures). The script exits nonzero if the warm-up or any run fails.
LEVEL=${1:-4}
cd /home/deano/fes/.claude/worktrees/de25-486sx-cfe307/sources/misteross || exit 2
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
S=~/fes-scratch/duke486; LOG=$S/logs-l$LEVEL; mkdir -p $LOG
run() { # id drive limit
  local name; name=$(basename "$2" .img)
  FES_PC386_DRIVES=$S/store nice python3 scripts/sim_fes_pc386.py --suite game --game $1 --drive $2 --cpu oracle \
    --cpu-level $LEVEL --x87-fitted --instruction-census --frame-every 1 --limit-seconds $3 --jobs 2 > $LOG/$1-$name.log 2>&1
  local rc=$?
  echo "$1 $name rc=$rc" >> $LOG/done.txt
  return $rc
}
OUT=build/sim/fes-pc386$([ $LEVEL = 4 ] && echo -level4)
# Warm-up: build the shared testbench once, synchronously, before anything runs in parallel.
# A sim exits 1 both for a FAIL verdict and for a build or setup error, so the exit cannot tell
# them apart: the warm-up counts only if the testbench ran to its one-second limit.
run duke3d $S/duke-vga0.img 1
grep -q "^END {'reason': 'limit'" $LOG/duke3d-duke-vga0.log ||
  { echo "warm-up did not reach its limit" >> $LOG/done.txt; echo ALLDONE >> $LOG/done.txt; exit 3; }
rm -rf $OUT/game-duke3d-x87
pids=()
( run duke3d $S/duke-vga0.img 50; a=$?; mv $OUT/game-duke3d-x87 $S/runs-l$LEVEL-duke-vga0 || a=2
  run duke3d $S/duke-vga2.img 50; b=$?; mv $OUT/game-duke3d-x87 $S/runs-l$LEVEL-duke-vga2 || b=2
  exit $(( a > b ? a : b )) ) & pids+=($!)
for t in war2 swshar z; do run $t $S/store/by-name/c486a.img 30 & pids+=($!); done
for t in rally pslave; do run $t $S/store/by-name/c486b.img 30 & pids+=($!); done
worst=0
for p in "${pids[@]}"; do wait $p; rc=$?; [ $rc -gt $worst ] && worst=$rc; done
echo "ALLDONE worst=$worst" >> $LOG/done.txt
exit $worst
