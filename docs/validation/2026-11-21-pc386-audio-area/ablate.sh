#!/bin/bash
# Scratch ablations (report-only estimates): $1 = adpcm|dc|div. DE10 module-alone fit, Powerboat Lite slot 2.
set -u
D=/home/deano/fes-scratch/audio-area; X=$D/exp-tree; R=$X/sources/misteross/cores/fes-pc386/rtl; T=$1
git -C $X checkout -q -- .
case $T in
  adpcm) sed -i 's/        encoding <= code;/        encoding <= PCM;/' $R/pc386_sb.sv ;;
  dc) sed -i "s/    wire signed \[19:0\] high_pass = 20'(error >>> 8);/    wire signed [19:0] high_pass = 20'(mean);/" $R/pc386_mixer.sv ;;
  div) python3 - $R/pc386_mixer.sv <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old="""                dividing <= 1;
                step <= 0;"""
assert s.count(old)==1
s=s.replace(old,"""                dividing <= 0;
                step <= 6'd32;""")
old2="""                quotient <= 0;"""
assert s.count(old2)==1
s=s.replace(old2,"""                quotient <= (area < 0 ? -area : area) >> 10;""")
open(p,'w').write(s)
PY
  ;;
esac
git -C $X diff --stat | tail -1
python3 /tmp/claude-1000/-home-deano-fes--claude-worktrees-blissful-maxwell-48aa5e/4d2dd598-05c9-4d82-ab57-44b860d6c2c8/scratchpad/audiofit/elab_audio.py $X $D/elab-fixed3 > /dev/null || { echo elab-fail; exit 1; }
rm -rf $D/elab-$T; mkdir -p $D/elab-$T; cp $D/elab-fixed3/pc386_audio.v $D/elab-$T/
(cd /home/deano/fes/.claude/worktrees/blissful-maxwell-48aa5e/sources/misteross && python3 -c "
import sys; sys.path.insert(0,'.')
from pathlib import Path
from scripts.quartus_x87_spike import plain_module_names
plain_module_names(Path('$D/elab-$T/pc386_audio.v'))")
git -C $X checkout -q -- .
sed -e 's#/tmp/fes-quartus-lite-1.lock#/tmp/fes-quartus-lite-2.lock#' $D/fit.sh > $D/fit-l2.sh; chmod +x $D/fit-l2.sh
$D/fit-l2.sh $T de10
grep -E "Logic utilization|RAM Blocks" $D/fit-$T-de10-s1/project/output_files/pc386_audio.fit.summary
