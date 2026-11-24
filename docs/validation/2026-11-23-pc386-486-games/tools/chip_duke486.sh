#!/bin/bash
# chip_duke486.sh: Duke Nukem 3D (chained VGA) with the RTL at I486 = 1 (no 387) taking over from
# the level-4 oracle at 18 s, before the BSWAP at about 21.5 s. Verilator 5.032.
cd /data/projects/fes-486sx-chip/wt/sources/misteross
export FES_TOOLCHAIN_CACHE_ROOT=$HOME/fes-toolchain-cache VERILATOR=/usr/local/bin/verilator
S=$HOME/fes-scratch/duke486; L=$S/chip-logs
nice python3 scripts/sim_fes_pc386.py --suite game --game duke3d --drive $S/duke-vga0.img --cpu rtl --cpu-level 4 --rtl-from 18 --instruction-census --frame-every 1 --limit-seconds 50 --jobs 4 > $L/duke-rtlfrom18.log 2>&1
echo "duke-rtlfrom18 rc=$?" >> $L/done.txt
