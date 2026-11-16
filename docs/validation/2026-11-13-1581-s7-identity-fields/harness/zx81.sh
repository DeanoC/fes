#!/bin/bash
# zx81 re-measured at the new merge base.
#
# D12's carry-forward clause: a recorded comparison holds for a core whose
# compiled source set and route-deciding inputs are unchanged between the old
# base and the new one, and must be re-measured where any of those moved. Main
# merged DeanoC/fes#1791, which changed cores/fes-zx81/rtl/top.v,
# zx81_machine.sv and zx81_machine_clock.v -- compiled sources -- so this core's
# comparison is re-taken. The other eleven carry forward: their only closure hit
# is scripts/core_package.py, which is neither compiled nor a route input.
#
# Conditions 1 and 2 (base vs two forced runs) and condition 3 (the PR's own
# unforced build through its gates), all in one checkout path.
set -u
export PATH=/usr/local/cuda/bin:$PATH
W=/data/d12s7/same
OUT=/data/d12s7/zx81new
CACHE=/data/fes-toolchain-cache
BASE=e9218eb5e
PR=574a73432
DIR=fes-zx81-oss
mkdir -p "$OUT"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
run() { # sha tag summary
  local sha=$1 tag=$2 summary=${3:-}
  git -C "$W" checkout -q --detach "$sha" || return 1
  local d=$W/sources/misteross/build/$DIR
  [ -d "$d" ] && rm -rf "$d"
  if [ -z "$summary" ]; then
    ( cd "$W/sources/misteross" && timeout 28800 python3 scripts/build_fes_zx81_oss.py \
        --cache-root "$CACHE" --gpu-router CUDA ) > "$OUT/$tag.log" 2>&1
  else
    ( cd "$W/sources/misteross" && timeout 28800 python3 \
        "$W/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py" \
        scripts.build_fes_zx81_oss "$summary" -- --cache-root "$CACHE" --gpu-router CUDA ) \
        > "$OUT/$tag.log" 2>&1
  fi
  local rc=$?; say "$tag rc=$rc"
  [ $rc -eq 0 ] || return 1
  [ -s "$d/core.rbf" ] && [ -s "$d/synth.json" ] || { say "$tag wrote no artifacts"; return 1; }
  for f in core.rbf synth.json build-summary.json; do
    [ -f "$d/$f" ] && cp --remove-destination "$d/$f" "$OUT/$tag.$f"
  done
}
git -C "$W" fetch -q origin deano/1581-s7-identity-fields main 2>/dev/null
run "$BASE" base || { say "base FAILED"; exit 1; }
s=$OUT/base.build-summary.json
say "base BUILD_ID $(python3 -c "import json;print(json.load(open('$s'))['build_id'])")"
run "$PR" forced1 "$s" && run "$PR" forced2 "$s" || { say "forced FAILED"; exit 1; }
ok=1
for f in core.rbf synth.json; do
  cmp -s "$OUT/base.$f" "$OUT/forced1.$f" || ok=0
  cmp -s "$OUT/forced1.$f" "$OUT/forced2.$f" || { ok=0; say "$f: forced runs DISAGREE"; }
done
[ $ok = 1 ] && say "RESULT zx81 conditions 1 and 2 at the new base: base == forced1 == forced2" \
            || say "RESULT zx81: NOT confirmed"
[ $ok = 1 ] && say "       rbf    $(sha256sum "$OUT/forced1.core.rbf" | cut -d' ' -f1)" \
            && say "       netlist $(sha256sum "$OUT/forced1.synth.json" | cut -d' ' -f1)"
run "$PR" c3 && say "RESULT zx81 condition 3: gates passed, BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/c3.build-summary.json'))['build_id'])")" || say "RESULT zx81 condition 3: FAILED"
say "zx81 re-measurement done"
