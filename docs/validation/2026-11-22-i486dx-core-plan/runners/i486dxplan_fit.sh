#!/bin/bash
# i486dxplan_fit.sh VARIANT SEED [NETLIST]: fit fes_i386_cpu (dx + VARIANT patch) on Agilex 5.
set -euo pipefail
V=$1; S=$2; NL=${3:-}
P=/data/projects/i486dx-plan; REC=/home/deano/fes/.claude/worktrees/happy-payne-d3b9ad/docs/validation/2026-11-22-i486dx-core-plan/patches
BASE=dafabb6db
WT=$P/wt-fit-$V
free=$(df --output=avail -B1G /data | tail -1); [ "$free" -ge 30 ] || { echo "refusing: ${free}G free"; exit 3; }
if [ ! -d "$WT" ]; then
  git -C /home/deano/fes worktree add -q --detach "$WT" $BASE
  if [ "$V" != 386 ]; then git -C "$WT" apply "$REC/dx.patch"; fi
  case $V in dx|386) ;; *) git -C "$WT" apply "$REC/$V.patch";; esac
fi
OUT=$P/fits/$V-s$S
cd "$WT/sources/misteross"
export YOSYS_MAX_THREADS=1 QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus
args=(--design cpu --device de25 --seed "$S" --cache-root /home/deano/fes/out/cache/misteross-toolchains --output "$OUT" --timeout-minutes 90)
if [ -n "$NL" ]; then args+=(--netlist "$NL" --netlist-source self --netlist-source-note "scratch: dafabb6db + dx.patch, elaborated in $WT"); fi
echo "start $(date -u +%FT%TZ) $V s$S"
python3 scripts/fit_i386_device.py "${args[@]}"
echo "done $(date -u +%FT%TZ) $V s$S rc=$?"
