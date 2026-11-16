#!/bin/bash
# Condition 3 for the HIP cores, and the 130 MHz ramtest package in full.
#
# Codex is right on both counts (DeanoC/fes#1806 review):
#
# - every branch build so far forced the BASE BUILD_ID, so none of them is the
#   PR's own build. Condition 3 asks for exactly that: this commit's real
#   BUILD_ID through the producer's normal timing and seal gates. A forced run
#   cannot stand in, because the constant changes mapping.
# - build/fes-ramtest-130 is a separately sealed package, not a conditional
#   extra: Makefile has build-fes-ramtest-130, and architecture.md says both
#   rates seal distinct packages with their own timing gates. So it needs
#   conditions 1 and 2 of its own.
set -u
W=/data/d12pb/same
OUT=/data/d12pb/c3
CACHE=/home/deano/fes/out/cache/misteross-toolchains
BASE=1a4bb8d41
PR=f07950e12
mkdir -p "$OUT"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
free_g() { df --output=avail -BG / | tail -1 | tr -dc '0-9'; }

run() { # sha, module, extra-args, build-dir, tag, forced-summary(optional)
  local sha=$1 mod=$2 extra=$3 dir=$4 tag=$5 summary=${6:-}
  [ "$(free_g)" -lt 40 ] && { say "STOP: under 40G"; return 1; }
  git -C "$W" checkout -q --detach "$sha" || return 1
  local d=$W/sources/misteross/build/$dir
  [ -d "$d" ] && rm -rf "$d"
  if [ -z "$summary" ]; then
    ( cd "$W/sources/misteross" && timeout 28800 python3 "scripts/$mod.py" $extra --cache-root "$CACHE" ) \
        > "$OUT/$tag.log" 2>&1
  else
    ( cd "$W/sources/misteross" && timeout 28800 python3 \
        "$W/docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py" \
        "scripts.$mod" "$summary" -- $extra --cache-root "$CACHE" ) > "$OUT/$tag.log" 2>&1
  fi
  local rc=$?
  say "$tag rc=$rc"
  [ $rc -eq 0 ] || return 1
  [ -s "$d/core.rbf" ] || { say "$tag wrote no core.rbf"; return 1; }
  for f in core.rbf synth.json build-summary.json; do
    [ -f "$d/$f" ] && cp --remove-destination "$d/$f" "$OUT/$tag.$f"
  done
  return 0
}

# 1. condition 3: the PR's own build, unforced, through its normal gates
for spec in pong:build_fes_pong::fes-pong menu:build_fes_menu_package::oss/fes-menu-package ramtest100:build_fes_ramtest:--memory-mhz_100:fes-ramtest-100 riscv:build_fes_riscv::fes-riscv; do
  core=${spec%%:*}; rest=${spec#*:}; mod=${rest%%:*}; rest=${rest#*:}; extra=${rest%%:*}; dir=${rest#*:}
  extra=${extra//_/ }
  say "########## $core: condition 3 (unforced, the PR's own BUILD_ID)"
  if run "$PR" "$mod" "$extra" "$dir" "$core.c3"; then
    say "RESULT $core condition 3: the producer's gates passed with BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/$core.c3.build-summary.json'))['build_id'])" 2>/dev/null)"
    say "       rbf sha256 $(sha256sum "$OUT/$core.c3.core.rbf" | cut -d' ' -f1)"
  else
    say "RESULT $core condition 3: FAILED"
  fi
done

# 2. the 130 MHz package: conditions 1, 2 and 3 of its own
say "########## ramtest130: conditions 1 and 2"
if run "$BASE" build_fes_ramtest "--memory-mhz 130" fes-ramtest-130 ramtest130.base; then
  s=$OUT/ramtest130.base.build-summary.json
  say "ramtest130 base BUILD_ID $(python3 -c "import json;print(json.load(open('$s'))['build_id'])")"
  if run "$PR" build_fes_ramtest "--memory-mhz 130" fes-ramtest-130 ramtest130.forced1 "$s" \
     && run "$PR" build_fes_ramtest "--memory-mhz 130" fes-ramtest-130 ramtest130.forced2 "$s"; then
    ok=1
    for f in core.rbf synth.json; do
      cmp -s "$OUT/ramtest130.base.$f" "$OUT/ramtest130.forced1.$f" || ok=0
      cmp -s "$OUT/ramtest130.forced1.$f" "$OUT/ramtest130.forced2.$f" || { ok=0; say "ramtest130 $f: forced runs DISAGREE"; }
    done
    [ $ok = 1 ] && say "RESULT ramtest130: base == forced1 == forced2 -- conditions 1 and 2, reproduced" \
                || say "RESULT ramtest130: NOT confirmed"
    [ $ok = 1 ] && say "       rbf sha256 $(sha256sum "$OUT/ramtest130.forced1.core.rbf" | cut -d' ' -f1)"
  fi
  say "########## ramtest130: condition 3"
  if run "$PR" build_fes_ramtest "--memory-mhz 130" fes-ramtest-130 ramtest130.c3; then
    say "RESULT ramtest130 condition 3: gates passed with BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/ramtest130.c3.build-summary.json'))['build_id'])" 2>/dev/null)"
  else
    say "RESULT ramtest130 condition 3: FAILED"
  fi
fi
say "c3 sweep done"
