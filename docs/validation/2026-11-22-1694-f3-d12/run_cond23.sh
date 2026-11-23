#!/bin/bash
# D12 conditions 2 and 3 for F3 (#1694), one stream of rows in ONE checkout.
#
#   run_cond23.sh <tree> <out> <cache> <router|-> <base-sha> <head-sha> <spec>...
#
# spec = name:module:model:builddir:kind
#   module   producer under scripts/ (build_fes_c64_oss, build_fes_zx81_quartus, ...)
#   model    the producer's --model value, or - for none
#   builddir the producer's build directory under sources/misteross/build
#   kind     full = base, forced (head with base's BUILD_ID), c3 (head, unforced)
#            c2   = base and forced only
#            c3   = c3 only (rows whose netlist moved: a normal reseal)
#
# Condition 2: base.core.rbf == forced.core.rbf, both built in <tree> with the
# same tool install. Condition 3: the head's own build passes the producer's
# gates (exit 0, core.rbf written). The producers lease their own GPU through
# gpu-run and their own Quartus slot through quartus_tools.
set -u
export PATH=$HOME/bin:/usr/local/cuda/bin:$PATH
TREE=$1; OUT=$2; CACHE=$3; ROUTER=$4; BASE=$5; HEAD=$6; shift 6
FORCED=docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py
mkdir -p "$OUT"
say() { echo "[$(date '+%m-%d %H:%M:%S')] $*"; }
free_g() { df --output=avail -BG "$TREE" | tail -1 | tr -dc '0-9'; }

build() { # sha tag module model builddir [summary]
  local sha=$1 tag=$2 module=$3 model=$4 dir=$5 summary=${6:-}
  [ "$(free_g)" -lt 30 ] && { say "$tag STOP: under 30G free"; return 1; }
  git -C "$TREE" checkout -q --detach "$sha" || { say "$tag checkout failed"; return 1; }
  test -z "$(git -C "$TREE" status --porcelain)" || { say "$tag tree dirty"; return 1; }
  local d=$TREE/sources/misteross/build/$dir
  rm -rf "$d"
  local args=(--cache-root "$CACHE")
  [ "$ROUTER" != - ] && args+=(--gpu-router "$ROUTER")
  [ "$model" != - ] && args+=(--model "$model")
  if [ -z "$summary" ]; then
    ( cd "$TREE/sources/misteross" && timeout 36000 python3 "scripts/$module.py" "${args[@]}" ) \
      > "$OUT/$tag.log" 2>&1
  else
    ( cd "$TREE/sources/misteross" && timeout 36000 python3 "$TREE/$FORCED" \
        "scripts.$module" "$summary" -- "${args[@]}" ) > "$OUT/$tag.log" 2>&1
  fi
  local rc=$?; say "$tag rc=$rc ($(git -C "$TREE" rev-parse --short HEAD))"
  [ $rc -eq 0 ] || return 1
  # The producer's own output: core.rbf at the top of the build directory (OSS)
  # or of the newest attempt directory (Quartus). Never a QoR-search candidate.
  local rbf=$d/core.rbf
  [ -f "$rbf" ] || rbf=$(find "$d" -mindepth 3 -maxdepth 3 -path '*/attempt-*/core.rbf' -printf '%T@ %p\n' |
      sort -n | tail -1 | cut -d' ' -f2-)
  [ -n "$rbf" ] && [ -f "$rbf" ] || { say "$tag wrote no core.rbf"; return 1; }
  local src; src=$(dirname "$rbf")
  for f in core.rbf synth.json build-summary.json manifest.toml timing.json; do
    [ -f "$src/$f" ] && cp --remove-destination "$src/$f" "$OUT/$tag.$f"
  done
  [ -f "$OUT/$tag.build-summary.json" ] || { say "$tag: no build-summary.json beside $rbf"; return 1; }
  say "$tag rbf $(sha256sum "$OUT/$tag.core.rbf" | cut -c1-64) from ${rbf#$TREE/}"
}

for spec in "$@"; do
  IFS=: read -r name module model dir kind <<< "$spec"
  say "########## $name ($kind)"
  if [ "$kind" = full ] || [ "$kind" = c2 ]; then
    if build "$BASE" "$name.base" "$module" "$model" "$dir" \
       && build "$HEAD" "$name.forced" "$module" "$model" "$dir" "$OUT/$name.base.build-summary.json"; then
      if cmp -s "$OUT/$name.base.core.rbf" "$OUT/$name.forced.core.rbf"; then
        say "RESULT $name condition 2: RBF reproduced byte for byte"
      else
        say "RESULT $name condition 2: RBF DIFFERS"
      fi
    else
      say "RESULT $name condition 2: build FAILED"
    fi
  fi
  [ "$kind" = c2 ] && continue
  touch "$OUT/$name.c3.start"
  if build "$HEAD" "$name.c3" "$module" "$model" "$dir"; then
    say "RESULT $name condition 3: gates passed, BUILD_ID $(python3 -c "import json;print(json.load(open('$OUT/$name.c3.build-summary.json'))['build_id'])" 2>/dev/null)"
    find "$TREE/sources/misteross/build/packages" -mindepth 1 -maxdepth 1 -newer "$OUT/$name.c3.start" 2>/dev/null |
      while read -r pkg; do cp -a "$pkg" "$OUT/" && say "       package $(basename "$pkg")"; done
  else
    say "RESULT $name condition 3: FAILED"
  fi
done
say "stream done"
