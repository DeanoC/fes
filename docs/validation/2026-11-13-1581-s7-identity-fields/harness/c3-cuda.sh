#!/bin/bash
# Condition 3 for the eight CUDA cores: the PR's own build, unforced.
#
# Every branch build in the campaign so far forced the base BUILD_ID, so none of
# them is the PR's own build. D12's third condition asks for exactly that -- this
# commit's real BUILD_ID through the producer's normal timing and seal gates --
# and a forced run cannot stand in for it, because the constant changes mapping
# (DeanoC/fes#1806 review).
set -u
export PATH=/usr/local/cuda/bin:$PATH
W=/data/d12s7/same
OUT=/data/d12s7/c3
CACHE=/data/fes-toolchain-cache
PR=f07950e12
mkdir -p "$OUT"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
free_g() { df --output=avail -BG / | tail -1 | tr -dc '0-9'; }

git -C "$W" fetch -q origin deano/1581-s7-identity-fields 2>/dev/null
for spec in sg1000:build_fes_sg1000_oss:fes-sg1000-oss zx81:build_fes_zx81_oss:fes-zx81-oss sms:build_fes_sms_oss:fes-sms-oss coleco_v2:build_fes_coleco_socket_v2:fes-coleco-socket-v2 apple2:build_fes_apple2_oss:fes-apple2-oss spectrum:build_fes_spectrum_oss:fes-spectrum-oss atari_st:build_fes_atari_st_oss:fes-atari-st-oss c64:build_fes_c64_oss:fes-c64-oss; do
  core=${spec%%:*}; rest=${spec#*:}; mod=${rest%%:*}; dir=${rest#*:}
  say "########## $core: condition 3 (unforced)"
  [ "$(free_g)" -lt 40 ] && { say "STOP: under 40G"; break; }
  git -C "$W" checkout -q --detach "$PR" || { say "checkout failed"; break; }
  d=$W/sources/misteross/build/$dir
  [ -d "$d" ] && rm -rf "$d"
  ( cd "$W/sources/misteross" && timeout 28800 python3 "scripts/$mod.py" \
      --cache-root "$CACHE" --gpu-router CUDA ) > "$OUT/$core.c3.log" 2>&1
  rc=$?; say "$core c3 rc=$rc"
  if [ $rc -ne 0 ]; then say "RESULT $core condition 3: FAILED"; continue; fi
  [ -s "$d/core.rbf" ] || { say "RESULT $core condition 3: wrote no core.rbf"; continue; }
  for f in core.rbf synth.json build-summary.json; do
    [ -f "$d/$f" ] && cp --remove-destination "$d/$f" "$OUT/$core.c3.$f"
  done
  say "RESULT $core condition 3: gates passed, BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/$core.c3.build-summary.json'))['build_id'])" 2>/dev/null)"
  say "       rbf sha256 $(sha256sum "$OUT/$core.c3.core.rbf" | cut -d' ' -f1)"
done
say "chip condition-3 sweep done"
