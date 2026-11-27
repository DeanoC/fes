#!/bin/bash
# D12 condition 1 for F3 (#1694): each affected core's Yosys netlist with
# BUILD_ID pinned to 32 zeros, at the merge base and at the head, one shared
# tool install. No GPU, no route.
#   run_cond1.sh <base|head> <p1|p2> <core...>
set -u
SIDE="$1"; PASS="$2"; shift 2
D=/data/fes-f3-d12
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="$D/cond1-$PASS"; mkdir -p "$OUT"
fail=0
for core in "$@"; do
  name="${core#fes.}"; dest="$OUT/$name-$SIDE"
  if [ -f "$dest/synth.json" ]; then echo "SKIP $core $SIDE $PASS"; continue; fi
  python3 "$HERE/netlist_digest.py" --tree "$D/$SIDE/sources/misteross" --core "$core" \
      --cache-root "$D/cache" --out "$dest" > "$OUT/$name-$SIDE.log" 2>&1
  rc=$?; echo "$core $SIDE $PASS rc=$rc"; [ $rc -ne 0 ] && fail=$((fail+1))
done
exit $fail
