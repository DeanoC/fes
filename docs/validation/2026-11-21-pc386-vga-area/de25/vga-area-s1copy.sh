#!/bin/bash
# vga-area-s1copy.sh: arm seed 1 as a copy of the arm's generated project (netlist aef0daf9), SEED 1, as seeds 2-4 were
# made. No Yosys. Holds Powerboat's Pro slot pro-1. Rerun because the board build's final identity guard fired during the
# shared core.worktree incident (22:06:46Z).
set -u
P=$HOME/fes-scratch/vga-area; L=$P/board-arm-s1.log; SRC=$P/board-arm-out/combined/hps/project
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat
echo "start $(date -u +%FT%TZ) copy of $SRC" > $L
D=$P/board-arm-s1; rm -rf $D; mkdir -p $D
rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox $SRC/ $D/project/
grep -q "^set_global_assignment -name SEED 1$" $D/project/golden_top.qsf || { echo "s1 seed-check-fail" >> $L; exit 2; }
echo "s1 netlist $(sha256sum $D/project/fes_mailbox/pc386_system.v | cut -c1-64)" >> $L
(cd $D/project && flock /tmp/fes-quartus-pro-1.lock timeout 4h $Q/bin/quartus_sh --flow compile golden_top > $D/compile.log 2>&1)
echo "s1 exit=$? $(date -u +%FT%TZ)" >> $L
echo DONE >> $L
