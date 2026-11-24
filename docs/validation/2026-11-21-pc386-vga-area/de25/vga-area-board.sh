#!/bin/bash
# vga-area-board.sh TAG REV: pc386 VGA area (method.md), DE25 387-off board build of REV (seed 1), then seeds 2-4
# as copies of its generated project with only SEED changed. Every Quartus run holds a Powerboat Pro lease slot.
set -u
TAG=$1; REV=$2; P=$HOME/fes-scratch/vga-area; WT=$P/wt-$TAG; L=$P/board-$TAG.log; O=$P/board-$TAG-out
REPO=/home/deano/fes/.claude/worktrees/affectionate-chaplygin-e92ab6
Q=/data/altera_pro/26.3/quartus
export QUARTUS_ROOTDIR=$Q LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat
export FES_TOOLCHAIN_CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
# Powerboat's first Pro slot only: Lander's legacy slot stays Lander's (its jobs wait on it with a timeout).
lease() {
  flock /tmp/fes-quartus-pro-1.lock "$@"; rc=$?
  echo "  lease pro-1 rc=$rc $(date -u +%FT%TZ)" >> $L; return $rc
}
echo "start $(date -u +%FT%TZ) tag $TAG rev $REV" >> $L
rm -rf $O
[ -d $WT ] || git -C $REPO worktree add -q --detach $WT $REV || { echo wt-fail >> $L; echo DONE >> $L; exit 2; }
cd $WT && test -z "$(git status --porcelain)" || { echo tree-fail >> $L; echo DONE >> $L; exit 2; }
[ "$(git rev-parse HEAD)" = "$(git -C $REPO rev-parse $REV)" ] || { echo rev-mismatch >> $L; echo DONE >> $L; exit 2; }
echo "vga sha256 $(sha256sum sources/misteross/cores/fes-pc386/rtl/pc386_vga.sv sources/misteross/cores/fes-pc386/rtl/pc386_vga_scanout.sv | awk '{print $1}' | tr '\n' ' ')" >> $L
cd sources/misteross
timeout 3600 nice python3 scripts/build_pc386_firmware.py --download > $P/board-$TAG-firmware.log 2>&1 || { echo firmware-fail >> $L; echo DONE >> $L; exit 3; }
cd boards/de25nano
lease timeout 5h python3 pc386/build.py build --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip --output $O > $P/board-$TAG-build.log 2>&1
echo "build exit=$? $(date -u +%FT%TZ)" >> $L
SRC=$O/combined/hps/project
[ -f $SRC/output_files/golden_top.fit.rpt ] || { echo no-seed1-fit >> $L; echo DONE >> $L; exit 4; }
echo "netlist $(sha256sum $SRC/fes_mailbox/pc386_system.v | cut -c1-64)" >> $L
for seed in 2 3 4; do
  D=$P/board-$TAG-s$seed; rm -rf $D; mkdir -p $D
  rsync -a --exclude qdb --exclude output_files --exclude tmp-clearbox $SRC/ $D/project/
  sed -i "s/^set_global_assignment -name SEED 1\$/set_global_assignment -name SEED $seed/" $D/project/golden_top.qsf
  grep -q "SEED $seed\$" $D/project/golden_top.qsf || { echo "s$seed seed-edit-fail" >> $L; continue; }
  echo "s$seed netlist $(sha256sum $D/project/fes_mailbox/pc386_system.v | cut -c1-64)" >> $L
  (cd $D/project && lease timeout 4h $Q/bin/quartus_sh --flow compile golden_top > $D/compile.log 2>&1)
  echo "s$seed exit=$? $(date -u +%FT%TZ)" >> $L
done
echo DONE >> $L
