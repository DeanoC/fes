#!/bin/bash
# The four HIP-pinned cores' D12 evidence, on Powerboat.
#
# pong, menu, ramtest and riscv take no --gpu-router and fes_build_common pins
# FES_GPU_ROUTER = "HIP" (DeanoC/fes#1862 fixes that), so an NVIDIA host cannot
# build them -- these are the four the CUDA campaign could not cover.
#
# Both sides in ONE checkout path, and the forced side measured TWICE, matching
# the chip-host sweep: a single pair cannot tell an inert change from two runs
# that produced the same wrong netlist, and 3a5accbd does that about one run in
# six (DeanoC/yosys#77).
#
# Disk: the shared cache builds each tool INSIDE the cache, which lives on /,
# and / is the tight filesystem here. Every step is gated on free space, with a
# floor well above the 30G the rules set.
set -u
W=/data/d12pb/same
OUT=/data/d12pb/results
CACHE=/home/deano/fes/out/cache/misteross-toolchains
BASE=1a4bb8d41
PR=8c8a9e5e4
FLOOR=40
mkdir -p "$OUT"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
free_g() { df --output=avail -BG / | tail -1 | tr -dc '0-9'; }
guard() { local f=$(free_g); if [ "$f" -lt $FLOOR ]; then say "STOP: / at ${f}G, under the ${FLOOR}G floor"; return 1; fi; say "/ at ${f}G free"; }

say "start: load $(cut -d' ' -f1-3 /proc/loadavg)"

for spec in pong:build_fes_pong:fes-pong:cores/fes-pong/toolchain.lock menu:build_fes_menu_package:oss/fes-menu-package:cores/fes-menu/toolchain.lock riscv:build_fes_riscv:fes-riscv:cores/fes-riscv/toolchain.lock; do
  core=${spec%%:*}; rest=${spec#*:}; mod=${rest%%:*}; rest=${rest#*:}; dir=${rest%%:*}; lock=${rest#*:}
  say "########## $core"
  guard || break

  # the toolchain this core pins, if the cache has not got it yet
  say "=== $core: bootstrap if needed"
  # FES_TOOLCHAIN_GPU_ROUTER defaults to OFF in bootstrap.sh. Omitting it built
  # CPU-router installs under different keys, reported success, and left the
  # producers' HIP keys missing -- the first run of this sweep lost pong, menu
  # and riscv that way. These four producers pin HIP (FES_GPU_ROUTER), so HIP is
  # what their toolchain must be built for.
  ( cd "$W/sources/misteross" && FES_TOOLCHAIN_CACHE_ROOT="$CACHE" \
      FES_TOOLCHAIN_LOCKFILE="$lock" FES_TOOLCHAIN_GPU_ROUTER=HIP \
      JOBS=8 timeout 28800 scripts/bootstrap.sh ) \
      > "$OUT/$core.bootstrap.log" 2>&1
  rc=$?; say "$core bootstrap rc=$rc"; guard || break
  [ $rc -eq 0 ] || { say "$core BOOTSTRAP FAILED -- $core.bootstrap.log"; continue; }

  for side in base forced1 forced2; do
    sha=$BASE; [ "$side" = base ] || sha=$PR
    git -C "$W" checkout -q --detach "$sha" || { say "$core: checkout failed"; continue 2; }
    d=$W/sources/misteross/build/$dir
    [ -d "$d" ] && rm -rf "$d"
    if [ "$side" = base ]; then
      ( cd "$W/sources/misteross" && timeout 28800 python3 "scripts/$mod.py" --cache-root "$CACHE" ) \
          > "$OUT/$core.$side.log" 2>&1
    else
      ( cd "$W/sources/misteross" && timeout 28800 python3 \
          "$W/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py" \
          "scripts.$mod" "$OUT/$core.base.build-summary.json" -- --cache-root "$CACHE" ) \
          > "$OUT/$core.$side.log" 2>&1
    fi
    rc=$?; say "$core $side rc=$rc"
    [ $rc -eq 0 ] || { say "$core $side FAILED"; continue 2; }
    [ -s "$d/synth.json" ] && [ -s "$d/core.rbf" ] || { say "$core $side wrote no artifacts"; continue 2; }
    for f in core.rbf synth.json build-summary.json; do
      [ -f "$d/$f" ] && cp --remove-destination "$d/$f" "$OUT/$core.$side.$f"
    done
    guard || break 2
  done

  ok=1
  for f in core.rbf synth.json; do
    cmp -s "$OUT/$core.base.$f" "$OUT/$core.forced1.$f" || ok=0
    cmp -s "$OUT/$core.forced1.$f" "$OUT/$core.forced2.$f" || { ok=0; say "$core $f: forced runs DISAGREE"; }
  done
  if [ $ok = 1 ]; then
    say "RESULT $core: base == forced1 == forced2 -- D12 conditions 1 and 2, reproduced"
    say "       rbf sha256 $(sha256sum "$OUT/$core.forced1.core.rbf" | cut -d' ' -f1)"
  else
    say "RESULT $core: NOT confirmed"
  fi
done
say "powerboat sweep done"
