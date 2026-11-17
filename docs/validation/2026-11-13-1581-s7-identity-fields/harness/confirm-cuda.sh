#!/bin/bash
# All eight cores at the CURRENT merge base, with the forced side measured
# TWICE.
#
# Why: a single measurement per side cannot tell "identical because the change
# is inert" from "identical because both runs produced the same wrong netlist".
# That is not hypothetical here -- 3a5accbd exits 0 with a 1,061-cell-smaller
# netlist about one run in six (DeanoC/yosys#77), which is what made one
# spectrum comparison read FAILS when the change was in fact inert. A base and
# two forced runs all agreeing would need the bad variant three times.
#
# Measuring at the current base also retires D12's carry-forward argument: no
# core's evidence then depends on reasoning about what main did or did not move.
set -u
export PATH=/usr/local/cuda/bin:$PATH
W=/data/d12s7/same
OUT=/data/d12s7/confirm
TRASH=/data/d12s7/trash
BASE=${BASE_SHA:?}
PR=${PR_SHA:?}
mkdir -p "$OUT" "$TRASH"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
free_g() { df --output=avail -BG / | tail -1 | tr -dc '0-9'; }

CORES="sg1000:build_fes_sg1000_oss:fes-sg1000-oss zx81:build_fes_zx81_oss:fes-zx81-oss sms:build_fes_sms_oss:fes-sms-oss coleco_v2:build_fes_coleco_socket_v2:fes-coleco-socket-v2 apple2:build_fes_apple2_oss:fes-apple2-oss spectrum:build_fes_spectrum_oss:fes-spectrum-oss atari_st:build_fes_atari_st_oss:fes-atari-st-oss c64:build_fes_c64_oss:fes-c64-oss"

run() { # tree-sha, module, build-dir, output-tag, forced-summary-or-empty
  local sha=$1 mod=$2 dir=$3 tag=$4 summary=$5
  git -C "$W" checkout -q --detach "$sha" || return 1
  local d=$W/sources/misteross/build/$dir
  [ -d "$d" ] && mv "$d" "$TRASH/$dir.$tag.$$"
  if [ -z "$summary" ]; then
    ( cd "$W/sources/misteross" && timeout 21600 python3 "scripts/$mod.py" \
        --cache-root /data/fes-toolchain-cache --gpu-router CUDA ) > "$OUT/$tag.log" 2>&1
  else
    ( cd "$W/sources/misteross" && timeout 21600 python3 \
        "$W/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py" \
        "scripts.$mod" "$summary" -- --cache-root /data/fes-toolchain-cache \
        --gpu-router CUDA ) > "$OUT/$tag.log" 2>&1
  fi
  local rc=$?
  [ $rc -eq 0 ] || { say "$tag rc=$rc FAILED"; return 1; }
  # a run that wrote no netlist is not a run, whatever it returned
  [ -s "$d/synth.json" ] && [ -s "$d/core.rbf" ] || { say "$tag wrote no artifacts"; return 1; }
  for f in core.rbf synth.json build-summary.json; do
    [ -f "$d/$f" ] && cp --remove-destination "$d/$f" "$OUT/$tag.$f"
  done
  return 0
}

for spec in $CORES; do
  core=${spec%%:*}; rest=${spec#*:}; mod=${rest%%:*}; dir=${rest#*:}
  say "########## $core"
  [ "$(free_g)" -lt 40 ] && { say "ABORT: under 40G free"; break; }
  run "$BASE" "$mod" "$dir" "$core.base" "" || continue
  s=$OUT/$core.base.build-summary.json
  [ -s "$s" ] || { say "$core: no base summary"; continue; }
  say "$core base BUILD_ID $(python3 -c "import json;print(json.load(open('$s'))['build_id'])")"
  run "$PR" "$mod" "$dir" "$core.forced1" "$s" || continue
  run "$PR" "$mod" "$dir" "$core.forced2" "$s" || continue
  ok=1
  for f in core.rbf synth.json; do
    cmp -s "$OUT/$core.base.$f" "$OUT/$core.forced1.$f" || ok=0
    cmp -s "$OUT/$core.forced1.$f" "$OUT/$core.forced2.$f" || { ok=0; say "$core $f: the two forced runs DISAGREE -- not reproducible"; }
  done
  if [ $ok = 1 ]; then
    say "RESULT $core: base == forced1 == forced2, rbf and netlist -- D12 conditions 1 and 2, reproduced"
    say "       rbf sha256 $(sha256sum "$OUT/$core.forced1.core.rbf" | cut -d' ' -f1)"
  else
    say "RESULT $core: NOT confirmed -- see the comparisons above"
  fi
done
say "confirmation sweep done"
