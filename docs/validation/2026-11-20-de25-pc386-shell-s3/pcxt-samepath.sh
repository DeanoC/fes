#!/bin/bash
# pcxt-samepath.sh: Claude #2036 D12: C for the PC/XT at A's own output path (A kept as A6-pcxt.orig).
set -u
D=/data/projects/fes-de25-pc386/d12; L=$D/pcxt-samepath.log
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
df -BG --output=avail /data | tail -1 | tr -dc 0-9 | awk '{ if ($1 < 30) exit 1 }' || { echo disk-fail >> $L; echo DONE >> $L; exit 2; }
[ -d $D/A6-pcxt.orig ] || mv $D/A6-pcxt $D/A6-pcxt.orig
wb=$D/wt6-branch/sources/misteross
echo "start $(date -u +%FT%TZ)" >> $L
(cd $wb && flock -w 36000 /tmp/lander-quartus-3090.lock timeout 4h python3 $D/wt6-branch/docs/validation/2026-11-20-de25-pc386-shell-s3/d12_repro.py $wb boards.de25nano.producers.build_pcxt_package c7b4df32d5efb00e7fa1fafefd9aabca -- --quartus-root $Q --vendor-archive $Z --license-file $LM_LICENSE_FILE --model xt --output $D/A6-pcxt > $D/C6s-pcxt.log 2>&1)
echo "C-samepath exit=$? $(date -u +%FT%TZ) rbf $(sha256sum $D/A6-pcxt/core.rbf 2>&1 | cut -c1-64) A $(sha256sum $D/A6-pcxt.orig/core.rbf | cut -c1-64)" >> $L
mv $D/A6-pcxt $D/C6s-pcxt; mv $D/A6-pcxt.orig $D/A6-pcxt
echo DONE >> $L
