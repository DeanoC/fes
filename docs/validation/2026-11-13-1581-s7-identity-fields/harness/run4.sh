#!/bin/bash
# ramtest, with the right output directory.
#
# My first two attempts used build/fes-ramtest, which does not exist: the
# producer writes build/fes-ramtest-100 and build/fes-ramtest-130 (the 100 and
# 130 MHz variants), and a default run produces the 100 one. The artifact guard
# caught it rather than letting an empty comparison pass.
#
# The base build from 15:20-15:45 is complete and in that directory, in this
# same checkout path, so it is adopted rather than rebuilt; the two forced runs
# follow.
set -u
W=/data/d12pb/same
OUT=/data/d12pb/results
CACHE=/home/deano/fes/out/cache/misteross-toolchains
DIR=$W/sources/misteross/build/fes-ramtest-100
PR=8c8a9e5e4
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }

[ -s "$DIR/core.rbf" ] && [ -s "$DIR/build-summary.json" ] || { say "no base build to adopt"; exit 1; }
for f in core.rbf synth.json build-summary.json; do
  [ -f "$DIR/$f" ] && cp --remove-destination "$DIR/$f" "$OUT/ramtest.base.$f"
done
say "adopted the base build: BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/ramtest.base.build-summary.json'))['build_id'])")"

for side in forced1 forced2; do
  git -C "$W" checkout -q --detach "$PR" || { say "checkout failed"; exit 1; }
  rm -rf "$DIR"
  ( cd "$W/sources/misteross" && timeout 28800 python3 \
      "$W/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py" \
      scripts.build_fes_ramtest "$OUT/ramtest.base.build-summary.json" -- \
      --cache-root "$CACHE" ) > "$OUT/ramtest.$side.log" 2>&1
  rc=$?; say "ramtest $side rc=$rc"
  [ $rc -eq 0 ] || { say "ramtest $side FAILED"; exit 1; }
  [ -s "$DIR/core.rbf" ] && [ -s "$DIR/synth.json" ] || { say "ramtest $side wrote no artifacts"; exit 1; }
  for f in core.rbf synth.json; do cp --remove-destination "$DIR/$f" "$OUT/ramtest.$side.$f"; done
done

ok=1
for f in core.rbf synth.json; do
  cmp -s "$OUT/ramtest.base.$f" "$OUT/ramtest.forced1.$f" || ok=0
  cmp -s "$OUT/ramtest.forced1.$f" "$OUT/ramtest.forced2.$f" || { ok=0; say "ramtest $f: forced runs DISAGREE"; }
done
[ $ok = 1 ] && say "RESULT ramtest: base == forced1 == forced2 -- D12 conditions 1 and 2, reproduced" \
            || say "RESULT ramtest: NOT confirmed"
[ $ok = 1 ] && say "       rbf sha256 $(sha256sum "$OUT/ramtest.forced1.core.rbf" | cut -d' ' -f1)"
say "ramtest run done"
