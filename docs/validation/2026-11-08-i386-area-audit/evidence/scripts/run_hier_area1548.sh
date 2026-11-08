#!/bin/bash
# run_hier_area1548.sh REPO COMMIT TAG FIRMWARE_DIR [PATCH]
# The pc386 Quartus producer at COMMIT with the Yosys hierarchy kept (no flatten),
# so the fit report lists ALMs per entity. Analysis only; the package is scratch.
#   REPO          a checkout of DeanoC/fes holding COMMIT
#   FIRMWARE_DIR  a built build/firmware/pc386 (the dev86-* toolchain dirs the producer needs)
#   PATCH         optional diff applied and committed first (tlb-write-enable.diff)
# Writes ./wt-TAG (a detached worktree) and ./hier-TAG.log; the fit is in
# wt-TAG/sources/misteross/build/fes-pc386-quartus/project/output_files.
set -eu
repo=$1 commit=$2 tag=$3 fw=$4 patch=${5:-}
here=$(cd "$(dirname "$0")" && pwd); out=$PWD; wt=$out/wt-$tag
git -C "$repo" worktree add --detach "$wt" "$commit"
if [ -n "$patch" ]; then
  git -C "$wt" apply "$(realpath "$patch")"
  git -C "$wt" -c user.name=area -c user.email=area@localhost commit -qam "scratch: $(basename "$patch")"
fi
mkdir -p "$wt/sources/misteross/build/firmware/pc386"
cp -a "$fw"/dev86-* "$wt/sources/misteross/build/firmware/pc386/"
export CACHE_ROOT=${CACHE_ROOT:-/home/deano/fes/out/cache/misteross-toolchains}
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}
cd "$wt/sources/misteross"
set +e
MAKEFLAGS= MFLAGS= nice -n 5 python3 "$here/hier_build_area1548.py" "$PWD" > "$out/hier-$tag.log" 2>&1
echo "exit $? $(date -Is)" >> "$out/hier-$tag.log"
