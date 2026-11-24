#!/bin/bash
# chip_reset486.sh: the from-reset RTL runs of the 486 games record's rule: the RTL at I486 = 1, no 387,
# no handover. Duke Nukem 3D (chained VGA) to 30 s; Shadow Warrior (SB 2.0, Y at its 320x200
# prompt) to 36 s. Verilator 5.032. A one-second warm-up builds the shared testbench first; the
# script exits nonzero unless both runs exit 0 and print "PASS game".
cd /data/projects/fes-486sx-chip/wt/sources/misteross || exit 2
export FES_TOOLCHAIN_CACHE_ROOT=$HOME/fes-toolchain-cache VERILATOR=/usr/local/bin/verilator
S=$HOME/fes-scratch/duke486; L=$S/chip-logs
KEYS="--game-keys 6:| --game-keys 9:  --game-keys 12:| --game-keys 24:y --game-keys 25:Y --game-keys 30:| --game-keys 34:|"
sim() { nice python3 scripts/sim_fes_pc386.py --suite game --cpu rtl --cpu-level 4 --instruction-census --frame-every 1 --jobs 4 "$@"; }
sim --game duke3d --drive $S/duke-vga0.img --limit-seconds 1 > $L/reset-warmup.log 2>&1; rc=$?
[ $rc -le 1 ] || { echo "warm-up rc=$rc" >> $L/done.txt; echo ALLDONE-RESET >> $L/done.txt; exit $rc; }
( sim --game duke3d --drive $S/duke-vga0.img --limit-seconds 30 > $L/duke-reset.log 2>&1; rc=$?
  echo "duke-reset rc=$rc" >> $L/done.txt; [ $rc -eq 0 ] && grep -qx 'PASS game' $L/duke-reset.log ) & d=$!
( sim --game swshar --drive $S/sw-vga0.img --limit-seconds 36 $KEYS > $L/sw-reset.log 2>&1; rc=$?
  echo "sw-reset rc=$rc" >> $L/done.txt; [ $rc -eq 0 ] && grep -qx 'PASS game' $L/sw-reset.log ) & w=$!
wait $d; rd=$?; wait $w; rw=$?
echo "ALLDONE-RESET duke=$rd sw=$rw" >> $L/done.txt
[ $rd -eq 0 ] && [ $rw -eq 0 ]
