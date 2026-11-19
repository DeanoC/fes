#!/bin/bash
# D12 condition 2 for one artefact: route both sides at the same forced
# BUILD_ID (main's own, derived on the base tree) and compare the RBFs.
set -u
CORE="$1"; BID="$2"
D=/data/fes1861-netlist
RBF="$D/identity/rbf_at_build_id.py"
CACHE="$D/cache"
OUT="$D/cond2"; LOGS="$D/cond2-logs"
mkdir -p "$OUT" "$LOGS"
name="${CORE#fes.}"
rc_total=0
for side in c2base c2head; do
  label=$([ "$side" = c2base ] && echo main-control || echo branch-forced)
  dest="$OUT/$name-$label"
  if [ -f "$dest/rbf.json" ]; then echo "SKIP $CORE $label"; continue; fi
  echo "RUN  $CORE $label build-id=$BID"
  python3 "$RBF" --tree "$D/identity/$side/sources/misteross" --core "$CORE" \
      --cache-root "$CACHE" --build-id "$BID" --out "$dest" \
      > "$LOGS/$name-$label.log" 2>&1
  rc=$?
  [ $rc -ne 0 ] && { echo "FAIL $CORE $label rc=$rc"; rc_total=$((rc_total+1)); }
done
echo "DONE $CORE failures=$rc_total"
exit $rc_total
