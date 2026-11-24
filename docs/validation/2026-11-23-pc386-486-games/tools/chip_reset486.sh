#!/bin/bash
# chip_reset486.sh: the from-reset RTL runs of the 486 games record's rule: the RTL at I486 = 1, no 387,
# no handover. Duke Nukem 3D (chained VGA) to 30 s; Shadow Warrior (SB 2.0, Y at its 320x200
# prompt) to 36 s. Verilator 5.032.
cd /data/projects/fes-486sx-chip/wt/sources/misteross
export FES_TOOLCHAIN_CACHE_ROOT=$HOME/fes-toolchain-cache VERILATOR=/usr/local/bin/verilator
S=$HOME/fes-scratch/duke486; L=$S/chip-logs
KEYS="--game-keys 6:| --game-keys 9:  --game-keys 12:| --game-keys 24:y --game-keys 25:Y --game-keys 30:| --game-keys 34:|"
( nice python3 scripts/sim_fes_pc386.py --suite game --game duke3d --drive $S/duke-vga0.img --cpu rtl --cpu-level 4 --instruction-census --frame-every 1 --limit-seconds 30 --jobs 4 > $L/duke-reset.log 2>&1; echo "duke-reset rc=$?" >> $L/done.txt ) &
sleep 120
( nice python3 scripts/sim_fes_pc386.py --suite game --game swshar --drive $S/sw-vga0.img --cpu rtl --cpu-level 4 --instruction-census --frame-every 1 --limit-seconds 36 $KEYS --jobs 4 > $L/sw-reset.log 2>&1; echo "sw-reset rc=$?" >> $L/done.txt ) &
wait; echo ALLDONE-RESET >> $L/done.txt
