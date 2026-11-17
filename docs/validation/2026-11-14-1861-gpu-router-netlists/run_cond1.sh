#!/bin/bash
# Condition 1 at the new merge base: each affected artefact's Yosys netlist,
# zeroed BUILD_ID, both sides, two passes. No GPU, no route.
set -u
SIDE="$1"          # idbase | idhead
PASS="$2"          # p1 | p2
D=/data/fes1861-netlist
DIG="$D/identity/netlist_digest.py"
CACHE="$D/cache"
CORES="fes.menu fes.splash fes.pong fes.ramtest fes.riscv fes.coleco fes.c64 fes.spectrum fes.spectrum128 fes.spectrum-plus3 fes.pentagon"
OUT="$D/cond1-$PASS"
LOGS="$D/cond1-logs/$PASS"
mkdir -p "$OUT" "$LOGS"
fail=0
for core in $CORES; do
  name="${core#fes.}"
  dest="$OUT/$name-$SIDE"
  log="$LOGS/$name-$SIDE.log"
  if [ -f "$dest/netlist.json" ]; then echo "SKIP $core $SIDE ($PASS) already recorded"; continue; fi
  echo "RUN  $core $SIDE ($PASS)"
  python3 "$DIG" --tree "$D/identity/$SIDE/sources/misteross" --core "$core" \
      --cache-root "$CACHE" --out "$dest" > "$log" 2>&1
  rc=$?
  if [ $rc -ne 0 ]; then echo "FAIL $core $SIDE ($PASS) rc=$rc -- see $log"; fail=$((fail+1)); fi
done
echo "DONE $SIDE $PASS failures=$fail"
exit $fail
