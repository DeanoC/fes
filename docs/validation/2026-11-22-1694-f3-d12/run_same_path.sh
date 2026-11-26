#!/bin/bash
# D12 condition 1, both sides in ONE checkout path (/data/fes-f3-d12/same),
# as the rule asks: the tree is checked out at each side's commit in turn.
#   run_same_path.sh <pass> <base-sha> <head-sha> <core...>
set -u
PASS="$1"; BASE="$2"; HEAD="$3"; shift 3
D=/data/fes-f3-d12; T=$D/same
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="$D/cond1-same-$PASS"; mkdir -p "$OUT"
fail=0
for side in base head; do
  sha=$BASE; [ $side = head ] && sha=$HEAD
  git -C "$T" checkout -q --detach "$sha" || exit 9
  git -C "$T" clean -qfdx -e build/toolchain >/dev/null
  for core in "$@"; do
    name="${core#fes.}"; dest="$OUT/$name-$side"
    python3 "$HERE/netlist_digest.py" --tree "$T/sources/misteross" --core "$core" \
        --cache-root "$D/cache" --out "$dest" > "$OUT/$name-$side.log" 2>&1
    rc=$?; echo "$core $side $PASS rc=$rc"; [ $rc -ne 0 ] && fail=$((fail+1))
  done
done
exit $fail
