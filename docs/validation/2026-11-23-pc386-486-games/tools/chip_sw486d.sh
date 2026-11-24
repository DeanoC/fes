#!/bin/bash
# chip_sw486d.sh: Shadow Warrior shareware (SB 2.0) answering Y to "Continue anyway in standard
# 320 x 200?", on the level-4 oracle and on the RTL at I486 = 1 (no 387). Verilator 5.032.
cd /data/projects/fes-486sx-chip/wt/sources/misteross
export FES_TOOLCHAIN_CACHE_ROOT=$HOME/fes-toolchain-cache VERILATOR=/usr/local/bin/verilator
S=$HOME/fes-scratch/duke486; L=$S/chip-logs
KEYS="--game-keys 6:| --game-keys 9:  --game-keys 12:| --game-keys 24:y --game-keys 25:Y --game-keys 30:| --game-keys 34:|"
( nice python3 scripts/sim_fes_pc386.py --suite game --game swshar --drive $S/sw-vga0.img --cpu oracle --cpu-level 4 --x87-fitted --instruction-census --frame-every 1 --limit-seconds 70 $KEYS --jobs 4 > $L/swy-l4.log 2>&1; echo "swy-l4 rc=$?" >> $L/done.txt ) &
sleep 60
( nice python3 scripts/sim_fes_pc386.py --suite game --game swshar --drive $S/sw-vga0.img --cpu rtl --cpu-level 4 --rtl-from 20 --instruction-census --frame-every 1 --limit-seconds 70 $KEYS --jobs 4 > $L/swy-rtl.log 2>&1; echo "swy-rtl rc=$?" >> $L/done.txt ) &
wait; echo ALLDONE3 >> $L/done.txt
