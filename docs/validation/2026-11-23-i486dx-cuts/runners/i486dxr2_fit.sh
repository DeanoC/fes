#!/bin/bash
# i486dxr2_fit.sh VARIANT SEED: fit fes_i386_cpu (dafabb6db + dx.patch + VARIANT patch) on Agilex 5.
# Seeds 2 and 3 reuse seed 1's netlist through --netlist.
set -euo pipefail
V=$1; S=$2
P=/data/projects/i486dx-plan; R2=$P/r2; PATCHES=$P/rec-r2/docs/validation/2026-11-23-i486dx-cuts/patches
C=/home/deano/fes/out/cache/misteross-toolchains; BASE=dafabb6db; WT=$P/wt-r2-fit-$V
free=$(df --output=avail -B1G /data | tail -1); [ "$free" -ge 30 ] || { echo "refusing: ${free}G free"; exit 3; }
if [ ! -d "$WT" ]; then
  flock $P/worktree.lock git -C /home/deano/fes worktree add -q --detach "$WT" $BASE
  git -C "$WT" apply "$PATCHES/dx.patch"
  git -C "$WT" apply "$PATCHES/$V.patch"
fi
OUT=$R2/fits/$V-s$S
cd "$WT/sources/misteross"
export YOSYS_MAX_THREADS=1 QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus
args=(--design cpu --device de25 --seed "$S" --cache-root $C --output "$OUT" --timeout-minutes 90)
if [ "$S" != 1 ]; then
  NL=$(ls $R2/fits/$V-s1/netlist/*.v | head -1)
  args+=(--netlist "$NL" --netlist-source self --netlist-source-note "scratch: dafabb6db + dx.patch + $V.patch, elaborated in $WT")
fi
echo "start $(date -u +%FT%TZ) $V s$S"
rc=0; python3 scripts/fit_i386_device.py "${args[@]}" || rc=$?
echo "done $(date -u +%FT%TZ) $V s$S rc=$rc"; exit $rc
