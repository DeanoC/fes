#!/bin/bash
# pcxt-arepeat.sh: Claude #2036 D12: main's A for the PC/XT again, at A's own output path (A kept as A6-pcxt.orig).
# Whether main's PC/XT compile reproduces its own RBF from the same inputs at the same path.
set -u
D=/data/projects/fes-de25-pc386/d12; L=$D/pcxt-arepeat.log
Q=/data/altera_pro/26.3/quartus; Z=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
export LM_LICENSE_FILE=$HOME/.altera.quartus/quartus2_lic.dat QUARTUS_ROOTDIR=$Q
df -BG --output=avail /data | tail -1 | tr -dc 0-9 | awk '{ if ($1 < 30) exit 1 }' || { echo disk-fail >> $L; echo DONE >> $L; exit 2; }
[ -d $D/A6-pcxt.orig ] || mv $D/A6-pcxt $D/A6-pcxt.orig
wm=$D/wt6-main/sources/misteross
echo "start $(date -u +%FT%TZ) main $(git -C $D/wt6-main rev-parse HEAD)" >> $L
(cd $wm && flock -w 36000 /tmp/lander-quartus-3090.lock timeout 4h python3 -m boards.de25nano.producers.build_pcxt_package --quartus-root $Q --vendor-archive $Z --license-file $LM_LICENSE_FILE --model xt --output $D/A6-pcxt --compile-only > $D/A6r-pcxt.log 2>&1)
echo "A-repeat exit=$? $(date -u +%FT%TZ) rbf $(sha256sum $D/A6-pcxt/core.rbf 2>&1 | cut -c1-64) A $(sha256sum $D/A6-pcxt.orig/core.rbf | cut -c1-64) id $(python3 -c "import json;print(json.load(open('$D/A6-pcxt/build-summary.json'))['build_id'])")" >> $L
mv $D/A6-pcxt $D/A6r-pcxt; mv $D/A6-pcxt.orig $D/A6-pcxt
echo DONE >> $L
