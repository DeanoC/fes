#!/bin/bash
# Condition 1 at the second merge base (e2b8130b5) for the artefacts whose
# closures main moved: fes.ramtest (#1949) and the four Spectrum machines
# (#1948). Two passes per side, as always -- yosys#77 silently writes a
# different netlist about one run in six.
set -u
SIDE="$1"; PASS="$2"
D=/data/fes1861-netlist
CORES="fes.ramtest fes.spectrum fes.spectrum128 fes.spectrum-plus3 fes.pentagon"
OUT="$D/cond1b-$PASS"; LOGS="$D/cond1b-logs/$PASS"
mkdir -p "$OUT" "$LOGS"
fail=0
for core in $CORES; do
  name="${core#fes.}"; dest="$OUT/$name-$SIDE"
  [ -f "$dest/netlist.json" ] && { echo "SKIP $core $SIDE ($PASS)"; continue; }
  echo "RUN  $core $SIDE ($PASS)"
  python3 "$D/identity/netlist_digest.py" --tree "$D/identity/$SIDE/sources/misteross" \
      --core "$core" --cache-root "$D/cache" --out "$dest" > "$LOGS/$name-$SIDE.log" 2>&1
  rc=$?
  [ $rc -ne 0 ] && { echo "FAIL $core $SIDE ($PASS) rc=$rc"; fail=$((fail+1)); }
done
echo "DONE $SIDE $PASS failures=$fail"
exit $fail
