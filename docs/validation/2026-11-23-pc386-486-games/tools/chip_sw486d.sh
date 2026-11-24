#!/bin/bash
# chip_sw486d.sh: Shadow Warrior shareware (SB 2.0) answering Y to "Continue anyway in standard
# 320 x 200?", on the level-4 oracle and on the RTL at I486 = 1 (no 387) from the oracle's state
# at 20 s. Verilator 5.032. The oracle and RTL runs are different testbench builds; each builds
# its own before the two run together. Exits nonzero if either run fails.
cd /data/projects/fes-486sx-chip/wt/sources/misteross || exit 2
export FES_TOOLCHAIN_CACHE_ROOT=$HOME/fes-toolchain-cache VERILATOR=/usr/local/bin/verilator
S=$HOME/fes-scratch/duke486; L=$S/chip-logs
KEYS="--game-keys 6:| --game-keys 9:  --game-keys 12:| --game-keys 24:y --game-keys 25:Y --game-keys 30:| --game-keys 34:|"
sim() { nice python3 scripts/sim_fes_pc386.py --suite game --game swshar --drive $S/sw-vga0.img --cpu-level 4 --instruction-census --frame-every 1 $KEYS --jobs 4 "$@"; }
sim --cpu oracle --x87-fitted --limit-seconds 1 > $L/swy-warmup-l4.log 2>&1; [ $? -le 1 ] || exit 3
sim --cpu rtl --limit-seconds 1 > $L/swy-warmup-rtl.log 2>&1; [ $? -le 1 ] || exit 3
( sim --cpu oracle --x87-fitted --limit-seconds 70 > $L/swy-l4.log 2>&1; rc=$?; echo "swy-l4 rc=$rc" >> $L/done.txt; exit $rc ) & a=$!
( sim --cpu rtl --rtl-from 20 --limit-seconds 70 > $L/swy-rtl.log 2>&1; rc=$?; echo "swy-rtl rc=$rc" >> $L/done.txt; exit $rc ) & b=$!
wait $a; ra=$?; wait $b; rb=$?
echo "ALLDONE3 l4=$ra rtl=$rb" >> $L/done.txt
[ $ra -eq 0 ] && [ $rb -eq 0 ]
